package utils

import (
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("❌ JWT_SECRET environment variable is required")
	}
	return []byte(secret)
}

func getRefreshSecret() []byte {
	secret := os.Getenv("JWT_REFRESH_SECRET")
	if secret == "" {
		log.Fatal("❌ JWT_REFRESH_SECRET environment variable is required")
	}
	return []byte(secret)
}

// GenerateTokenPair สร้าง access token (15 นาที) และ refresh token (7 วัน)
// คืนค่า accessToken, refreshToken, jti (ใช้ผูก refresh token กับ DB), error
func GenerateTokenPair(userID uint, role string) (string, string, string, error) {
	jti := uuid.New().String()

	// Access Token
	accessClaims := jwt.MapClaims{
		"sub":  userID,
		"role": role,
		"jti":  jti,
		"exp":  time.Now().Add(15 * time.Minute).Unix(),
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(getJWTSecret())
	if err != nil {
		return "", "", "", err
	}

	// Refresh Token
	refreshClaims := jwt.MapClaims{
		"sub": userID,
		"jti": jti,
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(getRefreshSecret())
	if err != nil {
		return "", "", "", err
	}

	return accessToken, refreshToken, jti, nil
}

type TokenClaims struct {
	UserID uint
	Role   string
	JTI    string
}

func ValidateAccessToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return getJWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}
	claims := token.Claims.(jwt.MapClaims)
	userID := uint(claims["sub"].(float64))
	role, _ := claims["role"].(string)
	jti, _ := claims["jti"].(string)
	return &TokenClaims{UserID: userID, Role: role, JTI: jti}, nil
}

func ValidateRefreshToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return getRefreshSecret(), nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}
	claims := token.Claims.(jwt.MapClaims)
	userID := uint(claims["sub"].(float64))
	jti, _ := claims["jti"].(string)
	return &TokenClaims{UserID: userID, JTI: jti}, nil
}

// GenerateToken ใช้สำหรับ backward compat (access token เดิม 24 ชั่วโมง)
func GenerateToken(userID uint, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  userID,
		"role": role,
		"exp":  time.Now().Add(time.Hour * 24).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
}
