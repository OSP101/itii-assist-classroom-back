package repositories

import (
	"fmt"
	"sort"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"gorm.io/gorm"
)

// ============================================================
// Assignment CRUD
// ============================================================

type AssignmentWithSubItems struct {
	models.Assignment
	SubItems                 []models.AssignmentSubItem `json:"subItems"`
	LinkedAttendanceSession  *LinkedAttendanceSession   `json:"linkedAttendanceSession,omitempty"`
	LinkedAttendanceSessions []LinkedAttendanceSession  `json:"linkedAttendanceSessions"`
}

type LinkedAttendanceSession struct {
	ID              uint      `json:"id"`
	Title           string    `json:"title"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	SessionType     string    `json:"session_type,omitempty"`
	CourseSectionID *uint     `json:"course_section_id,omitempty"`
}

func GetAssignments(courseID string) ([]AssignmentWithSubItems, error) {
	var assignments []models.Assignment
	err := config.DB.Where("course_id = ? AND is_active = true", courseID).
		Order("order_index ASC, created_at DESC").Find(&assignments).Error
	if err != nil {
		return nil, err
	}

	if len(assignments) == 0 {
		return []AssignmentWithSubItems{}, nil
	}

	assignmentIDs := make([]uint, len(assignments))
	for i, a := range assignments {
		assignmentIDs[i] = a.ID
	}

	var allSubItems []models.AssignmentSubItem
	config.DB.Where("assignment_id IN ?", assignmentIDs).Order("assignment_id ASC, order_index ASC").Find(&allSubItems)

	subItemsMap := map[uint][]models.AssignmentSubItem{}
	for _, si := range allSubItems {
		subItemsMap[si.AssignmentID] = append(subItemsMap[si.AssignmentID], si)
	}

	linkedSessionsMap, err := loadAssignmentAttendanceLinks(assignmentIDs)
	if err != nil {
		return nil, err
	}

	result := make([]AssignmentWithSubItems, len(assignments))
	for i, a := range assignments {
		subs := subItemsMap[a.ID]
		if subs == nil {
			subs = []models.AssignmentSubItem{}
		}
		linkedSessions := linkedSessionsMap[a.ID]
		if linkedSessions == nil {
			linkedSessions = []LinkedAttendanceSession{}
		}
		var legacyLinkedSession *LinkedAttendanceSession
		if len(linkedSessions) > 0 {
			first := linkedSessions[0]
			legacyLinkedSession = &first
		}
		result[i] = AssignmentWithSubItems{
			Assignment:               a,
			SubItems:                 subs,
			LinkedAttendanceSession:  legacyLinkedSession,
			LinkedAttendanceSessions: linkedSessions,
		}
	}
	return result, nil
}

func GetAssignmentWithSubItems(id uint) (*AssignmentWithSubItems, error) {
	var a models.Assignment
	if err := config.DB.First(&a, id).Error; err != nil {
		return nil, err
	}
	var subItems []models.AssignmentSubItem
	config.DB.Where("assignment_id = ?", id).Order("order_index ASC").Find(&subItems)
	if subItems == nil {
		subItems = []models.AssignmentSubItem{}
	}
	linkedSessionsMap, err := loadAssignmentAttendanceLinks([]uint{id})
	if err != nil {
		return nil, err
	}
	linkedSessions := linkedSessionsMap[id]
	if linkedSessions == nil {
		linkedSessions = []LinkedAttendanceSession{}
	}
	var legacyLinkedSession *LinkedAttendanceSession
	if len(linkedSessions) > 0 {
		first := linkedSessions[0]
		legacyLinkedSession = &first
	}
	return &AssignmentWithSubItems{
		Assignment:               a,
		SubItems:                 subItems,
		LinkedAttendanceSession:  legacyLinkedSession,
		LinkedAttendanceSessions: linkedSessions,
	}, nil
}

func CreateAssignment(a *models.Assignment, subItems []models.AssignmentSubItem) error {
	db := config.DB

	// Get max order index
	var maxOrder int
	db.Model(&models.Assignment{}).Where("course_id = ?", a.CourseID).
		Select("COALESCE(MAX(order_index), 0)").Scan(&maxOrder)
	a.OrderIndex = maxOrder + 1
	a.CreatedAt = time.Now()

	if err := db.Create(a).Error; err != nil {
		return err
	}

	if len(subItems) > 0 {
		for i := range subItems {
			subItems[i].AssignmentID = a.ID
			subItems[i].OrderIndex = i + 1
		}
		db.Create(&subItems)
	}
	InvalidateCourseOverviewCache(a.CourseID)
	return nil
}

// SubItemWithScores names a sub-item that is about to be removed together with
// the number of scores that would be destroyed with it.
type SubItemWithScores struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"score_count"`
}

// ErrSubItemsHaveScores reports an update that would drop sub-items students have
// already been graded on. Callers may retry with confirmDeleteScores set.
type ErrSubItemsHaveScores struct {
	Items []SubItemWithScores
}

func (e *ErrSubItemsHaveScores) Error() string {
	return fmt.Sprintf("%d sub-item(s) marked for removal still have scores", len(e.Items))
}

func (e *ErrSubItemsHaveScores) TotalScores() int64 {
	var total int64
	for _, item := range e.Items {
		total += item.Count
	}
	return total
}

// UpdateAssignment saves the assignment and, when subItems is non-nil, reconciles
// its sub-items against the payload.
//
// Sub-items carrying an ID that belongs to this assignment are updated in place.
// That is the whole point: scores reference assignment_sub_items.id and there is
// no foreign key, so re-creating a sub-item under a fresh ID silently detaches
// every score attached to it. Sub-items absent from the payload are removed, but
// only after their scores are accounted for — either there are none, or the
// caller has explicitly confirmed the deletion.
func UpdateAssignment(a *models.Assignment, subItems *[]models.AssignmentSubItem, confirmDeleteScores bool) error {
	// Invalidate after the transaction so a concurrent read cannot repopulate
	// the cache from uncommitted state.
	defer InvalidateCourseOverviewCache(a.CourseID)

	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(a).Error; err != nil {
			return err
		}
		if subItems == nil {
			return nil
		}
		return syncAssignmentSubItemsTx(tx, a.ID, *subItems, confirmDeleteScores)
	})
}

func syncAssignmentSubItemsTx(tx *gorm.DB, assignmentID uint, payload []models.AssignmentSubItem, confirmDeleteScores bool) error {
	var existing []models.AssignmentSubItem
	if err := tx.Where("assignment_id = ?", assignmentID).Find(&existing).Error; err != nil {
		return err
	}
	existingByID := make(map[uint]models.AssignmentSubItem, len(existing))
	for _, item := range existing {
		existingByID[item.ID] = item
	}

	// An ID is honored only when it already belongs to this assignment, so a stale
	// or hostile client cannot graft another assignment's sub-item onto this one.
	// A repeated ID is honored once; the duplicate falls through to insert.
	keep := make(map[uint]bool, len(payload))
	for i := range payload {
		id := payload[i].ID
		if id == 0 || keep[id] {
			continue
		}
		if _, ok := existingByID[id]; ok {
			keep[id] = true
		}
	}

	removedIDs := make([]uint, 0, len(existing))
	for _, item := range existing {
		if !keep[item.ID] {
			removedIDs = append(removedIDs, item.ID)
		}
	}

	if len(removedIDs) > 0 {
		blocking, err := subItemsWithScoresTx(tx, removedIDs, existingByID)
		if err != nil {
			return err
		}
		if len(blocking) > 0 && !confirmDeleteScores {
			return &ErrSubItemsHaveScores{Items: blocking}
		}
		// Scores go first: a sub-item must never outlive its scores, which is the
		// orphaning this whole function exists to prevent.
		if err := tx.Where("sub_item_id IN ?", removedIDs).Delete(&models.Score{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id IN ?", removedIDs).Delete(&models.AssignmentSubItem{}).Error; err != nil {
			return err
		}
	}

	now := time.Now()
	applied := make(map[uint]bool, len(payload))
	for i := range payload {
		item := &payload[i]
		item.AssignmentID = assignmentID
		item.OrderIndex = i + 1

		if item.ID != 0 && keep[item.ID] && !applied[item.ID] {
			applied[item.ID] = true
			if err := tx.Model(&models.AssignmentSubItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
				"name":        item.Name,
				"max_score":   item.MaxScore,
				"order_index": item.OrderIndex,
				"updated_at":  now,
			}).Error; err != nil {
				return err
			}
			continue
		}

		// Insert through a map: max_score is `default:10`, and GORM substitutes the
		// default for a zero value on a struct create.
		item.ID = 0
		if err := tx.Model(&models.AssignmentSubItem{}).Create(map[string]interface{}{
			"assignment_id": assignmentID,
			"name":          item.Name,
			"max_score":     item.MaxScore,
			"order_index":   item.OrderIndex,
			"created_at":    now,
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func subItemsWithScoresTx(tx *gorm.DB, subItemIDs []uint, existingByID map[uint]models.AssignmentSubItem) ([]SubItemWithScores, error) {
	var rows []struct {
		SubItemID uint  `gorm:"column:sub_item_id"`
		Total     int64 `gorm:"column:total"`
	}
	if err := tx.Model(&models.Score{}).
		Select("sub_item_id, COUNT(*) AS total").
		Where("sub_item_id IN ?", subItemIDs).
		Group("sub_item_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]SubItemWithScores, 0, len(rows))
	for _, row := range rows {
		result = append(result, SubItemWithScores{
			ID:    row.SubItemID,
			Name:  existingByID[row.SubItemID].Name,
			Count: row.Total,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func SoftDeleteAssignment(id uint) error {
	if err := config.DB.Model(&models.Assignment{}).Where("id = ?", id).Update("is_active", false).Error; err != nil {
		return err
	}
	InvalidateCourseOverviewCacheByAssignment(id)
	return nil
}

func ReorderAssignments(courseID string, orderedIDs []uint) error {
	db := config.DB
	for i, id := range orderedIDs {
		db.Model(&models.Assignment{}).Where("id = ? AND course_id = ?", id, courseID).
			Update("order_index", i+1)
	}
	return nil
}

// linkAttendanceSessions stores many-to-many links between assignment and attendance sessions
func LinkAttendanceSessions(assignmentID uint, sessionIDs []uint) error {
	db := config.DB
	db.Where("assignment_id = ?", assignmentID).Delete(&models.AssignmentAttendanceLink{})
	if len(sessionIDs) == 0 {
		return nil
	}
	links := make([]models.AssignmentAttendanceLink, len(sessionIDs))
	for i, sid := range sessionIDs {
		links[i] = models.AssignmentAttendanceLink{
			AssignmentID:        assignmentID,
			AttendanceSessionID: sid,
			CreatedAt:           time.Now(),
		}
	}
	return db.Create(&links).Error
}

func loadAssignmentAttendanceLinks(assignmentIDs []uint) (map[uint][]LinkedAttendanceSession, error) {
	result := make(map[uint][]LinkedAttendanceSession, len(assignmentIDs))
	if len(assignmentIDs) == 0 {
		return result, nil
	}

	type assignmentAttendanceRow struct {
		AssignmentID    uint      `gorm:"column:assignment_id"`
		ID              uint      `gorm:"column:id"`
		Title           string    `gorm:"column:title"`
		StartTime       time.Time `gorm:"column:start_time"`
		EndTime         time.Time `gorm:"column:end_time"`
		SessionType     string    `gorm:"column:session_type"`
		CourseSectionID *uint     `gorm:"column:course_section_id"`
	}

	var rows []assignmentAttendanceRow
	err := config.DB.
		Table("assignment_attendance_links AS aal").
		Select(`
			aal.assignment_id,
			s.id,
			s.title,
			s.start_time,
			s.end_time,
			s.session_type,
			s.course_section_id
		`).
		Joins("JOIN attendance_sessions AS s ON s.id = aal.attendance_session_id").
		Where("aal.assignment_id IN ?", assignmentIDs).
		Order("aal.assignment_id ASC, s.start_time ASC, s.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		result[row.AssignmentID] = append(result[row.AssignmentID], LinkedAttendanceSession{
			ID:              row.ID,
			Title:           row.Title,
			StartTime:       row.StartTime,
			EndTime:         row.EndTime,
			SessionType:     row.SessionType,
			CourseSectionID: row.CourseSectionID,
		})
	}

	return result, nil
}
