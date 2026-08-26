package utils

import (
	"errors"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// เข้ารหัสผ่าน
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10) // ความยากระดับ 10 (เท่ากับของเดิม)
	return string(bytes), err
}

// ตรวจสอบรหัสผ่าน
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ข้อความอธิบายนโยบายรหัสผ่าน สำหรับแสดงผลใน error message และหน้าฟอร์ม
const PasswordPolicyMessage = "รหัสผ่านต้องมีอย่างน้อย 8 ตัวอักษร ประกอบด้วยตัวพิมพ์ใหญ่ ตัวพิมพ์เล็ก ตัวเลข และอักขระพิเศษ อย่างละ 1 ตัวขึ้นไป"

// ValidatePasswordStrength บังคับนโยบายรหัสผ่านที่ยาก (ตาม TOR 3.10):
// อย่างน้อย 8 ตัวอักษร และมีครบทั้งตัวพิมพ์ใหญ่ ตัวพิมพ์เล็ก ตัวเลข และอักขระพิเศษ
func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return errors.New(PasswordPolicyMessage)
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return errors.New(PasswordPolicyMessage)
	}

	return nil
}
