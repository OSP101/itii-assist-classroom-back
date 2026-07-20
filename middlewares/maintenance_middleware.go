package middlewares

import (
	"itii-assist/repositories"
	"itii-assist/utils"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func MaintenanceModeGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		method := strings.ToUpper(strings.TrimSpace(c.Method()))
		if method == fiber.MethodOptions {
			return c.Next()
		}

		path := c.Path()
		// Always allow: auth, health, public maintenance status, announcements banner, websocket, static uploads
		if strings.HasPrefix(path, "/api/auth/") ||
			path == "/api/health" ||
			path == "/api/maintenance-status" ||
			path == "/api/system-settings/announcements/active" ||
			strings.HasPrefix(path, "/ws") ||
			strings.HasPrefix(path, "/api/uploads/") {
			return c.Next()
		}

		cfg, err := repositories.GetMaintenanceModeConfig()
		if err != nil || !repositories.IsMaintenanceActive(cfg) {
			return c.Next()
		}

		if isWhitelistedAdmin(c, cfg.WhitelistAdminUsers) {
			return c.Next()
		}

		resp := fiber.Map{
			"success":       false,
			"message":       cfg.Message,
			"code":          "MAINTENANCE_MODE",
			"schedule_type": cfg.ScheduleType,
		}
		if cfg.StartTime != nil {
			resp["start_time"] = cfg.StartTime
		}
		if cfg.EndTime != nil {
			resp["end_time"] = cfg.EndTime
		}
		return c.Status(503).JSON(resp)
	}
}

func isWhitelistedAdmin(c fiber.Ctx, whitelist []uint) bool {
	authHeader := c.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := utils.ValidateAccessToken(tokenString)
	if err != nil {
		return false
	}

	role := strings.TrimSpace(strings.ToLower(claims.Role))

	// Admin role always bypasses maintenance mode so they can always turn it off
	if role == "admin" {
		return true
	}

	// Non-admin users only bypass if explicitly whitelisted
	for _, allowed := range whitelist {
		if allowed == claims.UserID {
			return true
		}
	}
	return false
}

func ParseWhitelistUserIDs(rawIDs []string) []uint {
	result := make([]uint, 0, len(rawIDs))
	for _, raw := range rawIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parsed, err := strconv.ParseUint(trimmed, 10, 64)
		if err != nil {
			continue
		}
		result = append(result, uint(parsed))
	}
	return result
}
