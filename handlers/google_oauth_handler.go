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

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(raw, ",")[0])
}

func normalizePublicBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return "https://" + trimmed
}

func getRequestPublicBaseURL(c fiber.Ctx) string {
	// c.Hostname() strips the port (fine behind nginx on 443/80, but wrong
	// for a direct connection on a non-standard port like local dev :8000)
	// — c.Host() keeps host:port as sent by the client/proxy.
	host := firstHeaderValue(c.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Host())
	}
	if host == "" {
		return ""
	}

	// NOTE: fiber.Ctx.Protocol() returns the HTTP version ("HTTP/1.1"/
	// "HTTP/2"), not the URL scheme — a Fiber v2→v3 API change. Must not
	// be used here. X-Forwarded-Proto (always set by nginx in every real
	// deployment) covers production; the fallback below checks the actual
	// connection's TLS state directly, which is trust-proxy-config-
	// independent and correct for a direct/un-proxied connection (e.g.
	// local dev hitting the Go binary directly over plain HTTP).
	scheme := firstHeaderValue(c.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if c.RequestCtx().IsTLS() {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return normalizePublicBaseURL(scheme + "://" + host)
}

func getOAuthCallbackURL(c fiber.Ctx, envKey, defaultPath string) string {
	if publicBaseURL := getRequestPublicBaseURL(c); publicBaseURL != "" {
		return publicBaseURL + defaultPath
	}

	callbackURL := strings.TrimSpace(os.Getenv(envKey))
	if callbackURL == "" {
		callbackURL = "http://localhost:8000" + defaultPath
	}
	return callbackURL
}

func getGoogleOAuthConfig(c fiber.Ctx) *oauth2.Config {
	callbackURL := getOAuthCallbackURL(c, "GOOGLE_CALLBACK_URL", "/api/auth/google/callback")
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  callbackURL,
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     google.Endpoint,
	}
}

func getFrontendURL(c fiber.Ctx) string {
	if publicBaseURL := getRequestPublicBaseURL(c); publicBaseURL != "" {
		return publicBaseURL
	}

	u := os.Getenv("FRONTEND_URL")
	if u == "" {
		u = "http://localhost:3000"
	}
	return strings.TrimRight(u, "/")
}

func buildFrontendRedirectWithFragment(baseURL, path string, params map[string]string) string {
	fragment := url.Values{}
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		fragment.Set(key, value)
	}

	return fmt.Sprintf("%s%s#%s", baseURL, path, fragment.Encode())
}

// buildFrontendRedirectWithQuery is used once the auth cookies have already
// been set on this same response (via utils.SetAuthCookies) — unlike
// buildFrontendRedirectWithFragment, no tokens are ever passed here, only
// non-sensitive UI signals (e.g. "login=success", "linked=google").
func buildFrontendRedirectWithQuery(baseURL, path string, params map[string]string) string {
	query := url.Values{}
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}
	if len(query) == 0 {
		return baseURL + path
	}
	return fmt.Sprintf("%s%s?%s", baseURL, path, query.Encode())
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
	Audience  string `json:"u,omitempty"` // "" | "student"
}

func signOAuthState(action, linkToken, audience string) (string, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := oauthStatePayload{
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
		Action:    action,
		LinkToken: linkToken,
		Audience:  audience,
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
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// =============================================================================
// Find user by Google profile — checks UserOAuthAccount then legacy google_id
// =============================================================================

func upsertGoogleOAuthAccount(userID uint, profile googleProfile) *models.UserOAuthAccount {
	var linked models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", "google", profile.Sub).First(&linked).Error; err == nil && linked.UserID != userID {
		return nil
	}

	acc := models.UserOAuthAccount{
		UserID:         userID,
		Provider:       "google",
		ProviderUserID: profile.Sub,
		ProviderEmail:  profile.Email,
		ProviderName:   profile.Name,
		ProviderAvatar: profile.Picture,
		LinkedAt:       time.Now(),
	}
	config.DB.Where("user_id = ? AND provider = ?", userID, "google").FirstOrCreate(&acc, acc)
	config.DB.Model(&acc).Updates(map[string]interface{}{
		"provider_user_id": profile.Sub,
		"provider_email":   profile.Email,
		"provider_name":    profile.Name,
		"provider_avatar":  profile.Picture,
	})
	return &acc
}

func findGeneralUserByGoogle(profile googleProfile) (*models.User, *models.UserOAuthAccount) {
	if user, acc := findUserByOAuthAccount("google", profile.Sub, profile.Email); user != nil {
		if strings.EqualFold(strings.TrimSpace(user.Role), "student") {
			return nil, nil
		}
		return user, acc
	}

	var u models.User
	if err := config.DB.Where("google_id = ?", profile.Sub).First(&u).Error; err == nil {
		if strings.EqualFold(strings.TrimSpace(u.Role), "student") {
			return nil, nil
		}
		return &u, upsertGoogleOAuthAccount(u.ID, profile)
	}

	trimmedEmail := strings.ToLower(strings.TrimSpace(profile.Email))
	if profile.EmailVerified && trimmedEmail != "" {
		var existingUser models.User
		if err := config.DB.Where("LOWER(email) = ?", trimmedEmail).First(&existingUser).Error; err == nil {
			if strings.EqualFold(strings.TrimSpace(existingUser.Role), "student") {
				return nil, nil
			}
			updates := map[string]interface{}{}
			if strings.TrimSpace(existingUser.GoogleID) == "" {
				updates["google_id"] = profile.Sub
			}
			if strings.TrimSpace(existingUser.Avatar) == "" && strings.TrimSpace(profile.Picture) != "" {
				updates["avatar"] = profile.Picture
			}
			if len(updates) > 0 {
				config.DB.Model(&existingUser).Updates(updates)
			}
			return &existingUser, upsertGoogleOAuthAccount(existingUser.ID, profile)
		}
	}

	return nil, nil
}

func findOrProvisionStudentByGoogle(profile googleProfile) *models.Student {
	trimmedEmail := strings.ToLower(strings.TrimSpace(profile.Email))
	if !profile.EmailVerified || trimmedEmail == "" {
		return nil
	}

	student, err := repositories.FindStudentByEmail(trimmedEmail)
	if err != nil || student == nil || !student.IsActive {
		return nil
	}

	return student
}

func findUserByGoogle(profile googleProfile, audience string) (*models.User, *models.UserOAuthAccount) {
	if audience == "student" {
		return nil, nil
	}

	return findGeneralUserByGoogle(profile)
}

// issueStudentOAuthSession สร้าง session สำหรับ Student (ไม่มี User record)
func issueStudentOAuthSession(c fiber.Ctx, student *models.Student) (at, rt string, err error) {
	at, rt, jti, err := utils.GenerateStudentTokenPair(student.ID)
	if err != nil {
		return
	}
	meta := sessionMeta{
		IP:               c.IP(),
		UserAgent:        string(c.Request().Header.UserAgent()),
		Provider:         "google",
		SessionStartedAt: time.Now(),
	}
	metaJSON, _ := json.Marshal(meta)
	err = repositories.CreateRefreshToken(&models.RefreshToken{
		JTI:       jti,
		UserID:    student.ID,
		Kind:      "s",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		Meta:      datatypes.JSON(metaJSON),
	})
	return
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
		IP:               c.IP(),
		UserAgent:        string(c.Request().Header.UserAgent()),
		Provider:         provider,
		SessionStartedAt: time.Now(),
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

// linkTokenForRequest returns the current caller's access token for an
// action=link request, so it can be carried through the signed OAuth state
// to the callback (which validates it there — see the isLinkAction branch
// in GoogleCallbackHandler/GitHubCallbackHandler). Web callers never had
// this token in JS to send as a query param even before the httpOnly-cookie
// migration was the safer choice — this request itself (opened as a normal
// browser navigation while already logged in) carries the Bearer header
// (mobile, if ever used this way) or the access-token cookie (web)
// automatically, exactly like any other authenticated request.
func linkTokenForRequest(c fiber.Ctx, action string) string {
	if action != "link" {
		return ""
	}
	authHeader := c.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return c.Cookies(utils.AccessTokenCookieName)
}

// =============================================================================
// GET /api/auth/google
// Redirects browser to Google's consent screen.
// Optional query params:
//   action=link  — links the CURRENTLY authenticated caller's account. The
//   access token itself is taken from this request's own Authorization
//   header or httpOnly cookie (not a query param) and carried through the
//   signed state to the callback — see linkTokenForRequest.
// =============================================================================

func GoogleLoginHandler(c fiber.Ctx) error {
	if os.Getenv("GOOGLE_CLIENT_ID") == "" {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Google OAuth ยังไม่ได้ตั้งค่า (GOOGLE_CLIENT_ID missing)",
		})
	}

	action := c.Query("action")
	linkToken := linkTokenForRequest(c, action)
	audience := strings.ToLower(strings.TrimSpace(c.Query("audience")))
	if audience != "student" {
		audience = ""
	}

	stateStr, err := signOAuthState(action, linkToken, audience)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to generate state"})
	}

	authURL := getGoogleOAuthConfig(c).AuthCodeURL(stateStr, oauth2.AccessTypeOnline)
	return c.Redirect().To(authURL)
}

// =============================================================================
// GET /api/auth/google/callback
// =============================================================================

func GoogleCallbackHandler(c fiber.Ctx) error {
	frontendURL := getFrontendURL(c)

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
	loginPath := "/login"
	if payload.Audience == "student" {
		loginPath = "/student/login"
	}

	// --- Handle Google error (user cancelled, etc.) ---
	if errParam := c.Query("error"); errParam != "" {
		path := loginPath
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, c.Query("error_description", "Google login cancelled"))
	}

	// --- Exchange code ---
	code := c.Query("code")
	if code == "" {
		return redirectErr(loginPath, "No authorization code received")
	}
	oauthConfig := getGoogleOAuthConfig(c)
	oauthToken, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		return redirectErr(loginPath, "Failed to exchange OAuth code: "+err.Error())
	}

	// --- Fetch Google profile ---
	client := oauthConfig.Client(context.Background(), oauthToken)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil || resp.StatusCode != http.StatusOK {
		return redirectErr(loginPath, "Failed to fetch Google profile")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var profile googleProfile
	if err := json.Unmarshal(body, &profile); err != nil || profile.Sub == "" {
		return redirectErr(loginPath, "Invalid Google profile data")
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
		logPrivilegedAdminAction(c, existingUser.ID, "link_oauth_account", "info", "users", fmt.Sprint(existingUser.ID), fiber.Map{"provider": "google"})
		utils.SetAuthCookies(c, at, rt)
		return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/link-callback", map[string]string{
			"linked": "google",
		}))
	}

	// =========================================================================
	// Normal login
	// =========================================================================

	// Student audience — bypass user table entirely
	if payload.Audience == "student" {
		student := findOrProvisionStudentByGoogle(profile)
		if student == nil {
			return redirectErr(loginPath, "ไม่พบบัญชีนักศึกษาที่ใช้งานได้สำหรับอีเมลนี้")
		}
		at, rt, err := issueStudentOAuthSession(c, student)
		if err != nil {
			return redirectErr(loginPath, "Failed to create student session")
		}
		recordAuthLoginSystemLog(c, nil, "google_student", map[string]any{
			"student_id": student.ID,
			"student_no": student.StudentID,
			"full_name":  student.FullName,
			"email":      student.Email,
			"provider":   "google",
		})
		utils.SetAuthCookies(c, at, rt)
		return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/callback", map[string]string{
			"login": "success",
		}))
	}

	user, oauthAccount := findUserByGoogle(profile, payload.Audience)
	if user == nil {
		return redirectErr(loginPath, "ไม่พบบัญชีที่ใช้งานได้สำหรับการเข้าสู่ระบบนี้")
	}
	if !user.IsActive {
		return redirectErr(loginPath, "บัญชีนี้ถูกระงับการใช้งาน")
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
		return c.Redirect().To(buildFrontendRedirectWithFragment(frontendURL, "/auth/callback", map[string]string{
			"twoFactor": string(twoFactorJSON),
		}))
	}

	at, rt, err := issueOAuthSession(c, user, "google")
	if err != nil {
		return redirectErr(loginPath, "Failed to create session")
	}
	recordAuthLoginSystemLog(c, &user.ID, "google", map[string]any{
		"user_id":   user.ID,
		"username":  user.Username,
		"provider":  "google",
		"loginFlow": "oauth",
	})
	utils.SetAuthCookies(c, at, rt)
	return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/callback", map[string]string{
		"login": "success",
	}))
}
