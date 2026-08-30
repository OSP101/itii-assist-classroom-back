package middlewares

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// This file holds small IP+resource-scoped rate limiters for public,
// unauthenticated endpoints that read or mutate something sensitive keyed by
// a URL param (desk PIN lookup, queue session status toggling). Each guard
// gets its own bucket namespace so a burst on one doesn't affect the other.
type paramRateLimitConfig struct {
	Limit  int
	Window time.Duration
}

type paramRateLimitEntry struct {
	Count   int
	ResetAt time.Time
}

type paramRateLimiter struct {
	mu      sync.Mutex
	entries map[string]paramRateLimitEntry
}

func (limiter *paramRateLimiter) Allow(key string, config paramRateLimitConfig) (int, bool) {
	if key == "" {
		key = "unknown"
	}

	now := time.Now()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if len(limiter.entries) > 4096 {
		for existingKey, entry := range limiter.entries {
			if now.After(entry.ResetAt) {
				delete(limiter.entries, existingKey)
			}
		}
	}

	entry, exists := limiter.entries[key]
	if !exists || now.After(entry.ResetAt) {
		limiter.entries[key] = paramRateLimitEntry{
			Count:   1,
			ResetAt: now.Add(config.Window),
		}
		return 0, true
	}

	if entry.Count >= config.Limit {
		retryAfter := int(time.Until(entry.ResetAt).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return retryAfter, false
	}

	entry.Count++
	limiter.entries[key] = entry
	return 0, true
}

func paramClientKey(c fiber.Ctx, namespace string, paramName string) string {
	clientIP := strings.TrimSpace(c.IP())
	if clientIP == "" {
		clientIP = "unknown"
	}

	paramValue := strings.TrimSpace(c.Params(paramName))
	if paramValue == "" {
		paramValue = "unknown"
	}

	return strings.ToLower(namespace + "|" + clientIP + "|" + paramValue)
}

func readParamGuardIntEnv(key string, fallback int) int {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(rawValue)
	if err != nil {
		return fallback
	}

	return parsed
}

// DeskLookupGuard rate-limits GET /api/desks/:deskId. The endpoint is public
// by design (the student QR scanner has no session yet) and returns the
// active queue session's pin_code, so without a throttle here a script could
// scrape PINs across every desk in a building. Keyed per desk+IP so a normal
// student re-scanning one desk isn't affected.
var publicDeskLimiter = &paramRateLimiter{entries: map[string]paramRateLimitEntry{}}

func DeskLookupGuard() fiber.Handler {
	config := paramRateLimitConfig{
		Limit:  readParamGuardIntEnv("DESK_LOOKUP_RATE_LIMIT", 20),
		Window: time.Duration(readParamGuardIntEnv("DESK_LOOKUP_RATE_WINDOW_SECONDS", 60)) * time.Second,
	}
	if config.Limit < 1 {
		config.Limit = 20
	}
	if config.Window < 15*time.Second {
		config.Window = 60 * time.Second
	}

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		key := paramClientKey(c, "desk", "deskId")
		retryAfter, allowed := publicDeskLimiter.Allow(key, config)
		if allowed {
			return c.Next()
		}

		if retryAfter > 0 {
			c.Set("Retry-After", strconv.Itoa(retryAfter))
		}
		return c.Status(429).JSON(fiber.Map{
			"success": false,
			"message": "ส่งคำขอถี่เกินไป กรุณารอสักครู่แล้วลองใหม่อีกครั้ง",
		})
	}
}

// QueueStatusPublicGuard rate-limits POST /api/queue/sessions/:sessionId/status,
// the unauthenticated projector endpoint used to pause/resume a queue session
// from the classroom display (see app/queue/projector/[sessionId]/page.tsx).
// It has no session/JWT to key on, so this throttles per session+IP.
var publicQueueStatusLimiter = &paramRateLimiter{entries: map[string]paramRateLimitEntry{}}

func QueueStatusPublicGuard() fiber.Handler {
	config := paramRateLimitConfig{
		Limit:  readParamGuardIntEnv("QUEUE_STATUS_PUBLIC_RATE_LIMIT", 20),
		Window: time.Duration(readParamGuardIntEnv("QUEUE_STATUS_PUBLIC_RATE_WINDOW_SECONDS", 60)) * time.Second,
	}
	if config.Limit < 1 {
		config.Limit = 20
	}
	if config.Window < 15*time.Second {
		config.Window = 60 * time.Second
	}

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		key := paramClientKey(c, "queue-status", "sessionId")
		retryAfter, allowed := publicQueueStatusLimiter.Allow(key, config)
		if allowed {
			return c.Next()
		}

		if retryAfter > 0 {
			c.Set("Retry-After", strconv.Itoa(retryAfter))
		}
		return c.Status(429).JSON(fiber.Map{
			"success": false,
			"message": "ส่งคำขอถี่เกินไป กรุณารอสักครู่แล้วลองใหม่อีกครั้ง",
		})
	}
}
