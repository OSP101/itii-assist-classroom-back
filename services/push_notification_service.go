package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const fcmLegacyEndpoint = "https://fcm.googleapis.com/fcm/send"

// SendQueueWorkerAssignedPush sends a background push to the assigned worker.
// The notification falls back to a no-op when FCM credentials are not configured.
func SendQueueWorkerAssignedPush(sessionID string, workerID uint, booking *models.QueueBooking) {
	if booking == nil {
		return
	}

	worker, err := repositories.GetWorkerBySessionUser(sessionID, workerID)
	if err != nil {
		slog.Warn("push: failed to load worker profile", "worker_id", workerID, "session_id", sessionID, "error", err)
		return
	}
	if !worker.PushNotificationsEnabled {
		return
	}

	tokens, err := repositories.GetUserFcmTokens(workerID)
	if err != nil {
		slog.Warn("push: failed to load worker tokens", "worker_id", workerID, "session_id", sessionID, "error", err)
		return
	}
	if len(tokens) == 0 {
		return
	}

	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		slog.Warn("push: failed to load queue session", "session_id", sessionID, "worker_id", workerID, "error", err)
		return
	}

	workerURL := fmt.Sprintf("/classroom/%s/queue/%s/worker", session.CourseID, session.ID)
	title := "มีงานใหม่"
	body := fmt.Sprintf("คิว #%d โต๊ะ %s", booking.QueueNumber, booking.DeskID)
	if strings.TrimSpace(booking.BookingType) != "" {
		body = fmt.Sprintf("%s - %s", body, booking.BookingType)
	}

	data := map[string]string{
		"type":             "new-task",
		"notificationType": "new-task",
		"title":            title,
		"body":             body,
		"url":              workerURL,
		"workerUrl":        workerURL,
		"sessionId":        session.ID,
		"courseId":         session.CourseID,
		"bookingId":        strconv.Itoa(int(booking.ID)),
		"workerId":         strconv.Itoa(int(workerID)),
		"deskId":           booking.DeskID,
		"deskNumber":       strconv.Itoa(booking.DeskNumber),
		"queueNumber":      strconv.Itoa(booking.QueueNumber),
		"bookingType":      booking.BookingType,
	}

	serverKey := getFCMLegacyServerKey()
	if serverKey == "" {
		slog.Warn("push: FCM server key is not configured; skipping worker push", "session_id", sessionID, "worker_id", workerID)
		return
	}

	for _, token := range tokens {
		status, errMsg, sentAt := sendLegacyFCMMessage(serverKey, token.FcmToken, title, body, workerURL, data)
		if err := recordNotificationLog(token.ID, "new-task", title, body, data, status, errMsg, sentAt); err != nil {
			slog.Warn("push: failed to record notification log", "token_id", token.ID, "session_id", sessionID, "worker_id", workerID, "error", err)
		}
	}
}

func getFCMLegacyServerKey() string {
	for _, key := range []string{"FCM_SERVER_KEY", "FIREBASE_SERVER_KEY", "FCM_LEGACY_SERVER_KEY"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func sendLegacyFCMMessage(serverKey, registrationToken, title, body, clickURL string, data map[string]string) (string, string, *time.Time) {
	requestPayload := map[string]any{
		"to":         registrationToken,
		"priority":   "high",
		"notification": map[string]string{
			"title":        title,
			"body":         body,
			"icon":         "/icons/icon-192.png",
			"click_action": clickURL,
		},
		"data": data,
	}

	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return "failed", err.Error(), nil
	}

	req, err := http.NewRequest(http.MethodPost, fcmLegacyEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "failed", err.Error(), nil
	}
	req.Header.Set("Authorization", "key="+serverKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "failed", err.Error(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failureBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&failureBody)
		return "failed", fmt.Sprintf("fcm http %d", resp.StatusCode), nil
	}

	now := time.Now()
	return "sent", "", &now
}

func recordNotificationLog(tokenID uint, notificationType, title, body string, data map[string]string, status, errMsg string, sentAt *time.Time) error {
	encodedData, err := json.Marshal(data)
	if err != nil {
		encodedData = []byte("{}")
	}
	tokenRef := tokenID

	record := models.NotificationLog{
		FcmTokenID:       &tokenRef,
		NotificationType: notificationType,
		Title:            title,
		Body:             body,
		Data:             datatypes.JSON(encodedData),
		Status:           status,
		ErrorMessage:     errMsg,
		SentAt:           sentAt,
	}
	if sentAt != nil {
		record.DeliveredAt = sentAt
	}

	return config.DB.Create(&record).Error
}