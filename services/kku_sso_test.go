package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadKKUSSOConfigDefaultsToProduction(t *testing.T) {
	t.Setenv("KKU_SSO_ENV", "")
	t.Setenv("KKU_SSO_WEB_BASE_URL", "")
	t.Setenv("KKU_SSO_API_BASE_URL", "")
	t.Setenv("KKU_SSO_CLIENT_ID", "client-123")
	t.Setenv("KKU_SSO_APP_ID", "")
	t.Setenv("KKU_SSO_CLIENT_SECRET", "secret")

	cfg := LoadKKUSSOConfig()
	if cfg.WebBaseURL != kkuSSOProdWebBaseURL || cfg.APIBaseURL != kkuSSOProdAPIBaseURL {
		t.Fatalf("expected production endpoints, got %s / %s", cfg.WebBaseURL, cfg.APIBaseURL)
	}
	// AppID ต้อง fallback มาเป็น ClientID เมื่อไม่ได้ตั้งค่า
	if cfg.AppID != "client-123" {
		t.Fatalf("expected AppID to fall back to client id, got %q", cfg.AppID)
	}
	if !cfg.Configured() {
		t.Fatalf("expected config to be usable, missing: %v", cfg.MissingEnvKeys())
	}
	if cfg.LoginURL() != kkuSSOProdWebBaseURL+"/login?app=client-123" {
		t.Fatalf("unexpected login url: %s", cfg.LoginURL())
	}
	if cfg.LogoutURL() != kkuSSOProdWebBaseURL+"/logout?app=client-123" {
		t.Fatalf("unexpected logout url: %s", cfg.LogoutURL())
	}
}

func TestLoadKKUSSOConfigUATSwitch(t *testing.T) {
	t.Setenv("KKU_SSO_ENV", "uat")
	t.Setenv("KKU_SSO_WEB_BASE_URL", "")
	t.Setenv("KKU_SSO_API_BASE_URL", "")

	cfg := LoadKKUSSOConfig()
	if cfg.WebBaseURL != kkuSSOUATWebBaseURL || cfg.APIBaseURL != kkuSSOUATAPIBaseURL {
		t.Fatalf("expected UAT endpoints, got %s / %s", cfg.WebBaseURL, cfg.APIBaseURL)
	}
}

func TestMissingEnvKeys(t *testing.T) {
	t.Setenv("KKU_SSO_APP_ID", "")
	t.Setenv("KKU_SSO_CLIENT_ID", "")
	t.Setenv("KKU_SSO_CLIENT_SECRET", "")

	cfg := LoadKKUSSOConfig()
	if cfg.Configured() {
		t.Fatal("expected config to be incomplete")
	}
	missing := strings.Join(cfg.MissingEnvKeys(), ",")
	if !strings.Contains(missing, "KKU_SSO_CLIENT_ID") || !strings.Contains(missing, "KKU_SSO_CLIENT_SECRET") {
		t.Fatalf("unexpected missing keys: %s", missing)
	}
}

func TestExchangeKKUCodeSendsDocumentedPayload(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.token" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"accessToken":"tok","email":"someone@kku.ac.th","immutableId":"imm-1"}`))
	}))
	defer server.Close()

	cfg := KKUSSOConfig{ClientID: "cid", ClientSecret: "csecret", APIBaseURL: server.URL}
	resp, err := cfg.ExchangeKKUCode(context.Background(), "the-code", "https://app.example/api/auth/kku/callback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK || resp.AccessToken != "tok" || resp.ImmutableID != "imm-1" {
		t.Fatalf("unexpected token response: %+v", resp)
	}
	if got["code"] != "the-code" || got["clientId"] != "cid" || got["clientSecret"] != "csecret" ||
		got["redirectUrl"] != "https://app.example/api/auth/kku/callback" {
		t.Fatalf("unexpected request body: %+v", got)
	}
}

func TestExchangeKKUCodeFailureKeepsErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"AUTH0001"}`))
	}))
	defer server.Close()

	cfg := KKUSSOConfig{APIBaseURL: server.URL}
	resp, err := cfg.ExchangeKKUCode(context.Background(), "c", "r")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.OK || resp.Error != "AUTH0001" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if msg := KKUSSOErrorMessage(resp.Error); !strings.Contains(msg, "หมดอายุ") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestFetchKKUProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"profile":{"email":"a@kku.ac.th","userId":"u-1","type":"STAFF","firstname":"สมชาย","lastname":"ใจดี"}}`))
	}))
	defer server.Close()

	cfg := KKUSSOConfig{APIBaseURL: server.URL}
	profile, err := cfg.FetchKKUProfile(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.UserID != "u-1" || profile.FullNameTH() != "สมชาย ใจดี" {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	if _, err := cfg.FetchKKUProfile(context.Background(), "wrong"); err != ErrKKUSSOUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestFetchKKUAuthStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"user":{"sessionId":"s-1","email":"a@kku.ac.th","role":"STAFF"}}`))
	}))
	defer server.Close()

	cfg := KKUSSOConfig{APIBaseURL: server.URL}
	user, err := cfg.FetchKKUAuthStatus(context.Background(), "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.SessionID != "s-1" || user.Role != "STAFF" {
		t.Fatalf("unexpected status user: %+v", user)
	}
}
