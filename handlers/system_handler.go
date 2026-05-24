package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"
)

var processStartedAt = time.Now()

const monitoringTrendHistoryConfigKey = "system_monitoring_trends_v1"
const monitoringTrendHistoryMaxPoints = 3000

var monitoringTrendHistoryMu sync.Mutex

type monitoringTrendPoint struct {
	Timestamp         time.Time `json:"timestamp"`
	CPUPercent        float64   `json:"cpu_percent"`
	MemoryPercent     float64   `json:"memory_percent"`
	DiskPercent       float64   `json:"disk_percent"`
	ResponseAvgMs     float64   `json:"response_avg_ms"`
	ErrorPercent      float64   `json:"error_percent"`
	RequestsPerMinute int64     `json:"requests_per_minute"`
	RunningContainers int       `json:"running_containers"`
}

type systemSnapshot struct {
	CPUUsage          float64
	CPUCores          int
	CPUModel          string
	CPUSpeedMHz       float64
	MemoryTotalBytes  uint64
	MemoryFreeBytes   uint64
	MemoryUsedBytes   uint64
	MemoryUsagePct    float64
	DiskTotalBytes    uint64
	DiskFreeBytes     uint64
	DiskUsedBytes     uint64
	DiskUsagePct      float64
	Load1             float64
	Load5             float64
	Load15            float64
	HostUptimeSeconds uint64
	Hostname          string
	Platform          string
	Architecture      string
	OSType            string
	OSRelease         string
	NetReceiveBps     float64
	NetTransmitBps    float64
}

type websiteProbeSample struct {
	statusCode int
	latencyMs  float64
	success    bool
}

func loadMonitoringTrendHistory() ([]monitoringTrendPoint, error) {
	raw, err := repositories.GetAppConfigValue(monitoringTrendHistoryConfigKey)
	if err != nil {
		return []monitoringTrendPoint{}, nil
	}
	if strings.TrimSpace(raw) == "" {
		return []monitoringTrendPoint{}, nil
	}

	var points []monitoringTrendPoint
	if err := json.Unmarshal([]byte(raw), &points); err != nil {
		return []monitoringTrendPoint{}, err
	}
	return points, nil
}

func saveMonitoringTrendHistory(points []monitoringTrendPoint) error {
	if len(points) > monitoringTrendHistoryMaxPoints {
		points = points[len(points)-monitoringTrendHistoryMaxPoints:]
	}

	raw, err := json.Marshal(points)
	if err != nil {
		return err
	}
	return repositories.SetAppConfigValue(monitoringTrendHistoryConfigKey, string(raw))
}

func appendMonitoringTrendPoint(point monitoringTrendPoint) error {
	monitoringTrendHistoryMu.Lock()
	defer monitoringTrendHistoryMu.Unlock()

	points, err := loadMonitoringTrendHistory()
	if err != nil {
		points = []monitoringTrendPoint{}
	}

	if len(points) > 0 {
		last := points[len(points)-1]
		if point.Timestamp.Sub(last.Timestamp) < 20*time.Second {
			return nil
		}
	}

	points = append(points, point)
	return saveMonitoringTrendHistory(points)
}

func filterMonitoringTrendByRange(points []monitoringTrendPoint, rangeKey string) []monitoringTrendPoint {
	if len(points) == 0 {
		return points
	}

	now := time.Now().UTC()
	var cutoff time.Time
	switch strings.ToLower(strings.TrimSpace(rangeKey)) {
	case "15m":
		cutoff = now.Add(-15 * time.Minute)
	case "1h":
		cutoff = now.Add(-1 * time.Hour)
	case "6h":
		cutoff = now.Add(-6 * time.Hour)
	case "24h", "":
		cutoff = now.Add(-24 * time.Hour)
	case "all":
		return points
	default:
		cutoff = now.Add(-24 * time.Hour)
	}

	filtered := make([]monitoringTrendPoint, 0, len(points))
	for _, point := range points {
		if point.Timestamp.After(cutoff) || point.Timestamp.Equal(cutoff) {
			filtered = append(filtered, point)
		}
	}
	return filtered
}

type dockerContainerMetrics struct {
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsageMB float64 `json:"memoryUsageMB"`
	MemoryLimitMB float64 `json:"memoryLimitMB"`
	MemoryPercent float64 `json:"memoryPercent"`
	Restarts      int     `json:"restarts"`
	Status        string  `json:"status"`
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func bytesToGB(value uint64) float64 {
	if value == 0 {
		return 0
	}

	return round2(float64(value) / (1024 * 1024 * 1024))
}

func rootDiskPath() string {
	if runtime.GOOS == "windows" {
		return "C:\\"
	}

	return "/"
}

func formatUptime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}

	total := int64(seconds)
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	secs := total % 60

	return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, secs)
}

func computeUsageStatus(percent float64) string {
	if percent >= 90 {
		return "critical"
	}
	if percent >= 75 {
		return "warning"
	}
	return "normal"
}

func computeResponseStatus(avgMs float64) string {
	if avgMs >= 2000 {
		return "critical"
	}
	if avgMs >= 800 {
		return "slow"
	}
	return "good"
}

func computeErrorStatus(percent float64) string {
	if percent >= 5 {
		return "critical"
	}
	if percent >= 1 {
		return "warning"
	}
	return "normal"
}

func sampleNetworkThroughput() (float64, float64) {
	first, err := gopsnet.IOCounters(false)
	if err != nil || len(first) == 0 {
		return 0, 0
	}

	time.Sleep(250 * time.Millisecond)
	second, err := gopsnet.IOCounters(false)
	if err != nil || len(second) == 0 {
		return 0, 0
	}

	recvDiff := float64(second[0].BytesRecv - first[0].BytesRecv)
	transmitDiff := float64(second[0].BytesSent - first[0].BytesSent)
	seconds := 0.25
	if seconds <= 0 {
		return 0, 0
	}

	return round2(recvDiff / seconds), round2(transmitDiff / seconds)
}

func collectSystemSnapshot() systemSnapshot {
	snapshot := systemSnapshot{
		CPUCores:     runtime.NumCPU(),
		CPUModel:     "Unknown",
		Architecture: runtime.GOARCH,
		Platform:     runtime.GOOS,
	}

	if usageSamples, err := cpu.Percent(250*time.Millisecond, false); err == nil && len(usageSamples) > 0 {
		snapshot.CPUUsage = round2(usageSamples[0])
	}

	if cores, err := cpu.Counts(true); err == nil && cores > 0 {
		snapshot.CPUCores = cores
	}

	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		snapshot.CPUModel = infos[0].ModelName
		snapshot.CPUSpeedMHz = round2(infos[0].Mhz)
	}

	if virtualMem, err := mem.VirtualMemory(); err == nil {
		snapshot.MemoryTotalBytes = virtualMem.Total
		snapshot.MemoryFreeBytes = virtualMem.Available
		snapshot.MemoryUsedBytes = virtualMem.Used
		snapshot.MemoryUsagePct = round2(virtualMem.UsedPercent)
	}

	if usage, err := disk.Usage(rootDiskPath()); err == nil {
		snapshot.DiskTotalBytes = usage.Total
		snapshot.DiskFreeBytes = usage.Free
		snapshot.DiskUsedBytes = usage.Used
		snapshot.DiskUsagePct = round2(usage.UsedPercent)
	}

	if avg, err := load.Avg(); err == nil {
		snapshot.Load1 = round2(avg.Load1)
		snapshot.Load5 = round2(avg.Load5)
		snapshot.Load15 = round2(avg.Load15)
	}

	if hostInfo, err := host.Info(); err == nil {
		snapshot.Hostname = hostInfo.Hostname
		snapshot.Platform = hostInfo.Platform
		snapshot.Architecture = hostInfo.KernelArch
		snapshot.OSType = hostInfo.OS
		snapshot.OSRelease = hostInfo.KernelVersion
		snapshot.HostUptimeSeconds = hostInfo.Uptime
	}

	if snapshot.Hostname == "" {
		snapshot.Hostname = "unknown-host"
	}

	if snapshot.Architecture == "" {
		snapshot.Architecture = runtime.GOARCH
	}

	if snapshot.OSType == "" {
		snapshot.OSType = runtime.GOOS
	}

	if snapshot.OSRelease == "" {
		snapshot.OSRelease = runtime.Version()
	}

	recvBps, transmitBps := sampleNetworkThroughput()
	snapshot.NetReceiveBps = recvBps
	snapshot.NetTransmitBps = transmitBps

	return snapshot
}

// GetSystemMetricsHandler handles GET /api/system/metrics.
func GetSystemMetricsHandler(c fiber.Ctx) error {
	snapshot := collectSystemSnapshot()

	var processMemory runtime.MemStats
	runtime.ReadMemStats(&processMemory)

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"cpu": fiber.Map{
				"usagePercent": snapshot.CPUUsage,
				"cores":        snapshot.CPUCores,
				"model":        snapshot.CPUModel,
				"speedMHz":     snapshot.CPUSpeedMHz,
				"status":       computeUsageStatus(snapshot.CPUUsage),
			},
			"memory": fiber.Map{
				"totalBytes":     snapshot.MemoryTotalBytes,
				"availableBytes": snapshot.MemoryFreeBytes,
				"usedBytes":      snapshot.MemoryUsedBytes,
				"usagePercent":   snapshot.MemoryUsagePct,
				"status":         computeUsageStatus(snapshot.MemoryUsagePct),
			},
			"disk": fiber.Map{
				"totalBytes":     snapshot.DiskTotalBytes,
				"availableBytes": snapshot.DiskFreeBytes,
				"usedBytes":      snapshot.DiskUsedBytes,
				"usagePercent":   snapshot.DiskUsagePct,
				"status":         computeUsageStatus(snapshot.DiskUsagePct),
			},
			"network": fiber.Map{
				"receiveBytesPerSec":  snapshot.NetReceiveBps,
				"transmitBytesPerSec": snapshot.NetTransmitBps,
			},
			"load": fiber.Map{
				"load1":  snapshot.Load1,
				"load5":  snapshot.Load5,
				"load15": snapshot.Load15,
			},
			"uptime": fiber.Map{
				"seconds": snapshot.HostUptimeSeconds,
			},
			"system": fiber.Map{
				"platform":  snapshot.Platform,
				"arch":      snapshot.Architecture,
				"hostname":  snapshot.Hostname,
				"type":      snapshot.OSType,
				"release":   snapshot.OSRelease,
				"goVersion": runtime.Version(),
			},
			"process": fiber.Map{
				"pid":    os.Getpid(),
				"uptime": round2(time.Since(processStartedAt).Seconds()),
				"memoryUsage": fiber.Map{
					"rss":       processMemory.Sys,
					"heapTotal": processMemory.HeapSys,
					"heapUsed":  processMemory.HeapAlloc,
					"external":  processMemory.StackSys,
				},
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func parsePercentage(value string) float64 {
	trimmed := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if trimmed == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return round2(parsed)
}

func parseDockerSizeToMB(value string) float64 {
	trimmed := strings.TrimSpace(strings.ToUpper(value))
	if trimmed == "" {
		return 0
	}

	idx := 0
	for idx < len(trimmed) {
		ch := trimmed[idx]
		if (ch < '0' || ch > '9') && ch != '.' {
			break
		}
		idx++
	}

	if idx == 0 {
		return 0
	}

	numberPart := trimmed[:idx]
	unitPart := strings.TrimSpace(trimmed[idx:])
	parsed, err := strconv.ParseFloat(numberPart, 64)
	if err != nil {
		return 0
	}

	switch unitPart {
	case "B":
		return round2(parsed / (1024 * 1024))
	case "KB", "KIB":
		return round2(parsed / 1024)
	case "MB", "MIB":
		return round2(parsed)
	case "GB", "GIB":
		return round2(parsed * 1024)
	case "TB", "TIB":
		return round2(parsed * 1024 * 1024)
	default:
		return round2(parsed)
	}
}

func parseDockerMemUsage(value string) (float64, float64) {
	parts := strings.Split(value, "/")
	if len(parts) == 0 {
		return 0, 0
	}

	used := parseDockerSizeToMB(parts[0])
	if len(parts) == 1 {
		return used, 0
	}
	limit := parseDockerSizeToMB(parts[1])
	return used, limit
}

func runDockerCommand(timeout time.Duration, args ...string) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", ctx.Err()
	}
	if err != nil {
		return "", errors.New(strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func collectContainerMetrics() ([]dockerContainerMetrics, string) {
	psOutput, err := runDockerCommand(4*time.Second, "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return []dockerContainerMetrics{}, "docker_unavailable"
	}

	statsOutput, statsErr := runDockerCommand(4*time.Second, "stats", "--no-stream", "--format", "{{json .}}")
	statsByName := map[string]dockerContainerMetrics{}
	if statsErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(statsOutput), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var row map[string]string
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				continue
			}

			name := strings.TrimSpace(row["Name"])
			if name == "" {
				continue
			}

			usedMB, limitMB := parseDockerMemUsage(row["MemUsage"])
			memPercent := parsePercentage(row["MemPerc"])
			statsByName[name] = dockerContainerMetrics{
				Name:          name,
				CPUPercent:    parsePercentage(row["CPUPerc"]),
				MemoryUsageMB: usedMB,
				MemoryLimitMB: limitMB,
				MemoryPercent: memPercent,
			}
		}
	}

	inspectMap := map[string]dockerContainerMetrics{}
	idOutput, idErr := runDockerCommand(4*time.Second, "ps", "-aq")
	if idErr == nil {
		ids := make([]string, 0)
		for _, id := range strings.Split(strings.TrimSpace(idOutput), "\n") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			args := []string{"inspect", "--format", "{{.Name}}|{{.RestartCount}}|{{.State.Status}}"}
			args = append(args, ids...)
			inspectOutput, inspectErr := runDockerCommand(6*time.Second, args...)
			if inspectErr == nil {
				for _, line := range strings.Split(strings.TrimSpace(inspectOutput), "\n") {
					parts := strings.Split(line, "|")
					if len(parts) < 3 {
						continue
					}
					name := strings.TrimPrefix(strings.TrimSpace(parts[0]), "/")
					restarts, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
					status := strings.TrimSpace(parts[2])
					if status == "" {
						status = "stopped"
					}
					inspectMap[name] = dockerContainerMetrics{Name: name, Restarts: restarts, Status: status}
				}
			}
		}
	}

	containers := make([]dockerContainerMetrics, 0)
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var row map[string]string
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}

		name := strings.TrimSpace(row["Names"])
		if name == "" {
			continue
		}

		metric := dockerContainerMetrics{Name: name, Status: strings.ToLower(strings.TrimSpace(row["State"]))}
		if metric.Status == "" {
			metric.Status = "stopped"
		}

		if stats, ok := statsByName[name]; ok {
			metric.CPUPercent = stats.CPUPercent
			metric.MemoryUsageMB = stats.MemoryUsageMB
			metric.MemoryLimitMB = stats.MemoryLimitMB
			metric.MemoryPercent = stats.MemoryPercent
		}
		if inspect, ok := inspectMap[name]; ok {
			metric.Restarts = inspect.Restarts
			if inspect.Status != "" {
				metric.Status = strings.ToLower(inspect.Status)
			}
		}

		switch metric.Status {
		case "running", "restarting", "stopped":
		default:
			metric.Status = "stopped"
		}

		containers = append(containers, metric)
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})

	source := "docker_cli"
	if statsErr != nil {
		source = "docker_cli_partial"
	}
	return containers, source
}

func collectWebsiteProbes() []websiteProbeSample {
	urls := getProbeURLs()
	samplesPerURL := 2
	if raw := strings.TrimSpace(os.Getenv("MONITOR_PROBE_SAMPLES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 10 {
			samplesPerURL = parsed
		}
	}

	httpClient := &http.Client{Timeout: 3 * time.Second}
	samples := make([]websiteProbeSample, 0, len(urls)*samplesPerURL)

	for _, target := range urls {
		for i := 0; i < samplesPerURL; i++ {
			start := time.Now()
			resp, err := httpClient.Get(target)
			latencyMs := float64(time.Since(start).Milliseconds())
			if latencyMs < 1 {
				latencyMs = 1
			}

			if err != nil {
				samples = append(samples, websiteProbeSample{statusCode: 0, latencyMs: latencyMs, success: false})
				continue
			}

			_ = resp.Body.Close()
			success := resp.StatusCode >= 200 && resp.StatusCode < 500
			samples = append(samples, websiteProbeSample{statusCode: resp.StatusCode, latencyMs: latencyMs, success: success})
		}
	}

	return samples
}

func collectWebsiteTrendMetrics(samples []websiteProbeSample) (float64, float64, int64) {
	totalProbes := len(samples)
	if totalProbes == 0 {
		totalProbes = 1
	}

	latencies := make([]float64, 0, len(samples))
	probe4xx := int64(0)
	probe5xx := int64(0)
	for _, sample := range samples {
		latencies = append(latencies, sample.latencyMs)
		if sample.statusCode >= 500 {
			probe5xx++
		} else if sample.statusCode >= 400 {
			probe4xx++
		}
	}

	avgLatency := 0.0
	for _, l := range latencies {
		avgLatency += l
	}
	if len(latencies) > 0 {
		avgLatency = round2(avgLatency / float64(len(latencies)))
	}

	now := time.Now().UTC()
	oneMinuteAgo := now.Add(-1 * time.Minute)
	fiveMinutesAgo := now.Add(-5 * time.Minute)

	perMinute := int64(0)
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ?", oneMinuteAgo).Count(&perMinute).Error

	log4xx := int64(0)
	log5xx := int64(0)
	totalLogs := int64(0)
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ?", fiveMinutesAgo).Count(&totalLogs).Error
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ? AND status_code >= 400 AND status_code < 500", fiveMinutesAgo).Count(&log4xx).Error
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ? AND status_code >= 500", fiveMinutesAgo).Count(&log5xx).Error

	totalRequests := totalLogs
	totalErrors := log4xx + log5xx
	if totalRequests == 0 {
		totalRequests = int64(len(samples))
		totalErrors = probe4xx + probe5xx
	}

	errorPercent := 0.0
	if totalRequests > 0 {
		errorPercent = round2(float64(totalErrors) * 100 / float64(totalRequests))
	}

	return avgLatency, errorPercent, perMinute
}

func getProbeURLs() []string {
	raw := strings.TrimSpace(os.Getenv("MONITOR_PROBE_URLS"))
	if raw == "" {
		raw = "http://localhost:3000/,http://localhost:8000/api/health"
	}

	parts := strings.Split(raw, ",")
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	if len(urls) == 0 {
		urls = append(urls, "http://localhost:8000/api/health")
	}
	return urls
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[len(values)-1]
	}

	pos := p * float64(len(values)-1)
	idx := int(math.Round(pos))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return round2(values[idx])
}

// GetContainerMetricsHandler handles GET /api/system/containers.
func GetContainerMetricsHandler(c fiber.Ctx) error {
	containers, source := collectContainerMetrics()

	return c.JSON(fiber.Map{
		"success": true,
		"data":    containers,
		"meta": fiber.Map{
			"source":    source,
			"collected": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// GetWebsiteMetricsHandler handles GET /api/system/website.
func GetWebsiteMetricsHandler(c fiber.Ctx) error {
	samples := collectWebsiteProbes()

	totalProbes := len(samples)
	if totalProbes == 0 {
		totalProbes = 1
	}

	successCount := 0
	latencies := make([]float64, 0, len(samples))
	statusCounts := map[string]int64{}
	probe4xx := int64(0)
	probe5xx := int64(0)
	for _, sample := range samples {
		latencies = append(latencies, sample.latencyMs)
		if sample.success {
			successCount++
		}
		if sample.statusCode > 0 {
			code := strconv.Itoa(sample.statusCode)
			statusCounts[code]++
			if sample.statusCode >= 500 {
				probe5xx++
			} else if sample.statusCode >= 400 {
				probe4xx++
			}
		}
	}

	sort.Float64s(latencies)
	avgLatency := 0.0
	for _, l := range latencies {
		avgLatency += l
	}
	if len(latencies) > 0 {
		avgLatency = round2(avgLatency / float64(len(latencies)))
	}

	now := time.Now()
	oneMinuteAgo := now.Add(-1 * time.Minute)
	fiveMinutesAgo := now.Add(-5 * time.Minute)

	perMinute := int64(0)
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ?", oneMinuteAgo).Count(&perMinute).Error

	log4xx := int64(0)
	log5xx := int64(0)
	totalLogs := int64(0)
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ?", fiveMinutesAgo).Count(&totalLogs).Error
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ? AND status_code >= 400 AND status_code < 500", fiveMinutesAgo).Count(&log4xx).Error
	_ = config.DB.Model(&models.SystemLog{}).Where("created_at >= ? AND status_code >= 500", fiveMinutesAgo).Count(&log5xx).Error

	total5xx := log5xx
	total4xx := log4xx
	totalRequests := totalLogs
	if totalRequests == 0 {
		total5xx = probe5xx
		total4xx = probe4xx
		totalRequests = int64(len(samples))
	}

	errorPercent := 0.0
	if totalRequests > 0 {
		errorPercent = round2(float64(total5xx+total4xx) * 100 / float64(totalRequests))
	}

	uptimePercent := round2(float64(successCount) * 100 / float64(totalProbes))
	lastDowntime := interface{}(nil)
	if successCount < len(samples) {
		lastDowntime = now.UTC().Format(time.RFC3339)
	}

	codeRows := make([]fiber.Map, 0)
	if len(statusCounts) == 0 {
		type statusCodeCount struct {
			StatusCode int
			Count      int64
		}
		var rows []statusCodeCount
		if err := config.DB.Model(&models.SystemLog{}).
			Select("status_code, count(*) as count").
			Where("created_at >= ? AND status_code IS NOT NULL", fiveMinutesAgo).
			Group("status_code").
			Scan(&rows).Error; err == nil {
			for _, row := range rows {
				codeRows = append(codeRows, fiber.Map{
					"code":  strconv.Itoa(row.StatusCode),
					"count": row.Count,
				})
			}
		}
	} else {
		codes := make([]string, 0, len(statusCounts))
		for code := range statusCounts {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			codeRows = append(codeRows, fiber.Map{
				"code":  code,
				"count": statusCounts[code],
			})
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"uptime": fiber.Map{
				"isUp":          successCount == len(samples) && len(samples) > 0,
				"uptimePercent": uptimePercent,
				"lastDowntime":  lastDowntime,
			},
			"responseTime": fiber.Map{
				"avgMs":  avgLatency,
				"p50Ms":  percentile(latencies, 0.5),
				"p95Ms":  percentile(latencies, 0.95),
				"p99Ms":  percentile(latencies, 0.99),
				"status": computeResponseStatus(avgLatency),
			},
			"errorRate": fiber.Map{
				"percent":       errorPercent,
				"total5xx":      total5xx,
				"total4xx":      total4xx,
				"totalRequests": totalRequests,
				"status":        computeErrorStatus(errorPercent),
			},
			"statusCodes": codeRows,
			"requestRate": fiber.Map{
				"perSecond": round2(float64(perMinute) / 60),
				"perMinute": perMinute,
			},
			"timestamp": now.UTC().Format(time.RFC3339),
		},
	})
}

// GetMonitoringTrendsHandler handles GET /api/system/trends.
func GetMonitoringTrendsHandler(c fiber.Ctx) error {
	rangeKey := strings.TrimSpace(c.Query("range", "24h"))

	snapshot := collectSystemSnapshot()
	containers, _ := collectContainerMetrics()
	runningContainers := 0
	for _, container := range containers {
		if container.Status == "running" {
			runningContainers++
		}
	}

	websiteSamples := collectWebsiteProbes()
	responseAvg, errorPercent, perMinute := collectWebsiteTrendMetrics(websiteSamples)

	now := time.Now().UTC()
	point := monitoringTrendPoint{
		Timestamp:         now,
		CPUPercent:        snapshot.CPUUsage,
		MemoryPercent:     snapshot.MemoryUsagePct,
		DiskPercent:       snapshot.DiskUsagePct,
		ResponseAvgMs:     responseAvg,
		ErrorPercent:      errorPercent,
		RequestsPerMinute: perMinute,
		RunningContainers: runningContainers,
	}
	_ = appendMonitoringTrendPoint(point)

	history, err := loadMonitoringTrendHistory()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถอ่านประวัติแนวโน้มระบบได้"})
	}

	filtered := filterMonitoringTrendByRange(history, rangeKey)

	result := make([]fiber.Map, 0, len(filtered))
	for _, p := range filtered {
		result = append(result, fiber.Map{
			"timestamp":         p.Timestamp.Format(time.RFC3339),
			"cpuPercent":        p.CPUPercent,
			"memoryPercent":     p.MemoryPercent,
			"diskPercent":       p.DiskPercent,
			"responseAvgMs":     p.ResponseAvgMs,
			"errorPercent":      p.ErrorPercent,
			"requestsPerMinute": p.RequestsPerMinute,
			"runningContainers": p.RunningContainers,
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"range":  rangeKey,
			"points": result,
		},
	})
}

// GetCpuUsageHandler handles GET /api/system/cpu.
func GetCpuUsageHandler(c fiber.Ctx) error {
	snapshot := collectSystemSnapshot()

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"usage":     snapshot.CPUUsage,
			"cores":     snapshot.CPUCores,
			"model":     snapshot.CPUModel,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// GetMemoryUsageHandler handles GET /api/system/memory.
func GetMemoryUsageHandler(c fiber.Ctx) error {
	snapshot := collectSystemSnapshot()

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"total":        snapshot.MemoryTotalBytes,
			"free":         snapshot.MemoryFreeBytes,
			"used":         snapshot.MemoryUsedBytes,
			"usagePercent": snapshot.MemoryUsagePct,
			"totalGB":      bytesToGB(snapshot.MemoryTotalBytes),
			"freeGB":       bytesToGB(snapshot.MemoryFreeBytes),
			"usedGB":       bytesToGB(snapshot.MemoryUsedBytes),
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// GetServerInfoHandler handles GET /api/system/info.
func GetServerInfoHandler(c fiber.Ctx) error {
	snapshot := collectSystemSnapshot()
	processUptime := time.Since(processStartedAt).Seconds()

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"hostname":    snapshot.Hostname,
			"platform":    snapshot.Platform,
			"arch":        snapshot.Architecture,
			"nodeVersion": runtime.Version(),
			"goVersion":   runtime.Version(),
			"uptime": fiber.Map{
				"seconds":   snapshot.HostUptimeSeconds,
				"formatted": formatUptime(float64(snapshot.HostUptimeSeconds)),
			},
			"processUptime": fiber.Map{
				"seconds":   round2(processUptime),
				"formatted": formatUptime(processUptime),
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}
