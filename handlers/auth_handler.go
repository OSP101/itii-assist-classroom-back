package handlers

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

// =============================================================================
// Input structs
// =============================================================================

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RefreshInput struct {
	RefreshToken string `json:"refreshToken"`
}

type LogoutInput struct {
	RefreshToken string `json:"refreshToken"`
}

type ChangePasswordInput struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type ForceChangePasswordInput struct {
	NewPassword string `json:"newPassword"`
}

type UpdateProfileInput struct {
	FullName        string `json:"fullName"`
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
}

type UpdatePreferencesInput struct {
	Theme    string `json:"theme"`
	FontSize string `json:"fontSize"`
	Language string `json:"language"`
}

type ForgotPasswordInput struct {
	Email string `json:"email"`
}

type ValidateResetTokenInput struct {
	Token string `json:"token"`
}

type ResetPasswordInput struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

// =============================================================================
// Helper: สร้าง safe user object (ไม่มีรหัสผ่าน)
// =============================================================================

func normalizeThemePreference(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return "system"
	}
}

func normalizeFontSizePreference(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sm":
		return "sm"
	case "lg":
		return "lg"
	default:
		return "md"
	}
}

func normalizeLanguagePreference(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "th"
	}
}

func userPreferences(u *models.User) fiber.Map {
	return fiber.Map{
		"theme":    normalizeThemePreference(u.ThemePreference),
		"fontSize": normalizeFontSizePreference(u.FontSizePreference),
		"language": normalizeLanguagePreference(u.LanguagePreference),
	}
}

func safeUser(u *models.User) fiber.Map {
	var twoFactorMethod interface{}
	if u.TwoFactorMethod != "" {
		twoFactorMethod = u.TwoFactorMethod
	}
	return fiber.Map{
		"id":                      u.ID,
		"username":                u.Username,
		"role":                    u.Role,
		"full_name":               u.FullName,
		"email":                   u.Email,
		"google_id":               u.GoogleID,
		"provider":                u.Provider,
		"avatar":                  u.Avatar,
		"preferences":             userPreferences(u),
		"is_active":               u.IsActive,
		"must_change_password":    u.MustChangePassword,
		"two_factor_enabled":      u.TwoFactorEnabled,
		"two_factor_method":       twoFactorMethod,
		"two_factor_confirmed_at": u.TwoFactorConfirmedAt,
		"created_at":              u.CreatedAt,
		"updated_at":              u.UpdatedAt,
	}
}

// =============================================================================
// AuthHandler — struct-based handler with audit logger
// =============================================================================

type AuthHandler struct {
	auditLogger *services.AuditLogger
}

func NewAuthHandler(auditLogger *services.AuditLogger) *AuthHandler {
	return &AuthHandler{auditLogger: auditLogger}
}

// =============================================================================
// POST /api/auth/login
// =============================================================================

func (h *AuthHandler) Login(c fiber.Ctx) error {
	var input LoginInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.Username == "" || input.Password == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกชื่อผู้ใช้และรหัสผ่าน"})
	}

	user, err := repositories.FindUserByUsername(input.Username)
	if err != nil {
		reqID, traceID, ip := services.ExtractMeta(c)
		h.auditLogger.LogSystem(c.Context(), services.SystemEvent{
			Action:    services.ActionAuthLoginFailed,
			LogType:   "auth",
			Severity:  "warn",
			IPAddress: ip,
			UserAgent: c.Get("User-Agent"),
			RequestID: reqID,
			TraceID:   traceID,
			Detail:    map[string]any{"username_attempted": input.Username, "reason": "user_not_found"},
		})
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง"})
	}

	if strings.EqualFold(strings.TrimSpace(user.Role), "student") {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "บัญชีนักศึกษาต้องเข้าสู่ระบบผ่าน Google"})
	}

	if !utils.CheckPasswordHash(input.Password, user.PasswordHash) {
		reqID, traceID, ip := services.ExtractMeta(c)
		h.auditLogger.LogSystem(c.Context(), services.SystemEvent{
			Action:    services.ActionAuthLoginFailed,
			LogType:   "auth",
			Severity:  "warn",
			IPAddress: ip,
			UserAgent: c.Get("User-Agent"),
			RequestID: reqID,
			TraceID:   traceID,
			Detail:    map[string]any{"username_attempted": input.Username, "reason": "invalid_password"},
		})
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง"})
	}

	if user.TwoFactorEnabled {
		// Mask email for display
		maskedEmail := ""
		if user.Email != "" {
			// A simple mask implementation
			emailObj := []rune(user.Email)
			atIdx := -1
			for i, r := range emailObj {
				if r == '@' {
					atIdx = i
					break
				}
			}
			if atIdx > 2 {
				maskedEmail = string(emailObj[:2]) + "***" + string(emailObj[atIdx:])
			} else {
				maskedEmail = user.Email // fallback
			}
		}

		return c.JSON(fiber.Map{
			"success": true,
			"message": "Two-factor authentication required",
			"data": fiber.Map{
				"requiresTwoFactor": true,
				"twoFactorMethod":   user.TwoFactorMethod,
				"userId":            user.ID,
				"email":             maskedEmail,
			},
		})
	}

	accessToken, refreshToken, jti, err := utils.GenerateTokenPair(user.ID, user.Role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้าง Token ไม่สำเร็จ"})
	}

	if err := repositories.CreateRefreshToken(&models.RefreshToken{
		JTI:       jti,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Meta:      buildSessionMeta(c, "local"),
	}); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึก Token ไม่สำเร็จ"})
	}

	if utils.IsWebClient(c) {
		utils.SetAuthCookies(c, accessToken, refreshToken)
	}

	reqID, traceID, ip := services.ExtractMeta(c)
	h.auditLogger.LogSystem(c.Context(), services.SystemEvent{
		ActorUserID:  user.ID,
		Action:       services.ActionAuthLoginSuccess,
		LogType:      "auth",
		Severity:     "info",
		ResourceType: "user",
		ResourceID:   strconv.Itoa(int(user.ID)),
		IPAddress:    ip,
		UserAgent:    c.Get("User-Agent"),
		RequestID:    reqID,
		TraceID:      traceID,
		Detail:       map[string]any{"user_id": user.ID, "username": user.Username, "method": "password"},
	})
	return c.JSON(fiber.Map{
		"success": true,
		"message": "เข้าสู่ระบบสำเร็จ",
		"data": fiber.Map{
			"user":               safeUser(user),
			"accessToken":        accessToken,
			"refreshToken":       refreshToken,
			"mustChangePassword": user.MustChangePassword,
		},
	})
}

// =============================================================================
// POST /api/auth/refresh
// =============================================================================

func RefreshHandler(c fiber.Ctx) error {
	var input RefreshInput
	_ = c.Bind().JSON(&input) // web clients send an empty/no body and rely on the refresh cookie below

	refreshToken := input.RefreshToken
	if refreshToken == "" {
		refreshToken = c.Cookies(utils.RefreshTokenCookieName)
	}
	if refreshToken == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาส่ง refreshToken"})
	}

	claims, err := utils.ValidateRefreshToken(refreshToken)
	if err != nil {
		if utils.IsWebClient(c) {
			utils.ClearAuthCookies(c)
		}
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Refresh Token ไม่ถูกต้องหรือหมดอายุ"})
	}

	tokenRecord, err := repositories.FindRefreshTokenByJTI(claims.JTI)
	if err != nil {
		if utils.IsWebClient(c) {
			utils.ClearAuthCookies(c)
		}
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Refresh Token ถูกยกเลิกแล้ว"})
	}

	// Student session refresh
	if claims.Kind == "s" || tokenRecord.Kind == "s" {
		student, studentErr := repositories.FindStudentByID(tokenRecord.UserID)
		if studentErr != nil || !student.IsActive {
			if utils.IsWebClient(c) {
				utils.ClearAuthCookies(c)
			}
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษาหรือถูกระงับการใช้งาน"})
		}
		if err := repositories.RevokeRefreshToken(claims.JTI); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถยกเลิก Token เดิมได้"})
		}
		accessToken, refreshToken, newJTI, err := utils.GenerateStudentTokenPair(student.ID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้าง Token ใหม่ไม่สำเร็จ"})
		}
		if err := repositories.CreateRefreshToken(&models.RefreshToken{
			JTI:       newJTI,
			UserID:    student.ID,
			Kind:      "s",
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			Meta:      buildSessionMeta(c, "student"),
		}); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึก Token ใหม่ไม่สำเร็จ"})
		}
		if utils.IsWebClient(c) {
			utils.SetAuthCookies(c, accessToken, refreshToken)
		}
		return c.JSON(fiber.Map{
			"success": true,
			"message": "ต่ออายุ Token สำเร็จ",
			"data":    fiber.Map{"accessToken": accessToken, "refreshToken": refreshToken},
		})
	}

	user, err := repositories.FindUserByID(tokenRecord.UserID)
	if err != nil || !user.IsActive {
		if utils.IsWebClient(c) {
			utils.ClearAuthCookies(c)
		}
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้หรือถูกระงับการใช้งาน"})
	}

	// Rotate: revoke old token
	if err := repositories.RevokeRefreshToken(claims.JTI); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถยกเลิก Token เดิมได้"})
	}

	// Issue new pair
	accessToken, refreshToken, newJTI, err := utils.GenerateTokenPair(user.ID, user.Role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้าง Token ใหม่ไม่สำเร็จ"})
	}

	if err := repositories.CreateRefreshToken(&models.RefreshToken{
		JTI:       newJTI,
		UserID:    user.ID,
		Kind:      "u",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Meta:      buildSessionMeta(c, "refresh"),
	}); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึก Token ใหม่ไม่สำเร็จ"})
	}

	if utils.IsWebClient(c) {
		utils.SetAuthCookies(c, accessToken, refreshToken)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "ต่ออายุ Token สำเร็จ",
		"data": fiber.Map{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
		},
	})
}

// =============================================================================
// POST /api/auth/logout
// =============================================================================

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var jti string
	var actorID uint
	var input LogoutInput
	_ = c.Bind().JSON(&input)

	refreshToken := input.RefreshToken
	if refreshToken == "" {
		refreshToken = c.Cookies(utils.RefreshTokenCookieName)
	}
	if refreshToken != "" {
		claims, err := utils.ValidateRefreshToken(refreshToken)
		if err == nil {
			jti = claims.JTI
			actorID = claims.UserID
			_ = repositories.RevokeRefreshToken(claims.JTI)
		}
	}
	if utils.IsWebClient(c) {
		utils.ClearAuthCookies(c)
	}
	reqID, traceID, ip := services.ExtractMeta(c)
	h.auditLogger.LogSystem(c.Context(), services.SystemEvent{
		ActorUserID: actorID,
		Action:      services.ActionAuthLogout,
		LogType:     "auth",
		Severity:    "info",
		IPAddress:   ip,
		UserAgent:   c.Get("User-Agent"),
		RequestID:   reqID,
		TraceID:     traceID,
		Detail:      map[string]any{"session_jti": jti},
	})
	return c.JSON(fiber.Map{"success": true, "message": "ออกจากระบบสำเร็จ"})
}

// =============================================================================
// POST /api/auth/forgot-password
// =============================================================================

func ForgotPasswordHandler(c fiber.Ctx) error {
	var input ForgotPasswordInput
	if err := c.Bind().JSON(&input); err != nil || input.Email == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกอีเมล"})
	}

	successMessage := "หากอีเมลนี้มีในระบบ เราจะส่งลิงก์สำหรับรีเซ็ตรหัสผ่านไปยังอีเมลของคุณ"
	user, err := repositories.FindActiveUserByEmail(input.Email)
	if err == nil {
		if tokenRecord, tokenErr := repositories.CreatePasswordResetToken(user.ID, time.Hour); tokenErr == nil {
			if emailErr := services.SendPasswordResetEmail(user, tokenRecord.Token); emailErr != nil {
				services.LogEmailDeliveryError("forgot password", emailErr)
			}
			{
				dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
				config.DB.Create(&models.SystemLog{
					LogType:     "auth",
					Severity:    "info",
					ActorUserID: &user.ID,
					Action:      "password_reset_requested",
					IPAddress:   c.IP(),
					UserAgent:   c.Get("User-Agent"),
					DeviceType:  dt,
					Browser:     br,
					OS:          osn,
				})
			}
		}
	} else {
		{
			dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
			config.DB.Create(&models.SystemLog{
				LogType:    "auth",
				Severity:   "warn",
				Action:     "password_reset_nonexistent_email",
				IPAddress:  c.IP(),
				UserAgent:  c.Get("User-Agent"),
				DeviceType: dt,
				Browser:    br,
				OS:         osn,
			})
		}
	}

	return c.JSON(fiber.Map{"success": true, "message": successMessage})
}

// =============================================================================
// POST /api/auth/validate-reset-token
// =============================================================================

func ValidateResetTokenHandler(c fiber.Ctx) error {
	var input ValidateResetTokenInput
	if err := c.Bind().JSON(&input); err != nil || input.Token == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Token is required"})
	}

	if _, err := repositories.FindValidPasswordResetToken(input.Token); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ลิงก์รีเซ็ตรหัสผ่านไม่ถูกต้องหรือหมดอายุแล้ว"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Token is valid",
		"data":    fiber.Map{"valid": true},
	})
}

// =============================================================================
// POST /api/auth/reset-password
// =============================================================================

func ResetPasswordHandler(c fiber.Ctx) error {
	var input ResetPasswordInput
	if err := c.Bind().JSON(&input); err != nil || input.Token == "" || input.NewPassword == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Token and new password are required"})
	}
	if len(input.NewPassword) < 6 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านต้องมีอย่างน้อย 6 ตัวอักษร"})
	}

	tokenRecord, err := repositories.FindValidPasswordResetToken(input.Token)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ลิงก์รีเซ็ตรหัสผ่านไม่ถูกต้องหรือหมดอายุแล้ว"})
	}

	user, err := repositories.FindUserByID(tokenRecord.UserID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}
	if !user.IsActive {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "บัญชีผู้ใช้ถูกปิดการใช้งาน"})
	}

	hashed, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
	}

	user.PasswordHash = hashed
	user.MustChangePassword = false
	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกรหัสผ่านใหม่ไม่สำเร็จ"})
	}
	if err := repositories.MarkPasswordResetTokenUsed(input.Token); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "อัปเดตสถานะ token ไม่สำเร็จ"})
	}
	_ = repositories.RevokeAllUserRefreshTokens(user.ID)

	{
		dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
		config.DB.Create(&models.SystemLog{
			LogType:     "auth",
			Severity:    "info",
			ActorUserID: &user.ID,
			Action:      "password_reset_completed",
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
			DeviceType:  dt,
			Browser:     br,
			OS:          osn,
		})
	}

	return c.JSON(fiber.Map{"success": true, "message": "รหัสผ่านถูกเปลี่ยนเรียบร้อยแล้ว กรุณาเข้าสู่ระบบด้วยรหัสผ่านใหม่"})
}

// =============================================================================
// GET /api/auth/me
// =============================================================================

func GetMeHandler(c fiber.Ctx) error {
	// Student session: return student data from students table
	if studentID, ok := middlewares.GetStudentID(c); ok {
		student, err := repositories.FindStudentByID(studentID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"user": fiber.Map{
					"id":         student.ID,
					"student_id": student.StudentID,
					"username":   student.StudentID,
					"full_name":  student.FullName,
					"email":      student.Email,
					"role":       "student",
					"provider":   "google",
					"is_active":  student.IsActive,
					"avatar":     "",
					"extra":      student.Extra,
					"preferences": fiber.Map{
						"theme":    "system",
						"fontSize": "md",
						"language": "th",
					},
					"created_at": student.CreatedAt,
					"updated_at": student.UpdatedAt,
				},
			},
		})
	}

	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data":    fiber.Map{"user": safeUser(user)},
	})
}

// =============================================================================
// POST /api/auth/change-password
// =============================================================================

func ChangePasswordHandler(c fiber.Ctx) error {
	var input ChangePasswordInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.NewPassword == "" || len(input.NewPassword) < 6 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านใหม่ต้องมีอย่างน้อย 6 ตัวอักษร"})
	}

	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	if !utils.CheckPasswordHash(input.CurrentPassword, user.PasswordHash) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านปัจจุบันไม่ถูกต้อง"})
	}

	hashed, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
	}

	user.PasswordHash = hashed
	user.MustChangePassword = false
	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกไม่สำเร็จ"})
	}

	// Revoke all refresh tokens ของ user นี้
	_ = repositories.RevokeAllUserRefreshTokens(userID)

	// Log
	{
		dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
		config.DB.Create(&models.SystemLog{
			Action:      "change_password",
			LogType:     "auth",
			Severity:    "info",
			ActorUserID: &userID,
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
			DeviceType:  dt,
			Browser:     br,
			OS:          osn,
		})
	}

	return c.JSON(fiber.Map{"success": true, "message": "เปลี่ยนรหัสผ่านสำเร็จ กรุณาเข้าสู่ระบบใหม่"})
}

// =============================================================================
// POST /api/auth/force-change-password
// =============================================================================

func ForceChangePasswordHandler(c fiber.Ctx) error {
	var input ForceChangePasswordInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.NewPassword == "" || len(input.NewPassword) < 6 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านต้องมีอย่างน้อย 6 ตัวอักษร"})
	}

	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	if !user.MustChangePassword {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่จำเป็นต้องเปลี่ยนรหัสผ่าน"})
	}

	hashed, err := utils.HashPassword(input.NewPassword)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
	}

	user.PasswordHash = hashed
	user.MustChangePassword = false
	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกไม่สำเร็จ"})
	}

	_ = repositories.RevokeAllUserRefreshTokens(userID)

	return c.JSON(fiber.Map{"success": true, "message": "เปลี่ยนรหัสผ่านสำเร็จ กรุณาเข้าสู่ระบบใหม่"})
}

// =============================================================================
// PUT /api/auth/profile
// =============================================================================

func UpdateProfileHandler(c fiber.Ctx) error {
	var input UpdateProfileInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	if !utils.CheckPasswordHash(input.CurrentPassword, user.PasswordHash) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านไม่ถูกต้อง"})
	}

	if input.FullName != "" {
		user.FullName = input.FullName
	}
	if input.Email != "" && input.Email != user.Email {
		if repositories.IsEmailExists(input.Email, userID) {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
		}
		user.Email = input.Email
	}

	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "อัปเดตโปรไฟล์สำเร็จ",
		"data":    fiber.Map{"user": safeUser(user)},
	})
}

// =============================================================================
// PUT /api/auth/preferences
// =============================================================================

func UpdatePreferencesHandler(c fiber.Ctx) error {
	var input UpdatePreferencesInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	if strings.TrimSpace(input.Theme) != "" {
		user.ThemePreference = normalizeThemePreference(input.Theme)
	} else {
		user.ThemePreference = normalizeThemePreference(user.ThemePreference)
	}

	if strings.TrimSpace(input.FontSize) != "" {
		user.FontSizePreference = normalizeFontSizePreference(input.FontSize)
	} else {
		user.FontSizePreference = normalizeFontSizePreference(user.FontSizePreference)
	}

	if strings.TrimSpace(input.Language) != "" {
		user.LanguagePreference = normalizeLanguagePreference(input.Language)
	} else {
		user.LanguagePreference = normalizeLanguagePreference(user.LanguagePreference)
	}

	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกการตั้งค่าไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "อัปเดตการตั้งค่าสำเร็จ",
		"data": fiber.Map{
			"preferences": userPreferences(user),
			"user":        safeUser(user),
		},
	})
}

// =============================================================================
// POST /api/auth/avatar
// =============================================================================

func UploadAvatarHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	fileHeader, err := c.FormFile("avatar")
	if err != nil || fileHeader == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "No file uploaded"})
	}

	file, err := fileHeader.Open()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Unable to read uploaded file"})
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, 5*1024*1024+1))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Unable to read uploaded file"})
	}
	if len(content) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "No file uploaded"})
	}
	if len(content) > 5*1024*1024 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไฟล์มีขนาดใหญ่เกินไป สูงสุด 5MB"})
	}

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Only image files are allowed"})
	}

	base64Avatar := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(content)
	user.Avatar = base64Avatar
	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update avatar"})
	}

	{
		dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
		config.DB.Create(&models.SystemLog{
			LogType:     "auth",
			Severity:    "info",
			ActorUserID: &userID,
			Action:      "avatar_updated",
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
			DeviceType:  dt,
			Browser:     br,
			OS:          osn,
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Avatar updated successfully",
		"data": fiber.Map{
			"avatar": base64Avatar,
		},
	})
}

// =============================================================================
// DELETE /api/auth/avatar
// =============================================================================

func RemoveAvatarHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	user, err := repositories.FindUserByID(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	user.Avatar = ""
	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to remove avatar"})
	}

	{
		dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
		config.DB.Create(&models.SystemLog{
			LogType:     "auth",
			Severity:    "info",
			ActorUserID: &userID,
			Action:      "avatar_removed",
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
			DeviceType:  dt,
			Browser:     br,
			OS:          osn,
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Avatar removed successfully",
		"data": fiber.Map{
			"user": safeUser(user),
		},
	})
}

// =============================================================================
// GET /api/auth/sessions
// =============================================================================

type sessionMeta struct {
	IP        string `json:"ip"`
	UserAgent string `json:"userAgent"`
	Provider  string `json:"provider"`
}

func buildSessionMeta(c fiber.Ctx, provider string) datatypes.JSON {
	metaJSON, _ := json.Marshal(sessionMeta{
		IP:        c.IP(),
		UserAgent: string(c.Request().Header.UserAgent()),
		Provider:  provider,
	})
	return datatypes.JSON(metaJSON)
}

func loadNearestSessionMetaFallback(userID uint, token models.RefreshToken) sessionMeta {
	if token.CreatedAt.IsZero() {
		return sessionMeta{}
	}

	var previous models.RefreshToken
	err := config.DB.
		Where("user_id = ? AND id <> ? AND meta IS NOT NULL AND meta::text <> '' AND created_at >= ? AND created_at <= ?", userID, token.ID, token.CreatedAt.Add(-5*time.Minute), token.CreatedAt).
		Order("created_at DESC").
		First(&previous).Error
	if err != nil || previous.Meta == nil {
		return sessionMeta{}
	}

	var meta sessionMeta
	if err := json.Unmarshal(previous.Meta, &meta); err != nil {
		return sessionMeta{}
	}
	return meta
}

func parseSessionDisplay(meta sessionMeta) (ip, browser, osName, device, provider string) {
	ip = strings.TrimSpace(meta.IP)
	provider = strings.TrimSpace(meta.Provider)

	deviceType, parsedBrowser, parsedOS := utils.ParseUserAgent(strings.TrimSpace(meta.UserAgent))
	browser = parsedBrowser
	osName = parsedOS

	switch deviceType {
	case "mobile":
		device = "mobile"
	case "tablet":
		device = "tablet"
	case "bot":
		device = "bot"
	case "desktop":
		device = "desktop"
	default:
		device = ""
	}

	return ip, browser, osName, device, provider
}

func GetSessionsHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	// Current JTI comes from the access token stored in locals by Protected()
	currentJTI, _ := c.Locals("jti").(string)

	var tokens []models.RefreshToken
	config.DB.Where("user_id = ? AND revoked = ? AND expires_at > ?", userID, false, time.Now()).
		Order("created_at DESC").
		Find(&tokens)

	type sessionItem struct {
		ID        uint      `json:"id"`
		JTI       string    `json:"jti"`
		IP        string    `json:"ip"`
		Browser   string    `json:"browser"`
		OS        string    `json:"os"`
		Device    string    `json:"device"`
		Provider  string    `json:"provider"`
		LoginAt   time.Time `json:"loginAt"`
		ExpiresAt time.Time `json:"expiresAt"`
		IsCurrent bool      `json:"isCurrent"`
	}

	result := make([]sessionItem, 0, len(tokens))
	for _, t := range tokens {
		var meta sessionMeta
		if t.Meta != nil {
			_ = json.Unmarshal(t.Meta, &meta)
		}
		isCurrent := currentJTI != "" && t.JTI == currentJTI

		if strings.TrimSpace(meta.IP) == "" || strings.TrimSpace(meta.UserAgent) == "" {
			fallback := loadNearestSessionMetaFallback(userID, t)
			if strings.TrimSpace(meta.IP) == "" {
				meta.IP = fallback.IP
			}
			if strings.TrimSpace(meta.UserAgent) == "" {
				meta.UserAgent = fallback.UserAgent
			}
			if strings.TrimSpace(meta.Provider) == "" {
				meta.Provider = fallback.Provider
			}
		}
		if isCurrent {
			if strings.TrimSpace(meta.IP) == "" {
				meta.IP = c.IP()
			}
			if strings.TrimSpace(meta.UserAgent) == "" {
				meta.UserAgent = string(c.Request().Header.UserAgent())
			}
		}

		ip, browser, osName, device, provider := parseSessionDisplay(meta)
		if browser == "" {
			browser = "Unknown browser"
		}
		if osName == "" {
			osName = "Unknown device"
		}
		if device == "" {
			device = "desktop"
		}

		result = append(result, sessionItem{
			ID:        t.ID,
			JTI:       t.JTI,
			IP:        ip,
			Browser:   browser,
			OS:        osName,
			Device:    device,
			Provider:  provider,
			LoginAt:   t.CreatedAt,
			ExpiresAt: t.ExpiresAt,
			IsCurrent: isCurrent,
		})
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"sessions": result}})
}

// =============================================================================
// DELETE /api/auth/sessions/:sessionId
// =============================================================================

func (h *AuthHandler) RevokeSession(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	sessionID, err := strconv.Atoi(c.Params("sessionId"))
	if err != nil || sessionID <= 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid sessionId"})
	}

	var token models.RefreshToken
	if err := config.DB.Where("id = ? AND user_id = ? AND revoked = ?", uint(sessionID), userID, false).
		First(&token).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	if err := config.DB.Model(&token).Update("revoked", true).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to revoke session"})
	}

	reqID, traceID, ip := services.ExtractMeta(c)
	h.auditLogger.LogSystem(c.Context(), services.SystemEvent{
		ActorUserID: userID,
		Action:      services.ActionAuthTokenRevoked,
		LogType:     "auth",
		Severity:    "warn",
		IPAddress:   ip,
		UserAgent:   c.Get("User-Agent"),
		RequestID:   reqID,
		TraceID:     traceID,
		Detail:      map[string]any{"revoked_jti": token.JTI, "reason": "manual_revoke"},
	})
	return c.JSON(fiber.Map{"success": true, "message": "Session revoked successfully"})
}

// =============================================================================
// POST /api/auth/sessions/revoke-all
// =============================================================================

func RevokeAllSessionsHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	result := config.DB.Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked = ?", userID, false).
		Update("revoked", true)

	return c.JSON(fiber.Map{
		"success": true,
		"message": "ยกเลิกเซสชันทั้งหมดสำเร็จ",
		"data":    fiber.Map{"revokedCount": result.RowsAffected},
	})
}
