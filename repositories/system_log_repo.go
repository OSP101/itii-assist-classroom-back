package repositories

import (
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SystemLogListParams struct {
	LogType   string
	Severity  string
	UserID    string
	StartDate string
	EndDate   string
	Search    string
	Page      int
	Limit     int
	SortBy    string
	SortOrder string
}

type SystemLogListResult struct {
	Logs      []models.SystemLog `json:"logs"`
	Total     int64              `json:"total"`
	Page      int                `json:"page"`
	Limit     int                `json:"limit"`
	TotalPage int                `json:"totalPages"`
}

type SystemLogCountRow struct {
	Key   string `json:"key" gorm:"column:key"`
	Count int64  `json:"count" gorm:"column:count"`
}

type SystemLogStats struct {
	Total        int64               `json:"total"`
	UniqueIPs    int64               `json:"uniqueIps"`
	ByType       []SystemLogCountRow `json:"byType"`
	BySeverity   []SystemLogCountRow `json:"bySeverity"`
	ByStatusCode []SystemLogCountRow `json:"byStatusCode"`
}

type SystemLogTimelinePoint struct {
	TimeBucket string `json:"time_bucket"`
	LogType    string `json:"log_type"`
	Count      int64  `json:"count"`
}

type SystemLogCleanupResult struct {
	DeletedCount  int64     `json:"deletedCount"`
	RetentionDays int       `json:"retentionDays"`
	CutoffDate    time.Time `json:"cutoffDate"`
}

var validSystemLogSortFields = map[string]bool{
	"created_at":       true,
	"log_type":         true,
	"severity":         true,
	"action":           true,
	"status_code":      true,
	"response_time_ms": true,
	"ip_address":       true,
}

var systemLogTypes = []string{"access", "error", "auth", "security"}
var systemLogSeverities = []string{"debug", "info", "warn", "error", "critical"}

func applySystemLogDateRange(query *gorm.DB, startDate string, endDate string) *gorm.DB {
	if startDate != "" {
		if parsed, err := parseSystemLogDate(startDate, false); err == nil {
			query = query.Where("created_at >= ?", parsed)
		}
	}
	if endDate != "" {
		if parsed, err := parseSystemLogDate(endDate, true); err == nil {
			query = query.Where("created_at <= ?", parsed)
		}
	}
	return query
}

func parseSystemLogDate(value string, endOfDay bool) (time.Time, error) {
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			if layout == "2006-01-02" && endOfDay {
				return parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond), nil
			}
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid time format")
}

func GetLogs(params SystemLogListParams) (*SystemLogListResult, error) {
	var logs []models.SystemLog
	var count int64
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 50
	}

	query := config.DB.Model(&models.SystemLog{})

	if params.LogType != "" {
		query = query.Where("log_type = ?", params.LogType)
	}
	if params.Severity != "" {
		query = query.Where("severity = ?", params.Severity)
	}
	if params.UserID != "" {
		query = query.Where("actor_user_id = ?", params.UserID)
	}
	query = applySystemLogDateRange(query, params.StartDate, params.EndDate)
	if params.Search != "" {
		like := "%" + params.Search + "%"
		query = query.Where("action ILIKE ? OR url ILIKE ? OR ip_address ILIKE ? OR error_message ILIKE ?", like, like, like, like)
	}

	query.Count(&count)

	sortBy := params.SortBy
	if sortBy == "" || !validSystemLogSortFields[sortBy] {
		sortBy = "created_at"
	}
	sortOrder := params.SortOrder
	if strings.ToUpper(sortOrder) != "ASC" {
		sortOrder = "desc"
	} else {
		sortOrder = "asc"
	}
	query = query.Order(sortBy + " " + sortOrder)

	offset := (params.Page - 1) * params.Limit
	err := query.Limit(params.Limit).Offset(offset).Find(&logs).Error
	if err != nil {
		return nil, err
	}

	totalPages := int(count) / params.Limit
	if int(count)%params.Limit != 0 {
		totalPages++
	}

	return &SystemLogListResult{
		Logs:      logs,
		Total:     count,
		Page:      params.Page,
		Limit:     params.Limit,
		TotalPage: totalPages,
	}, nil
}

func GetLogByID(id string) (*models.SystemLog, error) {
	var log models.SystemLog
	err := config.DB.Where("id = ?", id).First(&log).Error
	return &log, err
}

func GetSystemLogStats(startDate string, endDate string) (*SystemLogStats, error) {
	query := applySystemLogDateRange(config.DB.Model(&models.SystemLog{}), startDate, endDate)

	stats := &SystemLogStats{}
	if err := query.Count(&stats.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Distinct("ip_address").Where("ip_address <> ''").Count(&stats.UniqueIPs).Error; err != nil {
		return nil, err
	}

	type row struct {
		Key   string
		Count int64
	}
	var byTypeRows []row
	if err := applySystemLogDateRange(config.DB.Model(&models.SystemLog{}), startDate, endDate).
		Select("log_type as key, COUNT(*) as count").
		Group("log_type").
		Order("log_type ASC").
		Scan(&byTypeRows).Error; err != nil {
		return nil, err
	}
	stats.ByType = make([]SystemLogCountRow, len(byTypeRows))
	for i, item := range byTypeRows {
		stats.ByType[i] = SystemLogCountRow{Key: item.Key, Count: item.Count}
	}

	var bySeverityRows []row
	if err := applySystemLogDateRange(config.DB.Model(&models.SystemLog{}), startDate, endDate).
		Select("severity as key, COUNT(*) as count").
		Group("severity").
		Order("severity ASC").
		Scan(&bySeverityRows).Error; err != nil {
		return nil, err
	}
	stats.BySeverity = make([]SystemLogCountRow, len(bySeverityRows))
	for i, item := range bySeverityRows {
		stats.BySeverity[i] = SystemLogCountRow{Key: item.Key, Count: item.Count}
	}

	var byStatusRows []row
	if err := applySystemLogDateRange(config.DB.Model(&models.SystemLog{}), startDate, endDate).
		Where("status_code IS NOT NULL").
		Select("CAST(status_code AS TEXT) as key, COUNT(*) as count").
		Group("status_code").
		Order("count DESC").
		Limit(10).
		Scan(&byStatusRows).Error; err != nil {
		return nil, err
	}
	stats.ByStatusCode = make([]SystemLogCountRow, len(byStatusRows))
	for i, item := range byStatusRows {
		stats.ByStatusCode[i] = SystemLogCountRow{Key: item.Key, Count: item.Count}
	}

	return stats, nil
}

func GetSystemLogTimeline(startDate string, endDate string, interval string, logType string) ([]SystemLogTimelinePoint, error) {
	bucket := "hour"
	format := "2006-01-02 15:00"
	switch strings.ToLower(interval) {
	case "day":
		bucket = "day"
		format = "2006-01-02"
	case "week":
		bucket = "week"
		format = "2006-01-02"
	}

	query := `
		SELECT date_trunc('` + bucket + `', created_at) AS time_bucket, log_type, COUNT(*) AS count
		FROM system_logs
		WHERE created_at BETWEEN ? AND ?
	`
	args := []interface{}{defaultSystemLogStart(startDate), defaultSystemLogEnd(endDate)}
	if logType != "" {
		query += " AND log_type = ?"
		args = append(args, logType)
	}
	query += " GROUP BY time_bucket, log_type ORDER BY time_bucket ASC"

	type timelineRow struct {
		TimeBucket time.Time `gorm:"column:time_bucket"`
		LogType    string    `gorm:"column:log_type"`
		Count      int64     `gorm:"column:count"`
	}
	var rows []timelineRow
	if err := config.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]SystemLogTimelinePoint, len(rows))
	for i, item := range rows {
		result[i] = SystemLogTimelinePoint{
			TimeBucket: item.TimeBucket.Format(format),
			LogType:    item.LogType,
			Count:      item.Count,
		}
	}
	return result, nil
}

func defaultSystemLogStart(startDate string) time.Time {
	if startDate != "" {
		if parsed, err := parseSystemLogDate(startDate, false); err == nil {
			return parsed
		}
	}
	return time.Now().Add(-24 * time.Hour)
}

func defaultSystemLogEnd(endDate string) time.Time {
	if endDate != "" {
		if parsed, err := parseSystemLogDate(endDate, true); err == nil {
			return parsed
		}
	}
	return time.Now()
}

func GetSystemLogFilterOptions() map[string]interface{} {
	return map[string]interface{}{
		"logTypes":       systemLogTypes,
		"severityLevels": systemLogSeverities,
		"httpMethods":    []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
	}
}

func GetRecentSystemLogs(logType string, limit int) ([]models.SystemLog, error) {
	if limit <= 0 {
		limit = 10
	}
	var logs []models.SystemLog
	err := config.DB.Where("log_type = ?", logType).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

func CleanupOldSystemLogs(retentionDays int) (*SystemLogCleanupResult, error) {
	if retentionDays < 90 {
		retentionDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := config.DB.Where("created_at < ?", cutoff).Delete(&models.SystemLog{})
	if result.Error != nil {
		return nil, result.Error
	}
	return &SystemLogCleanupResult{
		DeletedCount:  result.RowsAffected,
		RetentionDays: retentionDays,
		CutoffDate:    cutoff,
	}, nil
}
