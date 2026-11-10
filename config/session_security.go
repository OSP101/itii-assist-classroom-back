package config

import (
	"errors"
	"itii-assist/models"
	"itii-assist/utils"
	"time"
)

// ValidateSessionPrincipal is shared by HTTP and long-lived socket sessions.
// Never authorize a suspended account using a still-valid signed claim.
func ValidateSessionPrincipal(claims *utils.TokenClaims) error {
	if claims == nil || claims.UserID == 0 || claims.JTI == "" || claims.ExpiresAt.IsZero() || !claims.ExpiresAt.After(time.Now()) || DB == nil {
		return errors.New("invalid session")
	}
	if claims.JTI != "" {
		var session models.RefreshToken
		err := DB.Where("jti = ? AND revoked = ? AND expires_at > ?", claims.JTI, false, time.Now()).First(&session).Error
		if err != nil || session.UserID != claims.UserID || session.Kind != claims.Kind {
			return errors.New("session revoked")
		}
	}
	if claims.Kind == "s" {
		var student models.Student
		if err := DB.Select("id", "is_active").First(&student, claims.UserID).Error; err != nil || !student.IsActive {
			return errors.New("account disabled")
		}
		if claims.Role != "student" {
			return errors.New("invalid student role")
		}
		return nil
	}
	var user models.User
	if err := DB.Select("id", "is_active", "role").First(&user, claims.UserID).Error; err != nil || !user.IsActive || user.Role != claims.Role {
		return errors.New("account disabled or permissions changed")
	}
	return nil
}
