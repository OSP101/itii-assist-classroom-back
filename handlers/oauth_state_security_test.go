package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestOAuthStateRejectsTamperingExpiryLegacyAndMissingSigningKey(t *testing.T) {
	t.Setenv("JWT_SECRET", "oauth-state-test-key")
	state, err := signOAuthState("google", "link", "link-token", "student")
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := verifyOAuthState(state)
	if !ok || payload.Provider != "google" || payload.Action != "link" || payload.Audience != "student" {
		t.Fatal("signed state lost flow context")
	}
	if _, ok := verifyOAuthState(state + "x"); ok {
		t.Fatal("tampered state accepted")
	}
	for _, expiry := range []int64{0, time.Now().Add(-time.Second).Unix()} {
		payload.ExpiresAt = expiry
		data, _ := json.Marshal(payload)
		encoded := base64.RawURLEncoding.EncodeToString(data)
		mac := hmac.New(sha256.New, []byte("oauth-state-test-key"))
		mac.Write([]byte(encoded))
		if _, ok := verifyOAuthState(encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))); ok {
			t.Fatal("expired or legacy state accepted")
		}
	}
	t.Setenv("JWT_SECRET", "")
	if _, err := signOAuthState("google", "", "", ""); err == nil {
		t.Fatal("state signed without a configured key")
	}
}

func TestOAuthCallbackRequiresInitiatingBrowserAndProvider(t *testing.T) {
	t.Setenv("JWT_SECRET", "oauth-state-browser-test-key")
	t.Setenv("GOOGLE_CLIENT_ID", "fixture-google")
	t.Setenv("GITHUB_CLIENT_ID", "fixture-github")
	for _, provider := range []string{"google", "github"} {
		t.Run(provider, func(t *testing.T) {
			app := fiber.New()
			app.Get("/api/auth/google", GoogleLoginHandler)
			app.Get("/api/auth/google/callback", GoogleCallbackHandler)
			app.Get("/api/auth/github", GitHubLoginHandler)
			app.Get("/api/auth/github/callback", GitHubCallbackHandler)
			startRequest := httptest.NewRequest("GET", "https://app.test/api/auth/"+provider+"?audience=student", nil)
			startRequest.Header.Set("X-Forwarded-Proto", "https")
			start, err := app.Test(startRequest)
			if err != nil {
				t.Fatal(err)
			}
			start.Body.Close()
			authorization, err := url.Parse(start.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := authorization.Query().Get("state")
			var nonce *http.Cookie
			for _, cookie := range start.Cookies() {
				if cookie.Name == "oauth_state_"+provider {
					nonce = cookie
				}
			}
			if nonce == nil || !nonce.HttpOnly || !nonce.Secure || nonce.SameSite != http.SameSiteLaxMode || nonce.Path != "/api/auth/"+provider || nonce.MaxAge != 300 {
				t.Fatalf("unsafe or missing browser nonce: %#v", nonce)
			}
			for _, tc := range []struct {
				name, callbackProvider, cookie string
				valid                          bool
			}{
				{"missing cookie", provider, "", false},
				{"wrong browser", provider, nonce.Name + "=wrong-browser", false},
				{"correct browser", provider, nonce.Name + "=" + nonce.Value, true},
				{"wrong provider", map[string]string{"google": "github", "github": "google"}[provider], "oauth_state_" + map[string]string{"google": "github", "github": "google"}[provider] + "=" + nonce.Value, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest("GET", "https://app.test/api/auth/"+tc.callbackProvider+"/callback?error=access_denied&state="+url.QueryEscape(state), nil)
					req.Header.Set("Cookie", tc.cookie)
					req.Header.Set("X-Forwarded-Proto", "https")
					response, err := app.Test(req)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					location, _ := url.Parse(response.Header.Get("Location"))
					invalid := location.Query().Get("error") == "Invalid OAuth state"
					if invalid == tc.valid {
						t.Fatalf("unexpected callback authorization: %s", location)
					}
					if response.Header.Get("Cache-Control") != "no-store" {
						t.Fatal("OAuth callback may be cached")
					}
					cleared := false
					for _, cookie := range response.Cookies() {
						if cookie.Name == "oauth_state_"+tc.callbackProvider && cookie.MaxAge < 0 {
							cleared = true
						}
					}
					if !cleared {
						t.Fatal("callback retained the nonce cookie")
					}
				})
			}
		})
	}
}
