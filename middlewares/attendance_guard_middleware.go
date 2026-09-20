package middlewares

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/observability"
	"itii-assist/services"
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

// sessionAttendanceLimiter is a second, session-wide bucket. Where
// publicAttendanceLimiter is scoped per (session|principal) and so is evaded by
// an attacker rotating client_request_id, this one counts every attempt against
// a single session regardless of who sends it — a backstop against bulk
// scripting / PIN brute-forcing. It is opt-in (see attendanceSessionRateLimit)
// because a whole class checking in at once is legitimately high-volume.
var sessionAttendanceLimiter = &attendanceRateLimiter{
	entries: map[string]attendanceRateLimitEntry{},
}

func AttendanceCheckInGuard() fiber.Handler {
	enabled := attendanceRateLimitEnabled()
	config := loadAttendanceRateLimitConfig()
	backend := loadAttendanceRateLimitBackend()
	sessionConfig, sessionEnabled := loadAttendanceSessionRateLimitConfig()

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		if !enabled {
			return c.Next()
		}

		clientKey := attendanceClientKey(c)
		retryAfter, allowed := allowAttendanceCheckIn(clientKey, config, backend)
		if !allowed {
			return rejectAttendanceRateLimited(c, retryAfter)
		}

		// Session-wide backstop: catches a script that rotates its identity to
		// dodge the per-principal limit above. Shares allowAttendanceCheckIn's
		// Redis-first-then-memory-fallback path (plan.md ระยะ 2.2) — this used
		// to always use the in-memory sessionAttendanceLimiter regardless of
		// backend, which meant the "whole class checking in at once" backstop
		// reset independently per process the instant there was more than one,
		// silently multiplying the effective limit.
		if sessionEnabled {
			sessionKey := "sess:" + attendanceSessionScope(c)
			sessionRetryAfter, sessionAllowed := allowAttendanceSessionCheckIn(sessionKey, sessionConfig, backend)
			if !sessionAllowed {
				return rejectAttendanceRateLimited(c, sessionRetryAfter)
			}
		}

		return c.Next()
	}
}

// attendancePrincipalKey is the "who" half of a rate-limit key, shared by
// every attendance limiter in this file: the authenticated student id
// (services.StudentIDFromContext, set only after OptionalProtected verifies
// a real JWT) if present, else the caller's IP — never anything the request
// body claims. See attendanceClientKey's comment for why.
func attendancePrincipalKey(c fiber.Ctx) string {
	if studentID := services.StudentIDFromContext(c); studentID > 0 {
		return "student:" + strconv.FormatUint(uint64(studentID), 10)
	}
	return "ip:" + strings.TrimSpace(c.IP())
}

// AttendanceInfoRateLimit / AttendancePinLookupRateLimit /
// AttendanceVerifyStudentRateLimit are lighter defense-in-depth limits for
// the public attendance endpoints AttendanceCheckInGuard doesn't cover
// (plan.md ระยะ 2.2). nginx's own zones (nginx.conf.template: api_ip,
// pin_ip) are the primary defense for these; this only matters when a
// request reaches the backend without going through nginx, or once Redis
// makes the limit shared across more than one backend process.
//
// Three separate functions rather than one parameterized by "include session
// scope or not": AttendancePinLookupRateLimit specifically must NOT key on
// the PIN itself (attendanceSessionScope falls back to hashing pin_code from
// the body when no :sessionId route param exists, which is exactly this
// route) — a brute-forcer sending a different guess every request would get
// a fresh bucket every time and the limit would never engage, the same class
// of evasion attendanceClientKey's rewrite above just closed for a different
// field. Keeping that route's guard deliberately principal-only avoids
// reintroducing the same bug by accident via a shared helper.
func attendancePublicGuard(limit int, windowSeconds int, keyFn func(c fiber.Ctx) string) fiber.Handler {
	enabled := attendanceRateLimitEnabled()
	backend := loadAttendanceRateLimitBackend()
	cfg := attendanceRateLimitConfig{Limit: limit, Window: time.Duration(windowSeconds) * time.Second}

	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}
		if !enabled {
			return c.Next()
		}

		retryAfter, allowed := allowAttendanceCheckIn(keyFn(c), cfg, backend)
		if !allowed {
			return rejectAttendanceRateLimited(c, retryAfter)
		}
		return c.Next()
	}
}

func AttendanceInfoRateLimit() fiber.Handler {
	return attendancePublicGuard(10, 60, func(c fiber.Ctx) string {
		return "info:" + attendanceSessionScope(c) + "|" + attendancePrincipalKey(c)
	})
}

func AttendancePinLookupRateLimit() fiber.Handler {
	return attendancePublicGuard(5, 60, func(c fiber.Ctx) string {
		return "pinlookup:" + attendancePrincipalKey(c)
	})
}

func AttendanceVerifyStudentRateLimit() fiber.Handler {
	return attendancePublicGuard(10, 60, func(c fiber.Ctx) string {
		return "verifystudent:" + attendancePrincipalKey(c)
	})
}

func rejectAttendanceRateLimited(c fiber.Ctx, retryAfter int) error {
	observability.RecordAttendanceRateLimited()
	logCheckInGuardEvent(c, sessionIDFromCheckInRequest(c), services.AttendanceResultRateLimited, nil, 429)

	if retryAfter > 0 {
		c.Set("Retry-After", strconv.Itoa(retryAfter))
	}

	return c.Status(429).JSON(fiber.Map{
		"success": false,
		"message": "ส่งคำขอเช็กชื่อถี่เกินไป กรุณารอสักครู่แล้วลองใหม่อีกครั้ง",
	})
}

// attendanceSessionScope returns just the session portion of the rate-limit key
// (route param, or the PIN hash when checking in by PIN).
func attendanceSessionScope(c fiber.Ctx) string {
	if sessionScope := strings.TrimSpace(c.Params("sessionId")); sessionScope != "" {
		return strings.ToLower(sessionScope)
	}
	type pinOnly struct {
		PinCode string `json:"pin_code"`
	}
	body := pinOnly{}
	if raw := c.Body(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	if hashed := hashedAttendanceKeyPart(body.PinCode); hashed != "" {
		return hashed
	}
	return "session-unknown"
}

// loadAttendanceSessionRateLimitConfig reads the opt-in session-wide limit.
// Disabled unless ATTENDANCE_CHECKIN_SESSION_RATE_LIMIT is set to a positive
// number, so existing deployments are unaffected until an operator turns it on.
// Set it comfortably above the largest class size expected to check in within
// one window (default window matches the per-principal window).
func loadAttendanceSessionRateLimitConfig() (attendanceRateLimitConfig, bool) {
	limit := readAttendanceIntEnv("ATTENDANCE_CHECKIN_SESSION_RATE_LIMIT", 0)
	if limit < 1 {
		return attendanceRateLimitConfig{}, false
	}

	windowSeconds := readAttendanceIntEnv("ATTENDANCE_CHECKIN_SESSION_RATE_WINDOW_SECONDS", 60)
	if windowSeconds < 15 {
		windowSeconds = 15
	}

	return attendanceRateLimitConfig{
		Limit:  limit,
		Window: time.Duration(windowSeconds) * time.Second,
	}, true
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

// allowAttendanceSessionCheckIn is allowAttendanceCheckIn's counterpart for
// the session-wide backstop: same Redis-key namespace (the "sess:" prefix
// already baked into the key its one caller passes keeps it from colliding
// with per-principal keys), same Redis-first-then-memory-fallback shape, but
// its own dedicated in-memory limiter — sessionAttendanceLimiter, not
// publicAttendanceLimiter — since the two are different tiers with different
// configs and must not share entries.
func allowAttendanceSessionCheckIn(key string, config attendanceRateLimitConfig, backend string) (int, bool) {
	if strings.EqualFold(backend, "redis") {
		if retryAfter, allowed, ok := allowAttendanceCheckInRedis(key, config); ok {
			return retryAfter, allowed
		}
	}

	return sessionAttendanceLimiter.Allow(key, config)
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

// loadAttendanceRateLimitBackend defaults to "redis" (plan.md ระยะ 2.2) so
// the limit is shared across every process that ends up handling check-ins,
// not reset per-process. allowAttendanceCheckIn already falls back to the
// in-memory limiter whenever Redis errors (allowAttendanceCheckInRedis's
// third return value), so a Redis outage degrades this to the old
// per-process behavior rather than either blocking everyone (fail-closed) or
// disabling the limit outright — nginx's own zones (nginx.conf.template)
// stay in front of this regardless of which backend is active.
func loadAttendanceRateLimitBackend() string {
	backend := strings.TrimSpace(os.Getenv("ATTENDANCE_RATE_LIMITER_BACKEND"))
	if backend == "" {
		return "redis"
	}
	return strings.ToLower(backend)
}

// attendanceClientKey builds the rate-limit bucket key: session scope +
// principal scope.
//
// The principal half used to prefer whatever identity field the request body
// happened to carry — student_id, then google_email, then google_id, then
// client_request_id — all of which are client-supplied and unverified at
// this point in the middleware chain (OptionalProtected has only set
// c.Locals("student_id") when a real JWT cookie/token was present; nothing
// here checked that Locals value at all). A caller who wanted more than the
// configured limit just had to send a different fake student_id (or
// google_email, or a fresh client_request_id) on every request to land in a
// fresh, never-before-seen bucket every time — the rate limit never actually
// engaged for that caller (found during plan.md ระยะ 2 review).
//
// The fix: the only "who is this" the principal scope trusts is
// services.StudentIDFromContext(c), which reads the value OptionalProtected
// set after verifying a JWT — i.e. something the caller cannot pick for
// themselves per-request. Everyone else (anonymous / Google-token check-ins,
// which is most of this endpoint's real traffic) is keyed by IP, which nginx
// already resolves through real_ip_module to the genuine client address (see
// nginx.conf.template) — not by anything the request body claims.
// client_request_id/google_email/google_id remain exactly what they always
// were meant for: StudentCheckIn's idempotency cache
// (repositories/attendance_runtime_repo.go), never a rate-limit identity.
func attendanceClientKey(c fiber.Ctx) string {
	parts := []string{
		attendanceSessionScope(c),
		attendancePrincipalKey(c),
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
