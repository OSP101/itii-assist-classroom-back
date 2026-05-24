package repositories

import (
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	maintenanceModeConfigKey = "system.maintenance_mode"
	featureFlagConfigPrefix  = "feature_flag."
	announcementContentText  = "text"
	announcementContentImage = "image"
	announcementContentMixed = "mixed"
	announcementModeBanner   = "banner_top"
	announcementModeFull     = "fullscreen"
	announcementAllPages     = "all_pages"
)

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
}

type AnnouncementWithAck struct {
	models.SystemAnnouncement
	AckCount int64 `json:"ack_count"`
}

type ActiveAnnouncementForUser struct {
	models.SystemAnnouncement
	IsAcknowledged bool `json:"is_acknowledged"`
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

func ListAnnouncements(includeExpired bool) ([]AnnouncementWithAck, error) {
	rows := make([]models.SystemAnnouncement, 0)
	query := config.DB.Model(&models.SystemAnnouncement{}).Where("is_active = ?", true)
	if !includeExpired {
		query = query.Where("expires_at IS NULL OR expires_at > ?", time.Now())
	}
	query = query.Order("COALESCE(scheduled_at, created_at) DESC")

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]AnnouncementWithAck, 0, len(rows))
	for _, row := range rows {
		count := int64(0)
		if err := config.DB.Model(&models.SystemAnnouncementAck{}).Where("announcement_id = ?", row.ID).Count(&count).Error; err != nil {
			return nil, err
		}
		result = append(result, AnnouncementWithAck{
			SystemAnnouncement: row,
			AckCount:           count,
		})
	}

	return result, nil
}

func ListActiveAnnouncementsForUser(userID uint, role string) ([]ActiveAnnouncementForUser, error) {
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
		Where("scheduled_at IS NULL OR scheduled_at <= ?", now).
		Where("expires_at IS NULL OR expires_at > ?", now).
		Where("audience @> ?::jsonb OR audience @> ?::jsonb", string(allAudienceRaw), string(roleAudienceRaw)).
		Order("CASE WHEN display_mode = 'fullscreen' THEN 0 ELSE 1 END").
		Order("COALESCE(scheduled_at, created_at) DESC")

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	ackRows := make([]models.SystemAnnouncementAck, 0)
	if len(rows) > 0 {
		announcementIDs := make([]uint, 0, len(rows))
		for _, row := range rows {
			announcementIDs = append(announcementIDs, row.ID)
		}
		if err := config.DB.Where("user_id = ? AND announcement_id IN ?", userID, announcementIDs).Find(&ackRows).Error; err != nil {
			return nil, err
		}
	}

	ackSet := make(map[uint]struct{}, len(ackRows))
	for _, ack := range ackRows {
		ackSet[ack.AnnouncementID] = struct{}{}
	}

	result := make([]ActiveAnnouncementForUser, 0, len(rows))
	for _, row := range rows {
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
	default:
		return announcementModeBanner
	}
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
		IsActive:           input.IsActive,
		CreatedBy:          &createdBy,
	}
	if err := config.DB.Create(&announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
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
	announcement.IsActive = input.IsActive

	if err := config.DB.Save(&announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
}

func AcknowledgeAnnouncement(announcementID uint, userID uint) error {
	var announcement models.SystemAnnouncement
	if err := config.DB.Where("id = ? AND is_active = ?", announcementID, true).First(&announcement).Error; err != nil {
		return err
	}

	var existing models.SystemAnnouncementAck
	if err := config.DB.Where("announcement_id = ? AND user_id = ?", announcementID, userID).First(&existing).Error; err == nil {
		return nil
	}

	ack := models.SystemAnnouncementAck{
		AnnouncementID: announcementID,
		UserID:         userID,
		AcknowledgedAt: time.Now(),
	}
	return config.DB.Create(&ack).Error
}
