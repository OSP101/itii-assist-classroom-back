package handlers

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

type cloudStorageAggregate struct {
	TotalBytes   int64      `json:"total_bytes"`
	ObjectCount  int64      `json:"object_count"`
	LastBackupAt *time.Time `json:"last_backup_at,omitempty"`
}

func envFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func collectCloudStorageAggregate() cloudStorageAggregate {
	agg := cloudStorageAggregate{}
	_ = config.DB.Model(&models.DatabaseBackupRecord{}).
		Where("deleted_at IS NULL").
		Select("COALESCE(SUM(file_size_bytes), 0) AS total_bytes, COUNT(*) AS object_count, MAX(created_at) AS last_backup_at").
		Scan(&agg).Error
	return agg
}

// GetCloudOverviewHandler handles GET /api/system/cloud/overview.
func GetCloudOverviewHandler(c fiber.Ctx) error {
	agg := collectCloudStorageAggregate()
	r2Status, r2Detail := services.CheckR2Health()
	backupStatus, _ := services.GetBackupOperationStatus()

	overallStatus := "up"
	if r2Status == "down" {
		overallStatus = "degraded"
	}
	if backupStatus.Running {
		overallStatus = "warning"
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"provider":      "cloudflare_r2",
			"overallStatus": overallStatus,
			"storage": fiber.Map{
				"totalBytes":   agg.TotalBytes,
				"totalGB":      round4(float64(agg.TotalBytes) / (1024 * 1024 * 1024)),
				"objectCount":  agg.ObjectCount,
				"lastBackupAt": agg.LastBackupAt,
			},
			"r2": fiber.Map{
				"status": r2Status,
				"detail": r2Detail,
			},
			"backup": fiber.Map{
				"running":      backupStatus.Running,
				"lastStatus":   backupStatus.LastStatus,
				"lastError":    backupStatus.LastError,
				"lastBackupAt": backupStatus.LastBackupAt,
				"updatedAt":    backupStatus.UpdatedAt,
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// GetCloudCostHandler handles GET /api/system/cloud/cost.
func GetCloudCostHandler(c fiber.Ctx) error {
	agg := collectCloudStorageAggregate()
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	daysElapsed := now.Sub(monthStart).Hours()/24 + 1
	if daysElapsed < 1 {
		daysElapsed = 1
	}

	storageRatePerGBMonth := envFloat("R2_COST_PER_GB_MONTH_USD", 0.015)
	opsRatePer1000 := envFloat("R2_COST_PER_1000_OPS_USD", 0.0045)

	totalGB := float64(agg.TotalBytes) / (1024 * 1024 * 1024)
	storageCostMTD := (totalGB * storageRatePerGBMonth) * (daysElapsed / 30.0)
	opsCostMTD := (float64(agg.ObjectCount) / 1000.0) * opsRatePer1000
	totalCostMTD := storageCostMTD + opsCostMTD
	forecastMonthly := totalCostMTD / daysElapsed * 30.0

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"currency":  "USD",
			"estimated": true,
			"mtd": fiber.Map{
				"storage":    round4(storageCostMTD),
				"operations": round4(opsCostMTD),
				"total":      round4(totalCostMTD),
			},
			"forecast": fiber.Map{
				"monthly": round4(forecastMonthly),
			},
			"assumptions": fiber.Map{
				"storageRatePerGBMonth": storageRatePerGBMonth,
				"operationsRatePer1000": opsRatePer1000,
				"daysElapsed":           round4(daysElapsed),
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}
