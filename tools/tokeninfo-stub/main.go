// Command tokeninfo-stub is a drop-in replacement for Google's
// https://oauth2.googleapis.com/tokeninfo endpoint, used only for load
// testing (see ../../scripts/loadtest). Point the backend at it with:
//
//	GOOGLE_TOKENINFO_URL=http://127.0.0.1:9911/tokeninfo
//
// It never talks to Google and understands only the fake "id_token" format
// this stub and the k6 script agree on: base64url(JSON{email, sub, aud}).
// This lets a load test exercise the exact same code path anonymous
// students take (handlers/attendance_handler.go verifyGoogleIDToken)
// including a configurable artificial latency, without depending on
// Google's real service or real Google accounts (plan.md "ระยะ 0.3",
// Attack 9 — Google tokeninfo degradation).
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type stubTokenPayload struct {
	Email string `json:"email"`
	Sub   string `json:"sub"`
	Aud   string `json:"aud"`
}

type tokenInfoResponse struct {
	IssuedTo      string `json:"issued_to"`
	Audience      string `json:"aud"`
	UserID        string `json:"user_id"`
	Scope         string `json:"scope"`
	ExpiresIn     string `json:"expires_in"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	AccessType    string `json:"access_type"`
	IssuedAt      string `json:"iat"`
	ExpiresAt     string `json:"exp"`
	Issuer        string `json:"iss"`
	Subject       string `json:"sub"`
	ErrorDesc     string `json:"error_description,omitempty"`
}

// EncodeStubToken builds the fake id_token this stub accepts. Exported-style
// helper duplicated (not imported) in scripts/loadtest/seed so the seed tool
// has no compile dependency on this binary; keep the two in sync if the
// token shape ever changes.
func EncodeStubToken(email, sub, aud string) string {
	payload := stubTokenPayload{Email: email, Sub: sub, Aud: aud}
	raw, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func main() {
	addr := flag.String("addr", ":9911", "listen address")
	baseDelay := flag.Duration("delay", envDuration("TOKENINFO_STUB_DELAY", 0), "artificial latency applied to every response (env TOKENINFO_STUB_DELAY, e.g. 3s)")
	flag.Parse()

	http.HandleFunc("/tokeninfo", func(w http.ResponseWriter, r *http.Request) {
		delay := *baseDelay
		if override := r.URL.Query().Get("delay_ms"); override != "" {
			if ms, err := strconv.Atoi(override); err == nil && ms >= 0 {
				delay = time.Duration(ms) * time.Millisecond
			}
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}

		idToken := r.URL.Query().Get("id_token")
		payload, err := decodeStubToken(idToken)
		if err != nil {
			writeError(w, "invalid_token")
			return
		}

		now := time.Now()
		resp := tokenInfoResponse{
			IssuedTo:      payload.Aud,
			Audience:      payload.Aud,
			UserID:        payload.Sub,
			Scope:         "openid email profile",
			ExpiresIn:     "3600",
			Email:         payload.Email,
			EmailVerified: "true",
			AccessType:    "online",
			IssuedAt:      strconv.FormatInt(now.Unix(), 10),
			ExpiresAt:     strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			Issuer:        "accounts.google.com",
			Subject:       payload.Sub,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Printf("tokeninfo-stub listening on %s (base delay=%s)", *addr, *baseDelay)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

func decodeStubToken(idToken string) (*stubTokenPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(idToken))
	if err != nil {
		return nil, err
	}
	var payload stubTokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Email == "" || payload.Sub == "" || payload.Aud == "" {
		return nil, fmt.Errorf("incomplete stub token")
	}
	return &payload, nil
}

func writeError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(tokenInfoResponse{ErrorDesc: code})
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return fallback
}
