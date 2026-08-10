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

// GetStudentID ดึง studentID จาก context อย่างปลอดภัย (เฉพาะ student session)
func GetStudentID(c fiber.Ctx) (uint, bool) {
	raw := c.Locals("student_id")
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

// extractAccessToken reads the access token from the Authorization header
// first (the mobile app's only path, unchanged), falling back to the
// httpOnly access-token cookie set by the web login/refresh/OAuth flows
// when no Bearer header is present. Reports whether the cookie path was
// used, since only cookie-authenticated requests need the CSRF check below
// — a browser never auto-attaches a custom Authorization header cross-site,
// so Bearer requests (mobile, or any lingering non-cookie web client) are
// inherently CSRF-safe already.
func extractAccessToken(c fiber.Ctx) (token string, viaCookie bool) {
	authHeader := c.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer "), false
	}
	return c.Cookies(utils.AccessTokenCookieName), true
}

// csrfOK reports whether the double-submit-cookie CSRF check passes for a
// request that just authenticated via cookie: the frontend must echo the
// readable csrf_token cookie's value back in the X-CSRF-Token header, which
// a cross-site page cannot read to forge. Side-effect-free — callers decide
// how to respond to a failure (Protected() hard-errors; OptionalProtected()
// falls through as anonymous instead, matching its "optional" contract).
func csrfOK(c fiber.Ctx) bool {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return true
	}

	cookieToken := c.Cookies(utils.CSRFCookieName)
	headerToken := c.Get(utils.CSRFHeaderName)
	return cookieToken != "" && headerToken != "" && cookieToken == headerToken
}

func Protected() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Skip auth for CORS preflight requests
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		tokenString, viaCookie := extractAccessToken(c)
		if tokenString == "" {
			return c.Status(401).JSON(fiber.Map{
				"success": false,
				"message": "ไม่ได้ส่ง Token หรือรูปแบบไม่ถูกต้อง (ต้องการ Bearer Token)",
			})
		}

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

		if viaCookie && !csrfOK(c) {
			return c.Status(403).JSON(fiber.Map{
				"success": false,
				"message": "CSRF token ไม่ถูกต้องหรือหายไป กรุณาโหลดหน้าใหม่แล้วลองอีกครั้ง",
			})
		}

		c.Locals("jti", claims.JTI)

		// For student sessions: set student_id instead of user_id
		if claims.Kind == "s" {
			c.Locals("student_id", claims.UserID)
		} else {
			c.Locals("user_id", claims.UserID)
		}
		c.Locals("user_role", claims.Role)

		return c.Next()
	}
}

func OptionalProtected() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		tokenString, viaCookie := extractAccessToken(c)
		if tokenString == "" {
			return c.Next()
		}

		claims, err := utils.ValidateAccessToken(tokenString)
		if err != nil {
			return c.Next()
		}

		if claims.JTI != "" {
			if _, err := repositories.FindRefreshTokenByJTI(claims.JTI); err != nil {
				return c.Next()
			}
		}

		// Unlike Protected(), an optional session that fails CSRF just
		// falls through as anonymous (skip setting locals, keep going)
		// rather than hard-erroring — the route itself decides whether
		// anonymous access is acceptable.
		if viaCookie && !csrfOK(c) {
			return c.Next()
		}

		c.Locals("jti", claims.JTI)
		if claims.Kind == "s" {
			c.Locals("student_id", claims.UserID)
		} else {
			c.Locals("user_id", claims.UserID)
		}
		c.Locals("user_role", claims.Role)

		return c.Next()
	}
}

func RequireRole(allowedRoles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		allowedRoleSet := make(map[string]struct{}, len(allowedRoles))
		for _, role := range allowedRoles {
			normalized := strings.ToLower(strings.TrimSpace(role))
			if normalized != "" {
				allowedRoleSet[normalized] = struct{}{}
			}
		}

		userRole, _ := c.Locals("user_role").(string)
		userRole = strings.ToLower(strings.TrimSpace(userRole))
		if _, ok := allowedRoleSet[userRole]; ok {
			return c.Next()
		}

		userID, hasUserID := GetUserID(c)
		if hasUserID {
			if user, err := repositories.FindUserByID(userID); err == nil {
				dbRole := strings.ToLower(strings.TrimSpace(user.Role))
				if dbRole != "" {
					c.Locals("user_role", dbRole)
					if _, ok := allowedRoleSet[dbRole]; ok {
						return c.Next()
					}
				}
			}
		}

		if userRole == "" && !hasUserID {
			return c.Status(403).JSON(fiber.Map{
				"success": false,
				"message": "ไม่สามารถตรวจสอบสิทธิ์ได้",
			})
		}

		return c.Status(403).JSON(fiber.Map{
			"success": false,
			"message": "คุณไม่มีสิทธิ์เข้าถึงข้อมูลส่วนนี้ (Forbidden)",
		})
	}
}
