package handlers

import (
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

type announcementPayload struct {
	Title              string     `json:"title"`
	TitleTH            string     `json:"title_th"`
	TitleEN            string     `json:"title_en"`
	Message            string     `json:"message"`
	MessageTH          string     `json:"message_th"`
	MessageEN          string     `json:"message_en"`
	ContentType        string     `json:"content_type"`
	DisplayMode        string     `json:"display_mode"`
	ImageURL           string     `json:"image_url"`
	ActionLabel        string     `json:"action_label"`
	ActionLabelTH      string     `json:"action_label_th"`
	ActionLabelEN      string     `json:"action_label_en"`
	ActionURL          string     `json:"action_url"`
	IsDismissible      *bool      `json:"is_dismissible"`
	ScheduledAt        *time.Time `json:"scheduled_at"`
	ExpiresAt          *time.Time `json:"expires_at"`
	Audience           []string   `json:"audience"`
	DisplayPaths       []string   `json:"display_paths"`
	RequireAcknowledge bool       `json:"require_acknowledge"`
	IsActive           *bool      `json:"is_active"`
}

type featureFlagPayload struct {
	Enabled bool `json:"enabled"`
}

type maintenanceModePayload struct {
	Enabled             bool       `json:"enabled"`
	ScheduleType        string     `json:"schedule_type"`
	Message             string     `json:"message"`
	StartTime           *time.Time `json:"start_time"`
	EndTime             *time.Time `json:"end_time"`
	WhitelistAdminUsers []uint     `json:"whitelist_admin_users"`
}

type studentProgramsPayload struct {
	Programs []repositories.StudentProgramConfig `json:"programs"`
}

type runBackupNowPayload struct {
	Reason string `json:"reason"`
}

type restoreBackupPayload struct {
	BackupID    uint   `json:"backup_id"`
	ConfirmText string `json:"confirm_text"`
	Reason      string `json:"reason"`
}

func GetBackupRecordsHandler(c fiber.Ctx) error {
	limit := 20
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil {
			limit = parsed
		}
	}

	rows, err := repositories.GetDatabaseBackupRecords(limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงรายการ backup ได้"})
	}

	return c.JSON(fiber.Map{"success": true, "data": rows})
}

func GetBackupStatusHandler(c fiber.Ctx) error {
	status, err := services.GetBackupOperationStatus()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงสถานะ backup ได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": status})
}

func RunBackupNowHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload runBackupNowPayload
	if err := c.Bind().JSON(&payload); err != nil {
		payload = runBackupNowPayload{}
	}

	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "manual_backup_from_admin_settings"
	}

	record, err := services.RunDatabaseBackupNow(&actorID, reason)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "run_database_backup", "critical", "system_settings", strconv.FormatUint(uint64(record.ID), 10), fiber.Map{
		"storage_path": record.StoragePath,
		"storage_slot": record.StorageSlot,
		"reason":       reason,
	})

	return c.Status(201).JSON(fiber.Map{"success": true, "data": record})
}

func RestoreBackupHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload restoreBackupPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if payload.BackupID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "backup_id ต้องมากกว่า 0"})
	}
	if strings.TrimSpace(strings.ToUpper(payload.ConfirmText)) != "RESTORE" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "confirm_text ไม่ถูกต้อง"})
	}

	maintenanceCfg, err := repositories.GetMaintenanceModeConfig()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถตรวจสอบ maintenance mode ได้"})
	}
	if !repositories.IsMaintenanceActive(maintenanceCfg) {
		return c.Status(409).JSON(fiber.Map{"success": false, "message": "ต้องเปิด maintenance mode ก่อนทำการ restore"})
	}

	record, err := repositories.GetDatabaseBackupRecordByID(payload.BackupID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล backup ที่ต้องการ"})
	}

	preRestoreReason := fmt.Sprintf("auto_pre_restore_snapshot_target_%d", record.ID)
	if customReason := strings.TrimSpace(payload.Reason); customReason != "" {
		preRestoreReason = fmt.Sprintf("auto_pre_restore_snapshot_target_%d_%s", record.ID, customReason)
	}

	preRestoreRecord, err := services.RunDatabaseBackupNow(&actorID, preRestoreReason)
	if err != nil {
		logPrivilegedAdminAction(c, actorID, "restore_database_backup_pre_snapshot_failed", "critical", "system_settings", strconv.FormatUint(uint64(record.ID), 10), fiber.Map{
			"storage_path": record.StoragePath,
			"reason":       strings.TrimSpace(payload.Reason),
			"error":        err.Error(),
		})
		return c.Status(500).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("ไม่สามารถสำรองข้อมูลปัจจุบันก่อนกู้คืนได้: %s", err.Error())})
	}

	if err := services.RestoreDatabaseFromBackup(record); err != nil {
		logPrivilegedAdminAction(c, actorID, "restore_database_backup_failed", "critical", "system_settings", strconv.FormatUint(uint64(record.ID), 10), fiber.Map{
			"storage_path":          record.StoragePath,
			"pre_restore_backup_id": preRestoreRecord.ID,
			"pre_restore_storage":   preRestoreRecord.StoragePath,
			"reason":                strings.TrimSpace(payload.Reason),
			"error":                 err.Error(),
		})
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "restore_database_backup", "critical", "system_settings", strconv.FormatUint(uint64(record.ID), 10), fiber.Map{
		"storage_path":          record.StoragePath,
		"pre_restore_backup_id": preRestoreRecord.ID,
		"pre_restore_storage":   preRestoreRecord.StoragePath,
		"reason":                strings.TrimSpace(payload.Reason),
	})

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"restored_backup_id":    record.ID,
			"pre_restore_backup_id": preRestoreRecord.ID,
			"restored_at":           time.Now(),
		},
	})
}

func GetBackupDownloadURLHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	record, err := repositories.GetDatabaseBackupRecordByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล backup ที่ต้องการ"})
	}

	downloadURL, err := services.BuildBackupDownloadURL(record.StoragePath, 5*time.Minute)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "generate_backup_download_url", "warn", "system_settings", strconv.FormatUint(id, 10), fiber.Map{
		"storage_path": record.StoragePath,
	})

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"url": downloadURL}})
}

func ListAnnouncementsHandler(c fiber.Ctx) error {
	includeExpired := strings.EqualFold(c.Query("includeExpired"), "true")
	rows, err := repositories.ListAnnouncements(includeExpired)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงประกาศได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": rows})
}

func ListActiveAnnouncementsForCurrentUserHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	role, _ := middlewares.GetUserRole(c)
	rows, err := repositories.ListActiveAnnouncementsForUser(userID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงประกาศสำหรับผู้ใช้ได้"})
	}

	return c.JSON(fiber.Map{"success": true, "data": rows})
}

func UploadAnnouncementImageHandler(c fiber.Ctx) error {
	fileHeader, err := c.FormFile("image")
	if err != nil || fileHeader == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบไฟล์รูปภาพ"})
	}

	if fileHeader.Size <= 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไฟล์รูปภาพไม่ถูกต้อง"})
	}

	if fileHeader.Size > 8*1024*1024 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไฟล์มีขนาดใหญ่เกินไป สูงสุด 8MB"})
	}

	contentType := strings.ToLower(strings.TrimSpace(fileHeader.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "image/") {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รองรับเฉพาะไฟล์รูปภาพ"})
	}

	baseDir := filepath.Join("uploads", "system-announcements")
	if mkdirErr := os.MkdirAll(baseDir, 0o755); mkdirErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถสร้างโฟลเดอร์จัดเก็บรูปได้"})
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	fileName := fmt.Sprintf("announcement-%d%s", time.Now().UnixNano(), ext)
	filePath := filepath.Join(baseDir, fileName)

	if saveErr := c.SaveFile(fileHeader, filePath); saveErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถบันทึกรูปประกาศได้"})
	}

	publicPath := filepath.ToSlash(filepath.Join("/uploads", "system-announcements", fileName))
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"url": publicPath}})
}

func CreateAnnouncementHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload announcementPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	isActive := true
	if payload.IsActive != nil {
		isActive = *payload.IsActive
	}

	isDismissible := true
	if payload.IsDismissible != nil {
		isDismissible = *payload.IsDismissible
	}

	created, err := repositories.CreateAnnouncement(repositories.AnnouncementInput{
		Title:              payload.Title,
		TitleTH:            payload.TitleTH,
		TitleEN:            payload.TitleEN,
		Message:            payload.Message,
		MessageTH:          payload.MessageTH,
		MessageEN:          payload.MessageEN,
		ContentType:        payload.ContentType,
		DisplayMode:        payload.DisplayMode,
		ImageURL:           payload.ImageURL,
		ActionLabel:        payload.ActionLabel,
		ActionLabelTH:      payload.ActionLabelTH,
		ActionLabelEN:      payload.ActionLabelEN,
		ActionURL:          payload.ActionURL,
		IsDismissible:      isDismissible,
		DisplayPaths:       payload.DisplayPaths,
		ScheduledAt:        payload.ScheduledAt,
		ExpiresAt:          payload.ExpiresAt,
		Audience:           payload.Audience,
		RequireAcknowledge: payload.RequireAcknowledge,
		IsActive:           isActive,
	}, actorID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "create_system_announcement", "info", "system_settings", strconv.FormatUint(uint64(created.ID), 10), fiber.Map{
		"title": created.Title,
	})

	if created.IsActive {
		audienceRoles := payload.Audience
		if len(audienceRoles) == 0 {
			audienceRoles = []string{"all"}
		}
		fanoutAnnouncementNotification(actorID, created, audienceRoles)
	}

	return c.Status(201).JSON(fiber.Map{"success": true, "data": created})
}

func UpdateAnnouncementHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	var payload announcementPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	isActive := true
	if payload.IsActive != nil {
		isActive = *payload.IsActive
	}

	isDismissible := true
	if payload.IsDismissible != nil {
		isDismissible = *payload.IsDismissible
	}

	updated, updateErr := repositories.UpdateAnnouncement(uint(id), repositories.AnnouncementInput{
		Title:              payload.Title,
		TitleTH:            payload.TitleTH,
		TitleEN:            payload.TitleEN,
		Message:            payload.Message,
		MessageTH:          payload.MessageTH,
		MessageEN:          payload.MessageEN,
		ContentType:        payload.ContentType,
		DisplayMode:        payload.DisplayMode,
		ImageURL:           payload.ImageURL,
		ActionLabel:        payload.ActionLabel,
		ActionLabelTH:      payload.ActionLabelTH,
		ActionLabelEN:      payload.ActionLabelEN,
		ActionURL:          payload.ActionURL,
		IsDismissible:      isDismissible,
		DisplayPaths:       payload.DisplayPaths,
		ScheduledAt:        payload.ScheduledAt,
		ExpiresAt:          payload.ExpiresAt,
		Audience:           payload.Audience,
		RequireAcknowledge: payload.RequireAcknowledge,
		IsActive:           isActive,
	})
	if updateErr != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": updateErr.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "update_system_announcement", "info", "system_settings", strconv.FormatUint(id, 10), fiber.Map{
		"title": updated.Title,
	})

	return c.JSON(fiber.Map{"success": true, "data": updated})
}

func AcknowledgeAnnouncementHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	if err := repositories.AcknowledgeAnnouncement(uint(id), userID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถยืนยันประกาศได้"})
	}

	return c.JSON(fiber.Map{"success": true})
}

func ListFeatureFlagsHandler(c fiber.Ctx) error {
	flags, err := repositories.GetFeatureFlags()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึง feature flags ได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": flags})
}

func UpdateFeatureFlagHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	flagKey := strings.TrimSpace(c.Params("key"))
	if flagKey == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "feature flag key ว่างไม่ได้"})
	}

	var payload featureFlagPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	flag, err := repositories.SetFeatureFlag(flagKey, payload.Enabled)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "update_feature_flag", "warn", "system_settings", flagKey, fiber.Map{
		"enabled": payload.Enabled,
	})

	return c.JSON(fiber.Map{"success": true, "data": flag})
}

func GetMaintenanceModeHandler(c fiber.Ctx) error {
	cfg, err := repositories.GetMaintenanceModeConfig()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึง maintenance mode ได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": cfg})
}

func UpdateMaintenanceModeHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload maintenanceModePayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	nextCfg, err := repositories.SetMaintenanceModeConfig(repositories.MaintenanceModeConfig{
		Enabled:             payload.Enabled,
		ScheduleType:        payload.ScheduleType,
		Message:             payload.Message,
		StartTime:           payload.StartTime,
		EndTime:             payload.EndTime,
		WhitelistAdminUsers: payload.WhitelistAdminUsers,
		UpdatedBy:           &actorID,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถบันทึก maintenance mode ได้"})
	}

	logPrivilegedAdminAction(c, actorID, "update_maintenance_mode", "warn", "system_settings", "maintenance_mode", fiber.Map{
		"enabled": nextCfg.Enabled,
	})

	return c.JSON(fiber.Map{"success": true, "data": nextCfg})
}

func GetStudentProgramsHandler(c fiber.Ctx) error {
	programs, err := repositories.GetStudentPrograms()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงข้อมูลหลักสูตรได้"})
	}

	return c.JSON(fiber.Map{"success": true, "data": programs})
}

func UpdateStudentProgramsHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload studentProgramsPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	programs, err := repositories.SetStudentPrograms(payload.Programs)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "update_student_programs", "warn", "system_settings", "student_programs", fiber.Map{
		"program_count": len(programs),
	})

	return c.JSON(fiber.Map{"success": true, "data": programs})
}

// GetPublicMaintenanceStatusHandler returns minimal maintenance status — no auth required.
func GetPublicMaintenanceStatusHandler(c fiber.Ctx) error {
	cfg, err := repositories.GetMaintenanceModeConfig()
	if err != nil {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"active": false}})
	}

	active := repositories.IsMaintenanceActive(cfg)
	data := fiber.Map{
		"active":        active,
		"message":       cfg.Message,
		"schedule_type": cfg.ScheduleType,
	}
	if cfg.StartTime != nil {
		data["start_time"] = cfg.StartTime
	}
	if cfg.EndTime != nil {
		data["end_time"] = cfg.EndTime
	}
	return c.JSON(fiber.Map{"success": true, "data": data})
}

func GetServiceHealthHandler(c fiber.Ctx) error {
	dbStatus := fiber.Map{
		"name":   "database",
		"status": "up",
		"detail": "connected",
	}
	if sqlDB, err := config.DB.DB(); err != nil {
		dbStatus["status"] = "down"
		dbStatus["detail"] = err.Error()
	} else if pingErr := sqlDB.Ping(); pingErr != nil {
		dbStatus["status"] = "down"
		dbStatus["detail"] = pingErr.Error()
	}

	emailConfigured := os.Getenv("SMTP_HOST") != "" && os.Getenv("SMTP_USER") != ""
	emailStatus := fiber.Map{
		"name":   "email",
		"status": "down",
		"detail": "SMTP_HOST/SMTP_USER missing",
	}
	if emailConfigured {
		emailStatus["status"] = "up"
		emailStatus["detail"] = "configuration present"
	}

	uploadStatus := fiber.Map{
		"name":   "uploads",
		"status": "up",
		"detail": "uploads directory ready",
	}
	if _, err := os.Stat("./uploads"); err != nil {
		uploadStatus["status"] = "down"
		uploadStatus["detail"] = err.Error()
	}

	realtimeStatus := fiber.Map{
		"name":   "realtime",
		"status": "up",
		"detail": "websocket endpoint /ws registered",
	}

	r2HealthStatus, r2HealthDetail := services.CheckR2Health()
	r2Status := fiber.Map{
		"name":   "backup_storage_r2",
		"status": r2HealthStatus,
		"detail": r2HealthDetail,
	}

	dependencies := []fiber.Map{dbStatus, emailStatus, uploadStatus, realtimeStatus, r2Status}
	overall := "up"
	for _, item := range dependencies {
		status, ok := item["status"].(string)
		if ok && status == "down" {
			overall = "degraded"
			break
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"overall_status": overall,
			"timestamp":      time.Now(),
			"dependencies":   dependencies,
		},
	})
}

func fanoutAnnouncementNotification(actorID uint, announcement *models.SystemAnnouncement, audienceRoles []string) {
	if announcement == nil {
		return
	}

	userIDs, err := resolveAudienceUserIDs(audienceRoles)
	if err != nil || len(userIDs) == 0 {
		return
	}

	payload, _ := json.Marshal(fiber.Map{
		"announcement_id":     announcement.ID,
		"require_acknowledge": announcement.RequireAcknowledge,
	})
	data := datatypes.JSON(payload)
	link := "/student/notifications"

	for _, userID := range userIDs {
		if userID == actorID {
			continue
		}
		createNotificationForUser(
			userID,
			"",
			"announcement",
			announcement.Title,
			announcement.Message,
			link,
			data,
		)
	}
}

func resolveAudienceUserIDs(audienceRoles []string) ([]uint, error) {
	if len(audienceRoles) == 0 {
		audienceRoles = []string{"all"}
	}
	unique := map[uint]struct{}{}
	normalized := make([]string, 0, len(audienceRoles))
	for _, role := range audienceRoles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			continue
		}
		normalized = append(normalized, role)
	}

	for _, role := range normalized {
		if role == "all" {
			allUsers, err := repositories.GetAllActiveUserIDs()
			if err != nil {
				return nil, err
			}
			for _, userID := range allUsers {
				unique[userID] = struct{}{}
			}
			continue
		}

		users, err := repositories.GetUsersByRole(role)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			unique[user.ID] = struct{}{}
		}
	}

	result := make([]uint, 0, len(unique))
	for userID := range unique {
		result = append(result, userID)
	}
	return result, nil
}
