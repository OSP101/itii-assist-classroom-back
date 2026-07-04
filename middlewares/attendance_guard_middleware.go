package middlewares

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/observability"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
)

type attendanceRateLimitConfig struct {
	Limit  int
	Window time.Duration
}

type attendanceRateLimitEntry struct {
	Count   int
	ResetAt time.Time
}

type attendanceRateLimiter struct {
	mu      sync.Mutex
	entries map[string]attendanceRateLimitEntry
}

var publicAttendanceLimiter = &attendanceRateLimiter{
	entries: map[string]attendanceRateLimitEntry{},
}

func AttendanceCheckInGuard() fiber.Handler {
	enabled := attendanceRateLimitEnabled()
	config := loadAttendanceRateLimitConfig()
	backend := loadAttendanceRateLimitBackend()

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		if !enabled {
			return c.Next()
		}

		clientKey := attendanceClientKey(c)
		retryAfter, allowed := allowAttendanceCheckIn(clientKey, config, backend)
		if allowed {
			return c.Next()
		}

		observability.RecordAttendanceRateLimited()

		if retryAfter > 0 {
			c.Set("Retry-After", strconv.Itoa(retryAfter))
		}

		return c.Status(429).JSON(fiber.Map{
			"success": false,
			"message": "ส่งคำขอเช็คชื่อถี่เกินไป กรุณารอสักครู่แล้วลองใหม่อีกครั้ง",
		})
	}
}

func attendanceRateLimitEnabled() bool {
	rawValue := strings.TrimSpace(os.Getenv("ATTENDANCE_CHECKIN_RATE_LIMIT_ENABLED"))
	if rawValue == "" {
		return true
	}
	switch strings.ToLower(rawValue) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func allowAttendanceCheckIn(key string, config attendanceRateLimitConfig, backend string) (int, bool) {
	if strings.EqualFold(backend, "redis") {
		if retryAfter, allowed, ok := allowAttendanceCheckInRedis(key, config); ok {
			return retryAfter, allowed
		}
	}

	return publicAttendanceLimiter.Allow(key, config)
}

func allowAttendanceCheckInRedis(key string, cfg attendanceRateLimitConfig) (int, bool, bool) {
	if config.Redis == nil {
		return 0, false, false
	}
	if key == "" {
		key = "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	redisKey := "attendance:ratelimit:checkin:" + key
	count, err := config.Redis.Incr(ctx, redisKey).Result()
	if err != nil {
		return 0, false, false
	}

	if count == 1 {
		if err := config.Redis.Expire(ctx, redisKey, cfg.Window).Err(); err != nil {
			return 0, false, false
		}
	}

	if count <= int64(cfg.Limit) {
		return 0, true, true
	}

	ttl, ttlErr := config.Redis.TTL(ctx, redisKey).Result()
	if ttlErr != nil || errors.Is(ttlErr, redis.Nil) {
		return 1, false, true
	}

	retryAfter := int(math.Ceil(ttl.Seconds()))
	if retryAfter < 1 {
		retryAfter = 1
	}

	return retryAfter, false, true
}

func (limiter *attendanceRateLimiter) Allow(key string, config attendanceRateLimitConfig) (int, bool) {
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
		limiter.entries[key] = attendanceRateLimitEntry{
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

func loadAttendanceRateLimitConfig() attendanceRateLimitConfig {
	limit := readAttendanceIntEnv("ATTENDANCE_CHECKIN_RATE_LIMIT", 8)
	if limit < 1 {
		limit = 8
	}

	windowSeconds := readAttendanceIntEnv("ATTENDANCE_CHECKIN_RATE_WINDOW_SECONDS", 60)
	if windowSeconds < 15 {
		windowSeconds = 15
	}

	return attendanceRateLimitConfig{
		Limit:  limit,
		Window: time.Duration(windowSeconds) * time.Second,
	}
}

func loadAttendanceRateLimitBackend() string {
	backend := strings.TrimSpace(os.Getenv("ATTENDANCE_RATE_LIMITER_BACKEND"))
	if backend == "" {
		return "memory"
	}
	return strings.ToLower(backend)
}

func attendanceClientKey(c fiber.Ctx) string {
	type attendanceCheckInIdentity struct {
		StudentID       *uint  `json:"student_id"`
		GoogleEmail     string `json:"google_email"`
		GoogleID        string `json:"google_id"`
		ClientRequestID string `json:"client_request_id"`
		PinCode         string `json:"pin_code"`
	}

	identity := attendanceCheckInIdentity{}
	if body := c.Body(); len(body) > 0 {
		_ = json.Unmarshal(body, &identity)
	}

	sessionScope := strings.TrimSpace(c.Params("sessionId"))
	if sessionScope == "" {
		sessionScope = hashedAttendanceKeyPart(identity.PinCode)
	}
	if sessionScope == "" {
		sessionScope = "session-unknown"
	}

	principalScope := ""
	if identity.StudentID != nil && *identity.StudentID > 0 {
		principalScope = "student:" + strconv.FormatUint(uint64(*identity.StudentID), 10)
	} else if email := strings.ToLower(strings.TrimSpace(identity.GoogleEmail)); email != "" {
		principalScope = "email:" + hashedAttendanceKeyPart(email)
	} else if googleID := strings.TrimSpace(identity.GoogleID); googleID != "" {
		principalScope = "gid:" + hashedAttendanceKeyPart(googleID)
	} else if clientRequestID := strings.TrimSpace(identity.ClientRequestID); clientRequestID != "" {
		principalScope = "req:" + hashedAttendanceKeyPart(clientRequestID)
	} else {
		principalScope = "ip:" + strings.TrimSpace(c.IP())
	}

	parts := []string{
		sessionScope,
		principalScope,
	}
	return strings.ToLower(strings.Join(parts, "|"))
}

func hashedAttendanceKeyPart(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

func readAttendanceIntEnv(key string, fallback int) int {
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
