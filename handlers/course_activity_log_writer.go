package handlers

import (
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strings"

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
