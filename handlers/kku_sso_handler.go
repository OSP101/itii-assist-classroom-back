package handlers

// KKU Single Sign On (SSONext)
//
// ต่างจาก Google/GitHub ตรงที่ SSONext ไม่มีพารามิเตอร์ state ให้ฝากค่าไปกับ
// authorize request ดังนั้น CSRF และบริบทของการล็อกอิน (audience / link action)
// จึงเก็บไว้ในคุกกี้ kku_sso_nonce ที่เซ็นด้วย HMAC-SHA256 แทน

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const kkuSSONonceCookieName = "kku_sso_nonce"

// =============================================================================
// CSRF via nonce cookie (KKU SSO does not support a state param like OAuth2)
// =============================================================================

type kkuNoncePayload struct {
	Nonce     string `json:"n"`
	Audience  string `json:"u"`           // "" | "student"
	Action    string `json:"a,omitempty"` // "" | "link"
	LinkToken string `json:"l,omitempty"`
	Exp       int64  `json:"e"` // unix timestamp expiry
}

func writeKKUSSONonceCookie(c fiber.Ctx, value string, maxAge int) {
	c.Cookie(&fiber.Cookie{
		Name:     kkuSSONonceCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

func setKKUSSONonce(c fiber.Ctx, audience, action, linkToken string) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	payload := kkuNoncePayload{
		Nonce:     base64.RawURLEncoding.EncodeToString(raw),
		Audience:  audience,
		Action:    action,
		LinkToken: linkToken,
		Exp:       time.Now().Add(5 * time.Minute).Unix(),
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

	writeKKUSSONonceCookie(c, payloadB64+"."+sig, 300)
	return nil
}

func verifyAndClearKKUSSONonce(c fiber.Ctx) (*kkuNoncePayload, bool) {
	cookieValue := c.Cookies(kkuSSONonceCookieName)

	// Clear cookie immediately — one code exchange per login attempt
	writeKKUSSONonceCookie(c, "", -1)

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
// Helpers
// =============================================================================

// kkuRedirectURL คืน Redirect Login URL ที่ต้องส่งไปกับ /auth.token
//
// ฝั่ง SSO เทียบค่านี้กับที่ลงทะเบียนไว้แบบตัวต่อตัว จึงต้องยึด env เป็นหลัก
// แล้วค่อย fallback มาเดาจาก host ของ request (สำหรับ dev ที่ยังไม่ตั้ง env)
func kkuRedirectURL(c fiber.Ctx, cfg services.KKUSSOConfig) string {
	if cfg.RedirectURL != "" {
		return cfg.RedirectURL
	}
	return getOAuthCallbackURL(c, "KKU_SSO_REDIRECT_URL", "/api/auth/kku/callback")
}

// kkuProviderUserID เลือกตัวระบุผู้ใช้ที่คงที่ที่สุดเท่าที่ SSO ส่งมาให้
//
// ลำดับความชอบ: immutableId → profile.userId → citizenId → email:<email>
// (citizenId คือเลขบัตรประชาชน จึงเก็บเป็นทางเลือกสุดท้ายก่อน placeholder)
func kkuProviderUserID(token *services.KKUTokenResponse, profile *services.KKUProfile, email string) string {
	if token != nil && strings.TrimSpace(token.ImmutableID) != "" {
		return strings.TrimSpace(token.ImmutableID)
	}
	if profile != nil && strings.TrimSpace(profile.UserID) != "" {
		return strings.TrimSpace(profile.UserID)
	}
	if token != nil && strings.TrimSpace(token.CitizenID) != "" {
		return strings.TrimSpace(token.CitizenID)
	}
	return "email:" + email
}

// findKKUUserByLegacyCitizenID รองรับแถวที่ผูกไว้ตั้งแต่ก่อนเปลี่ยนมาใช้
// immutableId เป็น provider_user_id แล้วย้ายค่าให้เป็นตัวใหม่
func findKKUUserByLegacyCitizenID(citizenID, providerUserID string) (*models.User, *models.UserOAuthAccount) {
	citizenID = strings.TrimSpace(citizenID)
	if citizenID == "" || citizenID == providerUserID {
		return nil, nil
	}

	var acc models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", "kku", citizenID).First(&acc).Error; err != nil {
		return nil, nil
	}
	var user models.User
	if err := config.DB.First(&user, acc.UserID).Error; err != nil {
		return nil, nil
	}
	config.DB.Model(&acc).Update("provider_user_id", providerUserID)
	acc.ProviderUserID = providerUserID
	return &user, &acc
}

func kkuDisplayName(token *services.KKUTokenResponse, profile *services.KKUProfile) string {
	if profile != nil {
		if name := profile.FullNameTH(); name != "" {
			return name
		}
		if name := profile.FullNameEN(); name != "" {
			return name
		}
	}
	if token != nil {
		return strings.Join(strings.Fields(token.FirstName+" "+token.LastName), " ")
	}
	return ""
}

func upsertKKUOAuthAccount(userID uint, providerUserID, email, displayName, ssoAccessToken string) *models.UserOAuthAccount {
	// กันการยึดบัญชี: ถ้า provider_user_id นี้ผูกกับผู้ใช้คนอื่นอยู่แล้ว ไม่ทำอะไร
	var linked models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", "kku", providerUserID).
		First(&linked).Error; err == nil && linked.UserID != userID {
		return nil
	}

	acc := models.UserOAuthAccount{
		UserID:   userID,
		Provider: "kku",
	}
	config.DB.Where("user_id = ? AND provider = ?", userID, "kku").
		Attrs(models.UserOAuthAccount{
			ProviderUserID: providerUserID,
			ProviderEmail:  email,
			ProviderName:   displayName,
			AccessToken:    ssoAccessToken,
			LinkedAt:       time.Now(),
		}).
		FirstOrCreate(&acc)

	config.DB.Model(&acc).Updates(map[string]interface{}{
		"provider_user_id": providerUserID,
		"provider_email":   email,
		"provider_name":    displayName,
		"access_token":     ssoAccessToken,
	})
	return &acc
}

// kkuProfileMetadata คือข้อมูลเสริมที่บันทึกลง system log เพื่อการตรวจสอบย้อนหลัง
// (ไม่เก็บ citizenId / เบอร์โทร ลง log)
func kkuProfileMetadata(profile *services.KKUProfile) map[string]any {
	if profile == nil {
		return map[string]any{}
	}
	return map[string]any{
		"sso_user_id":   profile.UserID,
		"sso_type":      profile.Type,
		"faculty_name":  profile.FacultyName,
		"position":      profile.PositionName,
		"person_status": profile.PersonStatus,
	}
}

// kkuRequestOnRegisteredDomain บอกว่า request นี้เข้ามาทางโดเมนเดียวกับ
// Redirect Login URL ที่ลงทะเบียนไว้หรือไม่
//
// ระบบเปิดสองประตู คือโดเมนหลักของมหาวิทยาลัยกับโดเมนสำรองผ่าน Cloudflare Tunnel
// แต่ KKU SSO ผูก redirect ไว้กับโดเมนหลักตัวต่อตัว ถ้าเริ่ม flow จากโดเมนสำรอง
// ผู้ใช้จะถูกเด้งกลับไปโดเมนหลักซึ่งตอนนั้นมักล่มอยู่พอดี (เหตุผลที่ต้องมีโดเมน
// สำรองตั้งแต่แรก) จึงตัดจบตรงนี้พร้อมบอกให้ไปใช้ Google แทน
func kkuRequestOnRegisteredDomain(c fiber.Ctx, cfg services.KKUSSOConfig) bool {
	if cfg.RedirectURL == "" {
		return true // ไม่ได้ตั้ง env ไว้ (dev) ปล่อยผ่าน
	}
	registered, err := url.Parse(cfg.RedirectURL)
	if err != nil || registered.Host == "" {
		return true
	}

	current := getRequestPublicBaseURL(c)
	if current == "" {
		return true
	}
	currentURL, err := url.Parse(current)
	if err != nil || currentURL.Host == "" {
		return true
	}

	return strings.EqualFold(currentURL.Hostname(), registered.Hostname())
}

// =============================================================================
// GET /api/auth/kku
// พาเบราว์เซอร์ไปหน้าล็อกอินของ SSONext
//
// Query params:
//
//	audience=student — ล็อกอินฝั่งนักศึกษา (ไม่แตะตาราง users)
//	action=link      — ผูกบัญชี KKU เข้ากับผู้ใช้ที่ล็อกอินอยู่ตอนนี้
//
// =============================================================================

func KKULoginHandler(c fiber.Ctx) error {
	cfg := services.LoadKKUSSOConfig()
	if !cfg.Configured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"message": "KKU SSO ยังไม่ได้ตั้งค่า (ขาด " + strings.Join(cfg.MissingEnvKeys(), ", ") + ")",
		})
	}

	if !kkuRequestOnRegisteredDomain(c, cfg) {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"message": "โดเมนนี้เป็นโดเมนสำรอง ยังใช้ KKU SSO ไม่ได้ กรุณาเข้าสู่ระบบด้วยบัญชี Google",
			"code":    "KKU_SSO_WRONG_DOMAIN",
		})
	}

	audience := strings.ToLower(strings.TrimSpace(c.Query("audience")))
	if audience != "student" {
		audience = ""
	}

	action := strings.ToLower(strings.TrimSpace(c.Query("action")))
	if action != "link" {
		action = ""
	}
	linkToken := linkTokenForRequest(c, action)

	if err := setKKUSSONonce(c, audience, action, linkToken); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to generate session nonce",
		})
	}

	return c.Redirect().To(cfg.LoginURL())
}

// =============================================================================
// GET /api/auth/kku/callback
// Redirect Login URL ที่ลงทะเบียนไว้กับสำนักเทคโนโลยีดิจิทัล
// =============================================================================

func KKUCallbackHandler(c fiber.Ctx) error {
	cfg := services.LoadKKUSSOConfig()
	frontendURL := getFrontendURL(c)
	redirectErr := func(path, msg string) error {
		return c.Redirect().To(frontendURL + path + "?error=" + url.QueryEscape(msg))
	}

	// 1. ตรวจ CSRF nonce
	nonce, ok := verifyAndClearKKUSSONonce(c)
	if !ok {
		return redirectErr("/login", "เซสชันการเข้าสู่ระบบหมดอายุหรือไม่ถูกต้อง กรุณาลองใหม่")
	}

	isLinkAction := nonce.Action == "link"
	loginPath := "/login"
	if nonce.Audience == "student" {
		loginPath = "/student/login"
	}
	failPath := loginPath
	if isLinkAction {
		failPath = "/auth/link-callback"
	}

	if !cfg.Configured() {
		return redirectErr(failPath, "KKU SSO ยังไม่ได้ตั้งค่าบนเซิร์ฟเวอร์")
	}

	// 2. รับ code
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		if ssoErr := strings.TrimSpace(c.Query("error")); ssoErr != "" {
			return redirectErr(failPath, services.KKUSSOErrorMessage(ssoErr))
		}
		return redirectErr(failPath, "ไม่ได้รับรหัสยืนยันจาก KKU SSO")
	}

	// 3. แลก code เป็น access token
	tokenResp, err := cfg.ExchangeKKUCode(c.Context(), code, kkuRedirectURL(c, cfg))
	if err != nil {
		return redirectErr(failPath, "ติดต่อ KKU SSO ไม่สำเร็จ: "+err.Error())
	}
	if !tokenResp.OK {
		return redirectErr(failPath, services.KKUSSOErrorMessage(tokenResp.Error))
	}

	// 4. ดึงโปรไฟล์เต็มจาก /user.profile (ไม่บังคับ — ถ้าล้มเหลวยังล็อกอินต่อได้
	//    ด้วยข้อมูลจาก /auth.token)
	var profile *services.KKUProfile
	if strings.TrimSpace(tokenResp.AccessToken) != "" {
		if p, perr := cfg.FetchKKUProfile(c.Context(), tokenResp.AccessToken); perr == nil {
			profile = p
		} else if errors.Is(perr, services.ErrKKUSSOUnauthorized) {
			return redirectErr(failPath, "โทเคนจาก KKU SSO ใช้งานไม่ได้ กรุณาเข้าสู่ระบบใหม่")
		}
	}

	// 5. อีเมล — จาก auth.token ก่อน แล้วค่อย fallback ไปโปรไฟล์
	email := strings.ToLower(strings.TrimSpace(tokenResp.Email))
	if email == "" && profile != nil {
		email = strings.ToLower(strings.TrimSpace(profile.Email))
	}
	if email == "" {
		return redirectErr(failPath, "KKU SSO ไม่ได้ส่งอีเมลกลับมา")
	}

	providerUserID := kkuProviderUserID(tokenResp, profile, email)
	displayName := kkuDisplayName(tokenResp, profile)

	// =========================================================================
	// Link action — ผูกบัญชี KKU เข้ากับผู้ใช้ที่ล็อกอินอยู่
	// =========================================================================
	if isLinkAction {
		if nonce.LinkToken == "" {
			return redirectErr("/auth/link-callback", "ไม่พบข้อมูลเซสชันสำหรับการผูกบัญชี")
		}
		claims, err := utils.ValidateAccessToken(nonce.LinkToken)
		if err != nil || claims.UserID == 0 {
			return redirectErr("/auth/link-callback", "เซสชันหมดอายุ กรุณาเข้าสู่ระบบใหม่แล้วลองอีกครั้ง")
		}

		var existingUser models.User
		if err := config.DB.First(&existingUser, claims.UserID).Error; err != nil {
			return redirectErr("/auth/link-callback", "ไม่พบบัญชีผู้ใช้")
		}

		var conflict models.UserOAuthAccount
		if err := config.DB.Where("provider = ? AND provider_user_id = ?", "kku", providerUserID).
			First(&conflict).Error; err == nil && conflict.UserID != claims.UserID {
			return redirectErr("/auth/link-callback", "บัญชี KKU นี้ถูกผูกกับผู้ใช้รายอื่นแล้ว")
		}

		if upsertKKUOAuthAccount(existingUser.ID, providerUserID, email, displayName, tokenResp.AccessToken) == nil {
			return redirectErr("/auth/link-callback", "บัญชี KKU นี้ถูกผูกกับผู้ใช้รายอื่นแล้ว")
		}

		at, rt, err := issueOAuthSession(c, &existingUser, "kku")
		if err != nil {
			return redirectErr("/auth/link-callback", "สร้างเซสชันไม่สำเร็จ")
		}
		logPrivilegedAdminAction(c, existingUser.ID, "link_oauth_account", "info", "users",
			fmt.Sprint(existingUser.ID), fiber.Map{"provider": "kku"})
		utils.SetAuthCookies(c, at, rt)
		return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/link-callback", map[string]string{
			"linked": "kku",
		}))
	}

	// =========================================================================
	// Student path — นักศึกษาไม่มีแถวในตาราง users
	// =========================================================================
	if nonce.Audience == "student" {
		student, err := repositories.FindStudentByEmail(email)
		if err != nil || student == nil || !student.IsActive {
			return redirectErr(loginPath, "ไม่พบบัญชีนักศึกษาที่ใช้งานได้สำหรับอีเมลนี้")
		}
		at, rt, err := issueStudentOAuthSession(c, student)
		if err != nil {
			return redirectErr(loginPath, "สร้างเซสชันนักศึกษาไม่สำเร็จ")
		}
		detail := kkuProfileMetadata(profile)
		detail["student_id"] = student.ID
		detail["student_no"] = student.StudentID
		detail["full_name"] = student.FullName
		detail["email"] = email
		detail["provider"] = "kku"
		recordAuthLoginSystemLog(c, nil, "kku_student", detail)
		utils.SetAuthCookies(c, at, rt)
		return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/callback", map[string]string{
			"login": "success",
		}))
	}

	// =========================================================================
	// Staff path — อาจารย์/ทีเอ/แอดมิน ต้องมีบัญชีในระบบอยู่ก่อน
	// =========================================================================
	user, _ := findUserByOAuthAccount("kku", providerUserID, email)
	if user == nil {
		user, _ = findKKUUserByLegacyCitizenID(tokenResp.CitizenID, providerUserID)
	}
	if user == nil {
		var u models.User
		if config.DB.Where("LOWER(email) = ?", email).First(&u).Error == nil &&
			!strings.EqualFold(strings.TrimSpace(u.Role), "student") {
			user = &u
		}
	}
	if user == nil {
		return redirectErr(loginPath, "ไม่พบบัญชีที่ใช้งานได้สำหรับการเข้าสู่ระบบนี้")
	}
	if strings.EqualFold(strings.TrimSpace(user.Role), "student") {
		return redirectErr(loginPath, "บัญชีนี้เป็นบัญชีนักศึกษา กรุณาเข้าสู่ระบบที่หน้านักศึกษา")
	}
	if !user.IsActive {
		return redirectErr(loginPath, "บัญชีนี้ถูกระงับการใช้งาน")
	}

	upsertKKUOAuthAccount(user.ID, providerUserID, email, displayName, tokenResp.AccessToken)

	// 2FA gate — เหมือน flow ของ Google/GitHub
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
		return redirectErr(loginPath, "สร้างเซสชันไม่สำเร็จ")
	}
	detail := kkuProfileMetadata(profile)
	detail["user_id"] = user.ID
	detail["username"] = user.Username
	detail["provider"] = "kku"
	detail["loginFlow"] = "sso"
	recordAuthLoginSystemLog(c, &user.ID, "kku", detail)
	utils.SetAuthCookies(c, at, rt)
	return c.Redirect().To(buildFrontendRedirectWithQuery(frontendURL, "/auth/callback", map[string]string{
		"login": "success",
	}))
}

// =============================================================================
// GET /api/auth/kku/logout
// ออกจากระบบแบบ single logout: ล้างเซสชันฝั่งเรา แล้วส่งต่อไปหน้า logout ของ
// SSONext เพื่อปิดเซสชันกลางด้วย (SSO จะ redirect กลับมาที่ Redirect Logout URL)
//
// ปุ่มออกจากระบบปกติของเว็บไม่เรียกเส้นทางนี้ เพราะการปิดเซสชันกลางจะเตะผู้ใช้
// ออกจากทุกบริการที่ใช้ KKU SSO ร่วมกัน เส้นทางนี้ไว้ใช้เมื่อผู้ใช้ตั้งใจออกจาก
// ทุกบริการจริง ๆ เช่น เครื่องคอมพิวเตอร์ส่วนกลาง
// =============================================================================

func KKULogoutHandler(c fiber.Ctx) error {
	cfg := services.LoadKKUSSOConfig()

	// เพิกถอน refresh token ของเราเองก่อน (best effort — ผู้ใช้อาจล็อกเอาต์ไปแล้ว)
	if refreshToken := c.Cookies(utils.RefreshTokenCookieName); refreshToken != "" {
		if claims, err := utils.ValidateRefreshToken(refreshToken); err == nil {
			_ = repositories.RevokeRefreshToken(claims.JTI)
		}
	}
	utils.ClearAuthCookies(c)
	writeKKUSSONonceCookie(c, "", -1)

	if !cfg.Configured() {
		target := cfg.LogoutRedirectURL
		if target == "" {
			target = getFrontendURL(c) + "/logout"
		}
		return c.Redirect().To(target)
	}
	return c.Redirect().To(cfg.LogoutURL())
}

// =============================================================================
// GET /api/auth/kku/config
// บอกฝั่งหน้าเว็บว่าเปิดใช้ KKU SSO อยู่หรือไม่ (ไม่เปิดเผยความลับใด ๆ)
// =============================================================================

func KKUSSOConfigHandler(c fiber.Ctx) error {
	cfg := services.LoadKKUSSOConfig()
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"enabled": cfg.Configured(),
			// false เมื่อเปิดผ่านโดเมนสำรอง ซึ่งใช้ KKU SSO ไม่ได้
			"domainSupported": kkuRequestOnRegisteredDomain(c, cfg),
			"loginUrl":        "/api/auth/kku",
			"logoutUrl":       "/api/auth/kku/logout",
		},
	})
}
