package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/utils"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/datatypes"
)

// =============================================================================
// Config helpers
// =============================================================================

func getGoogleOAuthConfig() *oauth2.Config {
	callbackURL := os.Getenv("GOOGLE_CALLBACK_URL")
	if callbackURL == "" {
		callbackURL = "http://localhost:8000/api/auth/google/callback"
	}
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  callbackURL,
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     google.Endpoint,
	}
}

func getFrontendURL() string {
	u := os.Getenv("FRONTEND_URL")
	if u == "" {
		u = "http://localhost:3000"
	}
	return strings.TrimRight(u, "/")
}

// =============================================================================
// Stateless CSRF — encode action + nonce in the state parameter and sign
// with HMAC-SHA256 using JWT_SECRET.  No cookies needed, works across any
// hostname combination (10.x proxy → localhost callback).
//
// state = base64url(JSON payload) + "." + base64url(HMAC)
// payload = { nonce, action, linkToken }
// =============================================================================

type oauthStatePayload struct {
	Nonce     string `json:"n"`
	Action    string `json:"a"` // "" | "link"
	LinkToken string `json:"l"`
}

func signOAuthState(action, linkToken string) (string, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := oauthStatePayload{
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
		Action:    action,
		LinkToken: linkToken,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	key := []byte(os.Getenv("JWT_SECRET"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + sig, nil
}

func verifyOAuthState(state string) (*oauthStatePayload, bool) {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	payloadB64, sig := parts[0], parts[1]

	key := []byte(os.Getenv("JWT_SECRET"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, false
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, false
	}
	var payload oauthStatePayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, false
	}
	return &payload, true
}

func findUserByOAuthAccount(provider, providerUserID, providerEmail string) (*models.User, *models.UserOAuthAccount) {
	var acc models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", provider, providerUserID).
		First(&acc).Error; err == nil {
		var user models.User
		if err := config.DB.First(&user, acc.UserID).Error; err == nil {
			return &user, &acc
		}
	}

	if strings.TrimSpace(providerEmail) == "" {
		return nil, nil
	}

	placeholderID := "email:" + providerEmail
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", provider, placeholderID).
		First(&acc).Error; err == nil {
		var user models.User
		if err := config.DB.First(&user, acc.UserID).Error; err == nil {
			config.DB.Model(&acc).Updates(map[string]interface{}{
				"provider_user_id": providerUserID,
				"provider_email":   providerEmail,
			})
			acc.ProviderUserID = providerUserID
			acc.ProviderEmail = providerEmail
			return &user, &acc
		}
	}

	return nil, nil
}

// =============================================================================
// Google profile from userinfo endpoint
// =============================================================================

type googleProfile struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// =============================================================================
// Find user by Google profile — checks UserOAuthAccount then legacy google_id
// =============================================================================

func findUserByGoogle(profile googleProfile) (*models.User, *models.UserOAuthAccount) {
	if user, acc := findUserByOAuthAccount("google", profile.Sub, profile.Email); user != nil {
		return user, acc
	}

	var acc models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", "google", profile.Sub).
		First(&acc).Error; err == nil {
		var u models.User
		if err := config.DB.First(&u, acc.UserID).Error; err == nil {
			return &u, &acc
		}
	}

	// Legacy: google_id column on users
	var u models.User
	if err := config.DB.Where("google_id = ?", profile.Sub).First(&u).Error; err == nil {
		newAcc := models.UserOAuthAccount{
			UserID:         u.ID,
			Provider:       "google",
			ProviderUserID: profile.Sub,
			ProviderEmail:  profile.Email,
			ProviderName:   profile.Name,
			ProviderAvatar: profile.Picture,
			LinkedAt:       time.Now(),
		}
		config.DB.Where("user_id = ? AND provider = ?", u.ID, "google").FirstOrCreate(&newAcc)
		return &u, &newAcc
	}

	return nil, nil
}

// =============================================================================
// Issue a session (refresh token in DB) and return the token pair
// =============================================================================

func issueOAuthSession(c fiber.Ctx, user *models.User, provider string) (at, rt string, err error) {
	at, rt, jti, err := utils.GenerateTokenPair(user.ID, user.Role)
	if err != nil {
		return
	}
	meta := sessionMeta{
		IP:        c.IP(),
		UserAgent: string(c.Request().Header.UserAgent()),
		Provider:  provider,
	}
	metaJSON, _ := json.Marshal(meta)
	err = repositories.CreateRefreshToken(&models.RefreshToken{
		JTI:       jti,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Meta:      datatypes.JSON(metaJSON),
	})
	return
}

// =============================================================================
// GET /api/auth/google
// Redirects browser to Google's consent screen.
// Optional query params:
//   action=link&link_token=<access_token>
// =============================================================================

func GoogleLoginHandler(c fiber.Ctx) error {
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Google OAuth ยังไม่ได้ตั้งค่า (GOOGLE_CLIENT_ID missing)",
		})
	}

	action := c.Query("action")
	linkToken := c.Query("link_token")

	stateStr, err := signOAuthState(action, linkToken)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to generate state"})
	}

	authURL := getGoogleOAuthConfig().AuthCodeURL(stateStr, oauth2.AccessTypeOnline)
	return c.Redirect().To(authURL)
}

// =============================================================================
// GET /api/auth/google/callback
// =============================================================================

func GoogleCallbackHandler(c fiber.Ctx) error {
	frontendURL := getFrontendURL()

	redirectErr := func(path, msg string) error {
		return c.Redirect().To(frontendURL + path + "?error=" + url.QueryEscape(msg))
	}

	// --- Verify signed state (CSRF protection without cookies) ---
	stateParam := c.Query("state")
	payload, ok := verifyOAuthState(stateParam)
	if !ok {
		return redirectErr("/login", "Invalid OAuth state")
	}

	isLinkAction := payload.Action == "link"

	// --- Handle Google error (user cancelled, etc.) ---
	if errParam := c.Query("error"); errParam != "" {
		path := "/login"
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, c.Query("error_description", "Google login cancelled"))
	}

	// --- Exchange code ---
	code := c.Query("code")
	if code == "" {
		return redirectErr("/login", "No authorization code received")
	}
	oauthToken, err := getGoogleOAuthConfig().Exchange(context.Background(), code)
	if err != nil {
		return redirectErr("/login", "Failed to exchange OAuth code: "+err.Error())
	}

	// --- Fetch Google profile ---
	client := getGoogleOAuthConfig().Client(context.Background(), oauthToken)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil || resp.StatusCode != http.StatusOK {
		return redirectErr("/login", "Failed to fetch Google profile")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var profile googleProfile
	if err := json.Unmarshal(body, &profile); err != nil || profile.Sub == "" {
		return redirectErr("/login", "Invalid Google profile data")
	}

	// =========================================================================
	// Link action
	// =========================================================================
	if isLinkAction {
		if payload.LinkToken == "" {
			return redirectErr("/auth/link-callback", "Missing link_token")
		}
		claims, err := utils.ValidateAccessToken(payload.LinkToken)
		if err != nil || claims.UserID == 0 {
			return redirectErr("/auth/link-callback", "Invalid or expired session")
		}

		var existingUser models.User
		if err := config.DB.First(&existingUser, claims.UserID).Error; err != nil {
			return redirectErr("/auth/link-callback", "User not found")
		}

		// Conflict check
		var conflict models.UserOAuthAccount
		if err := config.DB.Where("provider = ? AND provider_user_id = ?", "google", profile.Sub).
			First(&conflict).Error; err == nil && conflict.UserID != claims.UserID {
			return redirectErr("/auth/link-callback", "This Google account is already linked to another user")
		}

		// Upsert link
		var acc models.UserOAuthAccount
		config.DB.Where("user_id = ? AND provider = ?", claims.UserID, "google").FirstOrCreate(&acc, models.UserOAuthAccount{
			UserID:         claims.UserID,
			Provider:       "google",
			ProviderUserID: profile.Sub,
			ProviderEmail:  profile.Email,
			ProviderName:   profile.Name,
			ProviderAvatar: profile.Picture,
			LinkedAt:       time.Now(),
		})
		config.DB.Model(&acc).Updates(map[string]interface{}{
			"provider_user_id": profile.Sub,
			"provider_email":   profile.Email,
			"provider_name":    profile.Name,
			"provider_avatar":  profile.Picture,
		})

		at, rt, err := issueOAuthSession(c, &existingUser, "google")
		if err != nil {
			return redirectErr("/auth/link-callback", "Failed to create session")
		}
		return c.Redirect().To(fmt.Sprintf(
			"%s/auth/link-callback?linked=google&accessToken=%s&refreshToken=%s",
			frontendURL, at, rt,
		))
	}

	// =========================================================================
	// Normal login
	// =========================================================================
	user, oauthAccount := findUserByGoogle(profile)
	if user == nil {
		return redirectErr("/login", "ไม่พบบัญชีที่ผูกกับ Google นี้ กรุณาติดต่อผู้ดูแลระบบ")
	}
	if !user.IsActive {
		return redirectErr("/login", "บัญชีนี้ถูกระงับการใช้งาน")
	}

	// Refresh cached profile info
	if oauthAccount != nil {
		config.DB.Model(oauthAccount).Updates(map[string]interface{}{
			"provider_email":  profile.Email,
			"provider_name":   profile.Name,
			"provider_avatar": profile.Picture,
		})
	}

	// 2FA gate
	if user.TwoFactorEnabled {
		twoFactorJSON, _ := json.Marshal(map[string]interface{}{
			"requiresTwoFactor": true,
			"twoFactorMethod":   user.TwoFactorMethod,
			"userId":            user.ID,
		})
		return c.Redirect().To(fmt.Sprintf(
			"%s/auth/callback?twoFactor=%s",
			frontendURL, url.QueryEscape(string(twoFactorJSON)),
		))
	}

	at, rt, err := issueOAuthSession(c, user, "google")
	if err != nil {
		return redirectErr("/login", "Failed to create session")
	}
	return c.Redirect().To(fmt.Sprintf(
		"%s/auth/callback?accessToken=%s&refreshToken=%s",
		frontendURL, at, rt,
	))
}
