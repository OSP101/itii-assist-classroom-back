package middlewares

import (
	"log/slog"
	"testing"
)

func TestAccessLogEnabledDefaultsOn(t *testing.T) {
	t.Setenv("APP_ENABLE_REQUEST_LOGGER", "")
	if !accessLogEnabled() {
		t.Fatal("access log must default to enabled when the env var is unset")
	}
}

func TestAccessLogEnabledParsing(t *testing.T) {
	cases := map[string]bool{
		"false":   false,
		"FALSE":   false,
		"0":       false,
		"no":      false,
		"off":     false,
		" Off ":   false,
		"true":    true,
		"1":       true,
		"yes":     true,
		"garbage": true, // unrecognised values must not silently disable logging
	}

	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("APP_ENABLE_REQUEST_LOGGER", value)
			if got := accessLogEnabled(); got != want {
				t.Fatalf("APP_ENABLE_REQUEST_LOGGER=%q: expected %v, got %v", value, want, got)
			}
		})
	}
}

func TestAccessLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":         slog.LevelInfo,
		"info":     slog.LevelInfo,
		"debug":    slog.LevelDebug,
		"DEBUG":    slog.LevelDebug,
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"nonsense": slog.LevelInfo,
	}

	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", value)
			if got := accessLogLevel(); got != want {
				t.Fatalf("LOG_LEVEL=%q: expected %v, got %v", value, want, got)
			}
		})
	}
}
