package middlewares

import (
	"itii-assist/repositories"
	"itii-assist/utils"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// GetUserID ดึง userID จาก context อย่างปลอดภัย ป้องกัน panic
func GetUserID(c fiber.Ctx) (uint, bool) {
	raw := c.Locals("user_id")
	if raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case uint:
		return v, true
	case float64:
		return uint(v), true
	}
	return 0, false
}

// GetUserRole ดึง user role จาก context อย่างปลอดภัย
func GetUserRole(c fiber.Ctx) (string, bool) {
	raw := c.Locals("user_role")
	if raw == nil {
		return "", false
	}
	role, ok := raw.(string)
	return role, ok
}

func Protected() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Skip auth for CORS preflight requests
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			return c.Status(401).JSON(fiber.Map{
				"success": false,
				"message": "ไม่ได้ส่ง Token หรือรูปแบบไม่ถูกต้อง (ต้องการ Bearer Token)",
			})
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		claims, err := utils.ValidateAccessToken(tokenString)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{
				"success": false,
				"message": "Token ไม่ถูกต้องหรือหมดอายุแล้ว",
			})
		}

		if claims.JTI != "" {
			if _, err := repositories.FindRefreshTokenByJTI(claims.JTI); err != nil {
				return c.Status(401).JSON(fiber.Map{
					"success": false,
					"message": "Session ถูกยกเลิกหรือหมดอายุแล้ว กรุณาเข้าสู่ระบบใหม่",
				})
			}
		}

		c.Locals("user_id", claims.UserID)
		c.Locals("user_role", claims.Role)
		c.Locals("jti", claims.JTI)

		return c.Next()
	}
}

func RequireRole(allowedRoles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		userRole, ok := c.Locals("user_role").(string)
		if !ok {
			return c.Status(403).JSON(fiber.Map{
				"success": false,
				"message": "ไม่สามารถตรวจสอบสิทธิ์ได้",
			})
		}

		for _, role := range allowedRoles {
			if userRole == role {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"message": "คุณไม่มีสิทธิ์เข้าถึงข้อมูลส่วนนี้ (Forbidden)",
		})
	}
}
