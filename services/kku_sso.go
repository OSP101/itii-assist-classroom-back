package services

// KKU Single Sign On (SSONext) client.
//
// เอกสารอ้างอิง: "คู่มือการใช้งาน KKU Single Sign On (SSONext)"
// สำนักเทคโนโลยีดิจิทัล มหาวิทยาลัยขอนแก่น
//
//	Login    : <web>/login?app=<AppID>
//	Logout   : <web>/logout?app=<AppID>
//	Token    : POST <api>/auth.token     { code, redirectUrl, clientId, clientSecret }
//	Profile  : POST <api>/user.profile   Authorization: Bearer <accessToken>
//	Status   : POST <api>/auth.status    Authorization: Bearer <accessToken>
//
// ทุก endpoint ตอบ HTTP 200 พร้อมฟิลด์ "ok" (true/false) ยกเว้น user.profile
// และ auth.status ที่ตอบ 401 เมื่อ access token ใช้ไม่ได้

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	kkuSSOProdWebBaseURL = "https://ssonext.kku.ac.th"
	kkuSSOProdAPIBaseURL = "https://ssonext-api.kku.ac.th"
	kkuSSOUATWebBaseURL  = "https://sso-uat-web.kku.ac.th"
	kkuSSOUATAPIBaseURL  = "https://sso-uat-api.kku.ac.th"

	// ขนาดสูงสุดของ response ที่ยอมอ่านจากฝั่ง SSO (กัน response ผิดปกติ/ใหญ่เกิน)
	kkuSSOMaxResponseBytes = 1 << 20 // 1 MB
)

// ErrKKUSSOUnauthorized คืนเมื่อ SSO ตอบ 401 (access token หมดอายุหรือถูกเพิกถอน)
var ErrKKUSSOUnauthorized = errors.New("kku sso: unauthorized")

// KKUSSOConfig เก็บค่าตั้งต้นทั้งหมดของการเชื่อมต่อ SSONext
type KKUSSOConfig struct {
	AppID             string
	ClientID          string
	ClientSecret      string
	RedirectURL       string
	LogoutRedirectURL string
	WebBaseURL        string
	APIBaseURL        string

	// SingleLogout เปิดเมื่อต้องการให้การกดออกจากระบบของเรา ไปปิดเซสชันกลางของ
	// มหาวิทยาลัยด้วย ค่าเริ่มต้นคือปิด เพราะการปิดเซสชันกลางจะทำให้ผู้ใช้หลุด
	// จากทุกบริการที่ใช้ KKU SSO ร่วมกัน ทั้งที่เขาตั้งใจออกจากระบบนี้ระบบเดียว
	SingleLogout bool
}

// Configured บอกว่าตั้งค่าครบพอจะเริ่ม flow ได้หรือยัง
func (cfg KKUSSOConfig) Configured() bool {
	return cfg.AppID != "" && cfg.ClientID != "" && cfg.ClientSecret != ""
}

// MissingEnvKeys คืนรายชื่อ env ที่ยังขาด (ใช้ในข้อความ error / health check)
func (cfg KKUSSOConfig) MissingEnvKeys() []string {
	missing := []string{}
	if cfg.AppID == "" {
		missing = append(missing, "KKU_SSO_APP_ID")
	}
	if cfg.ClientID == "" {
		missing = append(missing, "KKU_SSO_CLIENT_ID")
	}
	if cfg.ClientSecret == "" {
		missing = append(missing, "KKU_SSO_CLIENT_SECRET")
	}
	return missing
}

// LoginURL คือหน้าล็อกอินของ SSONext ที่ต้องพาเบราว์เซอร์ไป
func (cfg KKUSSOConfig) LoginURL() string {
	return cfg.WebBaseURL + "/login?app=" + url.QueryEscape(cfg.AppID)
}

// LogoutURL คือหน้าล็อกเอาต์ของ SSONext (จะ redirect กลับมาที่ Redirect Logout URL
// ที่ลงทะเบียนไว้กับสำนักเทคโนโลยีดิจิทัล)
func (cfg KKUSSOConfig) LogoutURL() string {
	return cfg.WebBaseURL + "/logout?app=" + url.QueryEscape(cfg.AppID)
}

// LoadKKUSSOConfig อ่านค่าจาก environment
//
// KKU_SSO_ENV=uat จะสลับไปใช้ระบบ UAT ทั้งชุด (sso-uat-web / sso-uat-api)
// ส่วน KKU_SSO_WEB_BASE_URL / KKU_SSO_API_BASE_URL ใช้ override รายตัวได้
// AppID จะ fallback เป็น ClientID เมื่อไม่ได้ตั้งค่าไว้ เพราะบางแอปที่สำนัก
// เทคโนโลยีดิจิทัลออกให้ ใช้ค่าเดียวกันทั้งสองช่อง
func LoadKKUSSOConfig() KKUSSOConfig {
	webBase := kkuSSOProdWebBaseURL
	apiBase := kkuSSOProdAPIBaseURL
	if strings.EqualFold(strings.TrimSpace(os.Getenv("KKU_SSO_ENV")), "uat") {
		webBase = kkuSSOUATWebBaseURL
		apiBase = kkuSSOUATAPIBaseURL
	}
	if v := trimBaseURL(os.Getenv("KKU_SSO_WEB_BASE_URL")); v != "" {
		webBase = v
	}
	if v := trimBaseURL(os.Getenv("KKU_SSO_API_BASE_URL")); v != "" {
		apiBase = v
	}

	clientID := strings.TrimSpace(os.Getenv("KKU_SSO_CLIENT_ID"))
	appID := strings.TrimSpace(os.Getenv("KKU_SSO_APP_ID"))
	if appID == "" {
		appID = clientID
	}

	return KKUSSOConfig{
		AppID:             appID,
		ClientID:          clientID,
		ClientSecret:      strings.TrimSpace(os.Getenv("KKU_SSO_CLIENT_SECRET")),
		RedirectURL:       strings.TrimSpace(os.Getenv("KKU_SSO_REDIRECT_URL")),
		LogoutRedirectURL: strings.TrimSpace(os.Getenv("KKU_SSO_LOGOUT_REDIRECT_URL")),
		SingleLogout:      strings.EqualFold(strings.TrimSpace(os.Getenv("KKU_SSO_SINGLE_LOGOUT")), "true"),
		WebBaseURL:        webBase,
		APIBaseURL:        apiBase,
	}
}

func trimBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// KKUTokenResponse คือผลลัพธ์ของ POST /auth.token
type KKUTokenResponse struct {
	OK          bool   `json:"ok"`
	AccessToken string `json:"accessToken"`
	Email       string `json:"email"`
	ImmutableID string `json:"immutableId"`
	CitizenID   string `json:"citizenId"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	EmployeeID  string `json:"employeeId"`
	Error       string `json:"error,omitempty"`
}

// KKUProfile คือ payload ใน POST /user.profile
type KKUProfile struct {
	Email            string `json:"email"`
	UserID           string `json:"userId"`
	Type             string `json:"type"`
	CitizenID        string `json:"citizenId"`
	Title            string `json:"title"`
	Firstname        string `json:"firstname"`
	Lastname         string `json:"lastname"`
	TitleEng         string `json:"titleEng"`
	FirstnameEng     string `json:"firstnameEng"`
	LastnameEng      string `json:"lastnameEng"`
	FacultyName      string `json:"facultyName"`
	PositionName     string `json:"positionName"`
	PositionTypeName string `json:"positionTypeName"`
	LevelID          string `json:"levelId"`
	LevelName        string `json:"levelName"`
	Gender           string `json:"gender"`
	Workline         string `json:"workline"`
	PhoneNumber      string `json:"phoneNumber"`
	PersonStatus     string `json:"personStatus"`
	LastVerify       string `json:"lastVerify"`
}

// FullNameTH คืนชื่อเต็มภาษาไทย (ตัดช่องว่างซ้ำออก)
func (p KKUProfile) FullNameTH() string {
	return strings.Join(strings.Fields(p.Firstname+" "+p.Lastname), " ")
}

// FullNameEN คืนชื่อเต็มภาษาอังกฤษ
func (p KKUProfile) FullNameEN() string {
	return strings.Join(strings.Fields(p.FirstnameEng+" "+p.LastnameEng), " ")
}

type kkuProfileResponse struct {
	OK      bool       `json:"ok"`
	Profile KKUProfile `json:"profile"`
	Error   string     `json:"error,omitempty"`
}

// KKUAuthStatusUser คือ payload ใน POST /auth.status
type KKUAuthStatusUser struct {
	SessionID string `json:"sessionId"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

type kkuAuthStatusResponse struct {
	OK    bool              `json:"ok"`
	User  KKUAuthStatusUser `json:"user"`
	Error string            `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// HTTP client
// ---------------------------------------------------------------------------

var kkuSSOHTTPClient = &http.Client{Timeout: 15 * time.Second}

func (cfg KKUSSOConfig) postJSON(ctx context.Context, path string, body any, bearer string) ([]byte, int, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+path, payload)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := kkuSSOHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, kkuSSOMaxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// ExchangeKKUCode แลก code จาก callback เป็น access token
//
// redirectURL ต้องตรงกับ Redirect Login URL ที่ลงทะเบียนไว้แบบตัวต่อตัว
// ไม่งั้นฝั่ง SSO จะปฏิเสธ
func (cfg KKUSSOConfig) ExchangeKKUCode(ctx context.Context, code, redirectURL string) (*KKUTokenResponse, error) {
	raw, status, err := cfg.postJSON(ctx, "/auth.token", map[string]string{
		"code":         code,
		"redirectUrl":  redirectURL,
		"clientId":     cfg.ClientID,
		"clientSecret": cfg.ClientSecret,
	}, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kku sso: auth.token ตอบ HTTP %d", status)
	}

	var tokenResp KKUTokenResponse
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return nil, fmt.Errorf("kku sso: อ่านผลลัพธ์ auth.token ไม่ได้: %w", err)
	}
	return &tokenResp, nil
}

// FetchKKUProfile ดึงข้อมูลผู้ใช้จาก /user.profile
func (cfg KKUSSOConfig) FetchKKUProfile(ctx context.Context, accessToken string) (*KKUProfile, error) {
	raw, status, err := cfg.postJSON(ctx, "/user.profile", nil, accessToken)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, ErrKKUSSOUnauthorized
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kku sso: user.profile ตอบ HTTP %d", status)
	}

	var profileResp kkuProfileResponse
	if err := json.Unmarshal(raw, &profileResp); err != nil {
		return nil, fmt.Errorf("kku sso: อ่านผลลัพธ์ user.profile ไม่ได้: %w", err)
	}
	if !profileResp.OK {
		if profileResp.Error != "" {
			return nil, fmt.Errorf("kku sso: user.profile ผิดพลาด (%s)", profileResp.Error)
		}
		return nil, errors.New("kku sso: user.profile ผิดพลาด")
	}
	return &profileResp.Profile, nil
}

// FetchKKUAuthStatus ตรวจว่า access token ยังใช้งานได้ (session ฝั่ง SSO ยังอยู่)
func (cfg KKUSSOConfig) FetchKKUAuthStatus(ctx context.Context, accessToken string) (*KKUAuthStatusUser, error) {
	raw, status, err := cfg.postJSON(ctx, "/auth.status", nil, accessToken)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, ErrKKUSSOUnauthorized
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kku sso: auth.status ตอบ HTTP %d", status)
	}

	var statusResp kkuAuthStatusResponse
	if err := json.Unmarshal(raw, &statusResp); err != nil {
		return nil, fmt.Errorf("kku sso: อ่านผลลัพธ์ auth.status ไม่ได้: %w", err)
	}
	if !statusResp.OK {
		return nil, ErrKKUSSOUnauthorized
	}
	return &statusResp.User, nil
}

// KKUSSOErrorMessage แปลงรหัสข้อผิดพลาดจาก SSO เป็นข้อความภาษาไทยที่ผู้ใช้อ่านรู้เรื่อง
func KKUSSOErrorMessage(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "":
		return "เข้าสู่ระบบผ่าน KKU SSO ไม่สำเร็จ"
	case "AUTH0001":
		return "รหัสยืนยันจาก KKU SSO ไม่ถูกต้องหรือหมดอายุ กรุณาเข้าสู่ระบบใหม่"
	default:
		return "เข้าสู่ระบบผ่าน KKU SSO ไม่สำเร็จ (รหัส " + strings.TrimSpace(code) + ")"
	}
}
