package utils

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const (
	AccessTokenCookieName  = "access_token"
	RefreshTokenCookieName = "refresh_token"

	// CSRFCookieName holds a random token, readable by JS (NOT httpOnly) so
	// the frontend can echo it back in CSRFHeaderName on mutating requests —
	// the standard double-submit-cookie CSRF defense. Only meaningful once
	// auth moves to cookies; Bearer-authenticated (mobile) requests skip
	// this check entirely since they were never CSRF-vulnerable.
	CSRFCookieName = "csrf_token"
	CSRFHeaderName = "X-CSRF-Token"

	// WebClientHeader is set by the Next.js frontend on every request so the
	// backend can tell a browser (cookie-based auth) apart from the Flutter
	// mobile app (Bearer token in Authorization, unaffected by this). Its
	// absence means "treat as a Bearer-token client" — this preserves the
	// mobile app's behavior exactly with zero client-side changes there.
	WebClientHeader      = "X-Client-Type"
	WebClientHeaderValue = "web"

	accessTokenCookieMaxAge  = 15 * 60         // matches the access JWT's own 15m exp (utils/jwt.go)
	refreshTokenCookieMaxAge = 7 * 24 * 60 * 60 // matches the refresh JWT's own 7d exp (utils/jwt.go)
	csrfTokenCookieMaxAge    = 7 * 24 * 60 * 60 // lives as long as the refresh session
)

// IsWebClient reports whether the request came from the web frontend, which
// authenticates via httpOnly cookies, rather than the mobile app.
func IsWebClient(c fiber.Ctx) bool {
	return strings.EqualFold(c.Get(WebClientHeader), WebClientHeaderValue)
}

// isSecureRequest reports whether this request arrived over HTTPS, to set
// the cookie's Secure flag correctly.
//
// NOTE: fiber.Ctx.Protocol() returns the HTTP version ("HTTP/1.1"/"HTTP/2"),
// NOT the URL scheme — a Fiber v2→v3 API change that silently breaks any
// `c.Protocol() == "https"` check (always false, since neither literal
// value is "https"). X-Forwarded-Proto (always set by nginx in every real
// deployment) covers production; the TLS fallback below is for a direct/
// un-proxied connection (e.g. local dev hitting the Go binary directly).
func isSecureRequest(c fiber.Ctx) bool {
	if proto := c.Get("X-Forwarded-Proto"); proto != "" {
		return strings.EqualFold(strings.TrimSpace(proto), "https")
	}
	return c.RequestCtx().IsTLS()
}

func generateCSRFToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// SetAuthCookies sets the httpOnly access/refresh token cookies plus a
// readable CSRF token cookie for a web session. Only call when
// IsWebClient(c) is true — mobile clients keep receiving tokens in the JSON
// response body only, unchanged.
//
// The refresh cookie is scoped to Path=/api/auth so it's never sent on
// unrelated API calls; the access and CSRF cookies are scoped to Path=/
// since every protected route needs them.
func SetAuthCookies(c fiber.Ctx, accessToken, refreshToken string) {
	secure := isSecureRequest(c)
	c.Cookie(&fiber.Cookie{
		Name:     AccessTokenCookieName,
		Value:    accessToken,
		Path:     "/",
		MaxAge:   accessTokenCookieMaxAge,
		Secure:   secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	c.Cookie(&fiber.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    refreshToken,
		Path:     "/api/auth",
		MaxAge:   refreshTokenCookieMaxAge,
		Secure:   secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	if csrfToken, err := generateCSRFToken(); err == nil {
		c.Cookie(&fiber.Cookie{
			Name:     CSRFCookieName,
			Value:    csrfToken,
			Path:     "/",
			MaxAge:   csrfTokenCookieMaxAge,
			Secure:   secure,
			HTTPOnly: false, // must be readable by frontend JS to echo back in CSRFHeaderName
			SameSite: "Lax",
		})
	}
}

// ClearAuthCookies clears the auth and CSRF cookies (logout, or a failed refresh).
func ClearAuthCookies(c fiber.Ctx) {
	secure := isSecureRequest(c)
	c.Cookie(&fiber.Cookie{
		Name:     AccessTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	c.Cookie(&fiber.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    "",
		Path:     "/api/auth",
		MaxAge:   -1,
		Secure:   secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	c.Cookie(&fiber.Cookie{
		Name:     CSRFCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		HTTPOnly: false,
		SameSite: "Lax",
	})
}
