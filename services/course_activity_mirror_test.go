package services

import "testing"

func TestOnlyRefusedCheckInsAreMirrored(t *testing.T) {
	// Routine outcomes must stay out of the course log: a row per student per
	// session would bury everything else that happened in the course.
	for _, result := range []string{
		AttendanceResultSuccess,
		AttendanceResultDuplicate,
		AttendanceResultGuardUnavailable,
		"",
		"something_new",
	} {
		if action, mirrored := mirroredCheckInActions[result]; mirrored {
			t.Fatalf("result %q must not be mirrored, got action %q", result, action)
		}
	}

	expected := map[string]string{
		AttendanceResultFailed:         ActionCheckInFailed,
		AttendanceResultNetworkBlocked: ActionCheckInBlocked,
		AttendanceResultRateLimited:    ActionCheckInRateLimited,
	}
	for result, wantAction := range expected {
		action, mirrored := mirroredCheckInActions[result]
		if !mirrored {
			t.Fatalf("result %q must be mirrored", result)
		}
		if action != wantAction {
			t.Fatalf("result %q: expected action %q, got %q", result, wantAction, action)
		}
	}
	if len(mirroredCheckInActions) != len(expected) {
		t.Fatalf("unexpected mirrored actions: %#v", mirroredCheckInActions)
	}
}

func TestStudentTargetIDLeavesUnknownIdentityEmpty(t *testing.T) {
	// StudentID is 0 when the check-in could not be tied to a student. Writing
	// "0" would resolve to nobody and read as a real reference.
	if got := studentTargetID(0); got != "" {
		t.Fatalf("expected an empty target for an unresolved student, got %q", got)
	}
	if got := studentTargetID(42); got != "42" {
		t.Fatalf("expected \"42\", got %q", got)
	}
}

func TestMirrorIsSafeWithoutADatabase(t *testing.T) {
	// The check-in path calls this unconditionally; a nil handle must not panic.
	MirrorAttendanceCheckIn(nil, AttendanceCheckInEvent{SessionID: 1, Result: AttendanceResultFailed})
	MirrorAttendanceCheckIn(nil, AttendanceCheckInEvent{SessionID: 0, Result: AttendanceResultFailed})
}
