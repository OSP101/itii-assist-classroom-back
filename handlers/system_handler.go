package handlers

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

var processStartedAt = time.Now()

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
				"usage": snapshot.CPUUsage,
				"free":  round2(100 - snapshot.CPUUsage),
				"info": fiber.Map{
					"model": snapshot.CPUModel,
					"cores": snapshot.CPUCores,
					"speed": snapshot.CPUSpeedMHz,
				},
			},
			"memory": fiber.Map{
				"total":        snapshot.MemoryTotalBytes,
				"free":         snapshot.MemoryFreeBytes,
				"used":         snapshot.MemoryUsedBytes,
				"usagePercent": snapshot.MemoryUsagePct,
				"totalGB":      bytesToGB(snapshot.MemoryTotalBytes),
				"freeGB":       bytesToGB(snapshot.MemoryFreeBytes),
				"usedGB":       bytesToGB(snapshot.MemoryUsedBytes),
			},
			"disk": fiber.Map{
				"total":        snapshot.DiskTotalBytes,
				"free":         snapshot.DiskFreeBytes,
				"used":         snapshot.DiskUsedBytes,
				"usagePercent": snapshot.DiskUsagePct,
				"totalGB":      bytesToGB(snapshot.DiskTotalBytes),
				"freeGB":       bytesToGB(snapshot.DiskFreeBytes),
				"usedGB":       bytesToGB(snapshot.DiskUsedBytes),
			},
			"system": fiber.Map{
				"platform":    snapshot.Platform,
				"arch":        snapshot.Architecture,
				"hostname":    snapshot.Hostname,
				"type":        snapshot.OSType,
				"release":     snapshot.OSRelease,
				"uptime":      snapshot.HostUptimeSeconds,
				"nodeVersion": runtime.Version(),
				"goVersion":   runtime.Version(),
			},
			"loadAverage": fiber.Map{
				"1min":  snapshot.Load1,
				"5min":  snapshot.Load5,
				"15min": snapshot.Load15,
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
