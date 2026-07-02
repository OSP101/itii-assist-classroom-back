package handlers

import (
	"encoding/json"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

type PushSubscriptionKeysInput struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type PushUnsubscribeInput struct {
	Endpoint string `json:"endpoint"`
}

// PushRegisterInput mirrors RegisterTokenInput (notification_handler.go) but
// carries a Web Push subscription instead of an FCM token. Public endpoint —
// students book queue tickets without a JWT session, same as the legacy flow.
type PushRegisterInput struct {
	Endpoint   string                    `json:"endpoint"`
	Keys       PushSubscriptionKeysInput `json:"keys"`
	UserType   string                    `json:"user_type"`
	UserID     *uint                     `json:"user_id"`
	TargetID   json.RawMessage           `json:"target_id"`
	DeviceInfo json.RawMessage           `json:"device_info"`
	StudentID  json.RawMessage           `json:"student_id"`
}

// =============================================================================
// GET /api/push/vapid-public-key
// =============================================================================
func GetVapidPublicKeyHandler(c fiber.Ctx) error {
	publicKey := services.GetVAPIDPublicKey()
	if publicKey == "" {
		return c.Status(503).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Web push ยังไม่ได้ตั้งค่า VAPID key บนเซิร์ฟเวอร์"}})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"public_key": publicKey}})
}

// =============================================================================
// POST /api/push/register (public — worker or student, no JWT required)
// =============================================================================
func RegisterPushHandler(c fiber.Ctx) error {
	var input PushRegisterInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "ข้อมูลไม่ถูกต้อง"}})
	}

	endpoint := strings.TrimSpace(input.Endpoint)
	p256dh := strings.TrimSpace(input.Keys.P256dh)
	auth := strings.TrimSpace(input.Keys.Auth)
	if endpoint == "" || p256dh == "" || auth == "" || input.UserType == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "endpoint, keys และ user_type จำเป็นต้องระบุ"}})
	}

	now := time.Now()
	var deviceInfo datatypes.JSON
	if trimmed := strings.TrimSpace(string(input.DeviceInfo)); trimmed != "" && trimmed != "null" {
		deviceInfo = datatypes.JSON(append([]byte(nil), input.DeviceInfo...))
	}

	sub := models.PushSubscription{
		Endpoint:   endpoint,
		P256dhKey:  p256dh,
		AuthKey:    auth,
		UserType:   input.UserType,
		DeviceInfo: deviceInfo,
		UserAgent:  strings.TrimSpace(c.Get("User-Agent")),
		IsActive:   true,
		LastUsedAt: &now,
	}

	if input.UserType == "worker" {
		if input.UserID == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "user_id is required for workers"}})
		}
		sub.UserID = input.UserID
		sub.SessionID = rawMessageToString(input.TargetID)
	}

	if input.UserType == "student" {
		bookingID, err := rawMessageToUint(input.TargetID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "target_id ต้องเป็น booking_id ที่ถูกต้อง"}})
		}
		sub.BookingID = bookingID
		sub.StudentID = rawMessageToString(input.StudentID)
	}

	res, created, err := repositories.CreateOrUpdatePushSubscription(&sub)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to save push subscription"}})
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"id": res.ID, "created": created}})
}

// =============================================================================
// POST /api/push/unsubscribe (public — a device may unregister itself)
// =============================================================================
func UnsubscribePushHandler(c fiber.Ctx) error {
	var input PushUnsubscribeInput
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.Endpoint) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "endpoint is required"}})
	}

	affected, err := repositories.DeletePushSubscriptionByEndpoint(strings.TrimSpace(input.Endpoint))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to unsubscribe"}})
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"deleted": affected > 0}})
}
