package repositories

import (
	"errors"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupAssignmentSubItemTestDB builds one assignment with sub-items and scores
// attached to them, matching the shape the score matrix reads back:
// scores are keyed by the sub-item primary key, with no FK to enforce it.
func setupAssignmentSubItemTestDB(t *testing.T) (func(), models.Assignment) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:assignment_subitem_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	migrateWithSQLiteTimestamps(t, db, &models.Assignment{}, &models.AssignmentSubItem{}, &models.Score{})

	prevDB := config.DB
	config.DB = db

	assignment := models.Assignment{
		CourseID:       "course_scores",
		Name:           "Lab01 – Introduction to HTML Part1",
		AssignmentType: "individual",
		MaxScore:       20,
		IsActive:       true,
		CreatedBy:      1,
	}
	if err := config.DB.Create(&assignment).Error; err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	cleanup := func() {
		db.Exec("DELETE FROM scores")
		db.Exec("DELETE FROM assignment_sub_items")
		db.Exec("DELETE FROM assignments")
		config.DB = prevDB
	}
	return cleanup, assignment
}

func createSubItems(t *testing.T, assignmentID uint, names ...string) []models.AssignmentSubItem {
	t.Helper()
	items := make([]models.AssignmentSubItem, len(names))
	for i, name := range names {
		items[i] = models.AssignmentSubItem{
			AssignmentID: assignmentID,
			Name:         name,
			MaxScore:     10,
			OrderIndex:   i + 1,
		}
	}
	if err := config.DB.Create(&items).Error; err != nil {
		t.Fatalf("create sub-items: %v", err)
	}
	return items
}

func createSubItemScore(t *testing.T, assignmentID uint, studentID uint, subItemID uint, value float64) models.Score {
	t.Helper()
	now := time.Now().UTC()
	grader := uint(9)
	score := models.Score{
		AssignmentID: assignmentID,
		StudentID:    &studentID,
		SubItemID:    &subItemID,
		Score:        value,
		GradedBy:     &grader,
		GradedAt:     &now,
		Status:       "graded",
	}
	if err := config.DB.Create(&score).Error; err != nil {
		t.Fatalf("create score: %v", err)
	}
	return score
}

// countResolvableScores counts scores that still join to a live sub-item. This
// mirrors how the score matrix resolves a cell (handlers/score_handler.go builds
// its lookup key from the current sub-item ID), so a score that stops being
// counted here is a score that has vanished from the instructor's grid.
func countResolvableScores(t *testing.T, assignmentID uint) int64 {
	t.Helper()
	var count int64
	if err := config.DB.Model(&models.Score{}).
		Joins("JOIN assignment_sub_items si ON si.id = scores.sub_item_id").
		Where("scores.assignment_id = ?", assignmentID).
		Count(&count).Error; err != nil {
		t.Fatalf("count resolvable scores: %v", err)
	}
	return count
}

// Editing an assignment must not re-key its sub-items. The client sends the
// existing IDs precisely so scores stay attached; dropping them detaches every
// score on the assignment while leaving the rows in the table, which is exactly
// how scores "disappear" from the grid without any delete taking place.
func TestUpdateAssignment_PreservesSubItemIDsWhenIDsProvided(t *testing.T) {
	cleanup, assignment := setupAssignmentSubItemTestDB(t)
	defer cleanup()

	existing := createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2")
	createSubItemScore(t, assignment.ID, 501, existing[0].ID, 10)
	createSubItemScore(t, assignment.ID, 501, existing[1].ID, 10)

	if got := countResolvableScores(t, assignment.ID); got != 2 {
		t.Fatalf("precondition: expected 2 resolvable scores, got %d", got)
	}

	// A pure rename — the modal always resends the full sub_items array with IDs.
	assignment.Name = "Lab01 – Introduction to HTML (renamed)"
	payload := []models.AssignmentSubItem{
		{ID: existing[0].ID, Name: "ข้อ 1", MaxScore: 10},
		{ID: existing[1].ID, Name: "ข้อ 2", MaxScore: 10},
	}
	if err := UpdateAssignment(&assignment, &payload, false); err != nil {
		t.Fatalf("update assignment: %v", err)
	}

	var after []models.AssignmentSubItem
	if err := config.DB.Where("assignment_id = ?", assignment.ID).
		Order("order_index ASC").Find(&after).Error; err != nil {
		t.Fatalf("load sub-items: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("expected 2 sub-items after update, got %d", len(after))
	}
	for i, item := range after {
		if item.ID != existing[i].ID {
			t.Errorf("sub-item %d was re-keyed: id %d → %d; every score pointing at the old id is now orphaned",
				i+1, existing[i].ID, item.ID)
		}
	}

	if got := countResolvableScores(t, assignment.ID); got != 2 {
		t.Errorf("expected 2 scores to still resolve to a live sub-item after a rename, got %d", got)
	}
}

// Adding a sub-item must not disturb the ones already carrying scores.
func TestUpdateAssignment_AddsNewSubItemWithoutRekeyingExisting(t *testing.T) {
	cleanup, assignment := setupAssignmentSubItemTestDB(t)
	defer cleanup()

	existing := createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2")
	createSubItemScore(t, assignment.ID, 501, existing[0].ID, 10)
	createSubItemScore(t, assignment.ID, 501, existing[1].ID, 8)

	payload := []models.AssignmentSubItem{
		{ID: existing[0].ID, Name: "ข้อ 1", MaxScore: 10},
		{ID: existing[1].ID, Name: "ข้อ 2", MaxScore: 10},
		{Name: "ข้อ 3", MaxScore: 10},
	}
	if err := UpdateAssignment(&assignment, &payload, false); err != nil {
		t.Fatalf("update assignment: %v", err)
	}

	var after []models.AssignmentSubItem
	if err := config.DB.Where("assignment_id = ?", assignment.ID).
		Order("order_index ASC").Find(&after).Error; err != nil {
		t.Fatalf("load sub-items: %v", err)
	}
	if len(after) != 3 {
		t.Fatalf("expected 3 sub-items, got %d", len(after))
	}
	if after[0].ID != existing[0].ID || after[1].ID != existing[1].ID {
		t.Errorf("existing sub-items were re-keyed: got ids %d,%d want %d,%d",
			after[0].ID, after[1].ID, existing[0].ID, existing[1].ID)
	}
	if after[2].ID == 0 || after[2].ID == existing[0].ID || after[2].ID == existing[1].ID {
		t.Errorf("expected the new sub-item to get a fresh id, got %d", after[2].ID)
	}
	for i, item := range after {
		if item.OrderIndex != i+1 {
			t.Errorf("sub-item %d has order_index %d, want %d", i+1, item.OrderIndex, i+1)
		}
	}
	if got := countResolvableScores(t, assignment.ID); got != 2 {
		t.Errorf("expected both existing scores to survive, got %d", got)
	}
}

// Removing a sub-item that students were graded on is destructive, so it must be
// refused until the caller confirms, and the refusal must change nothing.
func TestUpdateAssignment_RefusesToDropSubItemWithScores(t *testing.T) {
	cleanup, assignment := setupAssignmentSubItemTestDB(t)
	defer cleanup()

	existing := createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2")
	createSubItemScore(t, assignment.ID, 501, existing[1].ID, 7)
	createSubItemScore(t, assignment.ID, 502, existing[1].ID, 9)

	originalName := assignment.Name
	assignment.Name = "renamed while dropping ข้อ 2"
	payload := []models.AssignmentSubItem{
		{ID: existing[0].ID, Name: "ข้อ 1", MaxScore: 10},
	}

	err := UpdateAssignment(&assignment, &payload, false)
	if err == nil {
		t.Fatal("expected removing a graded sub-item to be refused")
	}
	var scoresErr *ErrSubItemsHaveScores
	if !errors.As(err, &scoresErr) {
		t.Fatalf("expected ErrSubItemsHaveScores, got %T: %v", err, err)
	}
	if len(scoresErr.Items) != 1 || scoresErr.Items[0].ID != existing[1].ID {
		t.Fatalf("expected the blocking sub-item to be reported, got %+v", scoresErr.Items)
	}
	if scoresErr.Items[0].Name != "ข้อ 2" {
		t.Errorf("expected the blocking sub-item to be named, got %q", scoresErr.Items[0].Name)
	}
	if scoresErr.TotalScores() != 2 {
		t.Errorf("expected 2 scores at risk, got %d", scoresErr.TotalScores())
	}

	// The whole update is one transaction, so the rename must have rolled back too.
	var reloaded models.Assignment
	if err := config.DB.First(&reloaded, assignment.ID).Error; err != nil {
		t.Fatalf("reload assignment: %v", err)
	}
	if reloaded.Name != originalName {
		t.Errorf("expected the refused update to roll back the rename, name is now %q", reloaded.Name)
	}
	var subItemCount int64
	config.DB.Model(&models.AssignmentSubItem{}).Where("assignment_id = ?", assignment.ID).Count(&subItemCount)
	if subItemCount != 2 {
		t.Errorf("expected both sub-items to survive a refused update, got %d", subItemCount)
	}
	if got := countResolvableScores(t, assignment.ID); got != 2 {
		t.Errorf("expected both scores to survive a refused update, got %d", got)
	}
}

// Once confirmed, the sub-item and its scores must go together — leaving the
// scores behind is exactly the orphaning this guard exists to prevent.
func TestUpdateAssignment_ConfirmedDropRemovesScoresToo(t *testing.T) {
	cleanup, assignment := setupAssignmentSubItemTestDB(t)
	defer cleanup()

	existing := createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2")
	createSubItemScore(t, assignment.ID, 501, existing[0].ID, 10)
	createSubItemScore(t, assignment.ID, 501, existing[1].ID, 7)

	payload := []models.AssignmentSubItem{
		{ID: existing[0].ID, Name: "ข้อ 1", MaxScore: 10},
	}
	if err := UpdateAssignment(&assignment, &payload, true); err != nil {
		t.Fatalf("confirmed update: %v", err)
	}

	var orphaned int64
	if err := config.DB.Model(&models.Score{}).
		Where("sub_item_id = ?", existing[1].ID).Count(&orphaned).Error; err != nil {
		t.Fatalf("count orphaned scores: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("expected the dropped sub-item's scores to be deleted, found %d orphaned rows", orphaned)
	}

	var remaining int64
	config.DB.Model(&models.Score{}).Where("assignment_id = ?", assignment.ID).Count(&remaining)
	if remaining != 1 {
		t.Errorf("expected the surviving sub-item's score to remain, got %d rows", remaining)
	}
	if got := countResolvableScores(t, assignment.ID); got != 1 {
		t.Errorf("expected 1 resolvable score, got %d", got)
	}
}

// A payload ID belonging to another assignment must not be adopted, or a stale
// client could steal another assignment's sub-item along with its scores.
func TestUpdateAssignment_IgnoresForeignSubItemID(t *testing.T) {
	cleanup, assignment := setupAssignmentSubItemTestDB(t)
	defer cleanup()

	other := models.Assignment{CourseID: "course_scores", Name: "Lab02", AssignmentType: "individual", IsActive: true, CreatedBy: 1}
	if err := config.DB.Create(&other).Error; err != nil {
		t.Fatalf("create other assignment: %v", err)
	}
	foreign := createSubItems(t, other.ID, "ข้อ 1 ของงานอื่น")

	payload := []models.AssignmentSubItem{
		{ID: foreign[0].ID, Name: "ข้อ 1", MaxScore: 10},
	}
	if err := UpdateAssignment(&assignment, &payload, false); err != nil {
		t.Fatalf("update assignment: %v", err)
	}

	var stolen models.AssignmentSubItem
	if err := config.DB.First(&stolen, foreign[0].ID).Error; err != nil {
		t.Fatalf("reload the other assignment's sub-item: %v", err)
	}
	if stolen.AssignmentID != other.ID {
		t.Errorf("a foreign sub-item was adopted: assignment_id is now %d, want %d", stolen.AssignmentID, other.ID)
	}
	if stolen.Name != "ข้อ 1 ของงานอื่น" {
		t.Errorf("a foreign sub-item was overwritten, name is now %q", stolen.Name)
	}

	var mine []models.AssignmentSubItem
	config.DB.Where("assignment_id = ?", assignment.ID).Find(&mine)
	if len(mine) != 1 || mine[0].ID == foreign[0].ID {
		t.Fatalf("expected a freshly inserted sub-item on this assignment, got %+v", mine)
	}
}
