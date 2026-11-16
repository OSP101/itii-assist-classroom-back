package handlers

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"testing"
	"time"
)

func TestLateEnrollmentQueueAttendanceEligibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&models.CourseSection{}, &models.CourseSectionStudent{}, &models.CourseSectionStudentRemoval{}, &models.AttendanceSession{}, &models.AttendanceSessionSection{}, &models.AttendanceRecord{}, &models.QueueSessionAttendanceLink{}); err != nil {
		t.Fatal(err)
	}
	previous := config.DB
	config.DB = db
	defer func() { config.DB = previous }()
	section := models.CourseSection{CourseID: "course", SectionNo: "1"}
	if err := db.Create(&section).Error; err != nil {
		t.Fatal(err)
	}
	attendance := models.AttendanceSession{CourseID: "course", CourseSectionID: &section.ID, StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)}
	if err := db.Create(&attendance).Error; err != nil {
		t.Fatal(err)
	}
	queue := models.QueueSession{ID: "queue", CourseID: "course", RequireAttendance: true, LinkedAttendanceSessionID: &attendance.ID}
	if err := repositories.AddStudentToSection(section.ID, 42); err != nil {
		t.Fatal(err)
	}
	enrolled, err := isStudentEnrolledInCourse(queue.CourseID, 42)
	if err != nil || !enrolled {
		t.Fatalf("new student not enrolled: %v %v", enrolled, err)
	}
	allowed, err := hasQueueAttendanceEligibility(&queue, 42)
	if err != nil || allowed {
		t.Fatalf("must check in before booking: %v %v", allowed, err)
	}
	for _, status := range []string{"present", "late"} {
		if err := db.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", attendance.ID, 42).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		allowed, err = hasQueueAttendanceEligibility(&queue, 42)
		if err != nil || !allowed {
			t.Fatalf("new student with %s cannot book: %v %v", status, allowed, err)
		}
	}
}
