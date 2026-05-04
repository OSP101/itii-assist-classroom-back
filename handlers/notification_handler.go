package handlers

import (
	"encoding/json"
	"itii-assist/models"
	"itii-assist/repositories"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

// =============================================================================
// Input structs
// =============================================================================

type RegisterTokenInput struct {
	FcmToken   string          `json:"fcm_token"`
	UserType   string          `json:"user_type"`
	UserID     *uint           `json:"user_id"`
	TargetID   json.RawMessage `json:"target_id"`
	DeviceInfo json.RawMessage `json:"device_info"`
	StudentID  json.RawMessage `json:"student_id"`
}

type UnregisterTokenInput struct {
	FcmToken string `json:"fcm_token"`
}

type UpdateBookingTokenInput struct {
	FcmToken  string `json:"fcm_token"`
	BookingID *uint  `json:"booking_id"`
}

func rawMessageToString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	return strings.Trim(trimmed, `"`)
}

func rawMessageToUint(raw json.RawMessage) (*uint, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var asUint uint
	if err := json.Unmarshal(raw, &asUint); err == nil {
		return &asUint, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		parsed, err := strconv.ParseUint(strings.TrimSpace(asString), 10, 64)
		if err != nil {
			return nil, err
		}
		result := uint(parsed)
		return &result, nil
	}

	parsed, err := strconv.ParseUint(strings.Trim(trimmed, `"`), 10, 64)
	if err != nil {
		return nil, err
	}
	result := uint(parsed)
	return &result, nil
}

// =============================================================================
// POST /api/notifications/register
// =============================================================================
func RegisterTokenHandler(c fiber.Ctx) error {
	var input RegisterTokenInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "ข้อมูลไม่ถูกต้อง"}})
	}

	if input.FcmToken == "" || input.UserType == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "fcm_token and user_type are required"}})
	}

	now := time.Now()
	var deviceInfo datatypes.JSON
	if trimmed := strings.TrimSpace(string(input.DeviceInfo)); trimmed != "" && trimmed != "null" {
		deviceInfo = datatypes.JSON(append([]byte(nil), input.DeviceInfo...))
	}

	tokenData := models.FcmToken{
		FcmToken:   input.FcmToken,
		UserType:   input.UserType,
		DeviceInfo: deviceInfo,
		IsActive:   true,
		LastUsedAt: &now,
	}

	// For workers
	if input.UserType == "worker" {
		if input.UserID == nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "user_id is required for workers"}})
		}
		tokenData.UserID = input.UserID
		tokenData.SessionID = rawMessageToString(input.TargetID)
	}

	// For students
	if input.UserType == "student" {
		bookingID, err := rawMessageToUint(input.TargetID)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "target_id ต้องเป็น booking_id ที่ถูกต้อง"}})
		}
		tokenData.BookingID = bookingID
		tokenData.StudentID = rawMessageToString(input.StudentID)
	}

	res, created, err := repositories.CreateOrUpdateFcmToken(&tokenData)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to register FCM token"}})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"id": res.ID, "created": created},
	})
}

// =============================================================================
// POST /api/notifications/unregister
// =============================================================================
func UnregisterTokenHandler(c fiber.Ctx) error {
	var input UnregisterTokenInput
	if err := c.Bind().JSON(&input); err != nil || input.FcmToken == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "fcm_token is required"}})
	}

	affected, err := repositories.DeleteFcmToken(input.FcmToken)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to unregister FCM token"}})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"deleted": affected > 0},
	})
}

// =============================================================================
// POST /api/notifications/update-booking
// =============================================================================
func UpdateBookingTokenHandler(c fiber.Ctx) error {
	var input UpdateBookingTokenInput
	if err := c.Bind().JSON(&input); err != nil || input.FcmToken == "" || input.BookingID == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "fcm_token and booking_id are required"}})
	}

	affected, err := repositories.UpdateStudentBookingID(input.FcmToken, input.BookingID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to update booking for token"}})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"updated": affected > 0},
	})
}

// =============================================================================
// GET /api/notifications/tokens
// =============================================================================
func GetUserTokensHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)

	tokens, err := repositories.GetUserFcmTokens(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to get user tokens"}})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    tokens,
	})
}

// =============================================================================
// GET /api/notifications/logs
// =============================================================================
func GetNotificationLogsHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	logs, err := repositories.GetUserNotificationLogs(userID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "Failed to get notification logs"}})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    logs,
	})
}
