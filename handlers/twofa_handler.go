package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
)

// =============================================================================
// 2FA encryption helpers (AES-256-GCM using TWO_FACTOR_ENCRYPTION_KEY env)
// =============================================================================

func get2FAKey() []byte {
	k := os.Getenv("TWO_FACTOR_ENCRYPTION_KEY")
	if k == "" {
		k = "00000000000000000000000000000000" // 32-byte fallback for dev
	}
	b := []byte(k)
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded, b)
	return padded
}

func encryptSecret(plaintext string) (string, error) {
	key := get2FAKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptSecret(ciphertext string) (string, error) {
	key := get2FAKey()
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// =============================================================================
// Backup codes helpers
// =============================================================================

// generateBackupCodes creates 10 random 8-char alphanumeric codes
func generateBackupCodes() ([]string, []string, error) {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars
	const count = 10
	const length = 8
	plain := make([]string, count)
	hashed := make([]string, count)
	buf := make([]byte, length)
	for i := 0; i < count; i++ {
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, err
		}
		var code strings.Builder
		for _, b := range buf {
			code.WriteByte(chars[int(b)%len(chars)])
		}
		plain[i] = code.String()
		h, err := bcrypt.GenerateFromPassword([]byte(plain[i]), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		hashed[i] = string(h)
	}
	return plain, hashed, nil
}

// verifyBackupCode checks a code against hashed backup codes.
// Returns (matched index, ok).
func verifyBackupCode(code string, hashedJSON datatypes.JSON) (int, bool) {
	if hashedJSON == nil {
		return -1, false
	}
	var hashed []interface{}
	if err := json.Unmarshal(hashedJSON, &hashed); err != nil {
		return -1, false
	}
	for i, h := range hashed {
		if h == nil {
			continue
		}
		hs, ok := h.(string)
		if !ok {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hs), []byte(code)) == nil {
			return i, true
		}
	}
	return -1, false
}

// maskEmail replaces middle part of email local with ***
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	local := parts[0]
	if len(local) <= 2 {
		return local + "@" + parts[1]
	}
	return local[:2] + "***@" + parts[1]
}

// =============================================================================
// GET /api/auth/2fa/status
// =============================================================================

func Get2FAStatusHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var user models.User
	if err := config.DB.Select("id, two_factor_enabled, two_factor_method, two_factor_confirmed_at").
		First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	var method interface{}
	if user.TwoFactorMethod != "" {
		method = user.TwoFactorMethod
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"enabled":     user.TwoFactorEnabled,
			"method":      method,
			"confirmedAt": user.TwoFactorConfirmedAt,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/setup/totp
// =============================================================================

func Setup2FATOTPHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	if user.TwoFactorEnabled {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "การยืนยันตัวตนสองขั้นตอนเปิดใช้งานอยู่แล้ว กรุณาปิดก่อนเพื่อตั้งค่าใหม่",
		})
	}

	issuer := os.Getenv("TWO_FACTOR_ISSUER")
	if issuer == "" {
		issuer = "ITII Assist Classroom"
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: user.Username,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดในการสร้าง TOTP"})
	}

	// Generate QR code as base64 PNG
	qrPNG, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดในการสร้าง QR code"})
	}
	qrBase64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG)

	encSecret, err := encryptSecret(key.Secret())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดภายใน"})
	}

	expiresAt := time.Now().Add(15 * time.Minute)

	// Upsert: delete existing then create
	config.DB.Where("user_id = ? AND method = ?", userID, "totp").Delete(&models.TwoFactorPending{})
	config.DB.Create(&models.TwoFactorPending{
		UserID:    userID,
		Method:    "totp",
		Secret:    encSecret,
		ExpiresAt: expiresAt,
	})

	// Cleanup expired records (best-effort)
	config.DB.Where("expires_at < ?", time.Now()).Delete(&models.TwoFactorPending{})

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"qrCode": qrBase64,
			"secret": key.Secret(),
			"issuer": issuer,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/setup/email
// =============================================================================

func Setup2FAEmailHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	if user.TwoFactorEnabled {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "การยืนยันตัวตนสองขั้นตอนเปิดใช้งานอยู่แล้ว กรุณาปิดก่อนเพื่อตั้งค่าใหม่",
		})
	}

	if user.Email == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "กรุณาเพิ่มอีเมลในโปรไฟล์ก่อนเปิดใช้งานการยืนยันทางอีเมล",
		})
	}

	emailCode := generateNumericCode(6)
	emailCodeExp := time.Now().Add(5 * time.Minute)
	expiresAt := time.Now().Add(15 * time.Minute)

	// Upsert
	config.DB.Where("user_id = ? AND method = ?", userID, "email").Delete(&models.TwoFactorPending{})
	config.DB.Create(&models.TwoFactorPending{
		UserID:             userID,
		Method:             "email",
		Secret:             "email",
		EmailCode:          emailCode,
		EmailCodeExpiresAt: &emailCodeExp,
		ExpiresAt:          expiresAt,
	})

	// Cleanup expired records (best-effort)
	config.DB.Where("expires_at < ?", time.Now()).Delete(&models.TwoFactorPending{})

	if err := services.SendTwoFactorCodeEmail(&user, emailCode, "setup"); err != nil {
		services.LogEmailDeliveryError("2fa setup email", err)
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "ไม่สามารถส่งอีเมลรหัสยืนยันได้ในขณะนี้",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("ส่งรหัสยืนยันไปที่ %s แล้ว", user.Email),
		"data": fiber.Map{
			"email": maskEmail(user.Email),
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/verify  (enable 2FA after setup)
// =============================================================================

type Verify2FAInput struct {
	Code   string `json:"code"`
	Method string `json:"method"`
}

func Verify2FAHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input Verify2FAInput
	if err := c.Bind().JSON(&input); err != nil || input.Code == "" || input.Method == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสยืนยัน"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	var pending models.TwoFactorPending
	if err := config.DB.Where("user_id = ? AND method = ?", userID, input.Method).First(&pending).Error; err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่า กรุณาเริ่มใหม่อีกครั้ง"})
	}

	if time.Now().After(pending.ExpiresAt) {
		config.DB.Delete(&pending)
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "หมดเวลา กรุณาเริ่มใหม่อีกครั้ง"})
	}

	isValid := false

	switch input.Method {
	case "totp":
		dec, err := decryptSecret(pending.Secret)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดในการตรวจสอบ"})
		}
		isValid = totp.Validate(input.Code, dec)

	case "email":
		if pending.EmailCodeExpiresAt != nil && time.Now().After(*pending.EmailCodeExpiresAt) {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสหมดอายุแล้ว กรุณาขอรหัสใหม่"})
		}
		isValid = pending.EmailCode == input.Code

	default:
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "method ไม่ถูกต้อง"})
	}

	if !isValid {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสไม่ถูกต้อง"})
	}

	plain, hashed, err := generateBackupCodes()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดภายใน"})
	}

	hashedJSON, err := json.Marshal(hashed)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดภายใน"})
	}

	now := time.Now()
	updates := map[string]interface{}{
		"two_factor_enabled":      true,
		"two_factor_method":       input.Method,
		"two_factor_backup_codes": datatypes.JSON(hashedJSON),
		"two_factor_confirmed_at": &now,
	}
	if input.Method == "totp" {
		updates["two_factor_secret"] = pending.Secret
	} else {
		updates["two_factor_secret"] = ""
	}

	if err := config.DB.Model(&user).Updates(updates).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดในการบันทึก"})
	}

	config.DB.Delete(&pending)

	return c.JSON(fiber.Map{
		"success": true,
		"message": "เปิดใช้งานการยืนยันตัวตนสองขั้นตอนสำเร็จ",
		"data": fiber.Map{
			"backupCodes": plain,
			"enabled":     true,
			"method":      input.Method,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/resend-email
// =============================================================================

func Resend2FAEmailHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil || user.Email == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบอีเมลในโปรไฟล์"})
	}

	var pending models.TwoFactorPending
	if err := config.DB.Where("user_id = ? AND method = ?", userID, "email").First(&pending).Error; err != nil {
		// Create new pending if user has email 2FA enabled
		if user.TwoFactorEnabled && user.TwoFactorMethod == "email" {
			emailCode := generateNumericCode(6)
			emailCodeExp := time.Now().Add(5 * time.Minute)
			pending = models.TwoFactorPending{
				UserID:             userID,
				Method:             "email",
				Secret:             "email",
				EmailCode:          emailCode,
				EmailCodeExpiresAt: &emailCodeExp,
				ExpiresAt:          time.Now().Add(15 * time.Minute),
			}
			config.DB.Create(&pending)
			if err := services.SendTwoFactorCodeEmail(&user, emailCode, "setup"); err != nil {
				services.LogEmailDeliveryError("2fa resend setup email", err)
				return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถส่งอีเมลรหัสยืนยันได้ในขณะนี้"})
			}
			return c.JSON(fiber.Map{"success": true, "message": "ส่งรหัสใหม่แล้ว"})
		}
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่า กรุณาเริ่มใหม่อีกครั้ง"})
	}

	emailCode := generateNumericCode(6)
	emailCodeExp := time.Now().Add(5 * time.Minute)
	config.DB.Model(&pending).Updates(map[string]interface{}{
		"email_code":            emailCode,
		"email_code_expires_at": emailCodeExp,
	})

	if err := services.SendTwoFactorCodeEmail(&user, emailCode, "setup"); err != nil {
		services.LogEmailDeliveryError("2fa resend email", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถส่งอีเมลรหัสยืนยันได้ในขณะนี้"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "ส่งรหัสใหม่แล้ว"})
}

// =============================================================================
// POST /api/auth/2fa/disable
// =============================================================================

type Disable2FAInput struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func Disable2FAHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input Disable2FAInput
	if err := c.Bind().JSON(&input); err != nil || input.Password == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสผ่าน"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านไม่ถูกต้อง"})
	}

	if user.TwoFactorEnabled {
		if input.Code == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัส 2FA"})
		}

		isValid := false

		if user.TwoFactorMethod == "totp" && user.TwoFactorSecret != "" {
			dec, err := decryptSecret(user.TwoFactorSecret)
			if err == nil {
				isValid = totp.Validate(input.Code, dec)
			}
		}

		if !isValid && user.TwoFactorMethod == "email" {
			var pending models.TwoFactorPending
			if err := config.DB.Where("user_id = ? AND method = ?", userID, "email").First(&pending).Error; err == nil {
				if pending.EmailCode == input.Code && (pending.EmailCodeExpiresAt == nil || time.Now().Before(*pending.EmailCodeExpiresAt)) {
					isValid = true
					config.DB.Delete(&pending)
				}
			}
		}

		if !isValid {
			idx, valid := verifyBackupCode(input.Code, user.TwoFactorBackupCodes)
			if valid {
				isValid = true
				var codes []interface{}
				json.Unmarshal(user.TwoFactorBackupCodes, &codes)
				codes[idx] = nil
				newCodes, _ := json.Marshal(codes)
				config.DB.Model(&user).Update("two_factor_backup_codes", datatypes.JSON(newCodes))
			}
		}

		if !isValid {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัส 2FA ไม่ถูกต้อง"})
		}
	}

	config.DB.Model(&user).Updates(map[string]interface{}{
		"two_factor_enabled":      false,
		"two_factor_method":       "",
		"two_factor_secret":       "",
		"two_factor_backup_codes": nil,
		"two_factor_confirmed_at": nil,
	})

	return c.JSON(fiber.Map{"success": true, "message": "ปิดใช้งานการยืนยันตัวตนสองขั้นตอนสำเร็จ"})
}

// =============================================================================
// POST /api/auth/2fa/backup-codes  (regenerate)
// =============================================================================

type RegenerateBackupCodesInput struct {
	Password string `json:"password"`
}

func RegenerateBackupCodesHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input RegenerateBackupCodesInput
	if err := c.Bind().JSON(&input); err != nil || input.Password == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสผ่าน"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสผ่านไม่ถูกต้อง"})
	}

	if !user.TwoFactorEnabled {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาเปิดใช้งาน 2FA ก่อน"})
	}

	plain, hashed, err := generateBackupCodes()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เกิดข้อผิดพลาดภายใน"})
	}

	hashedJSON, _ := json.Marshal(hashed)
	config.DB.Model(&user).Update("two_factor_backup_codes", datatypes.JSON(hashedJSON))

	return c.JSON(fiber.Map{
		"success": true,
		"message": "สร้างรหัสสำรองใหม่สำเร็จ",
		"data": fiber.Map{
			"backupCodes": plain,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/verify-login  (public — called during login flow)
// =============================================================================

type VerifyLoginWith2FAInput struct {
	UserID uint   `json:"userId"`
	Code   string `json:"code"`
}

func verify2FACodeForLogin(user *models.User, code string) (bool, bool) {
	isValid := false
	usedBackupCode := false

	switch user.TwoFactorMethod {
	case "totp":
		if user.TwoFactorSecret != "" {
			dec, err := decryptSecret(user.TwoFactorSecret)
			if err == nil {
				isValid = totp.Validate(code, dec)
			}
		}
	case "email":
		var pending models.TwoFactorPending
		if err := config.DB.Where("user_id = ? AND method = ?", user.ID, "email").First(&pending).Error; err == nil {
			if pending.EmailCode == code && (pending.EmailCodeExpiresAt == nil || time.Now().Before(*pending.EmailCodeExpiresAt)) {
				isValid = true
				config.DB.Delete(&pending)
			}
		}
	}

	if !isValid {
		idx, valid := verifyBackupCode(code, user.TwoFactorBackupCodes)
		if valid {
			isValid = true
			usedBackupCode = true

			var codes []interface{}
			if err := json.Unmarshal(user.TwoFactorBackupCodes, &codes); err == nil && idx >= 0 && idx < len(codes) {
				codes[idx] = nil
				if newCodes, err := json.Marshal(codes); err == nil {
					config.DB.Model(user).Update("two_factor_backup_codes", datatypes.JSON(newCodes))
				}
			}
		}
	}

	return isValid, usedBackupCode
}

func VerifyLoginWith2FAHandler(c fiber.Ctx) error {
	var input VerifyLoginWith2FAInput
	if err := c.Bind().JSON(&input); err != nil || input.UserID == 0 || input.Code == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ครบถ้วน"})
	}

	var user models.User
	if err := config.DB.First(&user, input.UserID).Error; err != nil || !user.TwoFactorEnabled {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลผู้ใช้หรือไม่ได้เปิดใช้งาน 2FA"})
	}

	isValid, usedBackupCode := verify2FACodeForLogin(&user, input.Code)

	if !isValid {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสยืนยันไม่ถูกต้อง"})
	}

	return c.JSON(fiber.Map{
		"success":        true,
		"verified":       true,
		"usedBackupCode": usedBackupCode,
	})
}

// =============================================================================
// POST /api/auth/2fa/complete-login  (public — complete login after 2FA verify)
// =============================================================================

type CompleteTwoFALoginInput struct {
	UserID uint   `json:"userId"`
	Code   string `json:"code"`
}

func CompleteTwoFALoginHandler(c fiber.Ctx) error {
	var input CompleteTwoFALoginInput
	if err := c.Bind().JSON(&input); err != nil || input.UserID == 0 || input.Code == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ครบถ้วน"})
	}

	var user models.User
	if err := config.DB.First(&user, input.UserID).Error; err != nil || !user.TwoFactorEnabled {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลผู้ใช้หรือไม่ได้เปิดใช้งาน 2FA"})
	}

	isValid, usedBackupCode := verify2FACodeForLogin(&user, input.Code)
	if !isValid {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสยืนยันไม่ถูกต้อง"})
	}

	accessToken, refreshToken, jti, err := utils.GenerateTokenPair(user.ID, user.Role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้าง Token ไม่สำเร็จ"})
	}

	metaJSON, _ := json.Marshal(sessionMeta{
		IP:        c.IP(),
		UserAgent: string(c.Request().Header.UserAgent()),
		Provider:  "local-2fa",
	})

	if err := repositories.CreateRefreshToken(&models.RefreshToken{
		JTI:       jti,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Meta:      datatypes.JSON(metaJSON),
	}); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึก Token ไม่สำเร็จ"})
	}

	if utils.IsWebClient(c) {
		utils.SetAuthCookies(c, accessToken, refreshToken)
	}

	actorUserID := user.ID
	{
		dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))
		_ = config.DB.Create(&models.SystemLog{
			LogType:     "auth",
			Severity:    "info",
			ActorUserID: &actorUserID,
			Action:      "2fa_login_success",
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
			DeviceType:  dt,
			Browser:     br,
			OS:          osn,
			CreatedAt:   time.Now(),
		}).Error
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Login successful",
		"data": fiber.Map{
			"user":               safeUser(&user),
			"accessToken":        accessToken,
			"refreshToken":       refreshToken,
			"mustChangePassword": user.MustChangePassword,
			"usedBackupCode":     usedBackupCode,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/send-login-code  (public — send email code for login)
// =============================================================================

type SendLoginCodeInput struct {
	UserID uint `json:"userId"`
}

func SendLoginCodeHandler(c fiber.Ctx) error {
	var input SendLoginCodeInput
	if err := c.Bind().JSON(&input); err != nil || input.UserID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ครบถ้วน"})
	}

	var user models.User
	if err := config.DB.First(&user, input.UserID).Error; err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลผู้ใช้"})
	}

	if !user.TwoFactorEnabled || user.TwoFactorMethod != "email" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่ได้ใช้งาน Email 2FA"})
	}

	if user.Email == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบอีเมลในโปรไฟล์"})
	}

	emailCode := generateNumericCode(6)
	emailCodeExp := time.Now().Add(5 * time.Minute)
	expiresAt := time.Now().Add(15 * time.Minute)

	config.DB.Where("user_id = ? AND method = ?", input.UserID, "email").Delete(&models.TwoFactorPending{})
	config.DB.Create(&models.TwoFactorPending{
		UserID:             input.UserID,
		Method:             "email",
		Secret:             "login",
		EmailCode:          emailCode,
		EmailCodeExpiresAt: &emailCodeExp,
		ExpiresAt:          expiresAt,
	})

	if err := services.SendTwoFactorCodeEmail(&user, emailCode, "login"); err != nil {
		services.LogEmailDeliveryError("2fa login code", err)
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถส่งอีเมลรหัสยืนยันได้ในขณะนี้"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "ส่งรหัสยืนยันไปที่อีเมลของคุณแล้ว"})
}

// =============================================================================
// POST /api/auth/2fa/step-up/challenge (protected)
// =============================================================================

type StepUpChallengeInput struct {
	Action string `json:"action"`
}

var privilegedStepUpActions = map[string]struct{}{
	"system_settings.maintenance.update":   {},
	"system_settings.feature_flags.update": {},
	"system_settings.backups.run_now":      {},
	"system_settings.backups.restore":      {},
	"system_settings.backups.download":     {},
	"system_logs.cleanup":                  {},
}

func normalizeStepUpAction(action string) string {
	return strings.TrimSpace(strings.ToLower(action))
}

func isStepUpActionAllowed(action string) bool {
	_, ok := privilegedStepUpActions[normalizeStepUpAction(action)]
	return ok
}

func RequestStepUpChallengeHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input StepUpChallengeInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	action := normalizeStepUpAction(input.Action)
	if !isStepUpActionAllowed(action) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "action ไม่รองรับ"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}
	if !user.TwoFactorEnabled {
		return c.Status(403).JSON(fiber.Map{"success": false, "code": "2FA_NOT_ENABLED", "message": "กรุณาเปิดใช้งาน 2FA ก่อนทำรายการสำคัญ"})
	}

	if user.TwoFactorMethod == "email" {
		if user.Email == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่พบอีเมลในโปรไฟล์"})
		}
		emailCode := generateNumericCode(6)
		emailCodeExp := time.Now().Add(5 * time.Minute)
		expiresAt := time.Now().Add(15 * time.Minute)

		config.DB.Where("user_id = ? AND method = ?", user.ID, "email").Delete(&models.TwoFactorPending{})
		config.DB.Create(&models.TwoFactorPending{
			UserID:             user.ID,
			Method:             "email",
			Secret:             "stepup:" + action,
			EmailCode:          emailCode,
			EmailCodeExpiresAt: &emailCodeExp,
			ExpiresAt:          expiresAt,
		})

		if err := services.SendTwoFactorCodeEmail(&user, emailCode, "stepup"); err != nil {
			services.LogEmailDeliveryError("2fa step-up code", err)
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถส่งอีเมลรหัสยืนยันได้ในขณะนี้"})
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"action":      action,
			"method":      user.TwoFactorMethod,
			"maskedEmail": maskEmail(user.Email),
			"expiresIn":   300,
		},
	})
}

// =============================================================================
// POST /api/auth/2fa/step-up/verify (protected)
// =============================================================================

type StepUpVerifyInput struct {
	Action string `json:"action"`
	Code   string `json:"code"`
}

func VerifyStepUpChallengeHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input StepUpVerifyInput
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.Code) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสยืนยัน"})
	}
	action := normalizeStepUpAction(input.Action)
	if !isStepUpActionAllowed(action) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "action ไม่รองรับ"})
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "User not found"})
	}
	if !user.TwoFactorEnabled {
		return c.Status(403).JSON(fiber.Map{"success": false, "code": "2FA_NOT_ENABLED", "message": "กรุณาเปิดใช้งาน 2FA ก่อนทำรายการสำคัญ"})
	}

	isValid, usedBackupCode := verify2FACodeForLogin(&user, strings.TrimSpace(input.Code))
	if !isValid {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสยืนยันไม่ถูกต้อง"})
	}

	stepUpToken, expiresAt, err := middlewares.GenerateStepUpToken(user.ID, action, 10*time.Minute)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถสร้าง step-up token ได้"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"action":         action,
			"stepUpToken":    stepUpToken,
			"expiresAt":      expiresAt,
			"usedBackupCode": usedBackupCode,
		},
	})
}

// =============================================================================
// Utility
// =============================================================================

func generateNumericCode(digits int) string {
	const numbers = "0123456789"
	buf := make([]byte, digits)
	rand.Read(buf)
	var sb strings.Builder
	for _, b := range buf {
		sb.WriteByte(numbers[int(b)%len(numbers)])
	}
	return sb.String()
}
