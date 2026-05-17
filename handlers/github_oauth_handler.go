package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/oauth2"
)

type githubProfile struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func getGitHubOAuthConfig() *oauth2.Config {
	callbackURL := os.Getenv("GITHUB_CALLBACK_URL")
	if callbackURL == "" {
		callbackURL = "http://localhost:8000/api/auth/github/callback"
	}

	return &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  callbackURL,
		Scopes:       []string{"user:email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:   "https://github.com/login/oauth/authorize",
			TokenURL:  "https://github.com/login/oauth/access_token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

func findUserByGitHub(profile githubProfile) (*models.User, *models.UserOAuthAccount) {
	return findUserByOAuthAccount("github", fmt.Sprintf("%d", profile.ID), profile.Email)
}

func githubAPIRequest(ctx context.Context, client *http.Client, endpoint string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "itii-assist-backend")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %s", strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func selectGitHubEmail(emails []githubEmail) string {
	for _, email := range emails {
		if email.Primary && email.Verified && strings.TrimSpace(email.Email) != "" {
			return email.Email
		}
	}
	for _, email := range emails {
		if email.Primary && strings.TrimSpace(email.Email) != "" {
			return email.Email
		}
	}
	for _, email := range emails {
		if email.Verified && strings.TrimSpace(email.Email) != "" {
			return email.Email
		}
	}
	for _, email := range emails {
		if strings.TrimSpace(email.Email) != "" {
			return email.Email
		}
	}
	return ""
}

func fetchGitHubProfile(ctx context.Context, oauthToken *oauth2.Token) (*githubProfile, error) {
	client := getGitHubOAuthConfig().Client(ctx, oauthToken)

	var profile githubProfile
	if err := githubAPIRequest(ctx, client, "https://api.github.com/user", &profile); err != nil {
		return nil, err
	}
	if profile.ID == 0 {
		return nil, fmt.Errorf("invalid github profile data")
	}
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = profile.Login
	}

	if strings.TrimSpace(profile.Email) == "" {
		var emails []githubEmail
		if err := githubAPIRequest(ctx, client, "https://api.github.com/user/emails", &emails); err == nil {
			profile.Email = selectGitHubEmail(emails)
		}
	}

	return &profile, nil
}

// GET /api/auth/github
func GitHubLoginHandler(c fiber.Ctx) error {
	if os.Getenv("GITHUB_CLIENT_ID") == "" {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "GitHub OAuth ยังไม่ได้ตั้งค่า (GITHUB_CLIENT_ID missing)",
		})
	}

	action := c.Query("action")
	linkToken := c.Query("link_token")

	stateStr, err := signOAuthState(action, linkToken, "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to generate state"})
	}

	authURL := getGitHubOAuthConfig().AuthCodeURL(stateStr, oauth2.AccessTypeOnline)
	return c.Redirect().To(authURL)
}

// GET /api/auth/github/callback
func GitHubCallbackHandler(c fiber.Ctx) error {
	frontendURL := getFrontendURL()

	redirectErr := func(path, msg string) error {
		return c.Redirect().To(frontendURL + path + "?error=" + url.QueryEscape(msg))
	}

	stateParam := c.Query("state")
	payload, ok := verifyOAuthState(stateParam)
	if !ok {
		return redirectErr("/login", "Invalid OAuth state")
	}

	isLinkAction := payload.Action == "link"

	if errParam := c.Query("error"); errParam != "" {
		path := "/login"
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, c.Query("error_description", "GitHub login cancelled"))
	}

	code := c.Query("code")
	if code == "" {
		path := "/login"
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, "No authorization code received")
	}

	oauthToken, err := getGitHubOAuthConfig().Exchange(context.Background(), code)
	if err != nil {
		path := "/login"
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, "Failed to exchange OAuth code: "+err.Error())
	}

	profile, err := fetchGitHubProfile(context.Background(), oauthToken)
	if err != nil {
		path := "/login"
		if isLinkAction {
			path = "/auth/link-callback"
		}
		return redirectErr(path, "Failed to fetch GitHub profile")
	}

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
		if strings.EqualFold(strings.TrimSpace(existingUser.Role), "student") {
			return redirectErr("/auth/link-callback", "บัญชีนักศึกษาต้องเข้าสู่ระบบผ่าน Google")
		}

		providerUserID := fmt.Sprintf("%d", profile.ID)
		var conflict models.UserOAuthAccount
		if err := config.DB.Where("provider = ? AND provider_user_id = ?", "github", providerUserID).First(&conflict).Error; err == nil && conflict.UserID != claims.UserID {
			return redirectErr("/auth/link-callback", "This GitHub account is already linked to another user")
		}

		var acc models.UserOAuthAccount
		config.DB.Where("user_id = ? AND provider = ?", claims.UserID, "github").FirstOrCreate(&acc, models.UserOAuthAccount{
			UserID:         claims.UserID,
			Provider:       "github",
			ProviderUserID: providerUserID,
			ProviderEmail:  profile.Email,
			ProviderName:   profile.Name,
			ProviderAvatar: profile.AvatarURL,
			LinkedAt:       time.Now(),
		})
		config.DB.Model(&acc).Updates(map[string]interface{}{
			"provider_user_id": providerUserID,
			"provider_email":   profile.Email,
			"provider_name":    profile.Name,
			"provider_avatar":  profile.AvatarURL,
		})

		at, rt, err := issueOAuthSession(c, &existingUser, "github")
		if err != nil {
			return redirectErr("/auth/link-callback", "Failed to create session")
		}

		return c.Redirect().To(fmt.Sprintf(
			"%s/auth/link-callback?linked=github&accessToken=%s&refreshToken=%s",
			frontendURL, at, rt,
		))
	}

	user, oauthAccount := findUserByGitHub(*profile)
	if user == nil {
		return redirectErr("/login", "ไม่พบบัญชีที่ผูกกับ GitHub นี้ กรุณาติดต่อผู้ดูแลระบบ")
	}
	if !user.IsActive {
		return redirectErr("/login", "บัญชีนี้ถูกระงับการใช้งาน")
	}
	if strings.EqualFold(strings.TrimSpace(user.Role), "student") {
		return redirectErr("/login", "บัญชีนักศึกษาต้องเข้าสู่ระบบผ่าน Google")
	}

	if oauthAccount != nil {
		config.DB.Model(oauthAccount).Updates(map[string]interface{}{
			"provider_email":  profile.Email,
			"provider_name":   profile.Name,
			"provider_avatar": profile.AvatarURL,
		})
	}

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

	at, rt, err := issueOAuthSession(c, user, "github")
	if err != nil {
		return redirectErr("/login", "Failed to create session")
	}

	return c.Redirect().To(fmt.Sprintf(
		"%s/auth/callback?accessToken=%s&refreshToken=%s",
		frontendURL, at, rt,
	))
}
