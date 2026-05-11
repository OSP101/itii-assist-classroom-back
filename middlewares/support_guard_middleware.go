package middlewares

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

type supportRateLimitConfig struct {
	Limit  int
	Window time.Duration
}

type supportRateLimitEntry struct {
	Count   int
	ResetAt time.Time
}

type supportRateLimiter struct {
	mu      sync.Mutex
	entries map[string]supportRateLimitEntry
}

var publicSupportLimiter = &supportRateLimiter{
	entries: map[string]supportRateLimitEntry{},
}

func SupportTicketGuard() fiber.Handler {
	config := loadSupportRateLimitConfig()

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		retryAfter, allowed := publicSupportLimiter.Allow(supportClientKey(c), config)
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

func (limiter *supportRateLimiter) Allow(key string, config supportRateLimitConfig) (int, bool) {
	if key == "" {
		key = "unknown"
	}

	now := time.Now()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if len(limiter.entries) > 1024 {
		for existingKey, entry := range limiter.entries {
			if now.After(entry.ResetAt) {
				delete(limiter.entries, existingKey)
			}
		}
	}

	entry, exists := limiter.entries[key]
	if !exists || now.After(entry.ResetAt) {
		limiter.entries[key] = supportRateLimitEntry{
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

func loadSupportRateLimitConfig() supportRateLimitConfig {
	limit := readSupportIntEnv("SUPPORT_TICKET_RATE_LIMIT", 3)
	if limit < 1 {
		limit = 3
	}

	windowSeconds := readSupportIntEnv("SUPPORT_TICKET_RATE_WINDOW_SECONDS", 600)
	if windowSeconds < 60 {
		windowSeconds = 60
	}

	return supportRateLimitConfig{
		Limit:  limit,
		Window: time.Duration(windowSeconds) * time.Second,
	}
}

func supportClientKey(c fiber.Ctx) string {
	clientIP := strings.TrimSpace(c.IP())
	if clientIP == "" {
		return "unknown"
	}

	return strings.ToLower(clientIP)
}

func readSupportIntEnv(key string, fallback int) int {
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
