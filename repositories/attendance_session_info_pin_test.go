package repositories

import (
	"encoding/json"
	"strings"
	"testing"
)

// GET /api/attendance/check-in/:sessionId/info is public — no auth middleware
// (routes/attendance_route.go). The rotating PIN is the one control that forces
// a student to physically be in the room reading the projector, so if it ever
// travels on that response again, every other check-in guard (mobile device,
// campus Wi-Fi, canonical domain) can be walked around by anyone who knows a
// session id. GetSessionInfoHandler blanks PinCode before replying; these tests
// pin down that a blank code actually disappears from the JSON, and that the
// student UI can still tell "no PIN yet" from "PIN withheld".
func TestAttendanceSessionInfo_BlankPinCodeIsNotSerialized(t *testing.T) {
	info := AttendanceSessionInfo{
		ID:        7,
		Status:    "active",
		PinCode:   "", // what GetSessionInfoHandler leaves behind
		PinIssued: true,
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if strings.Contains(string(encoded), "pin_code") {
		t.Errorf("public session info still carries a pin_code key: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"pin_issued":true`) {
		t.Errorf("pin_issued must survive so the check-in screen can tell an issued PIN from a pending one: %s", encoded)
	}
}

// The instructor live view and the paired classroom display read the code off
// this same struct, so omitempty must not hide a PIN that was deliberately set.
func TestAttendanceSessionInfo_PrivilegedPinCodeIsSerialized(t *testing.T) {
	info := AttendanceSessionInfo{
		ID:        7,
		Status:    "active",
		PinCode:   "482913",
		PinIssued: true,
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if !strings.Contains(string(encoded), `"pin_code":"482913"`) {
		t.Errorf("privileged consumers lost the PIN they render on the projector: %s", encoded)
	}
}

// A session whose PIN has not been generated yet must report pin_issued=false,
// otherwise the check-in screen shows a countdown for a code nobody can read.
func TestAttendanceSessionInfo_NoPinIssuedYet(t *testing.T) {
	info := AttendanceSessionInfo{ID: 7, Status: "active"}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if !strings.Contains(string(encoded), `"pin_issued":false`) {
		t.Errorf("expected pin_issued=false for a session with no PIN: %s", encoded)
	}
}
