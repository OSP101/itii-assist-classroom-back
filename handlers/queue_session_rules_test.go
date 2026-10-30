package handlers

import "testing"

func queueCodes(ps []queueSettingsProblem) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.Code] = true
	}
	return m
}

func TestValidateQueueSession(t *testing.T) {
	lookup := queueLinkLookup{
		ClassroomExists: func(id string) bool { return id == "room-1" },
		AssignmentCourse: func(id uint) (string, bool) {
			return map[uint]string{1: "course-a", 2: "course-b"}[id], id == 1 || id == 2
		},
		AttendanceCourse: func(id uint) (string, bool) { return map[uint]string{7: "course-a"}[id], id == 7 },
	}
	one, two, seven, nine := uint(1), uint(2), uint(7), uint(9)
	all := func(s queueSessionSettings) queueSessionSettings {
		s.CourseID = "course-a"
		s.CheckTitle, s.CheckDescription, s.CheckClassroom, s.CheckAssignment, s.CheckAttendance = true, true, true, true, true
		return s
	}

	if p := validateQueueSession(all(queueSessionSettings{Title: "Lab 1", ClassroomID: "room-1", AssignmentID: &one, AttendanceSessionIDs: []uint{seven}}), lookup); len(p) != 0 {
		t.Fatalf("valid queue refused: %v", p)
	}
	got := queueCodes(validateQueueSession(all(queueSessionSettings{Title: "   ", ClassroomID: "gone", AssignmentID: &two, AttendanceSessionIDs: []uint{seven, nine}}), lookup))
	for _, code := range []string{"title_required", "classroom_not_found", "assignment_not_in_course", "attendance_not_in_course"} {
		if !got[code] {
			t.Errorf("expected %s, got %v", code, got)
		}
	}
	long := make([]rune, queueTitleMaxLength+1)
	for i := range long {
		long[i] = 'ก'
	}
	if !queueCodes(validateQueueSession(all(queueSessionSettings{Title: string(long), ClassroomID: "room-1"}), lookup))["title_too_long"] {
		t.Error("expected title_too_long for 256 Thai characters")
	}
	// An update that leaves an old cross-course link alone must not be refused for it.
	if p := validateQueueSession(queueSessionSettings{CourseID: "course-a", AssignmentID: &two}, lookup); len(p) != 0 {
		t.Fatalf("unchanged link re-checked: %v", p)
	}
}

func TestQueueAttendanceIDHelpers(t *testing.T) {
	if got := normalizeQueueAttendanceIDs([]uint{3, 0, 1, 3, 2, 1}); len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("normalize kept order and dropped repeats wrongly: %v", got)
	}
	if !sameUintSet([]uint{1, 2}, []uint{2, 1}) || sameUintSet([]uint{1, 2}, []uint{1}) || !sameUintSet(nil, []uint{}) {
		t.Fatal("sameUintSet compares sets")
	}
	if firstUint(nil) != nil || *firstUint([]uint{5, 6}) != 5 {
		t.Fatal("firstUint")
	}
	many := make([]uint, queueMaxAttendanceLinks+1)
	for i := range many {
		many[i] = 7
	}
	lookup := queueLinkLookup{AttendanceCourse: func(uint) (string, bool) { return "course-a", true }}
	if !queueCodes(validateQueueSession(queueSessionSettings{CourseID: "course-a", AttendanceSessionIDs: many, CheckAttendance: true}, lookup))["too_many_attendance"] {
		t.Fatal("expected too_many_attendance")
	}
}
