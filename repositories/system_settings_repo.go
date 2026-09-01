package repositories

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	maintenanceModeConfigKey = "system.maintenance_mode"
	studentProgramsConfigKey = "student.programs"
	featureFlagConfigPrefix  = "feature_flag."
	announcementContentText  = "text"
	announcementContentImage = "image"
	announcementContentMixed = "mixed"
	announcementModeBanner   = "banner_top"
	announcementModeFull     = "fullscreen"
	// announcementModeTopbar is the thin full-width ribbon across the top of
	// the page, the shape most product sites use for a standing notice.
	announcementModeTopbar = "topbar"
	// announcementModeFullImage shows the image edge to edge with the text
	// laid over it, for posters and graphics that carry the message
	// themselves.
	announcementModeFullImage = "fullscreen_image"
	// announcementModeCorner is the small card that floats in the bottom-left
	// corner: visible while the reader keeps working, rather than blocking the
	// page or taking a line off the top of it.
	announcementModeCorner = "corner_card"
	announcementAllPages   = "all_pages"

	announcementSeverityInfo    = "info"
	announcementSeveritySuccess = "success"
	announcementSeverityWarning = "warning"
	announcementSeverityUrgent  = "urgent"

	announcementStatusDraft     = "draft"
	announcementStatusScheduled = "scheduled"
	announcementStatusPublished = "published"
	announcementStatusArchived  = "archived"

	// AnnouncementBatchLimit caps one batch create call. Each announcement in
	// a batch fans out to every recipient, so an unbounded batch is an
	// unbounded amount of work behind a single request.
	AnnouncementBatchLimit = 20
)

// announcementWritableColumns lists every column CreateAnnouncement writes.
// GORM omits zero-valued fields that carry a `default:` tag from the INSERT,
// which for is_active, is_dismissible and notify_inbox means saving one as
// false silently stores true instead — a draft would go out live. Naming the
// columns in Select forces them all into the statement.
var announcementWritableColumns = []string{
	"title", "title_th", "title_en",
	"message", "message_th", "message_en",
	"content_type", "display_mode", "severity", "priority", "status",
	"image_url",
	"action_label", "action_label_th", "action_label_en", "action_url",
	"is_dismissible", "notify_inbox", "display_paths",
	"scheduled_at", "expires_at", "audience",
	"require_acknowledge", "is_active", "created_by", "created_at", "updated_at",
}

type FeatureFlagState struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MaintenanceModeConfig struct {
	Enabled             bool       `json:"enabled"`
	ScheduleType        string     `json:"schedule_type"` // "indefinite" or "scheduled"
	Message             string     `json:"message"`
	StartTime           *time.Time `json:"start_time,omitempty"`
	EndTime             *time.Time `json:"end_time,omitempty"`
	WhitelistAdminUsers []uint     `json:"whitelist_admin_users"`
	UpdatedBy           *uint      `json:"updated_by,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type StudentProgramConfig struct {
	ShortName         string `json:"short_name"`
	FullName          string `json:"full_name"`
	OriginalShortName string `json:"original_short_name,omitempty"`
}

type AnnouncementInput struct {
	Title              string
	TitleTH            string
	TitleEN            string
	Message            string
	MessageTH          string
	MessageEN          string
	ContentType        string
	DisplayMode        string
	ImageURL           string
	ActionLabel        string
	ActionLabelTH      string
	ActionLabelEN      string
	ActionURL          string
	IsDismissible      bool
	DisplayPaths       []string
	ScheduledAt        *time.Time
	ExpiresAt          *time.Time
	Audience           []string
	RequireAcknowledge bool
	IsActive           bool
	Severity           string
	Priority           int
	Status             string
	NotifyInbox        bool
}

type AnnouncementWithAck struct {
	models.SystemAnnouncement
	AckCount      int64 `json:"ack_count"`
	DismissCount  int64 `json:"dismiss_count"`
	AudienceCount int64 `json:"audience_count"`
}

type ActiveAnnouncementForUser struct {
	models.SystemAnnouncement
	IsAcknowledged bool `json:"is_acknowledged"`
}

// AnnouncementListFilter is what the admin list screen can ask for. The list
// used to be hard-filtered to is_active = true, so the screen's own
// "deactivated" filter could never match anything and an announcement that had
// been switched off could not be found again to edit.
type AnnouncementListFilter struct {
	IncludeExpired bool
	Status         string
	Severity       string
	Search         string
}

// AnnouncementRecipient is one person an announcement was addressed to, with
// whether they have acknowledged it yet.
type AnnouncementRecipient struct {
	UserID         uint       `json:"user_id"`
	FullName       string     `json:"full_name"`
	Email          string     `json:"email"`
	Role           string     `json:"role"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
}

// AnnouncementStats is the reach summary shown next to an announcement.
type AnnouncementStats struct {
	AnnouncementID uint                    `json:"announcement_id"`
	AudienceCount  int64                   `json:"audience_count"`
	AckCount       int64                   `json:"ack_count"`
	DismissCount   int64                   `json:"dismiss_count"`
	AckPercent     float64                 `json:"ack_percent"`
	Pending        []AnnouncementRecipient `json:"pending"`
	Acknowledged   []AnnouncementRecipient `json:"acknowledged"`
}

type featureFlagDefinition struct {
	Label       string
	Description string
	Default     bool
}

var defaultFeatureFlags = map[string]featureFlagDefinition{
	"menu.attendance": {
		Label:       "เช็กชื่อ",
		Description: "ฟังก์ชันเช็กชื่อและจัดการเซสชันเช็กชื่อสำหรับอาจารย์และผู้ช่วยสอน",
		Default:     true,
	},
	"menu.assignments": {
		Label:       "งานและการบ้าน",
		Description: "ฟังก์ชันจัดการงาน การบ้าน และลำดับงานสำหรับอาจารย์และผู้ช่วยสอน",
		Default:     true,
	},
	"menu.queue": {
		Label:       "คิว",
		Description: "ฟังก์ชันจัดการคิว การจองคิว และการทำงานของผู้ช่วยสอน",
		Default:     true,
	},
	"menu.scores": {
		Label:       "คะแนน",
		Description: "ฟังก์ชันกรอกคะแนน สรุปคะแนน และคำขอแก้ไขคะแนน",
		Default:     true,
	},
	"menu.exams": {
		Label:       "สอบ",
		Description: "ฟังก์ชันจัดการการสอบ คะแนนสอบ เซสชันสอบ และผังที่นั่งสอบ",
		Default:     true,
	},
	"menu.teams": {
		Label:       "กลุ่มเรียน",
		Description: "ฟังก์ชันจัดการกลุ่มเรียน กลุ่มทำงาน และการจัดกลุ่มนักศึกษาในรายวิชา",
		Default:     true,
	},
	"menu.people": {
		Label:       "บุคลากร",
		Description: "ฟังก์ชันจัดการบุคลากร การเพิ่ม/ลบสมาชิก และการแก้ไขสิทธิ์ในรายวิชา",
		Default:     true,
	},
	"menu.activity-log": {
		Label:       "บันทึกกิจกรรม",
		Description: "ฟังก์ชันดูประวัติกิจกรรมและบันทึกการดำเนินงานในรายวิชาสำหรับอาจารย์",
		Default:     true,
	},
	"menu.ta-stats": {
		Label:       "สถิติ TA",
		Description: "ฟังก์ชันดูสถิติการทำงานของผู้ช่วยสอน (TA) ในรายวิชาสำหรับอาจารย์",
		Default:     true,
	},
	"menu.settings": {
		Label:       "ตั้งค่ารายวิชา",
		Description: "ฟังก์ชันตั้งค่ารายวิชา จัดการ PIN และกำหนดค่าต่าง ๆ สำหรับอาจารย์",
		Default:     true,
	},
}

type settingsCacheEntry struct {
	value     string
	expiresAt time.Time
	createdAt time.Time
}

var settingsCache = struct {
	sync.RWMutex
	values map[string]settingsCacheEntry
}{
	values: map[string]settingsCacheEntry{},
}

const settingsCacheTTL = 30 * time.Second

func getCachedConfigValue(key string) (string, bool) {
	settingsCache.RLock()
	entry, ok := settingsCache.values[key]
	settingsCache.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		settingsCache.Lock()
		delete(settingsCache.values, key)
		settingsCache.Unlock()
		return "", false
	}
	return entry.value, true
}

func setCachedConfigValue(key string, value string) {
	settingsCache.Lock()
	settingsCache.values[key] = settingsCacheEntry{
		value:     value,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(settingsCacheTTL),
	}
	settingsCache.Unlock()
}

func invalidateCachedConfigValue(key string) {
	settingsCache.Lock()
	delete(settingsCache.values, key)
	settingsCache.Unlock()
}

func configKeyForFeatureFlag(flagKey string) string {
	return featureFlagConfigPrefix + strings.TrimSpace(flagKey)
}

// PruneObsoleteFeatureFlags removes AppConfig rows for feature-flag keys that are
// no longer defined in defaultFeatureFlags. Call this once on startup after AutoMigrate.
func PruneObsoleteFeatureFlags() {
	var rows []models.AppConfig
	if err := config.DB.
		Where("key LIKE ?", featureFlagConfigPrefix+"%").
		Find(&rows).Error; err != nil {
		return
	}

	for _, row := range rows {
		flagKey := strings.TrimPrefix(row.Key, featureFlagConfigPrefix)
		if _, defined := defaultFeatureFlags[flagKey]; !defined {
			config.DB.Delete(&row)
			invalidateCachedConfigValue(row.Key)
		}
	}
}

func GetAppConfigValue(key string) (string, error) {
	if cached, ok := getCachedConfigValue(key); ok {
		return cached, nil
	}

	var item models.AppConfig
	err := config.DB.Where("key = ?", key).First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		return "", err
	}

	setCachedConfigValue(key, item.Value)
	return item.Value, nil
}

func SetAppConfigValue(key string, value string) error {
	var item models.AppConfig
	err := config.DB.Where("key = ?", key).First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			item.Key = key
			item.Value = value
			if createErr := config.DB.Create(&item).Error; createErr != nil {
				return createErr
			}
			setCachedConfigValue(key, value)
			return nil
		}
		return err
	}

	item.Value = value
	if err := config.DB.Save(&item).Error; err != nil {
		return err
	}
	setCachedConfigValue(key, value)
	return nil
}

func IsFeatureEnabled(flagKey string) bool {
	definition, ok := defaultFeatureFlags[flagKey]
	if !ok {
		return true
	}

	storedValue, err := GetAppConfigValue(configKeyForFeatureFlag(flagKey))
	if err != nil {
		return definition.Default
	}

	parsed, parseErr := strconv.ParseBool(storedValue)
	if parseErr != nil {
		return definition.Default
	}
	return parsed
}

func GetFeatureFlags() ([]FeatureFlagState, error) {
	keys := make([]string, 0, len(defaultFeatureFlags))
	for key := range defaultFeatureFlags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]FeatureFlagState, 0, len(keys))
	for _, key := range keys {
		definition := defaultFeatureFlags[key]
		enabled := definition.Default
		updatedAt := time.Time{}

		var configItem models.AppConfig
		err := config.DB.Where("key = ?", configKeyForFeatureFlag(key)).First(&configItem).Error
		if err == nil {
			parsed, parseErr := strconv.ParseBool(configItem.Value)
			if parseErr == nil {
				enabled = parsed
			}
			updatedAt = configItem.CreatedAt
			setCachedConfigValue(configKeyForFeatureFlag(key), configItem.Value)
		}

		result = append(result, FeatureFlagState{
			Key:         key,
			Label:       definition.Label,
			Description: definition.Description,
			Enabled:     enabled,
			UpdatedAt:   updatedAt,
		})
	}

	return result, nil
}

func SetFeatureFlag(flagKey string, enabled bool) (FeatureFlagState, error) {
	definition, ok := defaultFeatureFlags[flagKey]
	if !ok {
		return FeatureFlagState{}, errors.New("unknown feature flag")
	}

	value := strconv.FormatBool(enabled)
	if err := SetAppConfigValue(configKeyForFeatureFlag(flagKey), value); err != nil {
		return FeatureFlagState{}, err
	}

	return FeatureFlagState{
		Key:         flagKey,
		Label:       definition.Label,
		Description: definition.Description,
		Enabled:     enabled,
		UpdatedAt:   time.Now(),
	}, nil
}

func GetMaintenanceModeConfig() (MaintenanceModeConfig, error) {
	stored, err := GetAppConfigValue(maintenanceModeConfigKey)
	if err != nil {
		return MaintenanceModeConfig{
			Enabled:             false,
			Message:             "ระบบอยู่ระหว่างปรับปรุง กรุณาลองใหม่ภายหลัง",
			WhitelistAdminUsers: []uint{},
			UpdatedAt:           time.Time{},
		}, nil
	}

	var cfg MaintenanceModeConfig
	if unmarshalErr := json.Unmarshal([]byte(stored), &cfg); unmarshalErr != nil {
		return MaintenanceModeConfig{}, unmarshalErr
	}
	return cfg, nil
}

func SetMaintenanceModeConfig(next MaintenanceModeConfig) (MaintenanceModeConfig, error) {
	scheduleType := next.ScheduleType
	if scheduleType != "scheduled" {
		scheduleType = "indefinite"
	}
	normalized := MaintenanceModeConfig{
		Enabled:             next.Enabled,
		ScheduleType:        scheduleType,
		Message:             strings.TrimSpace(next.Message),
		StartTime:           next.StartTime,
		EndTime:             next.EndTime,
		WhitelistAdminUsers: next.WhitelistAdminUsers,
		UpdatedBy:           next.UpdatedBy,
		UpdatedAt:           time.Now(),
	}
	if normalized.Message == "" {
		normalized.Message = "ระบบอยู่ระหว่างปรับปรุง กรุณาลองใหม่ภายหลัง"
	}
	if normalized.WhitelistAdminUsers == nil {
		normalized.WhitelistAdminUsers = []uint{}
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return MaintenanceModeConfig{}, err
	}
	if err := SetAppConfigValue(maintenanceModeConfigKey, string(raw)); err != nil {
		return MaintenanceModeConfig{}, err
	}
	invalidateCachedConfigValue(maintenanceModeConfigKey)
	return normalized, nil
}

func defaultStudentPrograms() []StudentProgramConfig {
	return []StudentProgramConfig{
		{ShortName: "SC-IT", FullName: "SC-IT"},
		{ShortName: "SC-CS", FullName: "SC-CS"},
		{ShortName: "CP-Cy", FullName: "CP-Cy"},
		{ShortName: "CP-AI", FullName: "CP-AI"},
		{ShortName: "SC-GIS", FullName: "SC-GIS"},
	}
}

func normalizeStudentPrograms(programs []StudentProgramConfig) ([]StudentProgramConfig, error) {
	normalized := make([]StudentProgramConfig, 0, len(programs))
	seenShortNames := make(map[string]struct{}, len(programs))

	for _, program := range programs {
		shortName := strings.TrimSpace(program.ShortName)
		fullName := strings.TrimSpace(program.FullName)

		if shortName == "" || fullName == "" {
			return nil, errors.New("short_name and full_name are required")
		}

		key := strings.ToLower(shortName)
		if _, exists := seenShortNames[key]; exists {
			return nil, errors.New("duplicate short_name is not allowed")
		}
		seenShortNames[key] = struct{}{}

		normalized = append(normalized, StudentProgramConfig{
			ShortName:         shortName,
			FullName:          fullName,
			OriginalShortName: strings.TrimSpace(program.OriginalShortName),
		})
	}

	if len(normalized) == 0 {
		return nil, errors.New("at least one program is required")
	}

	return normalized, nil
}

func GetStudentPrograms() ([]StudentProgramConfig, error) {
	stored, err := GetAppConfigValue(studentProgramsConfigKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultStudentPrograms(), nil
		}
		return nil, err
	}

	var programs []StudentProgramConfig
	if unmarshalErr := json.Unmarshal([]byte(stored), &programs); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	normalized, normalizeErr := normalizeStudentPrograms(programs)
	if normalizeErr != nil {
		return nil, normalizeErr
	}

	return normalized, nil
}

func SetStudentPrograms(programs []StudentProgramConfig) ([]StudentProgramConfig, error) {
	normalized, err := normalizeStudentPrograms(programs)
	if err != nil {
		return nil, err
	}

	renamedShortNames := make(map[string]string)
	for _, program := range normalized {
		originalShortName := strings.TrimSpace(program.OriginalShortName)
		if originalShortName == "" || strings.EqualFold(originalShortName, program.ShortName) {
			continue
		}
		renamedShortNames[originalShortName] = program.ShortName
	}

	raw, marshalErr := json.Marshal(normalized)
	if marshalErr != nil {
		return nil, marshalErr
	}

	if err := SetAppConfigValue(studentProgramsConfigKey, string(raw)); err != nil {
		return nil, err
	}
	invalidateCachedConfigValue(studentProgramsConfigKey)

	for oldShortName, newShortName := range renamedShortNames {
		if execErr := config.DB.Exec(
			`UPDATE students
			 SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{program}', to_jsonb(?::text), true)
			 WHERE extra->>'program' = ?`,
			newShortName,
			oldShortName,
		).Error; execErr != nil {
			return nil, execErr
		}
	}

	return normalized, nil
}

// IsMaintenanceActive returns true if maintenance mode is currently active,
// taking into account the schedule type and time window.
func IsMaintenanceActive(cfg MaintenanceModeConfig) bool {
	if cfg.ScheduleType == "scheduled" {
		now := time.Now().UTC()
		if cfg.StartTime != nil && cfg.EndTime != nil {
			return !now.Before(*cfg.StartTime) && !now.After(*cfg.EndTime)
		}
		return false
	}
	return cfg.Enabled
}

func GetDatabaseBackupRecords(limit int) ([]models.DatabaseBackupRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	backups := make([]models.DatabaseBackupRecord, 0)
	err := config.DB.Where("deleted_at IS NULL").Order("created_at desc").Limit(limit).Find(&backups).Error
	if err != nil {
		return nil, err
	}
	return backups, nil
}

func GetDatabaseBackupRecordByID(id uint) (models.DatabaseBackupRecord, error) {
	var record models.DatabaseBackupRecord
	err := config.DB.Where("id = ? AND deleted_at IS NULL", id).First(&record).Error
	if err != nil {
		return models.DatabaseBackupRecord{}, err
	}
	return record, nil
}

func CreateDatabaseBackupRecord(record models.DatabaseBackupRecord) (models.DatabaseBackupRecord, error) {
	if err := config.DB.Create(&record).Error; err != nil {
		return models.DatabaseBackupRecord{}, err
	}
	return record, nil
}

func ListAnnouncements(filter AnnouncementListFilter) ([]AnnouncementWithAck, error) {
	rows := make([]models.SystemAnnouncement, 0)
	query := config.DB.Model(&models.SystemAnnouncement{})

	switch normalizeAnnouncementStatusFilter(filter.Status) {
	case announcementStatusDraft:
		query = query.Where("status = ?", announcementStatusDraft)
	case announcementStatusScheduled:
		query = query.Where("status = ?", announcementStatusScheduled)
	case announcementStatusPublished:
		query = query.Where("status = ?", announcementStatusPublished)
	case announcementStatusArchived:
		query = query.Where("status = ?", announcementStatusArchived)
	case "live":
		query = query.Where("status IN ?", []string{announcementStatusPublished, announcementStatusScheduled})
	}

	if severity := normalizeAnnouncementSeverityFilter(filter.Severity); severity != "" {
		query = query.Where("severity = ?", severity)
	}

	if !filter.IncludeExpired {
		query = query.Where("expires_at IS NULL OR expires_at > ?", time.Now())
	}

	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where(
			"LOWER(title) LIKE ? OR LOWER(title_th) LIKE ? OR LOWER(title_en) LIKE ? OR LOWER(message) LIKE ? OR LOWER(message_th) LIKE ? OR LOWER(message_en) LIKE ?",
			like, like, like, like, like, like,
		)
	}

	query = query.Order("priority DESC").Order("COALESCE(scheduled_at, created_at) DESC")

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return []AnnouncementWithAck{}, nil
	}

	announcementIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		announcementIDs = append(announcementIDs, row.ID)
	}

	// One grouped count each, instead of the two-queries-per-row this used to
	// run. The admin list loads every announcement at once, so the old shape
	// meant the screen got slower with every announcement ever written.
	ackCounts, err := countByAnnouncement(&models.SystemAnnouncementAck{}, announcementIDs)
	if err != nil {
		return nil, err
	}
	dismissCounts, err := countByAnnouncement(&models.SystemAnnouncementDismissal{}, announcementIDs)
	if err != nil {
		return nil, err
	}

	audienceCache := make(map[string]int64)
	result := make([]AnnouncementWithAck, 0, len(rows))
	for _, row := range rows {
		roles := decodeAnnouncementAudience(row.Audience)
		cacheKey := strings.Join(roles, ",")
		audienceCount, cached := audienceCache[cacheKey]
		if !cached {
			audienceCount, err = CountAnnouncementAudience(roles)
			if err != nil {
				return nil, err
			}
			audienceCache[cacheKey] = audienceCount
		}

		result = append(result, AnnouncementWithAck{
			SystemAnnouncement: row,
			AckCount:           ackCounts[row.ID],
			DismissCount:       dismissCounts[row.ID],
			AudienceCount:      audienceCount,
		})
	}

	return result, nil
}

func countByAnnouncement(model any, announcementIDs []uint) (map[uint]int64, error) {
	type countRow struct {
		AnnouncementID uint
		Total          int64
	}

	counts := make([]countRow, 0, len(announcementIDs))
	err := config.DB.Model(model).
		Select("announcement_id, COUNT(*) AS total").
		Where("announcement_id IN ?", announcementIDs).
		Group("announcement_id").
		Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]int64, len(counts))
	for _, row := range counts {
		result[row.AnnouncementID] = row.Total
	}
	return result, nil
}

func decodeAnnouncementAudience(raw []byte) []string {
	stored := make([]string, 0, 4)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &stored)
	}

	// Lower-cased here so every caller can compare against "all" and against
	// the users table's role values without repeating the normalisation.
	roles := make([]string, 0, len(stored))
	for _, role := range stored {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "" {
			roles = append(roles, role)
		}
	}

	if len(roles) == 0 {
		return []string{"all"}
	}
	return roles
}

// CountAnnouncementAudience counts the active users an announcement addressed
// to these roles reaches, so the admin list can show acknowledgements as a
// share of the audience rather than a bare number with nothing to compare it
// against.
func CountAnnouncementAudience(roles []string) (int64, error) {
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			continue
		}
		if role == "all" {
			var total int64
			err := config.DB.Model(&models.User{}).Where("is_active = ?", true).Count(&total).Error
			return total, err
		}
		normalized = append(normalized, role)
	}

	if len(normalized) == 0 {
		var total int64
		err := config.DB.Model(&models.User{}).Where("is_active = ?", true).Count(&total).Error
		return total, err
	}

	var total int64
	err := config.DB.Model(&models.User{}).
		Where("is_active = ? AND LOWER(role) IN ?", true, normalized).
		Count(&total).Error
	return total, err
}

func ListActiveAnnouncementsForUser(userID uint, studentID uint, role string) ([]ActiveAnnouncementForUser, error) {
	normalizedRole := strings.ToLower(strings.TrimSpace(role))
	if normalizedRole == "" {
		normalizedRole = "student"
	}

	now := time.Now()
	allAudienceRaw, _ := json.Marshal([]string{"all"})
	roleAudienceRaw, _ := json.Marshal([]string{normalizedRole})

	rows := make([]models.SystemAnnouncement, 0)
	query := config.DB.Model(&models.SystemAnnouncement{}).
		Where("is_active = ?", true).
		Where("status IN ?", []string{announcementStatusPublished, announcementStatusScheduled}).
		Where("scheduled_at IS NULL OR scheduled_at <= ?", now).
		Where("expires_at IS NULL OR expires_at > ?", now).
		Where("audience @> ?::jsonb OR audience @> ?::jsonb", string(allAudienceRaw), string(roleAudienceRaw)).
		// Urgent first, then whatever the admin pinned, then newest. The
		// display layer walks this order straight down the page, so the order
		// chosen here is the order people read.
		Order("CASE severity WHEN 'urgent' THEN 0 WHEN 'warning' THEN 1 WHEN 'success' THEN 2 ELSE 3 END").
		Order("priority DESC").
		// Both fullscreen shapes block the page, so they lead; the topbar
		// ribbon is the least intrusive and sorts last.
		Order("CASE display_mode WHEN 'fullscreen' THEN 0 WHEN 'fullscreen_image' THEN 0 WHEN 'banner_top' THEN 1 WHEN 'corner_card' THEN 2 ELSE 3 END").
		Order("COALESCE(scheduled_at, created_at) DESC")

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	ackRows := make([]models.SystemAnnouncementAck, 0)
	dismissRows := make([]models.SystemAnnouncementDismissal, 0)
	if len(rows) > 0 {
		announcementIDs := make([]uint, 0, len(rows))
		for _, row := range rows {
			announcementIDs = append(announcementIDs, row.ID)
		}

		scopeToActor := func(query *gorm.DB) *gorm.DB {
			switch {
			case userID > 0:
				return query.Where("user_id = ?", userID)
			case studentID > 0:
				return query.Where("student_id = ?", studentID)
			default:
				return nil
			}
		}

		if ackQuery := scopeToActor(config.DB.Where("announcement_id IN ?", announcementIDs)); ackQuery != nil {
			if err := ackQuery.Find(&ackRows).Error; err != nil {
				return nil, err
			}
		}
		if dismissQuery := scopeToActor(config.DB.Where("announcement_id IN ?", announcementIDs)); dismissQuery != nil {
			if err := dismissQuery.Find(&dismissRows).Error; err != nil {
				return nil, err
			}
		}
	}

	ackSet := make(map[uint]struct{}, len(ackRows))
	for _, ack := range ackRows {
		ackSet[ack.AnnouncementID] = struct{}{}
	}
	dismissedSet := make(map[uint]struct{}, len(dismissRows))
	for _, dismissal := range dismissRows {
		dismissedSet[dismissal.AnnouncementID] = struct{}{}
	}

	result := make([]ActiveAnnouncementForUser, 0, len(rows))
	for _, row := range rows {
		// A dismissal only counts for announcements that can be dismissed and
		// do not demand an acknowledgement; anything else stays on screen no
		// matter what the viewer clicked before.
		if _, dismissed := dismissedSet[row.ID]; dismissed && row.IsDismissible && !row.RequireAcknowledge {
			continue
		}

		_, acknowledged := ackSet[row.ID]
		result = append(result, ActiveAnnouncementForUser{
			SystemAnnouncement: row,
			IsAcknowledged:     acknowledged,
		})
	}

	return result, nil
}

func normalizeAnnouncementContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case announcementContentImage:
		return announcementContentImage
	case announcementContentMixed:
		return announcementContentMixed
	default:
		return announcementContentText
	}
}

func normalizeAnnouncementDisplayMode(displayMode string) string {
	switch strings.ToLower(strings.TrimSpace(displayMode)) {
	case announcementModeFull:
		return announcementModeFull
	case announcementModeTopbar:
		return announcementModeTopbar
	case announcementModeFullImage:
		return announcementModeFullImage
	case announcementModeCorner:
		return announcementModeCorner
	default:
		return announcementModeBanner
	}
}

func normalizeAnnouncementSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case announcementSeveritySuccess:
		return announcementSeveritySuccess
	case announcementSeverityWarning:
		return announcementSeverityWarning
	case announcementSeverityUrgent:
		return announcementSeverityUrgent
	default:
		return announcementSeverityInfo
	}
}

func normalizeAnnouncementSeverityFilter(severity string) string {
	trimmed := strings.ToLower(strings.TrimSpace(severity))
	if trimmed == "" || trimmed == "all" {
		return ""
	}
	return normalizeAnnouncementSeverity(trimmed)
}

// normalizeAnnouncementStatusFilter maps what the list screen can ask for.
// "live" covers published plus scheduled, which is what an admin means by
// "currently in use"; an unrecognised value means no status filter at all.
func normalizeAnnouncementStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case announcementStatusDraft:
		return announcementStatusDraft
	case announcementStatusScheduled:
		return announcementStatusScheduled
	case announcementStatusPublished:
		return announcementStatusPublished
	case announcementStatusArchived:
		return announcementStatusArchived
	case "live", "active":
		return "live"
	default:
		return ""
	}
}

// resolveAnnouncementStatus decides the stored state. An explicit status wins;
// otherwise it is derived from the legacy is_active flag so an older client
// that only sends is_active keeps behaving the way it always did. A published
// announcement with a future start date is stored as scheduled, which is what
// the list screen shows and what makes "what is live right now" answerable.
func resolveAnnouncementStatus(status string, isActive bool, scheduledAt *time.Time) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case announcementStatusDraft, announcementStatusArchived:
		return normalized
	case announcementStatusScheduled, announcementStatusPublished:
		if scheduledAt != nil && scheduledAt.After(time.Now()) {
			return announcementStatusScheduled
		}
		return announcementStatusPublished
	}

	if !isActive {
		return announcementStatusArchived
	}
	if scheduledAt != nil && scheduledAt.After(time.Now()) {
		return announcementStatusScheduled
	}
	return announcementStatusPublished
}

// announcementStatusIsLive reports whether a status should keep is_active set,
// which is the flag every older reader of this table still checks.
func announcementStatusIsLive(status string) bool {
	return status == announcementStatusPublished || status == announcementStatusScheduled
}

func normalizeAnnouncementPriority(priority int) int {
	if priority < 0 {
		return 0
	}
	if priority > 100 {
		return 100
	}
	return priority
}

func normalizeAnnouncementDisplayPaths(displayPaths []string) []string {
	if len(displayPaths) == 0 {
		return []string{announcementAllPages}
	}

	unique := make(map[string]struct{}, len(displayPaths))
	normalized := make([]string, 0, len(displayPaths))
	for _, pathRule := range displayPaths {
		rule := strings.ToLower(strings.TrimSpace(pathRule))
		if rule == "" {
			continue
		}
		if _, exists := unique[rule]; exists {
			continue
		}
		unique[rule] = struct{}{}
		normalized = append(normalized, rule)
	}

	if len(normalized) == 0 {
		return []string{announcementAllPages}
	}

	for _, rule := range normalized {
		if rule == announcementAllPages {
			return []string{announcementAllPages}
		}
	}

	return normalized
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func validateAnnouncementInput(input AnnouncementInput) error {
	if firstNonEmpty(input.Title, input.TitleTH, input.TitleEN) == "" {
		return errors.New("title is required")
	}

	contentType := normalizeAnnouncementContentType(input.ContentType)
	message := firstNonEmpty(input.Message, input.MessageTH, input.MessageEN)
	imageURL := strings.TrimSpace(input.ImageURL)

	switch contentType {
	case announcementContentText:
		if message == "" {
			return errors.New("message is required for text announcements")
		}
	case announcementContentImage:
		if imageURL == "" {
			return errors.New("image_url is required for image announcements")
		}
	case announcementContentMixed:
		if message == "" && imageURL == "" {
			return errors.New("message or image_url is required for mixed announcements")
		}
	}

	if input.ExpiresAt != nil && input.ScheduledAt != nil && input.ExpiresAt.Before(*input.ScheduledAt) {
		return errors.New("expires_at must be later than scheduled_at")
	}

	return nil
}

func CreateAnnouncement(input AnnouncementInput, createdBy uint) (*models.SystemAnnouncement, error) {
	if err := validateAnnouncementInput(input); err != nil {
		return nil, err
	}

	audience := input.Audience
	if len(audience) == 0 {
		audience = []string{"all"}
	}
	rawAudience, err := json.Marshal(audience)
	if err != nil {
		return nil, err
	}
	rawDisplayPaths, err := json.Marshal(normalizeAnnouncementDisplayPaths(input.DisplayPaths))
	if err != nil {
		return nil, err
	}

	status := resolveAnnouncementStatus(input.Status, input.IsActive, input.ScheduledAt)

	announcement := models.SystemAnnouncement{
		Title:              firstNonEmpty(input.Title, input.TitleTH, input.TitleEN),
		TitleTH:            strings.TrimSpace(input.TitleTH),
		TitleEN:            strings.TrimSpace(input.TitleEN),
		Message:            firstNonEmpty(input.Message, input.MessageTH, input.MessageEN),
		MessageTH:          strings.TrimSpace(input.MessageTH),
		MessageEN:          strings.TrimSpace(input.MessageEN),
		ContentType:        normalizeAnnouncementContentType(input.ContentType),
		DisplayMode:        normalizeAnnouncementDisplayMode(input.DisplayMode),
		ImageURL:           strings.TrimSpace(input.ImageURL),
		ActionLabel:        firstNonEmpty(input.ActionLabel, input.ActionLabelTH, input.ActionLabelEN),
		ActionLabelTH:      strings.TrimSpace(input.ActionLabelTH),
		ActionLabelEN:      strings.TrimSpace(input.ActionLabelEN),
		ActionURL:          strings.TrimSpace(input.ActionURL),
		IsDismissible:      input.IsDismissible,
		DisplayPaths:       rawDisplayPaths,
		ScheduledAt:        input.ScheduledAt,
		ExpiresAt:          input.ExpiresAt,
		Audience:           rawAudience,
		RequireAcknowledge: input.RequireAcknowledge,
		Severity:           normalizeAnnouncementSeverity(input.Severity),
		Priority:           normalizeAnnouncementPriority(input.Priority),
		Status:             status,
		NotifyInbox:        input.NotifyInbox,
		IsActive:           announcementStatusIsLive(status),
		CreatedBy:          &createdBy,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	// Select is what makes the false values stick — see
	// announcementWritableColumns.
	if err := config.DB.Select(announcementWritableColumns).Create(&announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
}

// CreateAnnouncementsBatch writes several announcements in one transaction so a
// set that belongs together (the same notice in several variants, a run of
// term-start notices) either all lands or none of it does. It returns the
// created rows in input order.
func CreateAnnouncementsBatch(inputs []AnnouncementInput, createdBy uint) ([]models.SystemAnnouncement, error) {
	if len(inputs) == 0 {
		return nil, errors.New("no announcements to create")
	}
	if len(inputs) > AnnouncementBatchLimit {
		return nil, fmt.Errorf("batch is limited to %d announcements", AnnouncementBatchLimit)
	}

	for index, input := range inputs {
		if err := validateAnnouncementInput(input); err != nil {
			return nil, fmt.Errorf("announcement %d: %w", index+1, err)
		}
	}

	created := make([]models.SystemAnnouncement, 0, len(inputs))
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			row, buildErr := buildAnnouncementForCreate(input, createdBy)
			if buildErr != nil {
				return buildErr
			}
			if createErr := tx.Select(announcementWritableColumns).Create(row).Error; createErr != nil {
				return createErr
			}
			created = append(created, *row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

func buildAnnouncementForCreate(input AnnouncementInput, createdBy uint) (*models.SystemAnnouncement, error) {
	audience := input.Audience
	if len(audience) == 0 {
		audience = []string{"all"}
	}
	rawAudience, err := json.Marshal(audience)
	if err != nil {
		return nil, err
	}
	rawDisplayPaths, err := json.Marshal(normalizeAnnouncementDisplayPaths(input.DisplayPaths))
	if err != nil {
		return nil, err
	}

	status := resolveAnnouncementStatus(input.Status, input.IsActive, input.ScheduledAt)
	now := time.Now()

	return &models.SystemAnnouncement{
		Title:              firstNonEmpty(input.Title, input.TitleTH, input.TitleEN),
		TitleTH:            strings.TrimSpace(input.TitleTH),
		TitleEN:            strings.TrimSpace(input.TitleEN),
		Message:            firstNonEmpty(input.Message, input.MessageTH, input.MessageEN),
		MessageTH:          strings.TrimSpace(input.MessageTH),
		MessageEN:          strings.TrimSpace(input.MessageEN),
		ContentType:        normalizeAnnouncementContentType(input.ContentType),
		DisplayMode:        normalizeAnnouncementDisplayMode(input.DisplayMode),
		ImageURL:           strings.TrimSpace(input.ImageURL),
		ActionLabel:        firstNonEmpty(input.ActionLabel, input.ActionLabelTH, input.ActionLabelEN),
		ActionLabelTH:      strings.TrimSpace(input.ActionLabelTH),
		ActionLabelEN:      strings.TrimSpace(input.ActionLabelEN),
		ActionURL:          strings.TrimSpace(input.ActionURL),
		IsDismissible:      input.IsDismissible,
		DisplayPaths:       rawDisplayPaths,
		ScheduledAt:        input.ScheduledAt,
		ExpiresAt:          input.ExpiresAt,
		Audience:           rawAudience,
		RequireAcknowledge: input.RequireAcknowledge,
		Severity:           normalizeAnnouncementSeverity(input.Severity),
		Priority:           normalizeAnnouncementPriority(input.Priority),
		Status:             status,
		NotifyInbox:        input.NotifyInbox,
		IsActive:           announcementStatusIsLive(status),
		CreatedBy:          &createdBy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func UpdateAnnouncement(id uint, input AnnouncementInput) (*models.SystemAnnouncement, error) {
	if err := validateAnnouncementInput(input); err != nil {
		return nil, err
	}

	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ?", id).First(&announcement).Error; err != nil {
		return nil, err
	}

	audience := input.Audience
	if len(audience) == 0 {
		audience = []string{"all"}
	}
	rawAudience, err := json.Marshal(audience)
	if err != nil {
		return nil, err
	}
	rawDisplayPaths, err := json.Marshal(normalizeAnnouncementDisplayPaths(input.DisplayPaths))
	if err != nil {
		return nil, err
	}

	announcement.Title = firstNonEmpty(input.Title, input.TitleTH, input.TitleEN)
	announcement.TitleTH = strings.TrimSpace(input.TitleTH)
	announcement.TitleEN = strings.TrimSpace(input.TitleEN)
	announcement.Message = firstNonEmpty(input.Message, input.MessageTH, input.MessageEN)
	announcement.MessageTH = strings.TrimSpace(input.MessageTH)
	announcement.MessageEN = strings.TrimSpace(input.MessageEN)
	announcement.ContentType = normalizeAnnouncementContentType(input.ContentType)
	announcement.DisplayMode = normalizeAnnouncementDisplayMode(input.DisplayMode)
	announcement.ImageURL = strings.TrimSpace(input.ImageURL)
	announcement.ActionLabel = firstNonEmpty(input.ActionLabel, input.ActionLabelTH, input.ActionLabelEN)
	announcement.ActionLabelTH = strings.TrimSpace(input.ActionLabelTH)
	announcement.ActionLabelEN = strings.TrimSpace(input.ActionLabelEN)
	announcement.ActionURL = strings.TrimSpace(input.ActionURL)
	announcement.IsDismissible = input.IsDismissible
	announcement.DisplayPaths = rawDisplayPaths
	announcement.ScheduledAt = input.ScheduledAt
	announcement.ExpiresAt = input.ExpiresAt
	announcement.Audience = rawAudience
	announcement.RequireAcknowledge = input.RequireAcknowledge
	announcement.Severity = normalizeAnnouncementSeverity(input.Severity)
	announcement.Priority = normalizeAnnouncementPriority(input.Priority)
	announcement.Status = resolveAnnouncementStatus(input.Status, input.IsActive, input.ScheduledAt)
	announcement.NotifyInbox = input.NotifyInbox
	announcement.IsActive = announcementStatusIsLive(announcement.Status)
	// Select names updated_at explicitly, which stops GORM's autoUpdateTime
	// from filling it in, so set it here.
	announcement.UpdatedAt = time.Now()

	// Save skips no columns, but it also skips nothing it should keep: Select
	// keeps the false booleans in the UPDATE for the same reason Create needs
	// it.
	if err := config.DB.Model(&announcement).Select(announcementWritableColumns).Updates(announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
}

// SetAnnouncementStatus moves one announcement between editorial states
// without touching its content, which is what the list screen's archive and
// republish buttons need.
func SetAnnouncementStatus(id uint, status string) (*models.SystemAnnouncement, error) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case announcementStatusDraft, announcementStatusScheduled, announcementStatusPublished, announcementStatusArchived:
	default:
		return nil, errors.New("invalid announcement status")
	}

	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ?", id).First(&announcement).Error; err != nil {
		return nil, err
	}

	resolved := resolveAnnouncementStatus(normalized, announcementStatusIsLive(normalized), announcement.ScheduledAt)
	updates := map[string]any{
		"status":     resolved,
		"is_active":  announcementStatusIsLive(resolved),
		"updated_at": time.Now(),
	}
	if err := config.DB.Model(&announcement).Updates(updates).Error; err != nil {
		return nil, err
	}

	announcement.Status = resolved
	announcement.IsActive = announcementStatusIsLive(resolved)
	return &announcement, nil
}

// DeleteAnnouncement removes an announcement and everything recorded about it.
// The table has no soft-delete column, so this is a real delete; archiving via
// SetAnnouncementStatus is the reversible option and is what the UI offers
// first.
func DeleteAnnouncement(id uint) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("announcement_id = ?", id).Delete(&models.SystemAnnouncementAck{}).Error; err != nil {
			return err
		}
		if err := tx.Where("announcement_id = ?", id).Delete(&models.SystemAnnouncementDismissal{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ?", id).Delete(&models.SystemAnnouncement{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// GetAnnouncementByID returns one announcement, whatever its status.
func GetAnnouncementByID(id uint) (*models.SystemAnnouncement, error) {
	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ?", id).First(&announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
}

// GetAnnouncementStats answers "who has actually seen this", which the old
// bare acknowledgement count could not: it had no denominator and no way to
// find the people still outstanding.
func GetAnnouncementStats(id uint) (*AnnouncementStats, error) {
	announcement, err := GetAnnouncementByID(id)
	if err != nil {
		return nil, err
	}

	roles := decodeAnnouncementAudience(announcement.Audience)
	audienceCount, err := CountAnnouncementAudience(roles)
	if err != nil {
		return nil, err
	}

	var dismissCount int64
	if err := config.DB.Model(&models.SystemAnnouncementDismissal{}).
		Where("announcement_id = ?", id).Count(&dismissCount).Error; err != nil {
		return nil, err
	}

	type recipientRow struct {
		UserID         uint
		FullName       string
		Email          string
		Role           string
		AcknowledgedAt *time.Time
	}

	query := config.DB.Model(&models.User{}).
		Select("users.id AS user_id, users.full_name, users.email, users.role, acks.acknowledged_at").
		Joins("LEFT JOIN system_announcement_acks AS acks ON acks.user_id = users.id AND acks.announcement_id = ?", id).
		Where("users.is_active = ?", true)

	if !slices.Contains(roles, "all") {
		lowered := make([]string, 0, len(roles))
		for _, role := range roles {
			lowered = append(lowered, strings.ToLower(strings.TrimSpace(role)))
		}
		query = query.Where("LOWER(users.role) IN ?", lowered)
	}

	rows := make([]recipientRow, 0)
	if err := query.Order("users.full_name ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}

	stats := &AnnouncementStats{
		AnnouncementID: id,
		AudienceCount:  audienceCount,
		DismissCount:   dismissCount,
		Pending:        make([]AnnouncementRecipient, 0),
		Acknowledged:   make([]AnnouncementRecipient, 0),
	}

	for _, row := range rows {
		recipient := AnnouncementRecipient{
			UserID:         row.UserID,
			FullName:       row.FullName,
			Email:          row.Email,
			Role:           row.Role,
			AcknowledgedAt: row.AcknowledgedAt,
		}
		if row.AcknowledgedAt != nil {
			stats.Acknowledged = append(stats.Acknowledged, recipient)
			continue
		}
		stats.Pending = append(stats.Pending, recipient)
	}

	stats.AckCount = int64(len(stats.Acknowledged))
	if stats.AudienceCount > 0 {
		stats.AckPercent = float64(stats.AckCount) * 100 / float64(stats.AudienceCount)
	}

	return stats, nil
}

// DismissAnnouncement records that this viewer closed the announcement. It
// used to live in the browser's localStorage, which meant a dismissal did not
// follow the person to their phone and came undone whenever site data was
// cleared.
func DismissAnnouncement(announcementID uint, userID uint, studentID uint) error {
	if userID == 0 && studentID == 0 {
		return errors.New("announcement dismissal actor is required")
	}

	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ?", announcementID).First(&announcement).Error; err != nil {
		return err
	}
	if !announcement.IsDismissible || announcement.RequireAcknowledge {
		return errors.New("announcement cannot be dismissed")
	}

	dismissal := models.SystemAnnouncementDismissal{
		AnnouncementID: announcementID,
		UserID:         userID,
		DismissedAt:    time.Now(),
		CreatedAt:      time.Now(),
	}
	if studentID > 0 {
		dismissal.StudentID = &studentID
	}

	if err := config.DB.Create(&dismissal).Error; err != nil {
		// The unique indexes turn a double click into a duplicate-key error
		// rather than a second row. That is the outcome this function wants, so
		// it is not worth surfacing.
		if isDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

// isDuplicateKeyError reports whether the write lost a race to an identical one
// that already landed.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}

func AcknowledgeAnnouncement(announcementID uint, userID uint, studentID uint) error {
	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ? AND is_active = ?", announcementID, true).First(&announcement).Error; err != nil {
		return err
	}

	var existing models.SystemAnnouncementAck
	lookup := config.DB.Where("announcement_id = ?", announcementID)
	switch {
	case userID > 0:
		lookup = lookup.Where("user_id = ?", userID)
	case studentID > 0:
		lookup = lookup.Where("student_id = ?", studentID)
	default:
		return errors.New("announcement acknowledgement actor is required")
	}
	if err := lookup.First(&existing).Error; err == nil {
		return nil
	}

	ack := models.SystemAnnouncementAck{
		AnnouncementID: announcementID,
		UserID:         userID,
		AcknowledgedAt: time.Now(),
	}
	if studentID > 0 {
		ack.StudentID = &studentID
	}
	if err := config.DB.Create(&ack).Error; err != nil {
		// Two clicks landing together both miss the lookup above; the unique
		// index catches the loser, and an already-recorded acknowledgement is
		// exactly what the caller asked for.
		if isDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}
