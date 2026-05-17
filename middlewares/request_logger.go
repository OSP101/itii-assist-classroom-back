package middlewares

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

var httpLogger *slog.Logger

func init() {
	httpLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
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
		userAgent := string(c.Request().Header.UserAgent())

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
		durationMs := time.Since(start).Milliseconds()

		// User ID
		userIDRaw := c.Locals("userID")
		userIDStr := ""
		if userIDRaw != nil {
			userIDStr = fmt.Sprintf("%v", userIDRaw)
		}

		status := c.Response().StatusCode()

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
