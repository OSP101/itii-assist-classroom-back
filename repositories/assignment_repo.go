package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"
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
	return nil
}

func UpdateAssignment(a *models.Assignment, subItems *[]models.AssignmentSubItem) error {
	db := config.DB
	if err := db.Save(a).Error; err != nil {
		return err
	}

	if subItems != nil {
		// Replace sub-items
		db.Where("assignment_id = ?", a.ID).Delete(&models.AssignmentSubItem{})
		if len(*subItems) > 0 {
			for i := range *subItems {
				(*subItems)[i].AssignmentID = a.ID
				(*subItems)[i].OrderIndex = i + 1
			}
			db.Create(subItems)
		}
	}
	return nil
}

func SoftDeleteAssignment(id uint) error {
	return config.DB.Model(&models.Assignment{}).Where("id = ?", id).Update("is_active", false).Error
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
