package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Container metrics come from the Docker Engine HTTP API, not the docker CLI:
// the backend container has no Docker socket of its own, so it reaches the
// engine through the read-only docker-socket-proxy service (DOCKER_API_URL,
// see deploy-vm-https/docker-compose.yml) which only allows GET on
// /containers/*. A local /var/run/docker.sock is used when present so
// running the backend directly on a Docker host still works.

type dockerContainerMetrics struct {
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	CPUPercent    float64 `json:"cpuPercent"`
	CPULimitCores float64 `json:"cpuLimitCores"`
	MemoryUsageMB float64 `json:"memoryUsageMB"`
	MemoryLimitMB float64 `json:"memoryLimitMB"`
	MemoryPercent float64 `json:"memoryPercent"`
	Restarts      int     `json:"restarts"`
	Status        string  `json:"status"`
	Health        string  `json:"health"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    string  `json:"finishedAt"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
}

const (
	containerMetricsCacheTTL = 4 * time.Second
	// stats?stream=false blocks ~1-2s per container while the engine takes
	// two samples for the CPU delta, so containers are fetched in parallel.
	containerStatsConcurrency = 8
)

var (
	containerMetricsGroup    singleflight.Group
	containerMetricsCacheMu  sync.Mutex
	containerMetricsCache    []dockerContainerMetrics
	containerMetricsSource   string
	containerMetricsCachedAt time.Time

	dockerAPIOnce    sync.Once
	dockerAPIHTTP    *http.Client
	dockerAPIBaseURL string
)

func dockerAPI() (*http.Client, string) {
	dockerAPIOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("DOCKER_API_URL"))
		if raw == "" {
			if _, err := os.Stat("/var/run/docker.sock"); err == nil {
				raw = "unix:///var/run/docker.sock"
			}
		}
		if raw == "" {
			return
		}

		transport := &http.Transport{MaxIdleConnsPerHost: containerStatsConcurrency}
		if strings.HasPrefix(raw, "unix://") {
			socketPath := strings.TrimPrefix(raw, "unix://")
			transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			}
			raw = "http://docker"
		}
		if _, err := url.Parse(raw); err != nil {
			return
		}

		dockerAPIHTTP = &http.Client{Transport: transport, Timeout: 8 * time.Second}
		dockerAPIBaseURL = strings.TrimRight(raw, "/")
	})
	return dockerAPIHTTP, dockerAPIBaseURL
}

func dockerAPIGet(ctx context.Context, path string, out any) error {
	client, baseURL := dockerAPI()
	if client == nil {
		return errors.New("docker api not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker api %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type dockerListEntry struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
	Image string   `json:"Image"`
	State string   `json:"State"`
}

type dockerInspect struct {
	RestartCount int `json:"RestartCount"`
	State        struct {
		Status     string `json:"Status"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		NanoCpus int64 `json:"NanoCpus"`
	} `json:"HostConfig"`
}

type dockerStats struct {
	CPUStats    dockerCPUStats `json:"cpu_stats"`
	PreCPUStats dockerCPUStats `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

type dockerCPUStats struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs  uint32 `json:"online_cpus"`
}

// dockerCPUPercent matches `docker stats`: 100% = one full core.
func dockerCPUPercent(s dockerStats) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	online := float64(s.CPUStats.OnlineCPUs)
	if online == 0 {
		online = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if online == 0 {
		online = 1
	}
	return round2(cpuDelta / systemDelta * online * 100)
}

// dockerMemoryUsed matches `docker stats`: page cache that the kernel can
// reclaim is not counted (inactive_file on cgroup v2, total_inactive_file on v1).
func dockerMemoryUsed(s dockerStats) uint64 {
	usage := s.MemoryStats.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := s.MemoryStats.Stats[key]; ok && v < usage {
			return usage - v
		}
	}
	return usage
}

func normalizeContainerStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "running"
	case "restarting":
		return "restarting"
	default:
		return "stopped"
	}
}

// Docker reports never-set timestamps as 0001-01-01T00:00:00Z.
func dockerTime(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil || t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}

func collectContainerMetrics() ([]dockerContainerMetrics, string) {
	containerMetricsCacheMu.Lock()
	if containerMetricsCache != nil && time.Since(containerMetricsCachedAt) < containerMetricsCacheTTL {
		cached, source := containerMetricsCache, containerMetricsSource
		containerMetricsCacheMu.Unlock()
		return cached, source
	}
	containerMetricsCacheMu.Unlock()

	type result struct {
		containers []dockerContainerMetrics
		source     string
	}
	v, _, _ := containerMetricsGroup.Do("containers", func() (any, error) {
		containers, source := fetchContainerMetrics()
		if source != "docker_unavailable" {
			containerMetricsCacheMu.Lock()
			containerMetricsCache, containerMetricsSource = containers, source
			containerMetricsCachedAt = time.Now()
			containerMetricsCacheMu.Unlock()
		}
		return result{containers, source}, nil
	})
	r := v.(result)
	return r.containers, r.source
}

func fetchContainerMetrics() ([]dockerContainerMetrics, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var list []dockerListEntry
	if err := dockerAPIGet(ctx, "/containers/json?all=1", &list); err != nil {
		return []dockerContainerMetrics{}, "docker_unavailable"
	}

	now := time.Now()
	containers := make([]dockerContainerMetrics, len(list))
	var partialMu sync.Mutex
	partial := false
	markPartial := func() {
		partialMu.Lock()
		partial = true
		partialMu.Unlock()
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(containerStatsConcurrency)
	for i, entry := range list {
		name := entry.ID
		if len(entry.Names) > 0 {
			name = strings.TrimPrefix(entry.Names[0], "/")
		}
		containers[i] = dockerContainerMetrics{
			Name:   name,
			Image:  entry.Image,
			Status: normalizeContainerStatus(entry.State),
		}

		g.Go(func() error {
			metric := &containers[i]

			var inspect dockerInspect
			if err := dockerAPIGet(gctx, "/containers/"+entry.ID+"/json", &inspect); err != nil {
				markPartial()
			} else {
				metric.Restarts = inspect.RestartCount
				metric.Status = normalizeContainerStatus(inspect.State.Status)
				if inspect.State.Health != nil {
					metric.Health = inspect.State.Health.Status
				}
				if inspect.HostConfig.NanoCpus > 0 {
					metric.CPULimitCores = round2(float64(inspect.HostConfig.NanoCpus) / 1e9)
				}
				if started, ok := dockerTime(inspect.State.StartedAt); ok {
					metric.StartedAt = started.UTC().Format(time.RFC3339)
					if metric.Status == "running" {
						metric.UptimeSeconds = int64(now.Sub(started).Seconds())
					}
				}
				if finished, ok := dockerTime(inspect.State.FinishedAt); ok && metric.Status != "running" {
					metric.FinishedAt = finished.UTC().Format(time.RFC3339)
				}
			}

			if metric.Status != "running" {
				return nil
			}

			var stats dockerStats
			if err := dockerAPIGet(gctx, "/containers/"+entry.ID+"/stats?stream=false", &stats); err != nil {
				markPartial()
				return nil
			}
			metric.CPUPercent = dockerCPUPercent(stats)
			used := dockerMemoryUsed(stats)
			metric.MemoryUsageMB = round2(float64(used) / (1024 * 1024))
			metric.MemoryLimitMB = round2(float64(stats.MemoryStats.Limit) / (1024 * 1024))
			if stats.MemoryStats.Limit > 0 {
				metric.MemoryPercent = round2(float64(used) / float64(stats.MemoryStats.Limit) * 100)
			}
			return nil
		})
	}
	_ = g.Wait()

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})

	if partial {
		return containers, "docker_api_partial"
	}
	return containers, "docker_api"
}
