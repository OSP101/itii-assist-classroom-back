package repositories

import (
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupScheduledPublishTestDB(t *testing.T) func() {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:assignment_scheduled_publish_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	migrateWithSQLiteTimestamps(t, db, &models.Course{}, &models.Assignment{})

	prevDB := config.DB
	config.DB = db

	courses := []map[string]interface{}{
		{"id": "course_open", "code": "CP101", "name": "Open", "year": 2569, "semester": 1, "is_active": true},
		{"id": "course_closed", "code": "CP102", "name": "Closed", "year": 2569, "semester": 1, "is_active": false},
	}
	for _, c := range courses {
		if err := db.Model(&models.Course{}).Create(c).Error; err != nil {
			t.Fatalf("create course: %v", err)
		}
	}

	return func() {
		db.Exec("DELETE FROM assignments")
		db.Exec("DELETE FROM courses")
		config.DB = prevDB
	}
}

// createScheduledAssignment goes through a map so is_draft and is_active land
// exactly as given (a struct create would let a default override a false).
func createScheduledAssignment(t *testing.T, courseID string, isDraft, isActive bool, publishAt *time.Time) uint {
	t.Helper()
	row := map[string]interface{}{
		"course_id":       courseID,
		"name":            "Lab",
		"assignment_type": "individual",
		"max_score":       10,
		"is_draft":        isDraft,
		"is_active":       isActive,
		"publish_at":      publishAt,
		"created_by":      1,
		"created_at":      time.Now().UTC(),
	}
	if err := config.DB.Model(&models.Assignment{}).Create(row).Error; err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	var a models.Assignment
	if err := config.DB.Order("id DESC").First(&a).Error; err != nil {
		t.Fatalf("reload assignment: %v", err)
	}
	return a.ID
}

func loadAssignment(t *testing.T, id uint) models.Assignment {
	t.Helper()
	var a models.Assignment
	if err := config.DB.First(&a, id).Error; err != nil {
		t.Fatalf("load assignment %d: %v", id, err)
	}
	return a
}

func TestPublishDueScheduledAssignmentsPublishesOnlyDueDraftsInOpenCourses(t *testing.T) {
	cleanup := setupScheduledPublishTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	due := createScheduledAssignment(t, "course_open", true, true, &past)
	notYet := createScheduledAssignment(t, "course_open", true, true, &future)
	plainDraft := createScheduledAssignment(t, "course_open", true, true, nil)
	deleted := createScheduledAssignment(t, "course_open", true, false, &past)
	closedCourse := createScheduledAssignment(t, "course_closed", true, true, &past)

	published, err := PublishDueScheduledAssignments(now)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(published) != 1 || published[0].ID != due {
		t.Fatalf("expected only assignment %d published, got %+v", due, published)
	}
	if published[0].IsDraft {
		t.Fatalf("returned row should be marked published")
	}
	if published[0].PublishAt == nil || !published[0].PublishAt.Equal(past) {
		t.Fatalf("returned row should keep the scheduled time, got %v", published[0].PublishAt)
	}

	got := loadAssignment(t, due)
	if got.IsDraft || got.PublishAt != nil {
		t.Fatalf("due draft should be published with publish_at cleared, got draft=%v publish_at=%v", got.IsDraft, got.PublishAt)
	}
	for _, id := range []uint{notYet, plainDraft, deleted, closedCourse} {
		if a := loadAssignment(t, id); !a.IsDraft {
			t.Fatalf("assignment %d should still be a draft", id)
		}
	}

	again, err := PublishDueScheduledAssignments(now)
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("a second run must not publish anything again, got %d", len(again))
	}
}
