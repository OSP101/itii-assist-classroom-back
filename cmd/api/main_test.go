package main

import (
	"testing"
	"time"
)

func TestAttendancePinTickIntervalDefault(t *testing.T) {
	t.Setenv("ATTENDANCE_PIN_TICK_SECONDS", "")
	if got := attendancePinTickInterval(); got != 5*time.Second {
		t.Fatalf("expected 5s default, got %s", got)
	}
}

func TestAttendancePinTickIntervalOverride(t *testing.T) {
	t.Setenv("ATTENDANCE_PIN_TICK_SECONDS", "2")
	if got := attendancePinTickInterval(); got != 2*time.Second {
		t.Fatalf("expected 2s, got %s", got)
	}
}

// A bad value must fall back rather than produce a pathological ticker: 0 would
// panic time.NewTicker outright, and a huge value would stall PIN rotation.
func TestAttendancePinTickIntervalRejectsOutOfRange(t *testing.T) {
	for _, value := range []string{"0", "-1", "31", "3600", "abc", "  "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ATTENDANCE_PIN_TICK_SECONDS", value)
			got := attendancePinTickInterval()
			if got != 5*time.Second {
				t.Fatalf("value %q should fall back to 5s, got %s", value, got)
			}
		})
	}
}

// The ticker interval has to stay well under the one-minute PIN rotation
// cadence or rotations would be served late.
func TestAttendancePinTickIntervalStaysBelowRotationCadence(t *testing.T) {
	t.Setenv("ATTENDANCE_PIN_TICK_SECONDS", "30")
	if got := attendancePinTickInterval(); got >= time.Minute {
		t.Fatalf("even the maximum interval must stay below 1m, got %s", got)
	}
}
