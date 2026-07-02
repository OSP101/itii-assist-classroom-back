package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
)

// CreateOrUpdatePushSubscription upserts a subscription by its unique endpoint,
// re-attaching it to the requesting user (a device may re-subscribe under a
// different logged-in user, e.g. shared TA workstation).
func CreateOrUpdatePushSubscription(data *models.PushSubscription) (*models.PushSubscription, bool, error) {
	var existing models.PushSubscription
	err := config.DB.Where("endpoint = ?", data.Endpoint).First(&existing).Error
	if err == nil {
		existing.UserType = data.UserType
		existing.UserID = data.UserID
		existing.StudentID = data.StudentID
		existing.BookingID = data.BookingID
		existing.SessionID = data.SessionID
		existing.P256dhKey = data.P256dhKey
		existing.AuthKey = data.AuthKey
		existing.DeviceInfo = data.DeviceInfo
		existing.UserAgent = data.UserAgent
		existing.IsActive = true
		existing.LastUsedAt = data.LastUsedAt
		errUpdate := config.DB.Save(&existing).Error
		return &existing, false, errUpdate
	}

	errCreate := config.DB.Create(data).Error
	return data, true, errCreate
}

// GetActivePushSubscriptionsByUserID returns all active Web Push subscriptions for a user.
func GetActivePushSubscriptionsByUserID(userID uint) ([]models.PushSubscription, error) {
	var subs []models.PushSubscription
	err := config.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&subs).Error
	return subs, err
}

// GetActivePushSubscriptionsByBookingID returns active subscriptions registered by a
// student against a specific queue booking (public, unauthenticated registration flow).
func GetActivePushSubscriptionsByBookingID(bookingID uint) ([]models.PushSubscription, error) {
	var subs []models.PushSubscription
	err := config.DB.Where("booking_id = ? AND is_active = ?", bookingID, true).Find(&subs).Error
	return subs, err
}

// UpdatePushSubscriptionBookingID re-attaches an existing subscription to a new
// booking id (mirrors the FcmToken update-booking flow used when a student
// re-verifies/re-books under the same device).
func UpdatePushSubscriptionBookingID(endpoint string, bookingID *uint) (int64, error) {
	result := config.DB.Model(&models.PushSubscription{}).
		Where("endpoint = ? AND user_type = ?", endpoint, "student").
		Updates(map[string]interface{}{"booking_id": bookingID, "last_used_at": config.DB.NowFunc()})
	return result.RowsAffected, result.Error
}

// DeletePushSubscriptionByEndpoint removes a subscription (e.g. on client unsubscribe request).
func DeletePushSubscriptionByEndpoint(endpoint string) (int64, error) {
	result := config.DB.Where("endpoint = ?", endpoint).Delete(&models.PushSubscription{})
	return result.RowsAffected, result.Error
}

// DeactivatePushSubscriptionByEndpoint marks a subscription inactive (e.g. push provider returned 404/410 Gone).
func DeactivatePushSubscriptionByEndpoint(endpoint string) error {
	return config.DB.Model(&models.PushSubscription{}).
		Where("endpoint = ?", endpoint).
		Update("is_active", false).Error
}
