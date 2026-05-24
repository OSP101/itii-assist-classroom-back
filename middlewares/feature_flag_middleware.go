package middlewares

import (
	"itii-assist/repositories"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func RequireAdminFeature(flagKey string) fiber.Handler {
	return func(c fiber.Ctx) error {
		role, _ := GetUserRole(c)
		normalizedRole := strings.ToLower(strings.TrimSpace(role))

		// Admin is always allowed; this flag gate is intended for instructor/TA-facing functions.
		if normalizedRole == "admin" {
			return c.Next()
		}
		if normalizedRole != "instructor" && normalizedRole != "ta" {
			return c.Next()
		}

		if repositories.IsFeatureEnabled(flagKey) {
			return c.Next()
		}

		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "ฟังก์ชันนี้ถูกปิดใช้งานโดยผู้ดูแลระบบ",
			"code":    "FEATURE_DISABLED",
		})
	}
}
