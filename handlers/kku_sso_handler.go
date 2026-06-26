package handlers

import (
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
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// CSRF via nonce cookie (KKU SSO does not support a state param like OAuth2)
// =============================================================================

type kkuNoncePayload struct {
	Nonce    string `json:"n"`
	Audience string `json:"u"` // "" | "student"
	Exp      int64  `json:"e"` // unix timestamp expiry
}

func setKKUSSONonce(c fiber.Ctx, audience string) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	payload := kkuNoncePayload{
		Nonce:    base64.RawURLEncoding.EncodeToString(raw),
		Audience: audience,
		Exp:      time.Now().Add(5 * time.Minute).Unix(),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	key := []byte(os.Getenv("JWT_SECRET"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payloadB64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	cookieValue := payloadB64 + "." + sig

	c.Cookie(&fiber.Cookie{
		Name:     "kku_sso_nonce",
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   300,
		Secure:   true,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	return nil
}

func verifyAndClearKKUSSONonce(c fiber.Ctx) (*kkuNoncePayload, bool) {
	cookieValue := c.Cookies("kku_sso_nonce")

	// Clear cookie immediately
	c.Cookie(&fiber.Cookie{
		Name:     "kku_sso_nonce",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	if cookieValue == "" {
		return nil, false
	}

	parts := strings.SplitN(cookieValue, ".", 2)
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
	var payload kkuNoncePayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, false
	}
	if payload.Exp <= time.Now().Unix() {
		return nil, false
	}
	return &payload, true
}

// =============================================================================
// KKU SSO API types
// =============================================================================

type kkuTokenResponse struct {
	OK          bool   `json:"ok"`
	AccessToken string `json:"accessToken"`
	Email       string `json:"email"`
	CitizenID   string `json:"citizenId"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	EmployeeID  string `json:"employeeId"`
	Error       string `json:"error,omitempty"`
}

// =============================================================================
// KKU API helpers
// =============================================================================

func kkuExchangeCode(code, redirectURL, clientID, clientSecret string) (*kkuTokenResponse, error) {
	body, err := json.Marshal(map[string]string{
		"code":         code,
		"redirectUrl":  redirectURL,
		"clientId":     clientID,
		"clientSecret": clientSecret,
	})
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		"https://ssonext-api.kku.ac.th/auth.token",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResp kkuTokenResponse
	if err := json.Unmarshal(respBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse KKU token response: %w", err)
	}
	return &tokenResp, nil
}

func upsertKKUOAuthAccount(userID uint, citizenID, email, firstName, lastName string) {
	acc := models.UserOAuthAccount{
		UserID:         userID,
		Provider:       "kku",
		ProviderUserID: citizenID,
		ProviderEmail:  email,
		ProviderName:   strings.TrimSpace(firstName + " " + lastName),
		LinkedAt:       time.Now(),
	}
	config.DB.Where("user_id = ? AND provider = ?", userID, "kku").FirstOrCreate(&acc, acc)
	config.DB.Model(&acc).Updates(map[string]interface{}{
		"provider_user_id": citizenID,
		"provider_email":   email,
		"provider_name":    strings.TrimSpace(firstName + " " + lastName),
	})
}

// =============================================================================
// Handlers
// =============================================================================

func KKULoginHandler(c fiber.Ctx) error {
	if os.Getenv("KKU_SSO_APP_ID") == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"message": "KKU SSO ยังไม่ได้ตั้งค่า (KKU_SSO_APP_ID missing)",
		})
	}

	audience := c.Query("audience")
	if audience != "student" {
		audience = ""
	}

	if err := setKKUSSONonce(c, audience); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to generate session nonce",
		})
	}

	loginURL := "https://ssonext.kku.ac.th/login?app=" + url.QueryEscape(os.Getenv("KKU_SSO_APP_ID"))
	return c.Redirect().To(loginURL)
}

func KKUCallbackHandler(c fiber.Ctx) error {
	frontendURL := getFrontendURL(c)
	redirectErr := func(path, msg string) error {
		return c.Redirect().To(frontendURL + path + "?error=" + url.QueryEscape(msg))
	}

	// 1. Verify CSRF nonce cookie
	nonce, ok := verifyAndClearKKUSSONonce(c)
	if !ok {
		return redirectErr("/login", "Invalid or expired session state")
	}

	loginPath := "/login"
	if nonce.Audience == "student" {
		loginPath = "/student/login"
	}

	// 2. Check for code
	code := c.Query("code")
	if code == "" {
		return redirectErr(loginPath, "No authorization code received from KKU SSO")
	}

	// 3. Determine redirect URL (needed by KKU API)
	redirectURL := getOAuthCallbackURL(c, "KKU_SSO_REDIRECT_URL", "/api/auth/kku/callback")

	// 4. Exchange code for token
	tokenResp, err := kkuExchangeCode(code, redirectURL, os.Getenv("KKU_SSO_CLIENT_ID"), os.Getenv("KKU_SSO_CLIENT_SECRET"))
	if err != nil {
		return redirectErr(loginPath, "Failed to contact KKU SSO: "+err.Error())
	}
	if !tokenResp.OK {
		msg := tokenResp.Error
		if msg == "" {
			msg = "KKU SSO authentication failed"
		}
		return redirectErr(loginPath, msg)
	}

	// 5. Validate email
	email := strings.ToLower(strings.TrimSpace(tokenResp.Email))
	if email == "" {
		return redirectErr(loginPath, "KKU SSO did not return a valid email address")
	}

	// 6. citizenId fallback
	citizenID := strings.TrimSpace(tokenResp.CitizenID)
	if citizenID == "" {
		citizenID = "email:" + email
	}

	// === Student path ===
	if nonce.Audience == "student" {
		student, err := repositories.FindStudentByEmail(email)
		if err != nil || student == nil || !student.IsActive {
			return redirectErr(loginPath, "ไม่พบบัญชีนักศึกษาที่ใช้งานได้สำหรับอีเมลนี้")
		}
		at, rt, err := issueStudentOAuthSession(c, student)
		if err != nil {
			return redirectErr(loginPath, "Failed to create student session")
		}
		recordAuthLoginSystemLog(c, nil, "kku_student", map[string]any{
			"student_id": student.ID,
			"email":      email,
			"provider":   "kku",
		})
		return c.Redirect().To(buildFrontendRedirectWithFragment(frontendURL, "/auth/callback", map[string]string{
			"accessToken":  at,
			"refreshToken": rt,
		}))
	}

	// === Staff path ===
	var user *models.User
	var oauthAcc *models.UserOAuthAccount
	user, oauthAcc = findUserByOAuthAccount("kku", citizenID, email)
	if user == nil {
		// fallback: lookup by email
		var u models.User
		if config.DB.Where("LOWER(email) = ?", email).First(&u).Error == nil &&
			!strings.EqualFold(strings.TrimSpace(u.Role), "student") {
			user = &u
		}
	}
	_ = oauthAcc

	if user == nil {
		return redirectErr(loginPath, "ไม่พบบัญชีที่ใช้งานได้สำหรับการเข้าสู่ระบบนี้")
	}
	if !user.IsActive {
		return redirectErr(loginPath, "บัญชีนี้ถูกระงับการใช้งาน")
	}

	upsertKKUOAuthAccount(user.ID, citizenID, email, tokenResp.FirstName, tokenResp.LastName)

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

	at, rt, err := issueOAuthSession(c, user, "kku")
	if err != nil {
		return redirectErr(loginPath, "Failed to create session")
	}
	recordAuthLoginSystemLog(c, &user.ID, "kku", map[string]any{
		"user_id":   user.ID,
		"username":  user.Username,
		"provider":  "kku",
		"loginFlow": "sso",
	})
	return c.Redirect().To(buildFrontendRedirectWithFragment(frontendURL, "/auth/callback", map[string]string{
		"accessToken":  at,
		"refreshToken": rt,
	}))
}
