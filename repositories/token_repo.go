package repositories

import (
	"crypto/rand"
	"encoding/base64"
	"itii-assist/config"
	"itii-assist/models"
	"time"
)

func CreateRefreshToken(token *models.RefreshToken) error {
	return config.DB.Create(token).Error
}

func FindRefreshTokenByJTI(jti string) (*models.RefreshToken, error) {
	var token models.RefreshToken
	err := config.DB.Where("jti = ? AND revoked = ? AND expires_at > ?", jti, false, time.Now()).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func RevokeRefreshToken(jti string) error {
	return config.DB.Model(&models.RefreshToken{}).
		Where("jti = ?", jti).
		Update("revoked", true).Error
}

func RevokeAllUserRefreshTokens(userID uint) error {
	return config.DB.Model(&models.RefreshToken{}).
		Where("user_id = ? AND kind = 'u' AND revoked = ?", userID, false).
		Update("revoked", true).Error
}

func RevokeAllStudentRefreshTokens(studentID uint) error {
	return config.DB.Model(&models.RefreshToken{}).
		Where("user_id = ? AND kind = 's' AND revoked = ?", studentID, false).
		Update("revoked", true).Error
}

func CreatePasswordResetToken(userID uint, validity time.Duration) (*models.PasswordResetToken, error) {
	if validity <= 0 {
		validity = time.Hour
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}

	if err := config.DB.Where("user_id = ? AND used_at IS NULL", userID).Delete(&models.PasswordResetToken{}).Error; err != nil {
		return nil, err
	}

	token := &models.PasswordResetToken{
		UserID:    userID,
		Token:     base64.RawURLEncoding.EncodeToString(buf),
		ExpiresAt: time.Now().Add(validity),
	}

	if err := config.DB.Create(token).Error; err != nil {
		return nil, err
	}

	return token, nil
}

func FindValidPasswordResetToken(token string) (*models.PasswordResetToken, error) {
	var record models.PasswordResetToken
	err := config.DB.Where("token = ? AND used_at IS NULL AND expires_at > ?", token, time.Now()).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func MarkPasswordResetTokenUsed(token string) error {
	now := time.Now()
	return config.DB.Model(&models.PasswordResetToken{}).
		Where("token = ? AND used_at IS NULL", token).
		Update("used_at", &now).Error
}
