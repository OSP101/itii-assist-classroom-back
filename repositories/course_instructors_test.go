package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateCourseInstructorSelection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	oldDB := config.DB
	config.DB = db
	t.Cleanup(func() { config.DB = oldDB })
	if err := db.AutoMigrate(&models.Course{}, &models.CourseInstructor{}); err != nil {
		t.Fatal(err)
	}

	primaryID := uint(12)
	course := models.Course{ID: "course", Code: "TEST101", Name: "Test", InstructorID: &primaryID}
	if err := db.Create(&course).Error; err != nil {
		t.Fatal(err)
	}
	member := models.CourseInstructor{CourseID: course.ID, UserID: primaryID, IsPrimary: true, Permissions: "custom"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	course.Name = "Updated test"
	if err := UpdateCourse(&course, nil); err != nil {
		t.Fatal(err)
	}

	newPrimaryID := uint(34)
	course.InstructorID = &newPrimaryID
	if err := UpdateCourse(&course, []uint{newPrimaryID, primaryID}); err != nil {
		t.Fatal(err)
	}
	var retained models.CourseInstructor
	if err := db.Select("id", "is_primary", "permissions").First(&retained, member.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.IsPrimary || retained.Permissions != member.Permissions {
		t.Fatalf("retained instructor has unexpected membership: %+v", retained)
	}
	var selectedPrimary uint
	if err := db.Model(&models.CourseInstructor{}).Select("user_id").
		Where("course_id = ? AND is_primary = ?", course.ID, true).Take(&selectedPrimary).Error; err != nil {
		t.Fatal(err)
	}
	if selectedPrimary != newPrimaryID {
		t.Fatalf("primary instructor = %d, want %d", selectedPrimary, newPrimaryID)
	}

	course.InstructorID = nil
	if err := UpdateCourse(&course, []uint{}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.CourseInstructor{}).Where("course_id = ?", course.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	var saved models.Course
	if err := db.Select("instructor_id").First(&saved, "id = ?", course.ID).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || saved.InstructorID != nil {
		t.Fatalf("empty selection left %d memberships and instructor_id %v", count, saved.InstructorID)
	}
}
