package repositories

import (
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGroupConsecutiveIDs(t *testing.T) {
	cases := []struct {
		name string
		ids  []uint
		want [][]uint
	}{
		{"empty", nil, nil},
		{"single run", []uint{10, 11, 12}, [][]uint{{10, 11, 12}}},
		{"two runs", []uint{10, 11, 12, 20, 21, 22}, [][]uint{{10, 11, 12}, {20, 21, 22}}},
		{"gap inside a generation", []uint{10, 12}, [][]uint{{10}, {12}}},
		{"unsorted input", []uint{22, 10, 21, 12, 20, 11}, [][]uint{{10, 11, 12}, {20, 21, 22}}},
		{"lone id", []uint{7}, [][]uint{{7}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := groupConsecutiveIDs(tc.ids)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if len(got[i]) != len(tc.want[i]) {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
				for j := range tc.want[i] {
					if got[i][j] != tc.want[i][j] {
						t.Fatalf("got %v, want %v", got, tc.want)
					}
				}
			}
		})
	}
}

// A full generation maps position by position, because the old code inserted the
// whole batch in one statement ordered by order_index.
func TestBuildOrphanGenerations_MapsFullGeneration(t *testing.T) {
	live := []LiveSubItem{{ID: 30, Name: "ข้อ 1"}, {ID: 31, Name: "ข้อ 2"}, {ID: 32, Name: "ข้อ 3"}}
	generations, remaps := buildOrphanGenerations([]uint{10, 11, 12}, live)

	if len(generations) != 1 || !generations[0].Mappable {
		t.Fatalf("expected one mappable generation, got %+v", generations)
	}
	want := []OrphanRemap{
		{FromSubItemID: 10, ToSubItemID: 30, ToName: "ข้อ 1"},
		{FromSubItemID: 11, ToSubItemID: 31, ToName: "ข้อ 2"},
		{FromSubItemID: 12, ToSubItemID: 32, ToName: "ข้อ 3"},
	}
	if len(remaps) != len(want) {
		t.Fatalf("got %+v, want %+v", remaps, want)
	}
	for i := range want {
		if remaps[i] != want[i] {
			t.Errorf("remap %d: got %+v, want %+v", i, remaps[i], want[i])
		}
	}
}

// Two generations, each complete, both map — a student graded twice is resolved
// later by graded_at rather than here.
func TestBuildOrphanGenerations_MapsEachSeparateGeneration(t *testing.T) {
	live := []LiveSubItem{{ID: 30, Name: "ข้อ 1"}, {ID: 31, Name: "ข้อ 2"}}
	generations, remaps := buildOrphanGenerations([]uint{10, 11, 20, 21}, live)

	if len(generations) != 2 {
		t.Fatalf("expected 2 generations, got %+v", generations)
	}
	for i, generation := range generations {
		if !generation.Mappable {
			t.Errorf("generation %d should be mappable: %+v", i, generation)
		}
	}
	if len(remaps) != 4 {
		t.Fatalf("expected 4 remaps, got %+v", remaps)
	}
	if remaps[2] != (OrphanRemap{FromSubItemID: 20, ToSubItemID: 30, ToName: "ข้อ 1"}) {
		t.Errorf("second generation mapped wrong: %+v", remaps[2])
	}
}

// A partially graded generation leaves gaps, and a gap destroys the positional
// evidence: {10,12} against 3 sub-items could be questions 1+2 or 1+3. Guessing
// would move a student's mark onto the wrong question, so nothing is mapped.
func TestBuildOrphanGenerations_RefusesIncompleteGeneration(t *testing.T) {
	live := []LiveSubItem{{ID: 30, Name: "ข้อ 1"}, {ID: 31, Name: "ข้อ 2"}, {ID: 32, Name: "ข้อ 3"}}
	generations, remaps := buildOrphanGenerations([]uint{10, 12}, live)

	if len(remaps) != 0 {
		t.Fatalf("expected no remaps for an ambiguous generation, got %+v", remaps)
	}
	if len(generations) != 2 {
		t.Fatalf("expected the run to split at the gap, got %+v", generations)
	}
	for i, generation := range generations {
		if generation.Mappable {
			t.Errorf("generation %d must not be mappable: %+v", i, generation)
		}
	}
}

// Back-to-back edits leave one long run. A run of 6 against 3 live sub-items is
// equally consistent with an assignment that once had 6 sub-items, so it is
// flagged for a human rather than split.
func TestBuildOrphanGenerations_FlagsRunThatIsAMultiple(t *testing.T) {
	live := []LiveSubItem{{ID: 30, Name: "ข้อ 1"}, {ID: 31, Name: "ข้อ 2"}, {ID: 32, Name: "ข้อ 3"}}
	generations, remaps := buildOrphanGenerations([]uint{10, 11, 12, 13, 14, 15}, live)

	if len(remaps) != 0 {
		t.Fatalf("expected no automatic remaps, got %+v", remaps)
	}
	if len(generations) != 1 || generations[0].Mappable {
		t.Fatalf("expected a single unmappable generation, got %+v", generations)
	}
	if generations[0].LikelyGenerations != 2 {
		t.Errorf("expected the report to hint at 2 generations, got %d", generations[0].LikelyGenerations)
	}
}

func setupOrphanRepairTestDB(t *testing.T) (func(), models.Assignment) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:orphan_repair_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	migrateWithSQLiteTimestamps(t, db, &models.Assignment{}, &models.AssignmentSubItem{},
		&models.Score{}, &ScoreOrphanRepairBackup{})

	prevDB := config.DB
	config.DB = db

	assignment := models.Assignment{
		CourseID: "course_orphan", Name: "Lab01", AssignmentType: "individual",
		MaxScore: 30, IsActive: true, CreatedBy: 1,
	}
	if err := config.DB.Create(&assignment).Error; err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	cleanup := func() {
		db.Exec("DELETE FROM scores")
		db.Exec("DELETE FROM assignment_sub_items")
		db.Exec("DELETE FROM assignments")
		db.Exec("DELETE FROM scores_orphan_repair_backup")
		config.DB = prevDB
	}
	return cleanup, assignment
}

// Rebuild the exact damage the old code caused: create sub-items, grade them,
// then hard-delete and re-create the sub-items so the scores are left behind.
func orphanScoresByRecreatingSubItems(t *testing.T, assignmentID uint, names []string, students []uint) []models.AssignmentSubItem {
	t.Helper()

	old := createSubItems(t, assignmentID, names...)
	for _, studentID := range students {
		for i, item := range old {
			createSubItemScore(t, assignmentID, studentID, item.ID, float64(10-i))
		}
	}
	if err := config.DB.Where("assignment_id = ?", assignmentID).
		Delete(&models.AssignmentSubItem{}).Error; err != nil {
		t.Fatalf("delete old sub-items: %v", err)
	}
	return createSubItems(t, assignmentID, names...)
}

func TestOrphanScoreRepair_ReattachesScoresAfterAnEdit(t *testing.T) {
	cleanup, assignment := setupOrphanRepairTestDB(t)
	defer cleanup()

	fresh := orphanScoresByRecreatingSubItems(t, assignment.ID,
		[]string{"ข้อ 1", "ข้อ 2", "ข้อ 3"}, []uint{501, 502})

	if got := countResolvableScores(t, assignment.ID); got != 0 {
		t.Fatalf("precondition: expected every score to be orphaned, %d still resolve", got)
	}

	plans, err := PlanOrphanScoreRepair("course_orphan")
	if err != nil {
		t.Fatalf("plan repair: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	if plans[0].OrphanRowCount != 6 {
		t.Errorf("expected 6 orphaned rows, got %d", plans[0].OrphanRowCount)
	}
	if len(plans[0].Unmappable()) != 0 {
		t.Errorf("expected a clean single generation, got %+v", plans[0].Unmappable())
	}

	result, err := ApplyOrphanScoreRepair(plans)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if result.Remapped != 6 {
		t.Errorf("expected 6 scores re-attached, got %d", result.Remapped)
	}
	if got := countResolvableScores(t, assignment.ID); got != 6 {
		t.Errorf("expected all 6 scores to resolve again, got %d", got)
	}

	// The score must land on the question it was given for, not merely on some
	// question — sub-item 1 was graded 10, sub-item 3 was graded 8.
	var first models.Score
	if err := config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id = ?",
		assignment.ID, 501, fresh[0].ID).First(&first).Error; err != nil {
		t.Fatalf("load re-attached score: %v", err)
	}
	if first.Score != 10 {
		t.Errorf("expected ข้อ 1 to keep its 10, got %v", first.Score)
	}
	var third models.Score
	if err := config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id = ?",
		assignment.ID, 501, fresh[2].ID).First(&third).Error; err != nil {
		t.Fatalf("load re-attached score: %v", err)
	}
	if third.Score != 8 {
		t.Errorf("expected ข้อ 3 to keep its 8, got %v", third.Score)
	}

	var backupCount int64
	config.DB.Model(&ScoreOrphanRepairBackup{}).Count(&backupCount)
	if backupCount != 6 {
		t.Errorf("expected every touched row to be backed up, got %d", backupCount)
	}
}

// A student re-graded after the edit holds a real score on the new sub-item.
// That score is the truth and the orphan must not overwrite it.
func TestOrphanScoreRepair_DoesNotOverwriteAScoreGradedAfterTheEdit(t *testing.T) {
	cleanup, assignment := setupOrphanRepairTestDB(t)
	defer cleanup()

	fresh := orphanScoresByRecreatingSubItems(t, assignment.ID, []string{"ข้อ 1", "ข้อ 2"}, []uint{501})
	// The TA re-graded ข้อ 1 on the new sub-item, giving 3 instead of the old 10.
	createSubItemScore(t, assignment.ID, 501, fresh[0].ID, 3)

	plans, err := PlanOrphanScoreRepair("course_orphan")
	if err != nil {
		t.Fatalf("plan repair: %v", err)
	}
	result, err := ApplyOrphanScoreRepair(plans)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("expected the re-graded cell to be skipped, got %d", result.Skipped)
	}
	if result.Remapped != 1 {
		t.Errorf("expected only ข้อ 2 to be re-attached, got %d", result.Remapped)
	}

	var scores []models.Score
	if err := config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id = ?",
		assignment.ID, 501, fresh[0].ID).Find(&scores).Error; err != nil {
		t.Fatalf("load scores: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("expected exactly one score on the re-graded sub-item, got %d", len(scores))
	}
	if scores[0].Score != 3 {
		t.Errorf("the re-graded score was overwritten by an orphan: got %v, want 3", scores[0].Score)
	}
}

// Two edits leave two complete generations competing for the same cell; the
// newer grading wins and the older is preserved in the backup, not left behind
// as a fresh orphan.
func TestOrphanScoreRepair_NewerGenerationWinsTheCell(t *testing.T) {
	cleanup, assignment := setupOrphanRepairTestDB(t)
	defer cleanup()

	older := time.Now().UTC().Add(-48 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)

	// Two dead generations, kept apart so each is its own run of consecutive ids.
	gen1 := createSubItems(t, assignment.ID, "ข้อ 1")
	createSubItemScore(t, assignment.ID, 501, gen1[0].ID, 4)
	config.DB.Model(&models.Score{}).Where("sub_item_id = ?", gen1[0].ID).Update("graded_at", older)
	config.DB.Where("assignment_id = ?", assignment.ID).Delete(&models.AssignmentSubItem{})

	spacer := createSubItems(t, assignment.ID, "spacer")
	config.DB.Where("id = ?", spacer[0].ID).Delete(&models.AssignmentSubItem{})

	gen2 := createSubItems(t, assignment.ID, "ข้อ 1")
	createSubItemScore(t, assignment.ID, 501, gen2[0].ID, 9)
	config.DB.Model(&models.Score{}).Where("sub_item_id = ?", gen2[0].ID).Update("graded_at", newer)
	config.DB.Where("assignment_id = ?", assignment.ID).Delete(&models.AssignmentSubItem{})

	live := createSubItems(t, assignment.ID, "ข้อ 1")

	plans, err := PlanOrphanScoreRepair("course_orphan")
	if err != nil {
		t.Fatalf("plan repair: %v", err)
	}
	result, err := ApplyOrphanScoreRepair(plans)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if result.Remapped != 1 {
		t.Errorf("expected 1 winner, got %d", result.Remapped)
	}
	if result.Superseded != 1 {
		t.Errorf("expected 1 superseded duplicate, got %d", result.Superseded)
	}

	var scores []models.Score
	if err := config.DB.Where("assignment_id = ? AND sub_item_id = ?", assignment.ID, live[0].ID).
		Find(&scores).Error; err != nil {
		t.Fatalf("load scores: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("expected exactly one score on the live sub-item, got %d", len(scores))
	}
	if scores[0].Score != 9 {
		t.Errorf("expected the newer grading (9) to win, got %v", scores[0].Score)
	}

	var superseded int64
	config.DB.Model(&ScoreOrphanRepairBackup{}).Where("action = ?", "superseded").Count(&superseded)
	if superseded != 1 {
		t.Errorf("expected the losing score to be preserved in the backup, got %d", superseded)
	}
}

// Ambiguous damage must be reported, never repaired on a guess.
func TestOrphanScoreRepair_LeavesAmbiguousGenerationAlone(t *testing.T) {
	cleanup, assignment := setupOrphanRepairTestDB(t)
	defer cleanup()

	old := createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2", "ข้อ 3")
	// Only ข้อ 1 and ข้อ 3 were graded, so the run has a hole in the middle.
	createSubItemScore(t, assignment.ID, 501, old[0].ID, 10)
	createSubItemScore(t, assignment.ID, 501, old[2].ID, 6)
	config.DB.Where("assignment_id = ?", assignment.ID).Delete(&models.AssignmentSubItem{})
	createSubItems(t, assignment.ID, "ข้อ 1", "ข้อ 2", "ข้อ 3")

	plans, err := PlanOrphanScoreRepair("course_orphan")
	if err != nil {
		t.Fatalf("plan repair: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected the damage to be reported, got %d plans", len(plans))
	}
	if len(plans[0].Remaps) != 0 {
		t.Errorf("expected no automatic remap for ambiguous data, got %+v", plans[0].Remaps)
	}
	if len(plans[0].Unmappable()) != 2 {
		t.Errorf("expected both fragments to be reported for review, got %+v", plans[0].Unmappable())
	}

	result, err := ApplyOrphanScoreRepair(plans)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if result.Remapped != 0 {
		t.Errorf("expected nothing to be written, got %d", result.Remapped)
	}
	if got := countResolvableScores(t, assignment.ID); got != 0 {
		t.Errorf("expected the orphans to be left untouched, %d were moved", got)
	}
}
