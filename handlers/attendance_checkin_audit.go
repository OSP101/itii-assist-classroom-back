package handlers

import (
	"errors"
	"itii-assist/config"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

// recordCheckInAttempt captures one attendance check-in attempt to SystemLog,
// filling in the request-derived fields (IP, proxy headers, device, request id)
// from the Fiber context. Handlers supply only the outcome. The raw
// X-Forwarded-For / X-Real-IP headers are captured verbatim so an operator can
// later spot a spoofed campus IP (a header that disagrees with c.IP()).
func recordCheckInAttempt(c fiber.Ctx, ev services.AttendanceCheckInEvent) {
	reqID, _, _ := services.ExtractMeta(c)
	// Every caller of this helper has already parsed the request body, so the
	// official web client will have supplied client_signals; a nil value here
	// is therefore itself worth recording. Guard middleware logs without this
	// helper precisely because it rejects before the body is read.
	ev.ClientSignalsExpected = true
	ev.IP = c.IP()
	ev.RealIP = c.Get("X-Real-IP")
	ev.ForwardedFor = c.Get(fiber.HeaderXForwardedFor)
	ev.Host = c.Get(fiber.HeaderHost)
	ev.UserAgent = c.Get(fiber.HeaderUserAgent)
	ev.RequestID = reqID
	ev.Method = c.Method()
	ev.URL = c.OriginalURL()
	services.LogAttendanceCheckIn(config.DB, ev)
}

// attendanceErrCode extracts the stable machine code from a check-in error so
// it can be recorded and queried (e.g. "ATTENDANCE_INVALID_PIN"). Falls back to
// a generic code for unclassified errors.
func attendanceErrCode(err error) string {
	var publicErr *repositories.AttendancePublicError
	if errors.As(err, &publicErr) {
		return publicErr.Code
	}
	if err == nil {
		return ""
	}
	return "ATTENDANCE_ERROR"
}
