package middlewares

import (
	"fmt"
	"itii-assist/observability"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

var (
	httpLogger      *slog.Logger
	accessLogActive bool
)

func init() {
	accessLogActive = accessLogEnabled()
	httpLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: accessLogLevel(),
	}))
}

// accessLogEnabled gates the per-request access log line only. Panic recovery,
// the Prometheus metrics and 5xx/panic logging in this middleware always run —
// turning off the access log must never turn off error visibility or let a
// handler panic escape and kill the process.
//
// Writing two JSON lines per request to a container's stdout is a genuine
// bottleneck under load (it is a blocking write), so this switch exists to be
// turned off during load tests and on very busy deployments.
func accessLogEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENABLE_REQUEST_LOGGER"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// accessLogLevel defaults to info. The previous hardcoded debug level meant the
// handler could never filter anything out.
func accessLogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RequestLogger returns a Fiber middleware that logs every HTTP request as a
// structured JSON record and recovers from handler panics.
func RequestLogger() fiber.Handler {
	return func(c fiber.Ctx) error {
		path := c.Path()

		// Skip health check and WebSocket paths
		if path == "/api/health" || strings.HasPrefix(path, "/ws") {
			return c.Next()
		}

		// Request ID
		reqID := uuid.New().String()
		c.Locals("requestID", reqID)
		c.Set("X-Request-ID", reqID)

		// Trace ID
		traceID := c.Get("X-Trace-ID")
		if traceID == "" {
			traceID = reqID
		}
		c.Locals("traceID", traceID)

		// Start time
		start := time.Now()
		c.Locals("startTime", start)

		method := c.Method()
		ip := c.IP()
		userAgent := ""
		if accessLogActive {
			// Only materialised when it will actually be logged: this copies
			// the header out of the request buffer on every single request.
			userAgent = string(c.Request().Header.UserAgent())
		}

		// Panic recovery wrapping c.Next()
		var nextErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()
					httpLogger.Error("panic recovered",
						slog.String("request_id", reqID),
						slog.String("trace_id", traceID),
						slog.String("method", method),
						slog.String("path", path),
						slog.String("ip", ip),
						slog.Any("panic_value", r),
						slog.String("stack", string(stack)),
					)
					nextErr = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"success": false,
						"message": "internal server error",
					})
				}
			}()
			nextErr = c.Next()
		}()

		// Duration
		duration := time.Since(start)
		durationMs := duration.Milliseconds()

		// User ID (staff sessions use user_id, student sessions use student_id)
		userIDRaw := c.Locals("user_id")
		if userIDRaw == nil {
			userIDRaw = c.Locals("userID")
		}
		if userIDRaw == nil {
			userIDRaw = c.Locals("student_id")
		}
		userIDStr := ""
		if userIDRaw != nil {
			userIDStr = strings.TrimSpace(fmt.Sprintf("%v", userIDRaw))
		}

		status := c.Response().StatusCode()
		routePath := path
		if route := c.Route(); route != nil {
			if candidate := strings.TrimSpace(route.Path); candidate != "" {
				routePath = candidate
			}
		}

		if routePath != "/metrics" {
			observability.RecordHTTPRequest(routePath, method, status, duration)
		}
		if userID, ok := GetUserID(c); ok {
			observability.TrackAuthenticatedPrincipal("user", userID)
		}
		if studentID, ok := GetStudentID(c); ok {
			observability.TrackAuthenticatedPrincipal("student", studentID)
		}

		// Errors are always reported; the routine access line is what the
		// switch silences.
		if !accessLogActive && status < 500 {
			return nextErr
		}

		attrs := []any{
			slog.String("request_id", reqID),
			slog.String("trace_id", traceID),
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Int64("duration_ms", durationMs),
			slog.String("ip", ip),
			slog.String("user_agent", userAgent),
			slog.String("user_id", userIDStr),
		}

		switch {
		case status >= 500:
			httpLogger.Error("request completed", attrs...)
		case status >= 400:
			httpLogger.Warn("request completed", attrs...)
		default:
			httpLogger.Info("request completed", attrs...)
		}

		return nextErr
	}
}
