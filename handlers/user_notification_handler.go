package handlers

import (
	"encoding/json"
	"fmt"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

func getNotificationUserID(c fiber.Ctx) (uint, bool) {
	return middlewares.GetUserID(c)
}

// =============================================================================
// Notification helpers (called from other handlers)
// =============================================================================

type notifData struct {
	CourseID     string `json:"course_id,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ActorName    string `json:"actor_name,omitempty"`
}

func buildNotifData(courseID, resourceID, resourceType, actorName string) datatypes.JSON {
	payload, _ := json.Marshal(notifData{
		CourseID:     courseID,
		ResourceID:   resourceID,
		ResourceType: resourceType,
		ActorName:    actorName,
	})
	return datatypes.JSON(payload)
}

// createNotificationsForCourseMembers creates DB notifications for all
// instructors/TAs in the course (excluding actorID), then pushes realtime
// events to each user's personal WebSocket room.
func createNotificationsForCourseMembers(courseID string, actorID uint, notifType, title, message, link string, data datatypes.JSON) {
	userIDs, err := repositories.GetCourseUserIDs(courseID)
	if err != nil || len(userIDs) == 0 {
		return
	}

	now := time.Now()
	notifications := make([]models.UserNotification, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid == actorID {
			continue
		}
		notifications = append(notifications, models.UserNotification{
			UserID:    uid,
			Type:      notifType,
			Title:     title,
			Message:   message,
			CourseID:  courseID,
			Link:      link,
			Data:      data,
			IsRead:    false,
			CreatedAt: now,
		})
	}
	if len(notifications) == 0 {
		return
	}
	if err := repositories.CreateUserNotifications(notifications); err != nil {
		return
	}

	// Emit to each user's private room
	for _, n := range notifications {
		count, _ := repositories.GetUnreadNotificationCount(n.UserID)
		realtime.EmitToUser(n.UserID, "notification", fiber.Map{
			"id":           n.ID,
			"type":         n.Type,
			"title":        n.Title,
			"message":      n.Message,
			"course_id":    n.CourseID,
			"link":         n.Link,
			"data":         n.Data,
			"is_read":      false,
			"created_at":   n.CreatedAt,
			"unread_count": count,
		})
	}
}

// createNotificationForUser creates a single DB notification for a user and
// pushes a realtime event to their personal room.
func createNotificationForUser(userID uint, courseID, notifType, title, message, link string, data datatypes.JSON) {
	n := models.UserNotification{
		UserID:    userID,
		Type:      notifType,
		Title:     title,
		Message:   message,
		CourseID:  courseID,
		Link:      link,
		Data:      data,
		IsRead:    false,
		CreatedAt: time.Now(),
	}
	if err := repositories.CreateUserNotifications([]models.UserNotification{n}); err != nil {
		return
	}
	count, _ := repositories.GetUnreadNotificationCount(userID)
	realtime.EmitToUser(userID, "notification", fiber.Map{
		"id":           n.ID,
		"type":         n.Type,
		"title":        n.Title,
		"message":      n.Message,
		"course_id":    n.CourseID,
		"link":         n.Link,
		"data":         n.Data,
		"is_read":      false,
		"created_at":   n.CreatedAt,
		"unread_count": count,
	})
}

// =============================================================================
// Admin broadcast
// =============================================================================

// POST /api/admin/notifications/broadcast
func AdminBroadcastNotificationHandler(c fiber.Ctx) error {
	var input struct {
		UserIDs []uint `json:"user_ids"` // empty = all users
		Title   string `json:"title"`
		Message string `json:"message"`
		Link    string `json:"link"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.Title == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "title is required"})
	}

	actorID, ok := getNotificationUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	actorName := fmt.Sprint(actorID)
	if u, ok := c.Locals("user_full_name").(string); ok && u != "" {
		actorName = u
	}

	var targetIDs []uint
	if len(input.UserIDs) > 0 {
		targetIDs = input.UserIDs
	} else {
		allUserIDs, err := repositories.GetAllActiveUserIDs()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load users for broadcast"})
		}
		targetIDs = allUserIDs
	}

	if len(targetIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "No target users found"})
	}

	data := buildNotifData("", "", "admin_message", actorName)
	now := time.Now()
	notifications := make([]models.UserNotification, 0, len(targetIDs))
	for _, uid := range targetIDs {
		notifications = append(notifications, models.UserNotification{
			UserID:    uid,
			Type:      "admin_message",
			Title:     input.Title,
			Message:   input.Message,
			Link:      input.Link,
			Data:      data,
			IsRead:    false,
			CreatedAt: now,
		})
	}
	if err := repositories.CreateUserNotifications(notifications); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create notifications"})
	}
	for _, n := range notifications {
		count, _ := repositories.GetUnreadNotificationCount(n.UserID)
		realtime.EmitToUser(n.UserID, "notification", fiber.Map{
			"id":           n.ID,
			"type":         n.Type,
			"title":        n.Title,
			"message":      n.Message,
			"link":         n.Link,
			"data":         n.Data,
			"is_read":      false,
			"created_at":   n.CreatedAt,
			"unread_count": count,
		})
	}
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("Sent to %d users", len(notifications))})
}

// =============================================================================
// HTTP API Handlers
// =============================================================================

// GET /api/notifications
func GetUserNotificationsHandler(c fiber.Ctx) error {
	limit := 20
	offset := 0
	if l, err := strconv.Atoi(c.Query("limit", "20")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	if o, err := strconv.Atoi(c.Query("offset", "0")); err == nil && o >= 0 {
		offset = o
	}

	courseID := strings.TrimSpace(c.Query("course_id", ""))

	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.JSON(fiber.Map{
			"success": true,
			"data":    []models.UserNotification{},
			"meta": fiber.Map{
				"total":        0,
				"unread_count": 0,
				"limit":        limit,
				"offset":       offset,
			},
		})
	}

	notifications, total, err := repositories.GetUserNotifications(userID, limit, offset, courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch notifications"})
	}

	unread, _ := repositories.GetUnreadNotificationCount(userID)

	return c.JSON(fiber.Map{
		"success": true,
		"data":    notifications,
		"meta": fiber.Map{
			"total":        total,
			"unread_count": unread,
			"limit":        limit,
			"offset":       offset,
		},
	})
}

// GET /api/notifications/count
func GetNotificationCountHandler(c fiber.Ctx) error {
	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"unread_count": 0}})
	}
	count, err := repositories.GetUnreadNotificationCount(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get count"})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"unread_count": count}})
}

// PATCH /api/notifications/:id/read
func MarkNotificationReadHandler(c fiber.Ctx) error {
	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	if err := repositories.MarkNotificationRead(uint(id), userID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to mark as read"})
	}
	count, _ := repositories.GetUnreadNotificationCount(userID)
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"unread_count": count}})
}

// PATCH /api/notifications/read-all
func MarkAllNotificationsReadHandler(c fiber.Ctx) error {
	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	if err := repositories.MarkAllNotificationsRead(userID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to mark all as read"})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"unread_count": 0}})
}

// DELETE /api/notifications/clear
func ClearReadNotificationsHandler(c fiber.Ctx) error {
	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	if err := repositories.DeleteReadNotifications(userID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to clear notifications"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Read notifications cleared"})
}

// POST /api/notifications/announcements/:id/ack
func AcknowledgeAnnouncementFromInboxHandler(c fiber.Ctx) error {
	userID, ok := getNotificationUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid announcement ID"})
	}

	if err := repositories.AcknowledgeAnnouncement(uint(id), userID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Failed to acknowledge announcement"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Acknowledged"})
}
