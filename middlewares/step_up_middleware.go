package middlewares

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

const StepUpHeaderName = "X-Step-Up-Token"

const stepUpTokenPurpose = "privileged_step_up"

func getStepUpSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-step-up-secret"
	}
	return []byte(secret)
}

func GenerateStepUpToken(userID uint, action string, ttl time.Duration) (string, time.Time, error) {
	normalizedAction := strings.TrimSpace(strings.ToLower(action))
	if normalizedAction == "" {
		return "", time.Time{}, errors.New("action is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	expiresAt := time.Now().Add(ttl)
	claims := jwt.MapClaims{
		"sub":     userID,
		"purpose": stepUpTokenPurpose,
		"action":  normalizedAction,
		"exp":     expiresAt.Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getStepUpSecret())
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func ValidateStepUpToken(tokenString string, userID uint, action string) error {
	normalizedAction := strings.TrimSpace(strings.ToLower(action))
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return getStepUpSecret(), nil
	})
	if err != nil || !token.Valid {
		return errors.New("invalid step-up token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return errors.New("invalid step-up claims")
	}
	claimPurpose, _ := claims["purpose"].(string)
	if claimPurpose != stepUpTokenPurpose {
		return errors.New("invalid step-up purpose")
	}

	claimAction, _ := claims["action"].(string)
	if strings.TrimSpace(strings.ToLower(claimAction)) != normalizedAction {
		return errors.New("step-up action mismatch")
	}

	rawSub, ok := claims["sub"].(float64)
	if !ok || uint(rawSub) != userID {
		return errors.New("step-up subject mismatch")
	}

	return nil
}

func RequirePrivilegedStepUp(action string) fiber.Handler {
	normalizedAction := strings.TrimSpace(strings.ToLower(action))
	return func(c fiber.Ctx) error {
		userID, ok := GetUserID(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
		}

		stepUpToken := strings.TrimSpace(c.Get(StepUpHeaderName))
		if stepUpToken == "" {
			return c.Status(428).JSON(fiber.Map{
				"success": false,
				"code":    "STEP_UP_REQUIRED",
				"action":  normalizedAction,
				"message": fmt.Sprintf("Step-up verification is required for action %s", normalizedAction),
			})
		}

		if err := ValidateStepUpToken(stepUpToken, userID, normalizedAction); err != nil {
			return c.Status(428).JSON(fiber.Map{
				"success": false,
				"code":    "STEP_UP_REQUIRED",
				"action":  normalizedAction,
				"message": "Step-up token is invalid or expired",
			})
		}

		return c.Next()
	}
}
