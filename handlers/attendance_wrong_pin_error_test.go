package handlers

import (
	"testing"

	"itii-assist/repositories"
)

// On 2026-08-31 a rotating-PIN session logged 42 failed check-ins across 19
// students under the catch-all code ATTENDANCE_ERROR. They were all wrong
// PINs: LookupAttendanceSessionIDByPIN returns the plain ErrAttendanceInvalidPIN
// sentinel when a PIN matches no open session, and StudentCheckIn passed it
// straight up, so it matched neither the *AttendancePublicError branch nor any
// of the string checks below it. The student was shown the raw internal string
// "attendance invalid pin" under the title "เช็กชื่อไม่สำเร็จ" — nothing telling
// them to re-read the projector — so they kept resubmitting the same dead PIN.
func TestWrongPinSentinelBecomesTheThaiPublicError(t *testing.T) {
	status, payload := attendancePublicErrorResponse(
		repositories.ErrAttendanceInvalidPIN,
		400,
		"เช็กชื่อไม่สำเร็จ",
		"ไม่สามารถเช็กชื่อได้ในขณะนี้",
	)

	if status != repositories.ErrAttendanceInvalidPINPublic.HTTPStatus {
		t.Errorf("status = %d, want %d", status, repositories.ErrAttendanceInvalidPINPublic.HTTPStatus)
	}
	if got := payload["code"]; got != repositories.ErrAttendanceInvalidPINPublic.Code {
		t.Errorf("code = %v, want %s", got, repositories.ErrAttendanceInvalidPINPublic.Code)
	}
	if got := payload["message"]; got != repositories.ErrAttendanceInvalidPINPublic.Message {
		t.Errorf("message = %v, want the Thai wrong-PIN message", got)
	}
	if got, ok := payload["message"].(string); ok && got == "attendance invalid pin" {
		t.Error("the internal English sentinel text is being shown to the student again")
	}
}

// The audit log has to name the failure too, or an incident like this reads as
// an unexplained server fault instead of "everyone is typing a stale PIN".
func TestWrongPinSentinelIsNotFiledAsGenericError(t *testing.T) {
	if got := attendanceErrCode(repositories.ErrAttendanceInvalidPIN); got != repositories.ErrAttendanceInvalidPINPublic.Code {
		t.Errorf("attendanceErrCode = %q, want %q", got, repositories.ErrAttendanceInvalidPINPublic.Code)
	}
}

// The public error itself must keep working unchanged — it is what the in-session
// PIN comparison already returns.
func TestPublicWrongPinErrorUnchanged(t *testing.T) {
	status, payload := attendancePublicErrorResponse(
		repositories.ErrAttendanceInvalidPINPublic, 400, "x", "y",
	)
	if status != 400 || payload["code"] != "ATTENDANCE_INVALID_PIN" {
		t.Errorf("unexpected mapping: status=%d payload=%v", status, payload)
	}
	if got := attendanceErrCode(repositories.ErrAttendanceInvalidPINPublic); got != "ATTENDANCE_INVALID_PIN" {
		t.Errorf("attendanceErrCode = %q", got)
	}
}
