package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"testing"
	"time"
)

func TestEnrollmentSyncsExistingAttendanceRosters(t *testing.T) {
	for _, mode := range []string{"single", "bulk", "move", "restore"} {
		t.Run(mode, func(t *testing.T) {
			cleanup := setupLeaveRepoTestDB(t)
			defer cleanup()
			f := seedLeaveFixture(t)
			target := models.CourseSection{CourseID: f.course.ID, SectionNo: "2"}
			if err := config.DB.Create(&target).Error; err != nil {
				t.Fatal(err)
			}
			createSession := func(course string, legacy *uint, links []uint) models.AttendanceSession {
				s := models.AttendanceSession{CourseID: course, CourseSectionID: legacy, StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)}
				if err := config.DB.Create(&s).Error; err != nil {
					t.Fatal(err)
				}
				for _, id := range links {
					if err := config.DB.Create(&models.AttendanceSessionSection{AttendanceSessionID: s.ID, CourseSectionID: id}).Error; err != nil {
						t.Fatal(err)
					}
				}
				return s
			}
			all := createSession(f.course.ID, nil, nil)
			linked := createSession(f.course.ID, nil, []uint{target.ID, f.section.ID})
			legacy := createSession(f.course.ID, &target.ID, nil)
			excluded := createSession(f.course.ID, &f.section.ID, nil)
			// Explicit links take priority over the legacy column.
			explicit := createSession(f.course.ID, &target.ID, []uint{f.section.ID})
			foreign := createSession("another-course", nil, nil)
			now := time.Now()
			preserved := models.AttendanceRecord{AttendanceSessionID: all.ID, StudentID: f.other.ID, Status: "present", CheckInTime: &now, Note: "keep"}
			if err := config.DB.Create(&preserved).Error; err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "single":
				if err := AddStudentToSection(target.ID, f.other.ID); err != nil {
					t.Fatal(err)
				}
			case "bulk":
				result, err := BulkAddStudentsToSection(f.course.ID, target.ID, []uint{f.other.ID, f.other.ID}, "skip")
				if err != nil || result.Added != 1 {
					t.Fatalf("bulk: %+v %v", result, err)
				}
			case "move":
				if err := config.DB.Create(&models.CourseSectionStudent{CourseSectionID: f.section.ID, StudentID: f.other.ID, EnrolledAt: now}).Error; err != nil {
					t.Fatal(err)
				}
				moved, err := MoveStudentBetweenSections(f.course.ID, f.section.ID, target.ID, f.other.ID)
				if err != nil || !moved {
					t.Fatalf("move: %v %v", moved, err)
				}
			case "restore":
				if err := config.DB.Create(&models.CourseSectionStudentRemoval{CourseID: f.course.ID, CourseSectionID: target.ID, StudentID: f.other.ID, RemovedAt: now, RestoreUntil: now.Add(time.Hour)}).Error; err != nil {
					t.Fatal(err)
				}
				restored, err := RestoreStudentToSection(target.ID, f.other.ID)
				if err != nil || !restored {
					t.Fatalf("restore: %v %v", restored, err)
				}
			}
			for _, s := range []models.AttendanceSession{all, linked, legacy} {
				r := recordFor(t, s.ID, f.other.ID)
				if s.ID == all.ID {
					if r.ID != preserved.ID || r.Status != "present" || r.Note != "keep" || r.CheckInTime == nil {
						t.Fatalf("overwrote check-in: %+v", r)
					}
				} else if r.Status != "absent" {
					t.Fatalf("unexpected default: %+v", r)
				}
				if _, err := GetAttendanceStudentSessionStatus(s.ID, f.other.ID); err != nil {
					t.Fatalf("verification rejected: %v", err)
				}
			}
			for _, s := range []models.AttendanceSession{excluded, explicit, foreign} {
				var count int64
				if err := config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", s.ID, f.other.ID).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("student added to unrelated session %d", s.ID)
				}
			}
		})
	}
}

func TestEnrollmentRollsBackWhenAttendanceSyncFails(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	seedLeaveSession(t, f, time.Now())
	if err := config.DB.Migrator().DropTable(&models.AttendanceRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := AddStudentToSection(f.section.ID, f.other.ID); err == nil {
		t.Fatal("expected sync error")
	}
	var count int64
	if err := config.DB.Model(&models.CourseSectionStudent{}).Where("course_section_id = ? AND student_id = ?", f.section.ID, f.other.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("enrollment committed without attendance roster")
	}
}
