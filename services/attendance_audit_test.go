package services

import (
	"os"
	"strings"
	"testing"
)

// The device-flip probe only performs acceptably while
// idx_system_logs_device_guard_flip can actually be used, and Postgres will
// only use that partial index if the query's WHERE clause repeats its
// predicate as literals. The predicate therefore lives in two files that have
// no compile-time link to each other. This test is that link: rename an action
// constant or edit the migration alone and it fails here instead of silently
// turning a hot-path lookup into a full index scan in production.
func TestAttendanceDeviceFlipIndexPredicateMatchesMigration(t *testing.T) {
	migration, err := os.ReadFile("../config/database.go")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if !strings.Contains(string(migration), attendanceDeviceFlipIndexPredicate) {
		t.Fatalf("idx_system_logs_device_guard_flip no longer matches the query predicate %q;\n"+
			"update the CREATE INDEX in config/database.go and the constants in attendance_audit.go together",
			attendanceDeviceFlipIndexPredicate)
	}
}

// Same contract for the index behind the instructor-facing review list.
func TestAttendanceSessionLogsPredicateMatchesMigration(t *testing.T) {
	migration, err := os.ReadFile("../config/database.go")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	if !strings.Contains(string(migration), AttendanceSessionLogsPredicate) {
		t.Fatalf("idx_system_logs_attendance_session no longer matches %q", AttendanceSessionLogsPredicate)
	}
}

// The device-flip probe reads its own previous flag out of the same query it
// uses to find a prior block, so that action must be inside the index
// predicate. Miss this and the dedup lookup silently stops using the index.
func TestDeviceFlipPredicateCoversBothActions(t *testing.T) {
	for _, action := range []string{attendanceNetworkBlockedAction, attendanceDeviceFlipAction} {
		if !strings.Contains(attendanceDeviceFlipIndexPredicate, "'"+action+"'") {
			t.Fatalf("predicate %q does not cover action %q", attendanceDeviceFlipIndexPredicate, action)
		}
	}
}

func TestDecodeAttendanceLogDetail(t *testing.T) {
	decoded := decodeAttendanceLogDetail([]byte(`{"student_id":42,"failed_checks":["device","network"]}`))
	if decoded.StudentID != 42 {
		t.Fatalf("student id = %d, want 42", decoded.StudentID)
	}
	if !containsString(decoded.FailedChecks, "device") {
		t.Fatal("expected the device check to be recognised")
	}
	if containsString(decodeAttendanceLogDetail(nil).FailedChecks, "device") {
		t.Fatal("an empty detail payload must not look like a device block")
	}
	if containsString(decodeAttendanceLogDetail([]byte(`not json`)).FailedChecks, "device") {
		t.Fatal("an unreadable detail payload must not look like a device block")
	}
}

// The predicate is interpolated into SQL rather than bound as a parameter, so
// it must never be able to carry a quote. Both inputs are constants, and this
// pins that fact.
func TestAttendanceDeviceFlipIndexPredicateHasNoInjectableChars(t *testing.T) {
	for _, value := range []string{AttendanceCheckInLogType, attendanceNetworkBlockedAction} {
		if strings.ContainsAny(value, "'\"\\;") {
			t.Fatalf("constant %q contains a character that must not be inlined into SQL", value)
		}
	}
}

func boolPtr(v bool) *bool { return &v }

func TestClientSignalMismatchReasons(t *testing.T) {
	tests := []struct {
		name        string
		deviceType  string
		signals     *ClientDeviceSignals
		wantReasons []string
		wantStrong  bool
	}{
		{
			name:       "desktop UA is not this check's business",
			deviceType: "desktop",
			signals:    &ClientDeviceSignals{Platform: "Windows"},
		},
		{
			name:       "missing signals are handled by the caller, not here",
			deviceType: "mobile",
			signals:    nil,
		},
		{
			// The case the old all-false rule was written for, and could never
			// catch: DevTools emulates touch, coarse pointer and screen, and
			// for a preset device it rewrites UA client hints too.
			name:       "devtools preset emulation stays invisible",
			deviceType: "mobile",
			signals:    &ClientDeviceSignals{Touch: true, CoarsePointer: true, Platform: "Android", Mobile: boolPtr(true)},
		},
		{
			// A UA-switcher extension, or DevTools with a hand-typed UA: the UA
			// string says phone, the client hints still say desktop.
			name:        "ua string says phone, client hints say windows",
			deviceType:  "mobile",
			signals:     &ClientDeviceSignals{Touch: true, CoarsePointer: true, Platform: "Windows", Mobile: boolPtr(false)},
			wantReasons: []string{"ua_platform_conflict", "ua_ch_not_mobile"},
			wantStrong:  true,
		},
		{
			name:        "no device traits at all is a weak hint only",
			deviceType:  "mobile",
			signals:     &ClientDeviceSignals{},
			wantReasons: []string{"no_device_traits"},
			wantStrong:  false,
		},
		{
			// Safari and Firefox do not implement UA client hints, so an empty
			// platform must never be read as a contradiction.
			name:       "absent client hints are not a conflict",
			deviceType: "mobile",
			signals:    &ClientDeviceSignals{Touch: true, CoarsePointer: true, Motion: true},
		},
		{
			// A real tablet with a mouse and motion permission denied. It gets
			// the weak reason, and must not be raised to warn.
			name:        "honest mouse only tablet is never strong",
			deviceType:  "tablet",
			signals:     &ClientDeviceSignals{Platform: "Android", Mobile: boolPtr(false)},
			wantReasons: []string{"no_device_traits"},
			wantStrong:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reasons, strong := clientSignalMismatchReasons(tc.deviceType, tc.signals)
			if strings.Join(reasons, ",") != strings.Join(tc.wantReasons, ",") {
				t.Fatalf("reasons = %v, want %v", reasons, tc.wantReasons)
			}
			if strong != tc.wantStrong {
				t.Fatalf("strong = %v, want %v", strong, tc.wantStrong)
			}
		})
	}
}

func TestSanitizeClientSignalsBoundsPlatform(t *testing.T) {
	long := strings.Repeat("x", 200)
	clean := sanitizeClientSignals(&ClientDeviceSignals{Platform: "  " + long + "  "})
	if len(clean.Platform) != 64 {
		t.Fatalf("platform length = %d, want 64", len(clean.Platform))
	}
	if sanitizeClientSignals(nil) != nil {
		t.Fatal("nil signals must stay nil")
	}
}
