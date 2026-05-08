package handlers

import (
	"errors"
	"fmt"
	"itii-assist/middlewares"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

const (
	attendanceDisplayBootstrapCookie = "attendance_display_bootstrap"
	attendanceDisplaySessionCookie   = "attendance_display_session"
	attendanceDisplaySocketTicketTTL = time.Minute
)

func isSecureRequest(c fiber.Ctx) bool {
	return strings.EqualFold(c.Protocol(), "https")
}

func setAttendanceDisplayCookie(c fiber.Ctx, name string, value string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		SameSite: "Lax",
		Secure:   isSecureRequest(c),
		HTTPOnly: true,
	})
}

func clearAttendanceDisplayCookie(c fiber.Ctx, name string) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		SameSite: "Lax",
		Secure:   isSecureRequest(c),
		HTTPOnly: true,
	})
}

func translateAttendanceDisplayError(err error) (int, string) {
	switch {
	case err == nil:
		return 200, ""
	case errors.Is(err, repositories.ErrAttendanceDisplayUnauthorized):
		return 403, "You do not have permission to manage this display pairing"
	case errors.Is(err, repositories.ErrAttendanceDisplayExpired):
		return 410, "Display pairing has expired"
	case errors.Is(err, repositories.ErrAttendanceDisplayInvalidCode):
		return 400, "Invalid verification code"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return 404, "Display pairing not found"
	default:
		return 500, "Failed to process display pairing"
	}
}

func BootstrapAttendanceDisplayHandler(c fiber.Ctx) error {
	result, err := repositories.BootstrapAttendanceDisplay(c.Get("User-Agent"), c.IP())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to bootstrap display pairing"})
	}
	setAttendanceDisplayCookie(c, attendanceDisplayBootstrapCookie, result.BootstrapSecret, result.ExpiresAt)
	clearAttendanceDisplayCookie(c, attendanceDisplaySessionCookie)
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"pairing_id":    result.PairingID,
			"pairing_token": result.PairingToken,
			"expires_at":    result.ExpiresAt,
		},
	})
}

func GetAttendanceDisplayPairingHandler(c fiber.Ctx) error {
	view, err := repositories.GetAttendanceDisplayPairing(c.Params("token"))
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	return c.JSON(fiber.Map{"success": true, "data": view})
}

func ClaimAttendanceDisplayPairingHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok || userID == 0 {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Authentication required"})
	}
	userRole, _ := middlewares.GetUserRole(c)

	var input struct {
		AttendanceSessionID uint `json:"attendance_session_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.AttendanceSessionID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "attendance_session_id is required"})
	}

	result, err := repositories.ClaimAttendanceDisplayPairing(c.Params("token"), input.AttendanceSessionID, userID, userRole, c.Get("User-Agent"), c.IP())
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func ConfirmAttendanceDisplayHandler(c fiber.Ctx) error {
	bootstrapSecret := strings.TrimSpace(c.Cookies(attendanceDisplayBootstrapCookie))
	if bootstrapSecret == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Display bootstrap cookie is missing or expired"})
	}

	var input struct {
		PairingID        string `json:"pairing_id"`
		VerificationCode string `json:"verification_code"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.PairingID) == "" || strings.TrimSpace(input.VerificationCode) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "pairing_id and verification_code are required"})
	}

	sessionSecret, current, err := repositories.ConfirmAttendanceDisplayPairing(strings.TrimSpace(input.PairingID), strings.TrimSpace(input.VerificationCode), bootstrapSecret, c.Get("User-Agent"), c.IP())
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}

	setAttendanceDisplayCookie(c, attendanceDisplaySessionCookie, sessionSecret, current.ExpiresAt)
	clearAttendanceDisplayCookie(c, attendanceDisplayBootstrapCookie)
	return c.JSON(fiber.Map{"success": true, "data": current})
}

func GetAttendanceDisplayCurrentHandler(c fiber.Ctx) error {
	sessionSecret := strings.TrimSpace(c.Cookies(attendanceDisplaySessionCookie))
	if sessionSecret == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Display session is not active"})
	}
	current, err := repositories.GetAttendanceDisplayCurrent(sessionSecret)
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		if status == 410 || status == 404 {
			clearAttendanceDisplayCookie(c, attendanceDisplaySessionCookie)
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	return c.JSON(fiber.Map{"success": true, "data": current})
}

func GetAttendanceDisplayRecordsHandler(c fiber.Ctx) error {
	sessionSecret := strings.TrimSpace(c.Cookies(attendanceDisplaySessionCookie))
	if sessionSecret == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Display session is not active"})
	}
	records, err := repositories.GetAttendanceDisplayRecords(sessionSecret)
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		if status == 410 || status == 404 {
			clearAttendanceDisplayCookie(c, attendanceDisplaySessionCookie)
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	payload := make([]fiber.Map, 0, len(records))
	for _, record := range records {
		payload = append(payload, fiber.Map{
			"id":                    record.ID,
			"attendance_session_id": record.AttendanceSessionID,
			"student_id":            record.StudentID,
			"check_in_time":         record.CheckInTime,
			"status":                record.Status,
			"pin_verified":          record.PinVerified,
			"location_verified":     record.LocationVerified,
			"distance_meters":       record.DistanceMeters,
			"note":                  nullableAttendanceString(record.Note),
			"updated_at":            record.UpdatedAt,
			"created_at":            record.CreatedAt,
			"student": fiber.Map{
				"id":         record.Student.ID,
				"student_id": record.Student.StudentID,
				"full_name":  record.Student.FullName,
			},
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": payload})
}

func GetAttendanceDisplaySocketTicketHandler(c fiber.Ctx) error {
	sessionSecret := strings.TrimSpace(c.Cookies(attendanceDisplaySessionCookie))
	if sessionSecret == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Display session is not active"})
	}
	grant, err := repositories.ResolveAttendanceDisplayGrant(sessionSecret)
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		if status == 410 || status == 404 {
			clearAttendanceDisplayCookie(c, attendanceDisplaySessionCookie)
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	ticket, expiresAt, err := realtime.IssueDisplaySocketTicket(fmt.Sprintf("display-attendance-%d", grant.AttendanceSessionID), attendanceDisplaySocketTicketTTL)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to issue display socket ticket"})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"ticket": ticket, "expires_at": expiresAt}})
}

func HeartbeatAttendanceDisplayHandler(c fiber.Ctx) error {
	sessionSecret := strings.TrimSpace(c.Cookies(attendanceDisplaySessionCookie))
	if sessionSecret == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Display session is not active"})
	}
	current, err := repositories.TouchAttendanceDisplayGrant(sessionSecret, c.Get("User-Agent"), c.IP())
	if err != nil {
		status, message := translateAttendanceDisplayError(err)
		if status == 410 || status == 404 {
			clearAttendanceDisplayCookie(c, attendanceDisplaySessionCookie)
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	return c.JSON(fiber.Map{"success": true, "data": current})
}

func RevokeAttendanceDisplayGrantHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok || userID == 0 {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Authentication required"})
	}
	userRole, _ := middlewares.GetUserRole(c)
	if err := repositories.RevokeAttendanceDisplayGrant(c.Params("id"), userID, userRole, c.Get("User-Agent"), c.IP()); err != nil {
		status, message := translateAttendanceDisplayError(err)
		return c.Status(status).JSON(fiber.Map{"success": false, "message": message})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Display access revoked"})
}
