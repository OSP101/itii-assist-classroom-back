package repositories

import (
	"fmt"
	"sort"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"gorm.io/gorm"
)

// Recovery of scores detached by the old UpdateAssignment, which hard-deleted an
// assignment's sub-items and re-created them under fresh IDs. The scores were
// never deleted — they still hold their score, comment, graded_by and graded_at,
// and only their sub_item_id points at a row that no longer exists.
//
// Re-attaching them relies on how the damage was done: the old code inserted a
// whole generation of sub-items in one multi-row INSERT ordered by order_index,
// so one generation is a run of consecutive IDs whose order matches order_index.
// A run whose length equals the current sub-item count can therefore be mapped
// position by position. Anything else is reported and left alone — a wrong guess
// would silently move a student's mark to a different question.

// ScoreOrphanRepairBackup preserves every score row the repair touches. It is the
// only way back: this repo has no version control, so the rows must be
// recoverable from inside the database itself.
type ScoreOrphanRepairBackup struct {
	ID           uint       `gorm:"primaryKey;autoIncrement"`
	RepairedAt   time.Time  `gorm:"type:timestamptz;index"`
	Action       string     `gorm:"type:varchar(20)"` // remapped, superseded
	ScoreID      uint       `gorm:"index"`
	AssignmentID uint       `gorm:"index"`
	StudentID    *uint      `gorm:"index"`
	OldSubItemID *uint      ``
	NewSubItemID *uint      ``
	Score        float64    `gorm:"type:decimal(5,2)"`
	Comment      string     `gorm:"type:text"`
	GradedBy     *uint      ``
	GradedAt     *time.Time `gorm:"type:timestamptz"`
	Status       string     `gorm:"type:varchar(20)"`
}

func (ScoreOrphanRepairBackup) TableName() string { return "scores_orphan_repair_backup" }

type LiveSubItem struct {
	ID   uint
	Name string
}

// OrphanGeneration is one run of consecutive orphaned sub-item IDs — one past
// incarnation of the assignment's sub-items.
type OrphanGeneration struct {
	SubItemIDs []uint
	// Mappable is true only when the run length matches the live sub-item count,
	// which is the one case where each ID's position can be inferred.
	Mappable bool
	// LikelyGenerations is set when the run is a whole multiple of the live count:
	// a hint that the assignment was edited repeatedly with nothing inserted in
	// between. Still not auto-mapped — a run of 6 against 3 live sub-items is
	// equally consistent with an assignment that once had 6.
	LikelyGenerations int
}

type OrphanRemap struct {
	FromSubItemID uint
	ToSubItemID   uint
	ToName        string
}

type AssignmentOrphanPlan struct {
	AssignmentID   uint
	AssignmentName string
	CourseID       string
	LiveSubItems   []LiveSubItem
	Generations    []OrphanGeneration
	Remaps         []OrphanRemap
	OrphanRowCount int64
}

// Unmappable reports generations the plan refuses to guess at.
func (p AssignmentOrphanPlan) Unmappable() []OrphanGeneration {
	var result []OrphanGeneration
	for _, generation := range p.Generations {
		if !generation.Mappable {
			result = append(result, generation)
		}
	}
	return result
}

type OrphanRepairResult struct {
	Remapped   int64
	Skipped    int64 // a live score already exists for that student and sub-item
	Superseded int64 // an older generation lost to a newer one on the same cell
}

// groupConsecutiveIDs splits sorted IDs into runs of consecutive integers.
func groupConsecutiveIDs(ids []uint) [][]uint {
	if len(ids) == 0 {
		return nil
	}
	sorted := append([]uint(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	groups := [][]uint{{sorted[0]}}
	for _, id := range sorted[1:] {
		current := &groups[len(groups)-1]
		if id == (*current)[len(*current)-1]+1 {
			*current = append(*current, id)
			continue
		}
		groups = append(groups, []uint{id})
	}
	return groups
}

// buildOrphanGenerations decides which runs can be mapped onto the live
// sub-items. Live must already be ordered by order_index.
func buildOrphanGenerations(orphanIDs []uint, live []LiveSubItem) ([]OrphanGeneration, []OrphanRemap) {
	runs := groupConsecutiveIDs(orphanIDs)
	generations := make([]OrphanGeneration, 0, len(runs))
	var remaps []OrphanRemap

	for _, run := range runs {
		generation := OrphanGeneration{SubItemIDs: run}
		switch {
		case len(live) == 0:
			// Nothing to map onto.
		case len(run) == len(live):
			generation.Mappable = true
			for i, id := range run {
				remaps = append(remaps, OrphanRemap{
					FromSubItemID: id,
					ToSubItemID:   live[i].ID,
					ToName:        live[i].Name,
				})
			}
		case len(run)%len(live) == 0:
			generation.LikelyGenerations = len(run) / len(live)
		}
		generations = append(generations, generation)
	}
	return generations, remaps
}

// PlanOrphanScoreRepair inspects the data and reports what would be re-attached.
// It writes nothing. Pass an empty courseID to cover every course.
func PlanOrphanScoreRepair(courseID string) ([]AssignmentOrphanPlan, error) {
	query := config.DB.Model(&models.Assignment{}).Select("id", "course_id", "name")
	if courseID != "" {
		query = query.Where("course_id = ?", courseID)
	}
	var assignments []models.Assignment
	if err := query.Order("id ASC").Find(&assignments).Error; err != nil {
		return nil, err
	}

	plans := make([]AssignmentOrphanPlan, 0)
	for _, assignment := range assignments {
		var orphanIDs []uint
		if err := config.DB.Model(&models.Score{}).
			Distinct("sub_item_id").
			Where(`assignment_id = ? AND sub_item_id IS NOT NULL
			       AND NOT EXISTS (SELECT 1 FROM assignment_sub_items si WHERE si.id = scores.sub_item_id)`, assignment.ID).
			Order("sub_item_id ASC").
			Pluck("sub_item_id", &orphanIDs).Error; err != nil {
			return nil, err
		}
		if len(orphanIDs) == 0 {
			continue
		}

		var rowCount int64
		if err := config.DB.Model(&models.Score{}).
			Where("assignment_id = ? AND sub_item_id IN ?", assignment.ID, orphanIDs).
			Count(&rowCount).Error; err != nil {
			return nil, err
		}

		var subItems []models.AssignmentSubItem
		if err := config.DB.Where("assignment_id = ?", assignment.ID).
			Order("order_index ASC, id ASC").Find(&subItems).Error; err != nil {
			return nil, err
		}
		live := make([]LiveSubItem, len(subItems))
		for i, item := range subItems {
			live[i] = LiveSubItem{ID: item.ID, Name: item.Name}
		}

		generations, remaps := buildOrphanGenerations(orphanIDs, live)
		plans = append(plans, AssignmentOrphanPlan{
			AssignmentID:   assignment.ID,
			AssignmentName: assignment.Name,
			CourseID:       assignment.CourseID,
			LiveSubItems:   live,
			Generations:    generations,
			Remaps:         remaps,
			OrphanRowCount: rowCount,
		})
	}
	return plans, nil
}

type orphanCandidate struct {
	ScoreID      uint
	StudentID    *uint
	SubItemID    *uint
	Score        float64
	Comment      string
	GradedBy     *uint
	GradedAt     *time.Time
	Status       string
	UpdatedAt    time.Time
	targetID     uint
	targetName   string
	assignmentID uint
}

// newer decides which of two scores for the same cell wins. graded_at is the
// meaningful timestamp; updated_at and then the row id only break ties.
func (c orphanCandidate) newer(other orphanCandidate) bool {
	switch {
	case c.GradedAt != nil && other.GradedAt != nil && !c.GradedAt.Equal(*other.GradedAt):
		return c.GradedAt.After(*other.GradedAt)
	case c.GradedAt != nil && other.GradedAt == nil:
		return true
	case c.GradedAt == nil && other.GradedAt != nil:
		return false
	case !c.UpdatedAt.Equal(other.UpdatedAt):
		return c.UpdatedAt.After(other.UpdatedAt)
	}
	return c.ScoreID > other.ScoreID
}

// ApplyOrphanScoreRepair re-attaches the scores the plans identified, in one
// transaction, backing up every row it touches first.
func ApplyOrphanScoreRepair(plans []AssignmentOrphanPlan) (OrphanRepairResult, error) {
	var result OrphanRepairResult

	if err := config.DB.AutoMigrate(&ScoreOrphanRepairBackup{}); err != nil {
		return result, fmt.Errorf("create backup table: %w", err)
	}

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		for _, plan := range plans {
			if len(plan.Remaps) == 0 {
				continue
			}
			applied, err := applyPlanTx(tx, plan, now)
			if err != nil {
				return err
			}
			result.Remapped += applied.Remapped
			result.Skipped += applied.Skipped
			result.Superseded += applied.Superseded
		}
		return nil
	})
	return result, err
}

func applyPlanTx(tx *gorm.DB, plan AssignmentOrphanPlan, now time.Time) (OrphanRepairResult, error) {
	var result OrphanRepairResult

	targetByOrphanID := make(map[uint]OrphanRemap, len(plan.Remaps))
	orphanIDs := make([]uint, 0, len(plan.Remaps))
	for _, remap := range plan.Remaps {
		targetByOrphanID[remap.FromSubItemID] = remap
		orphanIDs = append(orphanIDs, remap.FromSubItemID)
	}

	var rows []models.Score
	if err := tx.Where("assignment_id = ? AND sub_item_id IN ?", plan.AssignmentID, orphanIDs).
		Order("id ASC").Find(&rows).Error; err != nil {
		return result, err
	}

	// A student re-graded after the edit already has a real score on the new
	// sub-item. That score is the truth; the orphan must not overwrite it.
	type cell struct {
		studentID uint
		subItemID uint
	}
	occupied := map[cell]bool{}
	var liveIDs []uint
	for _, item := range plan.LiveSubItems {
		liveIDs = append(liveIDs, item.ID)
	}
	var liveRows []models.Score
	if err := tx.Select("student_id", "sub_item_id").
		Where("assignment_id = ? AND sub_item_id IN ?", plan.AssignmentID, liveIDs).
		Find(&liveRows).Error; err != nil {
		return result, err
	}
	for _, row := range liveRows {
		if row.StudentID != nil && row.SubItemID != nil {
			occupied[cell{*row.StudentID, *row.SubItemID}] = true
		}
	}

	// Several generations can compete for one cell; keep the newest score.
	winners := map[cell]orphanCandidate{}
	var losers []orphanCandidate
	for _, row := range rows {
		if row.StudentID == nil || row.SubItemID == nil {
			continue
		}
		remap := targetByOrphanID[*row.SubItemID]
		key := cell{*row.StudentID, remap.ToSubItemID}
		if occupied[key] {
			result.Skipped++
			continue
		}
		candidate := orphanCandidate{
			ScoreID: row.ID, StudentID: row.StudentID, SubItemID: row.SubItemID,
			Score: row.Score, Comment: row.Comment, GradedBy: row.GradedBy,
			GradedAt: row.GradedAt, Status: row.Status, UpdatedAt: row.UpdatedAt,
			targetID: remap.ToSubItemID, targetName: remap.ToName, assignmentID: plan.AssignmentID,
		}
		existing, found := winners[key]
		if !found {
			winners[key] = candidate
			continue
		}
		if candidate.newer(existing) {
			winners[key] = candidate
			losers = append(losers, existing)
			continue
		}
		losers = append(losers, candidate)
	}

	for _, candidate := range winners {
		if err := backupCandidateTx(tx, candidate, "remapped", now); err != nil {
			return result, err
		}
		if err := tx.Model(&models.Score{}).Where("id = ?", candidate.ScoreID).
			Updates(map[string]interface{}{
				"sub_item_id": candidate.targetID,
				"updated_at":  now,
			}).Error; err != nil {
			return result, err
		}
		result.Remapped++
	}

	for _, candidate := range losers {
		if err := backupCandidateTx(tx, candidate, "superseded", now); err != nil {
			return result, err
		}
		if err := tx.Where("id = ?", candidate.ScoreID).Delete(&models.Score{}).Error; err != nil {
			return result, err
		}
		result.Superseded++
	}

	return result, nil
}

func backupCandidateTx(tx *gorm.DB, candidate orphanCandidate, action string, now time.Time) error {
	target := candidate.targetID
	return tx.Create(&ScoreOrphanRepairBackup{
		RepairedAt:   now,
		Action:       action,
		ScoreID:      candidate.ScoreID,
		AssignmentID: candidate.assignmentID,
		StudentID:    candidate.StudentID,
		OldSubItemID: candidate.SubItemID,
		NewSubItemID: &target,
		Score:        candidate.Score,
		Comment:      candidate.Comment,
		GradedBy:     candidate.GradedBy,
		GradedAt:     candidate.GradedAt,
		Status:       candidate.Status,
	}).Error
}
