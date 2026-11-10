package middlewares

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

var authSecurityLimiter = &paramRateLimiter{entries: map[string]paramRateLimitEntry{}}

// Account buckets are shared across replicas through Redis. The IP bucket
// also covers attempts that rotate usernames; account keys never trust an IP.
func AuthAttemptGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		var body struct {
			Username string `json:"username"`
			UserID   uint   `json:"userId"`
		}
		_ = json.Unmarshal(c.Body(), &body)
		account := strings.ToLower(strings.TrimSpace(body.Username))
		if body.UserID != 0 {
			account = strconv.FormatUint(uint64(body.UserID), 10)
		}
		// A campus NAT can carry an entire class. Keep per-account limits
		// strict while permitting the expected classroom login burst.
		ipLimit := 6000
		if n, err := strconv.Atoi(os.Getenv("AUTH_MAX_ATTEMPTS_PER_IP_PER_MINUTE")); err == nil && n > 0 {
			ipLimit = n
		}
		checks := []struct {
			key    string
			limit  int
			window time.Duration
		}{{"ip:" + c.IP(), ipLimit, time.Minute}}
		if account != "" {
			checks = append(checks, struct {
				key    string
				limit  int
				window time.Duration
			}{"account:" + account, 20, 5 * time.Minute})
		}
		for _, check := range checks {
			retry, allowed := allowParamGuard("auth:attempt:", check.key, paramRateLimitConfig{Limit: check.limit, Window: check.window}, authSecurityLimiter)
			if !allowed {
				c.Set("Retry-After", strconv.Itoa(retry))
				return c.Status(429).JSON(fiber.Map{"success": false, "message": "ลองยืนยันตัวตนถี่เกินไป กรุณารอสักครู่"})
			}
		}
		return c.Next()
	}
}
