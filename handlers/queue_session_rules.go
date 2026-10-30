package handlers

import (
	"strings"
	"unicode/utf8"

	"itii-assist/config"
	"itii-assist/models"
)

// Limits for a queue session. The create modal enforces the same rules before
// it calls the API (lib/queue/session-rules.ts); the server repeats them so an
// old client or a script cannot save a queue nobody could use.
const (
	queueTitleMaxLength       = 255
	queueDescriptionMaxLength = 2000
	queueMaxAttendanceLinks   = 30
)

// queueSettingsProblem is one reason a queue cannot be saved.
type queueSettingsProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// queueSessionSettings is what the rules check. A nil link means "not linked";
// Check* flags say which fields this request set, so an update only re-checks
// what it changes.
type queueSessionSettings struct {
	CourseID             string
	Title                string
	Description          string
	ClassroomID          string
	AssignmentID         *uint
	AttendanceSessionIDs []uint
	CheckTitle           bool
	CheckDescription     bool
	CheckClassroom       bool
	CheckAssignment      bool
	CheckAttendance      bool
}

// queueLinkLookup answers the database questions, so the rules stay testable.
type queueLinkLookup struct {
	ClassroomExists  func(id string) bool
	AssignmentCourse func(id uint) (string, bool)
	AttendanceCourse func(id uint) (string, bool)
}

var dbQueueLinkLookup = queueLinkLookup{
	ClassroomExists: func(id string) bool {
		var n int64
		config.DB.Model(&models.Classroom{}).Where("id = ? AND is_deleted = false", id).Count(&n)
		return n > 0
	},
	AssignmentCourse: func(id uint) (string, bool) {
		var a models.Assignment
		if err := config.DB.Select("id", "course_id").First(&a, id).Error; err != nil {
			return "", false
		}
		return a.CourseID, true
	},
	AttendanceCourse: func(id uint) (string, bool) {
		var s models.AttendanceSession
		if err := config.DB.Select("id", "course_id").First(&s, id).Error; err != nil {
			return "", false
		}
		return s.CourseID, true
	},
}

func validateQueueSession(s queueSessionSettings, lookup queueLinkLookup) []queueSettingsProblem {
	var problems []queueSettingsProblem
	add := func(code, message string) {
		problems = append(problems, queueSettingsProblem{Code: code, Message: message})
	}
	if s.CheckTitle {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			add("title_required", "กรอกชื่อคิว")
		} else if utf8.RuneCountInString(title) > queueTitleMaxLength {
			add("title_too_long", "ชื่อคิวยาวเกินกำหนด")
		}
	}
	if s.CheckDescription && utf8.RuneCountInString(s.Description) > queueDescriptionMaxLength {
		add("description_too_long", "คำอธิบายยาวเกินกำหนด")
	}
	if s.CheckClassroom && (strings.TrimSpace(s.ClassroomID) == "" || !lookup.ClassroomExists(strings.TrimSpace(s.ClassroomID))) {
		add("classroom_not_found", "ไม่พบห้องที่เลือก")
	}
	if s.CheckAssignment && s.AssignmentID != nil {
		if course, ok := lookup.AssignmentCourse(*s.AssignmentID); !ok || course != s.CourseID {
			add("assignment_not_in_course", "หัวข้องานที่เลือกไม่ได้อยู่ในรายวิชานี้")
		}
	}
	if s.CheckAttendance {
		if len(s.AttendanceSessionIDs) > queueMaxAttendanceLinks {
			add("too_many_attendance", "เลือกรอบเช็กชื่อได้ไม่เกิน 30 รอบ")
		}
		for _, id := range s.AttendanceSessionIDs {
			if course, ok := lookup.AttendanceCourse(id); !ok || course != s.CourseID {
				add("attendance_not_in_course", "รอบเช็กชื่อที่เลือกไม่ได้อยู่ในรายวิชานี้")
				break
			}
		}
	}
	return problems
}

func queueSettingsResponse(problems []queueSettingsProblem) map[string]interface{} {
	messages := make([]string, 0, len(problems))
	for _, p := range problems {
		messages = append(messages, p.Message)
	}
	return map[string]interface{}{
		"success":  false,
		"code":     "QUEUE_SETTINGS_INVALID",
		"message":  strings.Join(messages, " / "),
		"problems": problems,
	}
}

// normalizeQueueAttendanceIDs drops zeros and repeats, keeping the order chosen.
func normalizeQueueAttendanceIDs(ids []uint) []uint {
	out := make([]uint, 0, len(ids))
	seen := map[uint]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func sameUintSet(a, b []uint) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[uint]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}

func firstUint(ids []uint) *uint {
	if len(ids) == 0 {
		return nil
	}
	v := ids[0]
	return &v
}

func sameUintPtr(a, b *uint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
