package config

import (
	"testing"
	"time"
)

func TestSlowQueryThresholdDefault(t *testing.T) {
	t.Setenv("DB_SLOW_QUERY_THRESHOLD_MS", "")
	if got := slowQueryThreshold(); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms default, got %s", got)
	}
}

func TestSlowQueryThresholdOverride(t *testing.T) {
	t.Setenv("DB_SLOW_QUERY_THRESHOLD_MS", "500")
	if got := slowQueryThreshold(); got != 500*time.Millisecond {
		t.Fatalf("expected 500ms, got %s", got)
	}
}

// getEnvInt backs every tuning knob added here, including the prepared
// statement cache bound. A value it accepts wrongly would silently un-bound
// that cache, so the rejection path matters.
func TestGetEnvIntRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc", "3.5", " "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_TEST_KNOB", value)
			if got := getEnvInt("DB_TEST_KNOB", 512); got != 512 {
				t.Fatalf("value %q should fall back to 512, got %d", value, got)
			}
		})
	}
}

func TestGetEnvIntAcceptsPositive(t *testing.T) {
	t.Setenv("DB_TEST_KNOB", "1024")
	if got := getEnvInt("DB_TEST_KNOB", 512); got != 1024 {
		t.Fatalf("expected 1024, got %d", got)
	}
}

// The pool gauges are registered at startup and scraped on a schedule that does
// not care whether the database is up, so this must not panic on a nil DB.
func TestDBPoolStatsIsSafeWithoutDatabase(t *testing.T) {
	original := DB
	DB = nil
	defer func() { DB = original }()

	inUse, idle, open, waitCount := DBPoolStats()
	if inUse != 0 || idle != 0 || open != 0 || waitCount != 0 {
		t.Fatalf("expected all zeroes with no DB, got %d %d %d %d", inUse, idle, open, waitCount)
	}
}

// EnablePreparedStatements runs unconditionally at startup; a nil DB (failed
// connect path) must be a no-op rather than a panic.
func TestEnablePreparedStatementsIsSafeWithoutDatabase(t *testing.T) {
	original := DB
	DB = nil
	defer func() { DB = original }()

	EnablePreparedStatements()

	if DB != nil {
		t.Fatal("EnablePreparedStatements must leave a nil DB alone")
	}
}
