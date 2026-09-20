package repositories

import (
	"testing"
	"time"
)

// Deterministic, DB-free counterpart to
// TestGetSessionInfo_ConcurrentCallersDoNotShareTheReturnedStruct (which
// depends on goroutine timing, and is skipped in this dev environment by a
// pre-existing sqlite driver limitation — see setupAttendanceRuntimeTest).
// This test exercises (*AttendanceSessionInfo).clone() directly: every
// pointer field must point at a NEW object, not the original's, so mutating
// the clone can never be observed through the original.
func TestAttendanceSessionInfo_CloneIsADeepCopy(t *testing.T) {
	pinIssuedAt := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	pinRotatesAt := time.Date(2026, 1, 1, 8, 1, 0, 0, time.UTC)

	original := &AttendanceSessionInfo{
		ID:           7,
		Title:        "original title",
		PinIssuedAt:  &pinIssuedAt,
		PinRotatesAt: &pinRotatesAt,
		Course:       &AttendanceCourseBasic{ID: "CP101", Name: "original course"},
		Section:      &AttendanceSectionBasic{ID: 1, SectionNo: "1"},
	}

	cloned := original.clone()

	// Every pointer field must be a distinct object...
	if cloned.PinIssuedAt == original.PinIssuedAt {
		t.Error("PinIssuedAt was not deep-copied — clone shares the original's pointer")
	}
	if cloned.PinRotatesAt == original.PinRotatesAt {
		t.Error("PinRotatesAt was not deep-copied — clone shares the original's pointer")
	}
	if cloned.Course == original.Course {
		t.Error("Course was not deep-copied — clone shares the original's pointer")
	}
	if cloned.Section == original.Section {
		t.Error("Section was not deep-copied — clone shares the original's pointer")
	}

	// ...and mutating the clone must never be observable through original.
	cloned.Title = "mutated"
	*cloned.PinIssuedAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	cloned.Course.Name = "mutated course"
	cloned.Section.SectionNo = "mutated"

	if original.Title != "original title" {
		t.Errorf("mutating clone.Title changed original.Title: %q", original.Title)
	}
	if !original.PinIssuedAt.Equal(pinIssuedAt) {
		t.Errorf("mutating *clone.PinIssuedAt changed *original.PinIssuedAt: %v", *original.PinIssuedAt)
	}
	if original.Course.Name != "original course" {
		t.Errorf("mutating clone.Course.Name changed original.Course.Name: %q", original.Course.Name)
	}
	if original.Section.SectionNo != "1" {
		t.Errorf("mutating clone.Section.SectionNo changed original.Section.SectionNo: %q", original.Section.SectionNo)
	}
}

func TestAttendanceSessionInfo_CloneHandlesNilFields(t *testing.T) {
	original := &AttendanceSessionInfo{ID: 7, Title: "no pin, no course/section yet"}

	cloned := original.clone()

	if cloned.PinIssuedAt != nil || cloned.PinRotatesAt != nil || cloned.Course != nil || cloned.Section != nil {
		t.Errorf("clone() invented non-nil pointer fields from nil inputs: %+v", cloned)
	}
	if cloned.Title != original.Title {
		t.Errorf("clone() lost a plain scalar field: got %q, want %q", cloned.Title, original.Title)
	}
}

func TestAttendanceSessionInfo_CloneOfNilIsNil(t *testing.T) {
	var original *AttendanceSessionInfo
	if cloned := original.clone(); cloned != nil {
		t.Errorf("clone() of a nil *AttendanceSessionInfo should return nil, got %+v", cloned)
	}
}
