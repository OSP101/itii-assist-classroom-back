package utils

import "golang.org/x/crypto/bcrypt"

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