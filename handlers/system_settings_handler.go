package handlers

import (
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/services"
	"log"
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
	Severity           string     `json:"severity"`
	Priority           int        `json:"priority"`
	Status             string     `json:"status"`
	NotifyInbox        *bool      `json:"notify_inbox"`
}

// announcementBatchPayload is how the composer sends several announcements at
// once. Shared defaults are applied to every item that leaves the field empty,
// so a run of notices that differ only in wording does not have to repeat the
// audience, schedule and display settings for each one.
type announcementBatchPayload struct {
	Defaults *announcementPayload  `json:"defaults"`
	Items    []announcementPayload `json:"items"`
}

type announcementStatusPayload struct {
	Status string `json:"status"`
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
	rows, err := repositories.ListAnnouncements(repositories.AnnouncementListFilter{
		IncludeExpired: strings.EqualFold(c.Query("includeExpired"), "true"),
		Status:         c.Query("status"),
		Severity:       c.Query("severity"),
		Search:         c.Query("search"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถดึงประกาศได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": rows})
}

func ListActiveAnnouncementsForCurrentUserHandler(c fiber.Ctx) error {
	role, _ := middlewares.GetUserRole(c)
	userID, ok := middlewares.GetUserID(c)
	studentID, hasStudentID := middlewares.GetStudentID(c)
	if !ok {
		// Student sessions only populate student_id in auth middleware.
		if !hasStudentID {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "user not found in session"})
		}
		userID = 0
	}

	rows, err := repositories.ListActiveAnnouncementsForUser(userID, studentID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "failed to load active announcements"})
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

	publicPath := filepath.ToSlash(filepath.Join("/api/uploads", "system-announcements", fileName))
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"url": publicPath}})
}

// toAnnouncementInput turns one request payload into repository input,
// applying the optional shared defaults a batch request carries.
func toAnnouncementInput(payload announcementPayload, defaults *announcementPayload) repositories.AnnouncementInput {
	if defaults != nil {
		payload = mergeAnnouncementPayload(payload, *defaults)
	}

	isActive := true
	if payload.IsActive != nil {
		isActive = *payload.IsActive
	}

	isDismissible := true
	if payload.IsDismissible != nil {
		isDismissible = *payload.IsDismissible
	}

	notifyInbox := true
	if payload.NotifyInbox != nil {
		notifyInbox = *payload.NotifyInbox
	}

	return repositories.AnnouncementInput{
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
		Severity:           payload.Severity,
		Priority:           payload.Priority,
		Status:             payload.Status,
		NotifyInbox:        notifyInbox,
		IsActive:           isActive,
	}
}

// mergeAnnouncementPayload fills the fields an item left empty from the batch
// defaults. Only the settings that sensibly apply to a whole batch are
// inherited; title, message and image always come from the item itself, since
// sharing those would just produce identical announcements.
func mergeAnnouncementPayload(item announcementPayload, defaults announcementPayload) announcementPayload {
	if strings.TrimSpace(item.ContentType) == "" {
		item.ContentType = defaults.ContentType
	}
	if strings.TrimSpace(item.DisplayMode) == "" {
		item.DisplayMode = defaults.DisplayMode
	}
	if strings.TrimSpace(item.Severity) == "" {
		item.Severity = defaults.Severity
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = defaults.Status
	}
	if item.Priority == 0 {
		item.Priority = defaults.Priority
	}
	if len(item.Audience) == 0 {
		item.Audience = defaults.Audience
	}
	if len(item.DisplayPaths) == 0 {
		item.DisplayPaths = defaults.DisplayPaths
	}
	if item.ScheduledAt == nil {
		item.ScheduledAt = defaults.ScheduledAt
	}
	if item.ExpiresAt == nil {
		item.ExpiresAt = defaults.ExpiresAt
	}
	if item.IsActive == nil {
		item.IsActive = defaults.IsActive
	}
	if item.IsDismissible == nil {
		item.IsDismissible = defaults.IsDismissible
	}
	if item.NotifyInbox == nil {
		item.NotifyInbox = defaults.NotifyInbox
	}
	if !item.RequireAcknowledge {
		item.RequireAcknowledge = defaults.RequireAcknowledge
	}
	return item
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

	input := toAnnouncementInput(payload, nil)
	created, err := repositories.CreateAnnouncement(input, actorID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "create_system_announcement", "info", "system_settings", strconv.FormatUint(uint64(created.ID), 10), fiber.Map{
		"title":    created.Title,
		"severity": created.Severity,
		"status":   created.Status,
	})

	publishAnnouncement(actorID, created, input.Audience)

	return c.Status(201).JSON(fiber.Map{"success": true, "data": created})
}

// CreateAnnouncementsBatchHandler publishes several announcements in one go.
// Writing them one request at a time meant an admin preparing a set of term
// notices sat through the fan-out for each; here the whole set is written in a
// single transaction and the notifications go out behind it.
func CreateAnnouncementsBatchHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	var payload announcementBatchPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	if len(payload.Items) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบรายการประกาศที่จะสร้าง"})
	}
	if len(payload.Items) > repositories.AnnouncementBatchLimit {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": fmt.Sprintf("สร้างประกาศพร้อมกันได้สูงสุด %d รายการ", repositories.AnnouncementBatchLimit),
		})
	}

	inputs := make([]repositories.AnnouncementInput, 0, len(payload.Items))
	for _, item := range payload.Items {
		inputs = append(inputs, toAnnouncementInput(item, payload.Defaults))
	}

	created, err := repositories.CreateAnnouncementsBatch(inputs, actorID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	createdIDs := make([]uint, 0, len(created))
	for _, row := range created {
		createdIDs = append(createdIDs, row.ID)
	}
	logPrivilegedAdminAction(c, actorID, "create_system_announcement_batch", "info", "system_settings", "", fiber.Map{
		"count": len(created),
		"ids":   createdIDs,
	})

	for index := range created {
		publishAnnouncement(actorID, &created[index], inputs[index].Audience)
	}

	return c.Status(201).JSON(fiber.Map{"success": true, "data": created})
}

// SetAnnouncementStatusHandler archives, republishes or returns an
// announcement to draft without touching its content.
func SetAnnouncementStatusHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	var payload announcementStatusPayload
	if err := c.Bind().JSON(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	updated, statusErr := repositories.SetAnnouncementStatus(uint(id), payload.Status)
	if statusErr != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": statusErr.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "set_system_announcement_status", "info", "system_settings", strconv.FormatUint(id, 10), fiber.Map{
		"status": updated.Status,
	})

	return c.JSON(fiber.Map{"success": true, "data": updated})
}

// DeleteAnnouncementHandler permanently removes an announcement. Archiving is
// the reversible option and is what the UI offers first; this is for clearing
// out mistakes.
func DeleteAnnouncementHandler(c fiber.Ctx) error {
	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้ใน session"})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	existing, lookupErr := repositories.GetAnnouncementByID(uint(id))
	if lookupErr != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบประกาศที่ต้องการลบ"})
	}

	if deleteErr := repositories.DeleteAnnouncement(uint(id)); deleteErr != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถลบประกาศได้"})
	}

	logPrivilegedAdminAction(c, actorID, "delete_system_announcement", "warning", "system_settings", strconv.FormatUint(id, 10), fiber.Map{
		"title": existing.Title,
	})

	return c.JSON(fiber.Map{"success": true})
}

// GetAnnouncementStatsHandler reports reach: how many people the announcement
// was addressed to, how many acknowledged it, and who is still outstanding.
func GetAnnouncementStatsHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "id ไม่ถูกต้อง"})
	}

	stats, statsErr := repositories.GetAnnouncementStats(uint(id))
	if statsErr != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบประกาศที่ต้องการ"})
	}

	return c.JSON(fiber.Map{"success": true, "data": stats})
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

	previous, lookupErr := repositories.GetAnnouncementByID(uint(id))
	if lookupErr != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบประกาศที่ต้องการแก้ไข"})
	}

	input := toAnnouncementInput(payload, nil)
	updated, updateErr := repositories.UpdateAnnouncement(uint(id), input)
	if updateErr != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": updateErr.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "update_system_announcement", "info", "system_settings", strconv.FormatUint(id, 10), fiber.Map{
		"title":    updated.Title,
		"severity": updated.Severity,
		"status":   updated.Status,
	})

	// An announcement that was a draft (or archived) until now has never
	// reached anyone, so publishing it from the editor has to fan out the same
	// way creating it live would. One that was already published does not, or
	// every wording fix would land in everybody's inbox again.
	if !previous.IsActive && updated.IsActive {
		publishAnnouncement(actorID, updated, input.Audience)
	}

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

	if err := repositories.AcknowledgeAnnouncement(uint(id), userID, 0); err != nil {
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

// publishAnnouncement sends an announcement to every recipient's notification
// inbox, in the background.
//
// This used to run inline in the create request, one insert plus one unread
// count plus one websocket emit per recipient. With a few thousand active
// users that is a few thousand round trips an admin waited through before the
// composer would close, and long enough to hit the proxy's timeout. Now the
// request returns as soon as the announcement is stored and the fan-out
// continues behind it.
func publishAnnouncement(actorID uint, announcement *models.SystemAnnouncement, audienceRoles []string) {
	if announcement == nil || !announcement.IsActive || !announcement.NotifyInbox {
		return
	}
	// A scheduled announcement has not started yet; notifying about it now
	// would arrive before the thing it announces is visible.
	if announcement.ScheduledAt != nil && announcement.ScheduledAt.After(time.Now()) {
		return
	}

	if len(audienceRoles) == 0 {
		audienceRoles = []string{"all"}
	}

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("⚠️  announcement fan-out panicked for announcement %d: %v", announcement.ID, recovered)
			}
		}()
		fanoutAnnouncementNotification(actorID, announcement, audienceRoles)
	}()
}

func fanoutAnnouncementNotification(actorID uint, announcement *models.SystemAnnouncement, audienceRoles []string) {
	if announcement == nil {
		return
	}

	userIDs, err := resolveAudienceUserIDs(audienceRoles)
	if err != nil || len(userIDs) == 0 {
		return
	}

	// Both language variants ride along in the payload so the inbox can render
	// the announcement in the reader's language instead of whichever one the
	// admin happened to type into the primary field.
	payload, _ := json.Marshal(fiber.Map{
		"announcement_id":     announcement.ID,
		"require_acknowledge": announcement.RequireAcknowledge,
		"severity":            announcement.Severity,
		"title_th":            announcement.TitleTH,
		"title_en":            announcement.TitleEN,
		"message_th":          announcement.MessageTH,
		"message_en":          announcement.MessageEN,
	})
	data := datatypes.JSON(payload)
	link := "/student/notifications"
	createdAt := time.Now()

	notifications := make([]models.UserNotification, 0, len(userIDs))
	recipients := make([]uint, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == actorID {
			continue
		}
		notifications = append(notifications, models.UserNotification{
			UserID:    userID,
			Type:      "announcement",
			Title:     announcement.Title,
			Message:   announcement.Message,
			Link:      link,
			Data:      data,
			IsRead:    false,
			CreatedAt: createdAt,
		})
		recipients = append(recipients, userID)
	}

	if len(notifications) == 0 {
		return
	}

	// One multi-row insert per chunk instead of one statement per recipient.
	const insertChunkSize = 500
	for start := 0; start < len(notifications); start += insertChunkSize {
		end := start + insertChunkSize
		if end > len(notifications) {
			end = len(notifications)
		}
		if err := repositories.CreateUserNotifications(notifications[start:end]); err != nil {
			log.Printf("⚠️  failed to write announcement notifications: %v", err)
			return
		}
	}

	for index, userID := range recipients {
		count, _ := repositories.GetUnreadNotificationCount(userID)
		realtime.EmitToUser(userID, "notification", fiber.Map{
			"id":           notifications[index].ID,
			"type":         notifications[index].Type,
			"title":        notifications[index].Title,
			"message":      notifications[index].Message,
			"link":         notifications[index].Link,
			"data":         notifications[index].Data,
			"is_read":      false,
			"created_at":   notifications[index].CreatedAt,
			"unread_count": count,
		})
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
