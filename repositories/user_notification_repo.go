package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"
)

// CreateUserNotifications bulk-inserts notification records.
func CreateUserNotifications(notifications []models.UserNotification) error {
	if len(notifications) == 0 {
		return nil
	}
	return config.DB.Create(&notifications).Error
}

// GetUserNotifications returns paginated notifications for a user (newest first).
func GetUserNotifications(userID uint, limit, offset int) ([]models.UserNotification, int64, error) {
	var notifications []models.UserNotification
	var total int64

	base := config.DB.Model(&models.UserNotification{}).Where("user_id = ?", userID)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := base.Order("created_at DESC").Limit(limit).Offset(offset).Find(&notifications).Error
	return notifications, total, err
}

// GetUnreadNotificationCount returns how many unread notifications the user has.
func GetUnreadNotificationCount(userID uint) (int64, error) {
	var count int64
	err := config.DB.Model(&models.UserNotification{}).
		Where("user_id = ? AND is_read = false", userID).
		Count(&count).Error
	return count, err
}

// MarkNotificationRead marks a single notification as read.
func MarkNotificationRead(notificationID uint, userID uint) error {
	now := time.Now()
	return config.DB.Model(&models.UserNotification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// MarkAllNotificationsRead marks all unread notifications for the user as read.
func MarkAllNotificationsRead(userID uint) error {
	now := time.Now()
	return config.DB.Model(&models.UserNotification{}).
		Where("user_id = ? AND is_read = false", userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// DeleteReadNotifications removes all read notifications for the user.
func DeleteReadNotifications(userID uint) error {
	return config.DB.Where("user_id = ? AND is_read = true", userID).
		Delete(&models.UserNotification{}).Error
}

// GetCourseUserIDs returns all unique user IDs (instructors + TAs) for a given course.
func GetCourseUserIDs(courseID string) ([]uint, error) {
	type row struct {
		UserID uint `gorm:"column:user_id"`
	}
	var rows []row
	err := config.DB.Raw(`
		SELECT DISTINCT user_id FROM (
			SELECT user_id FROM course_instructors WHERE course_id = ?
			UNION
			SELECT user_id FROM course_tas WHERE course_id = ?
		) sub
	`, courseID, courseID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UserID)
	}
	return ids, nil
}

// GetAllActiveUserIDs returns all active user IDs in the system.
func GetAllActiveUserIDs() ([]uint, error) {
	type row struct {
		ID uint `gorm:"column:id"`
	}
	var rows []row
	if err := config.DB.Raw(`SELECT id FROM users WHERE is_active = true`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// GetScoreEditRequestRequester looks up the requested_by field for a pending request.
func GetScoreEditRequestRequester(requestID uint) (uint, error) {
	var req models.ScoreEditRequest
	err := config.DB.Select("id", "requested_by").Where("id = ?", requestID).First(&req).Error
	return req.RequestedBy, err
}

// GetScoreEditRequestRequesters returns map of requestID → requestedBy for multiple requests.
func GetScoreEditRequestRequesters(requestIDs []uint) (map[uint]uint, error) {
	if len(requestIDs) == 0 {
		return map[uint]uint{}, nil
	}
	var reqs []models.ScoreEditRequest
	err := config.DB.Select("id", "requested_by").Where("id IN ?", requestIDs).Find(&reqs).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint]uint, len(reqs))
	for _, r := range reqs {
		result[r.ID] = r.RequestedBy
	}
	return result, nil
}
