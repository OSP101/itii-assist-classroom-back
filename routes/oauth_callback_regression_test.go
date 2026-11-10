package routes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"itii-assist/models"
	"itii-assist/utils"
)

type fixtureOAuthTransport func(*http.Request) (*http.Response, error)

func (f fixtureOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The real callbacks exchange a code and load an identity through a controlled
// transport. No request can escape to an actual provider from these tests.
func TestOAuthCallbacksExchangeIdentityAndRespectTwoFactor(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		for _, twoFactor := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/two_factor_%t", provider, twoFactor), func(t *testing.T) {
				app, db, _ := securityFixture(t)
				t.Setenv("GOOGLE_CLIENT_ID", "fixture-google")
				t.Setenv("GOOGLE_CLIENT_SECRET", "fixture-google-secret")
				t.Setenv("GITHUB_CLIENT_ID", "fixture-github")
				t.Setenv("GITHUB_CLIENT_SECRET", "fixture-github-secret")
				if err := db.Model(&models.User{}).Where("id = ?", 1).Updates(map[string]any{"two_factor_enabled": twoFactor, "two_factor_method": "email"}).Error; err != nil {
					t.Fatal(err)
				}
				providerID := "fixture-google-id"
				if provider == "github" {
					providerID = "12345"
				}
				if err := db.Create(&models.UserOAuthAccount{UserID: 1, Provider: provider, ProviderUserID: providerID, ProviderEmail: "ta@example.test", LinkedAt: time.Now()}).Error; err != nil {
					t.Fatal(err)
				}
				previousTransport := http.DefaultTransport
				exchanged, loaded := false, false
				http.DefaultTransport = fixtureOAuthTransport(func(r *http.Request) (*http.Response, error) {
					body := ""
					switch r.URL.Host + r.URL.Path {
					case "oauth2.googleapis.com/token", "github.com/login/oauth/access_token":
						if err := r.ParseForm(); err != nil {
							return nil, err
						}
						if r.Form.Get("code") != "fixture-code" || r.Form.Get("redirect_uri") != "https://app.test/api/auth/"+provider+"/callback" {
							return nil, fmt.Errorf("invalid code exchange contract")
						}
						exchanged = true
						body = `{"access_token":"fixture-provider-token","token_type":"Bearer","expires_in":3600}`
					case "www.googleapis.com/oauth2/v3/userinfo":
						loaded = true
						body = `{"sub":"fixture-google-id","email":"ta@example.test","email_verified":true,"name":"Fixture TA"}`
					case "api.github.com/user":
						loaded = true
						body = `{"id":12345,"login":"fixture-ta","name":"Fixture TA","email":"ta@example.test"}`
					case "api.github.com/user/emails":
						body = `[{"email":"ta@example.test","primary":true,"verified":true}]`
					default:
						return nil, fmt.Errorf("unexpected provider request: %s", r.URL)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = previousTransport })
				startRequest := httptest.NewRequest("GET", "https://app.test/api/auth/"+provider, nil)
				startRequest.Header.Set("X-Forwarded-Proto", "https")
				start, err := app.Test(startRequest)
				if err != nil {
					t.Fatal(err)
				}
				start.Body.Close()
				location, _ := url.Parse(start.Header.Get("Location"))
				callback := httptest.NewRequest("GET", "https://app.test/api/auth/"+provider+"/callback?code=fixture-code&state="+url.QueryEscape(location.Query().Get("state")), nil)
				callback.Header.Set("X-Forwarded-Proto", "https")
				for _, cookie := range start.Cookies() {
					callback.AddCookie(cookie)
				}
				response, err := app.Test(callback)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if !exchanged || !loaded {
					t.Fatal("callback skipped the identity exchange")
				}
				redirect, _ := url.Parse(response.Header.Get("Location"))
				access := ""
				for _, cookie := range response.Cookies() {
					if cookie.Name == utils.AccessTokenCookieName {
						access = cookie.Value
					}
				}
				if twoFactor {
					fragment, _ := url.ParseQuery(redirect.Fragment)
					var challenge struct {
						Challenge         string `json:"challenge"`
						RequiresTwoFactor bool   `json:"requiresTwoFactor"`
					}
					if err := json.Unmarshal([]byte(fragment.Get("twoFactor")), &challenge); err != nil {
						t.Fatal(err)
					}
					if !challenge.RequiresTwoFactor || len(challenge.Challenge) != 43 || access != "" {
						t.Fatalf("2FA gate failed: %s", redirect)
					}
					var count int64
					db.Model(&models.LoginChallenge{}).Where("user_id = ?", 1).Count(&count)
					if count != 1 {
						t.Fatal("callback did not persist its challenge")
					}
				} else {
					if redirect.Query().Get("login") != "success" || access == "" {
						t.Fatalf("OAuth login failed: %s", redirect)
					}
					if code, body := securityRequest(t, app, "GET", "/api/auth/me", "", access); code != 200 {
						t.Fatalf("OAuth session unusable: %d %s", code, body)
					}
				}
			})
		}
	}
}

func TestGoogleStudentOAuthRequiresVerifiedActiveRegisteredIdentity(t *testing.T) {
	for _, tc := range []struct {
		name             string
		active, verified bool
	}{
		{"active verified student", true, true},
		{"disabled student", false, true},
		{"unverified email", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, _ := securityFixture(t)
			t.Setenv("GOOGLE_CLIENT_ID", "fixture-google")
			t.Setenv("GOOGLE_CLIENT_SECRET", "fixture-secret")
			if err := db.Model(&models.Student{}).Where("id = ?", 11).Update("is_active", tc.active).Error; err != nil {
				t.Fatal(err)
			}
			previous := http.DefaultTransport
			http.DefaultTransport = fixtureOAuthTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch r.URL.Host + r.URL.Path {
				case "oauth2.googleapis.com/token":
					body = `{"access_token":"fixture-token","token_type":"Bearer","expires_in":3600}`
				case "www.googleapis.com/oauth2/v3/userinfo":
					body = fmt.Sprintf(`{"sub":"fixture-student-google-id","email":"a@example.test","email_verified":%t,"name":"Fixture Student"}`, tc.verified)
				default:
					return nil, fmt.Errorf("unexpected identity-provider request")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			req := httptest.NewRequest("GET", "https://app.test/api/auth/google?audience=student", nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			start, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			start.Body.Close()
			location, _ := url.Parse(start.Header.Get("Location"))
			req = httptest.NewRequest("GET", "https://app.test/api/auth/google/callback?code=fixture&state="+url.QueryEscape(location.Query().Get("state")), nil)
			req.Header.Set("X-Forwarded-Proto", "https")
			for _, cookie := range start.Cookies() {
				req.AddCookie(cookie)
			}
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			access := ""
			for _, cookie := range response.Cookies() {
				if cookie.Name == utils.AccessTokenCookieName {
					access = cookie.Value
				}
			}
			if !tc.active || !tc.verified {
				if access != "" {
					t.Fatal("ineligible student received a session")
				}
				return
			}
			claims, err := utils.ValidateAccessToken(access)
			if err != nil || claims.Kind != "s" || claims.UserID != 11 {
				t.Fatal("student received the wrong session principal")
			}
			if status, body := securityRequest(t, app, "GET", "/api/auth/me", "", access); status != 200 {
				t.Fatalf("student session failed: %d %s", status, body)
			}
		})
	}
}

func TestOAuthLinkRejectsStudentPrincipalCollisionAndRevokedSession(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		for _, principal := range []string{"anonymous", "student", "revoked"} {
			t.Run(provider+"/"+principal, func(t *testing.T) {
				app, db, tokens := securityFixture(t)
				t.Setenv("GOOGLE_CLIENT_ID", "fixture-google")
				t.Setenv("GITHUB_CLIENT_ID", "fixture-github")
				// Deliberately overlap the staff and student primary-key namespaces.
				if err := db.Create(&models.User{ID: 11, Username: "collision-admin", PasswordHash: "unused", Role: "admin", IsActive: true}).Error; err != nil {
					t.Fatal(err)
				}
				token, want := "", 401
				if principal == "student" {
					token, want = tokens["a"], 403
				}
				if principal == "revoked" {
					token = tokens["ta"]
					if err := db.Model(&models.RefreshToken{}).Where("user_id = ? AND kind = ?", 1, "u").Update("revoked", true).Error; err != nil {
						t.Fatal(err)
					}
				}
				code, body := securityRequest(t, app, "GET", "/api/auth/"+provider+"?action=link", "", token)
				if code != want {
					t.Fatalf("link initiation got %d want %d: %s", code, want, body)
				}
				var count int64
				if err := db.Model(&models.UserOAuthAccount{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("unauthorized principal created a link")
				}
			})
		}
	}
}

func TestOAuthLinkRechecksPrincipalAtCallback(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		for _, change := range []string{"none", "revoked", "disabled", "role_changed"} {
			t.Run(provider+"/"+change, func(t *testing.T) {
				app, db, tokens := securityFixture(t)
				t.Setenv("GOOGLE_CLIENT_ID", "fixture-google")
				t.Setenv("GOOGLE_CLIENT_SECRET", "fixture-secret")
				t.Setenv("GITHUB_CLIENT_ID", "fixture-github")
				t.Setenv("GITHUB_CLIENT_SECRET", "fixture-secret")
				previous := http.DefaultTransport
				http.DefaultTransport = fixtureOAuthTransport(func(r *http.Request) (*http.Response, error) {
					body := ""
					switch r.URL.Host + r.URL.Path {
					case "oauth2.googleapis.com/token", "github.com/login/oauth/access_token":
						body = `{"access_token":"fixture-token","token_type":"Bearer","expires_in":3600}`
					case "www.googleapis.com/oauth2/v3/userinfo":
						body = `{"sub":"new-google-link","email":"ta@example.test","email_verified":true}`
					case "api.github.com/user":
						body = `{"id":9876,"login":"new-link","email":"ta@example.test"}`
					case "api.github.com/user/emails":
						body = `[{"email":"ta@example.test","primary":true,"verified":true}]`
					default:
						return nil, fmt.Errorf("unexpected provider request: %s", r.URL)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = previous })
				req := httptest.NewRequest("GET", "https://app.test/api/auth/"+provider+"?action=link", nil)
				req.Header.Set("X-Forwarded-Proto", "https")
				req.Header.Set("Authorization", "Bearer "+tokens["ta"])
				start, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				start.Body.Close()
				if start.StatusCode != 302 && start.StatusCode != 303 {
					t.Fatalf("initiation: %d", start.StatusCode)
				}
				switch change {
				case "revoked":
					err = db.Model(&models.RefreshToken{}).Where("user_id = ? AND kind = ?", 1, "u").Update("revoked", true).Error
				case "disabled":
					err = db.Model(&models.User{}).Where("id = ?", 1).Update("is_active", false).Error
				case "role_changed":
					err = db.Model(&models.User{}).Where("id = ?", 1).Update("role", "admin").Error
				}
				if err != nil {
					t.Fatal(err)
				}
				location, _ := url.Parse(start.Header.Get("Location"))
				req = httptest.NewRequest("GET", "https://app.test/api/auth/"+provider+"/callback?code=fixture&state="+url.QueryEscape(location.Query().Get("state")), nil)
				req.Header.Set("X-Forwarded-Proto", "https")
				for _, cookie := range start.Cookies() {
					req.AddCookie(cookie)
				}
				response, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				var count int64
				if err := db.Model(&models.UserOAuthAccount{}).Where("user_id = ?", 1).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				access := ""
				for _, cookie := range response.Cookies() {
					if cookie.Name == utils.AccessTokenCookieName {
						access = cookie.Value
					}
				}
				if change == "none" {
					if count != 1 || access == "" {
						t.Fatalf("valid staff could not link: %s", response.Header.Get("Location"))
					}
				} else if count != 0 || access != "" {
					t.Fatalf("stale principal linked or received a session: %s", change)
				}
			})
		}
	}
}
