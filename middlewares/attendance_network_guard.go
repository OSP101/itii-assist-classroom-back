package middlewares

import (
	"context"
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/observability"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// logCheckInGuardEvent records a guard rejection (network-blocked or
// rate-limited) to SystemLog from middleware context, so blocked attempts are
// as visible as successful ones. Student identity is only known here when the
// student is signed in via cookie/JWT (OptionalProtected runs earlier in the
// route chain — see services.StudentIDFromContext) — the anonymous PIN+Google-Sign-In
// path only resolves identity later, in the handler, after body parsing.
func logCheckInGuardEvent(c fiber.Ctx, sessionID uint, result string, failedChecks []string, statusCode int) {
	reqID, _, _ := services.ExtractMeta(c)
	services.LogAttendanceCheckIn(config.DB, services.AttendanceCheckInEvent{
		SessionID:    sessionID,
		StudentID:    services.StudentIDFromContext(c),
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

// sessionIDFromCheckInRequest resolves the attendance session id for logging
// from the route param, and only from the route param.
//
// It used to fall back to looking the PIN up in Redis/Postgres. Its one caller
// is the rate-limit rejection path, so that fallback made every throttled
// request — the requests that exist precisely because something is hammering
// the endpoint — perform a backend lookup on the way out. Being rate limited
// has to be cheaper than being served, or the limiter amplifies the load it is
// supposed to shed.
//
// The cost is that a PIN-route 429 is recorded with an empty resource_id. That
// is acceptable: the log still carries IP, User-Agent, request id and time,
// which is the whole story of a throttling event, and the PIN itself must not
// be written to the log to recover the id — it is six digits, so any stored
// derivative of it is brute-forceable back to the live PIN by anyone who can
// read the logs.
func sessionIDFromCheckInRequest(c fiber.Ctx) uint {
	if idStr := c.Params("sessionId"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			return uint(id)
		}
	}
	return 0
}

// AttendanceNetworkGuard enforces that physical (non-online) attendance
// check-ins come from a mobile/tablet device, on the campus Wi-Fi network,
// through the canonical faculty domain. Online sessions are exempt.
//
// There are two different reasons the guard can end up without a session to
// judge, and they must not be treated the same way:
//
//   - The request simply doesn't name a resolvable session: no PIN, a
//     malformed id, or a PIN that matches nothing. Nothing to guard, so pass
//     through and let the handler return its normal "not found / invalid PIN".
//   - The lookup itself failed: DB error, or the 2s deadline expired because
//     the database is saturated. Passing through here would mean the guard
//     silently switches itself off exactly when the system is under the load
//     it sees every time a class starts, and an off-campus desktop would be
//     let straight in. That case fails closed with a retryable 503.
func AttendanceNetworkGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		sessionType, sessionID, found, err := resolveAttendanceSessionType(c)
		if err != nil {
			// Only refuse while the guard is actually enforcing: with the guard
			// switched off there is no verdict to be missing, and online
			// sessions would be exempt anyway.
			if !utils.CampusNetworkGuardEnabled() {
				return c.Next()
			}
			observability.RecordAttendanceGuardUnavailable()
			slog.Error("attendance guard: session lookup failed, failing closed", "error", err, "ip", c.IP())
			logCheckInGuardEvent(c, 0, services.AttendanceResultGuardUnavailable, nil, 503)
			return c.Status(503).JSON(fiber.Map{
				"success": false,
				"code":    "ATTENDANCE_GUARD_UNAVAILABLE",
				"message": "ระบบตรวจสอบเครือข่ายไม่พร้อมใช้งานชั่วคราว กรุณาลองใหม่อีกครั้ง",
			})
		}
		if !found {
			return c.Next()
		}

		result := utils.EvaluateCampusCheckIn(
			c.Get(fiber.HeaderHost),
			c.Get(fiber.HeaderUserAgent),
			c.IP(),
			sessionType,
			utils.ParseDeviceHints(c.Get(utils.DeviceHintsHeader)),
		)
		if result.Allowed || result.Exempt {
			nextErr := c.Next()

			// A pass right after this same IP was blocked for "device" on this
			// same session is the profile of DevTools device emulation (or a
			// UA-switcher) rather than someone who genuinely swapped to their
			// phone — flag it, never block on it.
			//
			// Deliberately after c.Next() and gated on the handler's own
			// verdict: probing on the way in flagged attempts that then failed
			// on a wrong PIN or a closed session, filing an accusation against
			// a check-in that never happened. Skipped when the guard is off too,
			// since nothing can ever have been blocked and the probe would
			// query for a row that cannot exist on every single check-in.
			if result.Allowed && !result.Exempt && utils.CampusNetworkGuardEnabled() && nextErr == nil {
				if status := c.Response().StatusCode(); status >= 200 && status < 300 {
					services.CheckAndLogDeviceGuardFlip(config.DB, sessionID, c.IP(), services.StudentIDFromContext(c), 3*time.Minute)
				}
			}
			return nextErr
		}

		logCheckInGuardEvent(c, sessionID, services.AttendanceResultNetworkBlocked, result.FailedChecks, 403)
		return c.Status(403).JSON(fiber.Map{
			"success":       false,
			"code":          "ATTENDANCE_NETWORK_BLOCKED",
			"failed_checks": result.FailedChecks,
			"message":       "ไม่สามารถเช็กชื่อได้ กรุณาใช้มือถือ/แท็บเล็ต เชื่อมต่อ WiFi มข. และเข้าผ่านลิงก์ของคณะเท่านั้น",
		})
	}
}

// resolveAttendanceSessionType also returns the resolved session ID so
// callers (the guard above) don't need a second, independent PIN lookup just
// to get the same session ID for logging.
//
// The three-way result matters. found=false with err=nil means "this request
// names no session the guard could judge" — no PIN, a malformed id, a PIN that
// matches nothing — so the caller passes through and the handler returns its
// own 400/404. A non-nil err means the lookup itself broke, and the caller
// must not quietly skip the guard on the strength of that. Every lookup shares
// one deadline so a saturated database becomes a fast, retryable failure
// instead of a hung request.
func resolveAttendanceSessionType(c fiber.Ctx) (sessionType string, sessionID uint, found bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if idStr := c.Params("sessionId"); idStr != "" {
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			return "", 0, false, nil
		}
		sessionType, err = repositories.GetAttendanceSessionTypeCtx(ctx, uint(id))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", 0, false, nil
			}
			return "", 0, false, err
		}
		return sessionType, uint(id), true, nil
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
		return "", 0, false, nil
	}

	sessionID, err = repositories.LookupAttendanceSessionIDByPIN(ctx, pin)
	if err != nil {
		// A PIN matching no active session is an ordinary wrong-PIN attempt;
		// anything else is an infrastructure failure.
		if errors.Is(err, repositories.ErrAttendanceInvalidPIN) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}

	sessionType, err = repositories.GetAttendanceSessionTypeCtx(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	return sessionType, sessionID, true, nil
}
