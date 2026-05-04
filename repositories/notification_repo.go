package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
)

func CreateOrUpdateFcmToken(data *models.FcmToken) (*models.FcmToken, bool, error) {
	var token models.FcmToken
	// หา Token เดิม
	err := config.DB.Where("fcm_token = ?", data.FcmToken).First(&token).Error
	if err == nil {
		// เจอ Token เดิม -> Update
		token.UserType = data.UserType
		token.UserID = data.UserID
		token.StudentID = data.StudentID
		token.SessionID = data.SessionID
		token.BookingID = data.BookingID
		token.DeviceInfo = data.DeviceInfo
		token.IsActive = true
		token.LastUsedAt = data.LastUsedAt
		errUpdate := config.DB.Save(&token).Error
		return &token, false, errUpdate
	}

	// ไม่เจอ -> Create
	errCreate := config.DB.Create(data).Error
	return data, true, errCreate
}

func DeleteFcmToken(fcmToken string) (int64, error) {
	result := config.DB.Where("fcm_token = ?", fcmToken).Delete(&models.FcmToken{})
	return result.RowsAffected, result.Error
}

func UpdateStudentBookingID(fcmToken string, bookingID *uint) (int64, error) {
	result := config.DB.Model(&models.FcmToken{}).
		Where("fcm_token = ? AND user_type = ?", fcmToken, "student").
		Updates(map[string]interface{}{"booking_id": bookingID, "last_used_at": config.DB.NowFunc()})
	return result.RowsAffected, result.Error
}

func GetUserFcmTokens(userID uint) ([]models.FcmToken, error) {
	var tokens []models.FcmToken
	err := config.DB.Where("user_id = ? AND is_active = ?", userID, true).
		Find(&tokens).Error
	return tokens, err
}

func GetUserNotificationLogs(userID uint, limit int, offset int) ([]models.NotificationLog, error) {
	var logs []models.NotificationLog
	// Join FcmToken to check owner
	err := config.DB.Joins("JOIN fcm_tokens ON fcm_tokens.id = notification_logs.fcm_token_id").
		Where("fcm_tokens.user_id = ?", userID).
		Order("notification_logs.created_at desc").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, err
}
