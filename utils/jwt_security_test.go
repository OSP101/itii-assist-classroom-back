package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAccessTokensRequireValidSubjectExpiryAndExactAlgorithm(t *testing.T) {
	t.Setenv("JWT_SECRET", "security-test-only-key-never-use-in-production")
	for _, tc := range []struct {
		claims jwt.MapClaims
		method jwt.SigningMethod
	}{
		{jwt.MapClaims{"sub": "not-a-number", "exp": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS256},
		{jwt.MapClaims{"sub": 1}, jwt.SigningMethodHS256},
		{jwt.MapClaims{"sub": 1, "exp": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS384},
		{jwt.MapClaims{"sub": 0, "exp": time.Now().Add(time.Hour).Unix()}, jwt.SigningMethodHS256},
	} {
		raw, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString(getJWTSecret())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateAccessToken(raw); err == nil {
			t.Fatalf("accepted malformed token: %v", tc.claims)
		}
	}
}

func TestUserAndStudentTokenPairsRemainCompatible(t *testing.T) {
	t.Setenv("JWT_SECRET", "access-test-only-key-never-use-in-production")
	t.Setenv("JWT_REFRESH_SECRET", "refresh-test-only-key-never-use-in-production")
	for _, student := range []bool{false, true} {
		var access, refresh, jti string
		var err error
		if student {
			access, refresh, jti, err = GenerateStudentTokenPair(42)
		} else {
			access, refresh, jti, err = GenerateTokenPair(42, "ta")
		}
		if err != nil {
			t.Fatal(err)
		}
		claims, err := ValidateAccessToken(access)
		if err != nil || claims.UserID != 42 || claims.JTI != jti || claims.ExpiresAt.IsZero() {
			t.Fatalf("bad access round trip: %+v %v", claims, err)
		}
		rc, err := ValidateRefreshToken(refresh)
		if err != nil || rc.UserID != 42 || rc.Kind != claims.Kind {
			t.Fatalf("bad refresh round trip: %+v %v", rc, err)
		}
		if _, err := ValidateAccessToken(refresh); err == nil {
			t.Fatal("refresh token accepted as access token")
		}
	}
}
