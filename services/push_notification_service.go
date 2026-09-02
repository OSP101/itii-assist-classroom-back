package services

import (
	"fmt"
	"itii-assist/models"
	"itii-assist/repositories"
	"log/slog"
	"strconv"
	"strings"
)

// SendQueueWorkerAssignedPush sends a background push to the assigned worker
// via self-hosted Web Push (webpush-go, VAPID) — the standard Push API, no
// external push provider account (e.g. Firebase) required. Falls back to a
// no-op when the worker has no active subscription or VAPID keys aren't set.
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

	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		slog.Warn("push: failed to load queue session", "session_id", sessionID, "worker_id", workerID, "error", err)
		return
	}

	workerURL := fmt.Sprintf("/classroom/%s/queue/%s/worker", session.CourseID, session.ID)
	title := "มีงานใหม่"
	body := fmt.Sprintf("คิว #%d โต๊ะ %d", booking.QueueNumber, booking.DeskNumber)
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

	SendWebPushToUser(workerID, title, body, data, true)
}
