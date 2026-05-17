package handlers

import (
	"fmt"
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/services"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

var validFeedbackTypes = map[string]bool{
	"bug":         true,
	"feature":     true,
	"improvement": true,
	"other":       true,
	"support":     true,
}

var validFeedbackPriorities = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

type createFeedbackInput struct {
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Attachments  datatypes.JSON `json:"attachments"`
	ContactEmail string         `json:"contact_email"`
	Priority     string         `json:"priority"`
	Website      string         `json:"website"`
}

func normalizeFeedbackType(input string, fallback string) string {
	feedbackType := strings.TrimSpace(strings.ToLower(input))
	if feedbackType == "" {
		feedbackType = fallback
	}
	if !validFeedbackTypes[feedbackType] {
		return fallback
	}
	return feedbackType
}

func normalizeFeedbackPriority(input string) string {
	priority := strings.TrimSpace(strings.ToLower(input))
	if priority == "" {
		return "medium"
	}
	if !validFeedbackPriorities[priority] {
		return "medium"
	}
	return priority
}

func supportTicketEmailLimit() int64 {
	limit := readSupportTicketIntEnv("SUPPORT_TICKET_EMAIL_RATE_LIMIT", 4)
	if limit < 1 {
		return 4
	}
	return int64(limit)
}

func supportTicketEmailWindow() time.Duration {
	seconds := readSupportTicketIntEnv("SUPPORT_TICKET_EMAIL_WINDOW_SECONDS", 3600)
	if seconds < 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func supportTicketDuplicateWindow() time.Duration {
	seconds := readSupportTicketIntEnv("SUPPORT_TICKET_DUPLICATE_WINDOW_SECONDS", 900)
	if seconds < 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func readSupportTicketIntEnv(key string, fallback int) int {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(rawValue)
	if err != nil {
		return fallback
	}

	return parsed
}

func shouldSilentlyDropSupportSubmission(input createFeedbackInput) bool {
	return strings.TrimSpace(input.Website) != ""
}

func enforceSupportSubmissionGuards(input createFeedbackInput) error {
	if input.ContactEmail == "" {
		return nil
	}

	recentCount, err := repositories.CountRecentFeedbacksByContactEmail("support", input.ContactEmail, time.Now().Add(-supportTicketEmailWindow()))
	if err == nil && recentCount >= supportTicketEmailLimit() {
		return fiber.NewError(429, "คำขอจากอีเมลนี้ถูกส่งถี่เกินไป กรุณารอสักครู่แล้วลองใหม่")
	}

	hasDuplicate, err := repositories.HasRecentFeedbackWithSameTitle("support", input.ContactEmail, input.Title, time.Now().Add(-supportTicketDuplicateWindow()))
	if err == nil && hasDuplicate {
		return fiber.NewError(429, "มีคำขอหัวข้อนี้ถูกส่งไปแล้วเมื่อไม่นานนี้ กรุณารอทีมงานตรวจสอบก่อนส่งซ้ำ")
	}

	return nil
}

func notifyAdminsOfSupportTicket(feedback models.Feedback) {
	adminUsers, err := repositories.GetUsersByRole("admin")
	if err != nil || len(adminUsers) == 0 {
		return
	}

	message := fmt.Sprintf("Ticket #%d • %s • ติดต่อกลับ %s", feedback.ID, strings.ToUpper(feedback.Priority), fallbackSupportContact(feedback.ContactEmail))
	data := buildNotifData("", fmt.Sprint(feedback.ID), "support_ticket", "")
	now := time.Now()
	notifications := make([]models.UserNotification, 0, len(adminUsers))

	for _, adminUser := range adminUsers {
		notifications = append(notifications, models.UserNotification{
			UserID:    adminUser.ID,
			Type:      "support_ticket",
			Title:     "คำขอสนับสนุนใหม่: " + feedback.Title,
			Message:   message,
			Link:      "/admin/feedback?type=support",
			Data:      data,
			IsRead:    false,
			CreatedAt: now,
		})
	}

	if err := repositories.CreateUserNotifications(notifications); err != nil {
		log.Printf("failed to create support ticket notifications: %v", err)
		return
	}

	for _, notification := range notifications {
		count, _ := repositories.GetUnreadNotificationCount(notification.UserID)
		realtime.EmitToUser(notification.UserID, "notification", fiber.Map{
			"id":           notification.ID,
			"type":         notification.Type,
			"title":        notification.Title,
			"message":      notification.Message,
			"link":         notification.Link,
			"data":         notification.Data,
			"is_read":      false,
			"created_at":   notification.CreatedAt,
			"unread_count": count,
		})
	}
}

func fallbackSupportContact(contactEmail string) string {
	trimmed := strings.TrimSpace(contactEmail)
	if trimmed == "" {
		return "ไม่ระบุอีเมล"
	}
	return trimmed
}

func feedbackSnapshotForAudit(feedback *repositories.FeedbackWithUsers) fiber.Map {
	if feedback == nil {
		return fiber.Map{}
	}

	return fiber.Map{
		"id":            feedback.ID,
		"type":          feedback.Type,
		"title":         feedback.Title,
		"status":        feedback.Status,
		"priority":      feedback.Priority,
		"resolved_by":   feedback.ResolvedBy,
		"contact_email": fallbackSupportContact(feedback.ContactEmail),
	}
}

func dispatchSupportTicketAlerts(feedback models.Feedback) {
	notifyAdminsOfSupportTicket(feedback)
	if err := services.SendSupportTicketAlert(&feedback); err != nil {
		log.Printf("failed to send support ticket alert email: %v", err)
	}
}

func createFeedbackFromRequest(c fiber.Ctx, forcedType string, successMessage string) error {
	var input createFeedbackInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.ContactEmail = strings.ToLower(strings.TrimSpace(input.ContactEmail))
	input.Website = strings.TrimSpace(input.Website)

	if input.Title == "" || input.Description == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "title and description are required"})
	}

	feedbackType := normalizeFeedbackType(input.Type, "other")
	if forcedType != "" {
		feedbackType = normalizeFeedbackType(forcedType, "support")
	}

	var userIDPtr *uint
	if raw := c.Locals("user_id"); raw != nil {
		uid := raw.(uint)
		userIDPtr = &uid
	}

	if userIDPtr == nil && input.ContactEmail == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "contact_email is required for guest submissions"})
	}

	if feedbackType == "support" && shouldSilentlyDropSupportSubmission(input) {
		return c.Status(202).JSON(fiber.Map{"success": true, "message": successMessage})
	}

	if feedbackType == "support" {
		if err := enforceSupportSubmissionGuards(input); err != nil {
			if fiberErr, ok := err.(*fiber.Error); ok {
				return c.Status(fiberErr.Code).JSON(fiber.Map{"success": false, "message": fiberErr.Message})
			}
			return c.Status(429).JSON(fiber.Map{"success": false, "message": "ส่งคำขอถี่เกินไป กรุณารอสักครู่แล้วลองใหม่"})
		}
	}

	feedback := models.Feedback{
		UserID:       userIDPtr,
		Type:         feedbackType,
		Title:        input.Title,
		Description:  input.Description,
		Attachments:  input.Attachments,
		Priority:     normalizeFeedbackPriority(input.Priority),
		Status:       "pending",
		ContactEmail: input.ContactEmail,
	}
	if err := repositories.CreateFeedback(&feedback); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create feedback"})
	}
	if feedback.Type == "support" {
		go dispatchSupportTicketAlerts(feedback)
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "message": successMessage, "data": feedback})
}

// POST /api/feedback
func CreateFeedbackHandler(c fiber.Ctx) error {
	return createFeedbackFromRequest(c, "", "ส่ง Feedback สำเร็จ")
}

// POST /api/feedback/support
func CreateSupportTicketHandler(c fiber.Ctx) error {
	return createFeedbackFromRequest(c, "support", "ส่งคำขอสนับสนุนสำเร็จ")
}

// GET /api/feedback/my  (authenticated)
func GetMyFeedbacksHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	result, err := repositories.GetMyFeedbacks(userID, page, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch feedbacks"})
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// GET /api/feedback  (admin)
func GetAllFeedbacksHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	params := repositories.FeedbackListParams{
		Page:      page,
		Limit:     limit,
		Search:    c.Query("search"),
		Type:      c.Query("type"),
		Status:    c.Query("status"),
		Priority:  c.Query("priority"),
		SortBy:    c.Query("sort_by", "created_at"),
		SortOrder: c.Query("sort_order", "DESC"),
	}

	result, err := repositories.GetFeedbacks(params)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch feedbacks"})
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// GET /api/feedback/stats  (admin)
func GetFeedbackStatsHandler(c fiber.Ctx) error {
	stats := repositories.GetFeedbackStats()
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// GET /api/feedback/:id  (admin)
func GetFeedbackByIDHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	feedback, err := repositories.GetFeedbackByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Feedback not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": feedback})
}

// PUT /api/feedback/:id  (admin)
func UpdateFeedbackHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	existing, err := repositories.GetFeedbackByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Feedback not found"})
	}
	var input struct {
		Status     string `json:"status"`
		Priority   string `json:"priority"`
		AdminNotes string `json:"admin_notes"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	resolvedBy := c.Locals("user_id").(uint)
	feedback, err2 := repositories.UpdateFeedback(uint(id), input.Status, input.Priority, input.AdminNotes, resolvedBy)
	if err2 != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update feedback"})
	}
	logPrivilegedAdminAction(c, resolvedBy, "update_feedback", "warn", "feedback", strconv.FormatUint(id, 10), fiber.Map{
		"target_type":     "feedback",
		"before_snapshot": feedbackSnapshotForAudit(existing),
		"target_snapshot": feedbackSnapshotForAudit(feedback),
		"admin_notes":     strings.TrimSpace(input.AdminNotes) != "",
	})
	return c.JSON(fiber.Map{"success": true, "message": "อัปเดต Feedback สำเร็จ", "data": feedback})
}

// DELETE /api/feedback/:id  (admin)
func DeleteFeedbackHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)
	feedback, err := repositories.GetFeedbackByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Feedback not found"})
	}
	if err := repositories.DeleteFeedback(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete feedback"})
	}
	logPrivilegedAdminAction(c, actorID, "delete_feedback", "critical", "feedback", strconv.FormatUint(id, 10), fiber.Map{
		"target_type":     "feedback",
		"target_snapshot": feedbackSnapshotForAudit(feedback),
	})
	return c.JSON(fiber.Map{"success": true, "message": "ลบ Feedback สำเร็จ"})
}
