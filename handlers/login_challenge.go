package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func loginChallengeHash(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func newLoginChallenge(user *models.User) (string, error) {
	if !user.IsActive || !user.TwoFactorEnabled {
		return "", fiber.ErrUnauthorized
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	challenge := models.LoginChallenge{ID: loginChallengeHash(token), UserID: user.ID, Method: user.TwoFactorMethod, ExpiresAt: time.Now().Add(5 * time.Minute)}
	return token, config.DB.Create(&challenge).Error
}

func lockLoginChallenge(tx *gorm.DB, token string, userID uint) (*models.LoginChallenge, error) {
	if len(token) != 43 || userID == 0 {
		return nil, fiber.ErrUnauthorized
	}
	var challenge models.LoginChallenge
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", loginChallengeHash(token), userID).First(&challenge).Error
	if err != nil || challenge.ConsumedAt != nil || challenge.Attempts >= 5 || !challenge.ExpiresAt.After(time.Now()) {
		return nil, fiber.NewError(401, "การยืนยันหมดอายุ กรุณาเข้าสู่ระบบใหม่")
	}
	return &challenge, nil
}

// Locking the user serializes attempts across challenges/replicas, including
// consumption of recovery codes and reuse of a TOTP in the same time window.
func consumeLoginChallenge(token string, userID uint, code string) (*models.User, bool, error) {
	return verifyLoginChallenge(token, userID, code, true)
}

func verifyLoginChallenge(token string, userID uint, code string, complete bool) (*models.User, bool, error) {
	var user models.User
	backup := false
	valid := false
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil || !user.IsActive || !user.TwoFactorEnabled {
			return fiber.ErrUnauthorized
		}
		challenge, err := lockLoginChallenge(tx, token, userID)
		if err != nil {
			return err
		}
		if challenge.Method != user.TwoFactorMethod {
			return fiber.ErrUnauthorized
		}
		code = strings.TrimSpace(code)
		if len(code) > 128 {
			code = ""
		}
		now := time.Now()
		proof := loginChallengeHash(fmt.Sprintf("%d:%s", user.ID, code))
		if challenge.VerifiedAt != nil {
			backup = challenge.UsedBackupCode
			valid = subtle.ConstantTimeCompare([]byte(challenge.ProofHash), []byte(proof)) == 1
		} else {
			switch user.TwoFactorMethod {
			case "totp":
				secret, err := decryptSecret(user.TwoFactorSecret)
				valid = err == nil && totp.Validate(code, secret)
				if valid {
					proof := loginChallengeHash(fmt.Sprintf("%d:%s", user.ID, code))
					var count int64
					if err := tx.Model(&models.LoginChallenge{}).Where("user_id = ? AND proof_hash = ? AND verified_at > ?", user.ID, proof, now.Add(-2*time.Minute)).Count(&count).Error; err != nil {
						return err
					}
					valid = count == 0
					if valid {
						challenge.ProofHash = proof
					}
				}
			case "email":
				var pending models.TwoFactorPending
				if err := tx.Where("user_id = ? AND method = ? AND secret = ?", user.ID, "email", "login:"+challenge.ID).First(&pending).Error; err == nil {
					valid = pending.EmailCodeExpiresAt != nil && pending.EmailCodeExpiresAt.After(now) && subtle.ConstantTimeCompare([]byte(pending.EmailCode), []byte(code)) == 1
					if valid {
						if err := tx.Delete(&pending).Error; err != nil {
							return err
						}
					}
				}
			}
		}
		if !valid {
			if idx, ok := verifyBackupCode(code, user.TwoFactorBackupCodes); ok {
				var codes []interface{}
				if err := json.Unmarshal(user.TwoFactorBackupCodes, &codes); err != nil {
					return err
				}
				codes[idx] = nil
				raw, err := json.Marshal(codes)
				if err != nil {
					return err
				}
				if err := tx.Model(&user).Update("two_factor_backup_codes", string(raw)).Error; err != nil {
					return err
				}
				valid, backup = true, true
			}
		}
		challenge.Attempts++
		if valid {
			if challenge.VerifiedAt == nil {
				challenge.VerifiedAt = &now
			}
			challenge.ProofHash = proof
			challenge.UsedBackupCode = backup
			if complete {
				challenge.ConsumedAt = &now
			}
		}
		return tx.Save(challenge).Error
	})
	if err != nil {
		return nil, false, err
	}
	if !valid {
		return nil, false, fiber.NewError(400, "รหัสยืนยันไม่ถูกต้อง")
	}
	return &user, backup, nil
}
