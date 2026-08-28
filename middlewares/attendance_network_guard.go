package middlewares

import (
	"context"
	"encoding/json"
	"itii-assist/config"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// logCheckInGuardEvent records a guard rejection (network-blocked or
// rate-limited) to SystemLog from middleware context, so blocked attempts are
// as visible as successful ones. Best-effort: the guards run before identity is
// resolved, so student/email fields are usually empty here — the raw proxy
// headers and IP are the useful forensic signal.
func logCheckInGuardEvent(c fiber.Ctx, sessionID uint, result string, failedChecks []string, statusCode int) {
	reqID, _, _ := services.ExtractMeta(c)
	services.LogAttendanceCheckIn(config.DB, services.AttendanceCheckInEvent{
		SessionID:    sessionID,
		Result:       result,
		FailedChecks: failedChecks,
		StatusCode:   statusCode,
		IP:           c.IP(),
		RealIP:       c.Get("X-Real-IP"),
		ForwardedFor: c.Get(fiber.HeaderXForwardedFor),
		Host:         c.Get(fiber.HeaderHost),
		UserAgent:    c.Get(fiber.HeaderUserAgent),
		RequestID:    reqID,
		Method:       c.Method(),
		URL:          c.OriginalURL(),
	})
}

// sessionIDFromCheckInRequest resolves the attendance session id from the
// route param or, failing that, the request-body PIN — for logging only.
func sessionIDFromCheckInRequest(c fiber.Ctx) uint {
	if idStr := c.Params("sessionId"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			return uint(id)
		}
	}
	type pinBody struct {
		PinCode string `json:"pin_code"`
	}
	var body pinBody
	if raw := c.Body(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	pin := strings.TrimSpace(body.PinCode)
	if pin == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sessionID, err := repositories.LookupAttendanceSessionIDByPIN(ctx, pin)
	if err != nil {
		return 0
	}
	return sessionID
}

// AttendanceNetworkGuard enforces that physical (non-online) attendance
// check-ins come from a mobile/tablet device, on the campus Wi-Fi network,
// through the canonical faculty domain. Online sessions are exempt. If the
// session/PIN can't be resolved here, the request is passed through and the
// real handler returns its normal "not found/invalid PIN" error.
func AttendanceNetworkGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		sessionType, resolved := resolveAttendanceSessionType(c)
		if !resolved {
			return c.Next()
		}

		result := utils.EvaluateCampusCheckIn(c.Get(fiber.HeaderHost), c.Get(fiber.HeaderUserAgent), c.IP(), sessionType)
		if result.Allowed || result.Exempt {
			return c.Next()
		}

		logCheckInGuardEvent(c, sessionIDFromCheckInRequest(c), services.AttendanceResultNetworkBlocked, result.FailedChecks, 403)
		return c.Status(403).JSON(fiber.Map{
			"success":       false,
			"code":          "ATTENDANCE_NETWORK_BLOCKED",
			"failed_checks": result.FailedChecks,
			"message":       "ไม่สามารถเช็กชื่อได้ กรุณาใช้มือถือ/แท็บเล็ต เชื่อมต่อ WiFi มข. และเข้าผ่านลิงก์ของคณะเท่านั้น",
		})
	}
}

func resolveAttendanceSessionType(c fiber.Ctx) (string, bool) {
	if idStr := c.Params("sessionId"); idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return "", false
		}
		sessionType, err := repositories.GetAttendanceSessionType(uint(id))
		if err != nil {
			return "", false
		}
		return sessionType, true
	}

	type pinBody struct {
		PinCode string `json:"pin_code"`
	}
	var body pinBody
	if raw := c.Body(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	pin := strings.TrimSpace(body.PinCode)
	if pin == "" {
		return "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sessionID, err := repositories.LookupAttendanceSessionIDByPIN(ctx, pin)
	if err != nil {
		return "", false
	}

	sessionType, err := repositories.GetAttendanceSessionType(sessionID)
	if err != nil {
		return "", false
	}
	return sessionType, true
}
