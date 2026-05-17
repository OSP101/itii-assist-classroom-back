package handlers

import (
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

func activityTargetID(value interface{}) string {
	if value == nil {
		return ""
	}
	result := strings.TrimSpace(fmt.Sprint(value))
	if result == "<nil>" {
		return ""
	}
	return result
}

// logCourseActivity is the primary writer for all handlers that have a Fiber context.
// It captures IP address, User-Agent, and actor role/email snapshots automatically.
func logCourseActivity(c fiber.Ctx, courseID string, actorUserID uint, action, category, targetType string, targetID interface{}, targetName string, detail interface{}) {
	if strings.TrimSpace(courseID) == "" || actorUserID == 0 || strings.TrimSpace(action) == "" {
		return
	}

	actorRole, _ := c.Locals("user_role").(string)
	ip := c.IP()
	ua := string(c.Request().Header.UserAgent())

	// Snapshot actor email at write time so log is readable even if user is later deleted.
	var actorEmail string
	var u models.User
	if err := config.DB.Select("email").First(&u, actorUserID).Error; err == nil {
		actorEmail = u.Email
	}

	entry := models.CourseActivityLog{
		CourseID:    courseID,
		ActorUserID: actorUserID,
		ActorEmail:  actorEmail,
		ActorRole:   strings.TrimSpace(actorRole),
		Action:      strings.TrimSpace(action),
		Category:    strings.TrimSpace(category),
		TargetType:  strings.TrimSpace(targetType),
		TargetID:    activityTargetID(targetID),
		TargetName:  strings.TrimSpace(targetName),
		IPAddress:   ip,
		UserAgent:   ua,
	}

	if detail != nil {
		if payload, err := json.Marshal(detail); err == nil && string(payload) != "null" {
			entry.Detail = datatypes.JSON(payload)
		}
	}

	_ = config.DB.Create(&entry).Error
}

// writeCourseActivityLog is kept for backward compatibility in goroutines or call sites
// where a Fiber context is not available. New code should prefer logCourseActivity.
func writeCourseActivityLog(courseID string, actorUserID uint, action string, category string, targetType string, targetID interface{}, targetName string, detail interface{}) {
	if strings.TrimSpace(courseID) == "" || actorUserID == 0 || strings.TrimSpace(action) == "" {
		return
	}

	entry := models.CourseActivityLog{
		CourseID:    courseID,
		ActorUserID: actorUserID,
		Action:      strings.TrimSpace(action),
		Category:    strings.TrimSpace(category),
		TargetType:  strings.TrimSpace(targetType),
		TargetID:    activityTargetID(targetID),
		TargetName:  strings.TrimSpace(targetName),
	}

	if detail != nil {
		if payload, err := json.Marshal(detail); err == nil && string(payload) != "null" {
			entry.Detail = datatypes.JSON(payload)
		}
	}

	_ = config.DB.Create(&entry).Error
}
