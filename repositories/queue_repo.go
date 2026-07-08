package repositories

import (
	"crypto/rand"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	queueOfferTimeoutSeconds      = 30 * time.Second
	queueOfferPauseDuration       = 1 * time.Minute
	queueOfferReassignGraceWindow = 90 * time.Second
	queueOfferPauseThreshold      = 3
	// Fairness gate for AssignNextWaitingBookingToWorker: if another eligible,
	// free worker currently has notably less completed work, a higher-load
	// worker's own request is briefly declined (never redirected) so the
	// less-loaded worker gets a chance to pick up the same booking on their
	// next poll. Bounded by queueFairnessGraceWindow so a booking can never
	// be stuck waiting on a stale/inactive worker - after the grace window it
	// is assigned to whoever asks, same as before.
	queueFairnessLoadMargin  = 2
	queueFairnessGraceWindow = 20 * time.Second
)

// ---------- helpers ----------

func generateQueuePIN() (string, error) {
	b := make([]byte, 3)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	n := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

// generateUniqueQueuePIN generates a PIN that does not collide with any
// currently active or paused queue session. Retries up to 20 times.
func generateUniqueQueuePIN() (string, error) {
	for i := 0; i < 20; i++ {
		pin, err := generateQueuePIN()
		if err != nil {
			return "", err
		}
		var count int64
		config.DB.Model(&models.QueueSession{}).
			Where("pin_code = ? AND status IN ('active','paused')", pin).
			Count(&count)
		if count == 0 {
			return pin, nil
		}
	}
	// Fallback: return a plain random PIN (collision chance is negligible)
	return generateQueuePIN()
}

// generateUniqueGroupPIN generates a PIN that does not collide with any active
// individual PIN (pin_code) or existing group_pin_code.
func generateUniqueGroupPIN() (string, error) {
	for i := 0; i < 20; i++ {
		pin, err := generateQueuePIN()
		if err != nil {
			return "", err
		}
		var count int64
		config.DB.Model(&models.QueueSession{}).
			Where("(pin_code = ? AND status IN ('active','paused')) OR (group_pin_code = ? AND status IN ('active','paused'))", pin, pin).
			Count(&count)
		if count == 0 {
			return pin, nil
		}
	}
	return "", fmt.Errorf("ไม่สามารถสร้าง group PIN ที่ไม่ซ้ำกันได้")
}

// GetSessionsByGroupPIN returns all active/paused sessions sharing a given group_pin_code.
func GetSessionsByGroupPIN(groupPin string) ([]models.QueueSession, error) {
	var sessions []models.QueueSession
	err := config.DB.
		Where("group_pin_code = ? AND status IN ('active','paused')", groupPin).
		Find(&sessions).Error
	return sessions, err
}

// ---------- ClassroomConflictError ----------

// ClassroomConflictError is returned when a classroom already has an active
// queue session belonging to a different (or the same) course.
type ClassroomConflictError struct {
	SessionID    string
	SessionTitle string
	CourseID     string
	CourseName   string
	StartedAt    *time.Time
}

func (e *ClassroomConflictError) Error() string {
	return fmt.Sprintf("ห้องนี้กำลังถูกใช้งานโดยคิว '%s' ของวิชา '%s'", e.SessionTitle, e.CourseName)
}

// CheckActiveQueueSessionForClassroom returns a *ClassroomConflictError when
// any queue session with status='active' exists for classroomID, excluding
// the session identified by excludeSessionID (the one being started/resumed).
// Returns nil when the classroom is free.
func CheckActiveQueueSessionForClassroom(classroomID, excludeSessionID, excludeCourseID string) error {
	if classroomID == "" {
		return nil
	}
	type conflictRow struct {
		SessionID    string     `gorm:"column:session_id"`
		SessionTitle string     `gorm:"column:session_title"`
		CourseID     string     `gorm:"column:course_id"`
		CourseName   string     `gorm:"column:course_name"`
		StartedAt    *time.Time `gorm:"column:started_at"`
	}
	var row conflictRow
	q := config.DB.
		Table("queue_sessions qs").
		Select("qs.id AS session_id, qs.title AS session_title, qs.course_id AS course_id, c.name AS course_name, qs.start_time AS started_at").
		Joins("JOIN courses c ON c.id = qs.course_id").
		Where("qs.classroom_id = ? AND qs.status = 'active'", classroomID)
	if excludeSessionID != "" {
		q = q.Where("qs.id != ?", excludeSessionID)
	}
	if excludeCourseID != "" {
		q = q.Where("qs.course_id != ?", excludeCourseID)
	}
	err := q.Limit(1).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.SessionID == "" {
		return nil
	}
	return &ClassroomConflictError{
		SessionID:    row.SessionID,
		SessionTitle: row.SessionTitle,
		CourseID:     row.CourseID,
		CourseName:   row.CourseName,
		StartedAt:    row.StartedAt,
	}
}

// GetConcurrentSessionIDs returns all session IDs sharing the same concurrent_group_id
// as sessionID (including sessionID itself). Returns just [sessionID] if no group.
func GetConcurrentSessionIDs(sessionID string) ([]string, error) {
	var s models.QueueSession
	if err := config.DB.Select("concurrent_group_id").Where("id = ?", sessionID).First(&s).Error; err != nil {
		return []string{sessionID}, nil
	}
	if s.ConcurrentGroupID == nil || *s.ConcurrentGroupID == "" {
		return []string{sessionID}, nil
	}
	var ids []string
	if err := config.DB.Model(&models.QueueSession{}).
		Where("concurrent_group_id = ?", *s.ConcurrentGroupID).
		Pluck("id", &ids).Error; err != nil {
		return []string{sessionID}, nil
	}
	return ids, nil
}

// LinkConcurrentSessions links two queue sessions as concurrent (same classroom required).
// Both sessions must belong to different courses. At most 2 sessions per group.
func LinkConcurrentSessions(sessionID1, sessionID2 string) error {
	if sessionID1 == sessionID2 {
		return fmt.Errorf("ไม่สามารถเชื่อมคิวกับตัวเอง")
	}
	var s1, s2 models.QueueSession
	if err := config.DB.Where("id = ?", sessionID1).First(&s1).Error; err != nil {
		return fmt.Errorf("ไม่พบ queue session: %s", sessionID1)
	}
	if err := config.DB.Where("id = ?", sessionID2).First(&s2).Error; err != nil {
		return fmt.Errorf("ไม่พบ queue session: %s", sessionID2)
	}
	if s1.ClassroomID != s2.ClassroomID {
		return fmt.Errorf("ต้องเป็นห้องเรียนเดียวกัน")
	}
	if s1.CourseID == s2.CourseID {
		return fmt.Errorf("ต้องเป็นคนละวิชา")
	}
	// Validate same instructor
	var c1, c2 models.Course
	if err := config.DB.Select("instructor_id").Where("id = ?", s1.CourseID).First(&c1).Error; err != nil {
		return fmt.Errorf("ไม่พบข้อมูลวิชา")
	}
	if err := config.DB.Select("instructor_id").Where("id = ?", s2.CourseID).First(&c2).Error; err != nil {
		return fmt.Errorf("ไม่พบข้อมูลวิชา")
	}
	if c1.InstructorID == nil || c2.InstructorID == nil || *c1.InstructorID != *c2.InstructorID {
		return fmt.Errorf("สามารถเชื่อมคิวร่วมได้เฉพาะวิชาที่สอนโดยอาจารย์คนเดียวกันเท่านั้น")
	}
	// Already linked together
	if s1.ConcurrentGroupID != nil && s2.ConcurrentGroupID != nil && *s1.ConcurrentGroupID == *s2.ConcurrentGroupID {
		return nil
	}
	// Reject if either already belongs to a different group
	if s1.ConcurrentGroupID != nil {
		return fmt.Errorf("session %s อยู่ในกลุ่มคิวอื่นแล้ว ถอดออกก่อน", sessionID1)
	}
	if s2.ConcurrentGroupID != nil {
		return fmt.Errorf("session %s อยู่ในกลุ่มคิวอื่นแล้ว ถอดออกก่อน", sessionID2)
	}
	groupID, err := utils.GenerateNanoID(21)
	if err != nil {
		return err
	}
	groupPIN, err := generateUniqueGroupPIN()
	if err != nil {
		return err
	}
	return config.DB.Model(&models.QueueSession{}).
		Where("id IN ?", []string{sessionID1, sessionID2}).
		Updates(map[string]interface{}{
			"concurrent_group_id": groupID,
			"group_pin_code":      groupPIN,
		}).Error
}

// UnlinkConcurrentSession removes sessionID from its concurrent group.
// If the group would be left with only one member, that member is also unlinked.
func UnlinkConcurrentSession(sessionID string) error {
	var s models.QueueSession
	if err := config.DB.Where("id = ?", sessionID).First(&s).Error; err != nil {
		return err
	}
	if s.ConcurrentGroupID == nil {
		return nil // already not in a group
	}
	groupID := *s.ConcurrentGroupID
	// Clear entire group (max 2 sessions supported)
	return config.DB.Model(&models.QueueSession{}).
		Where("concurrent_group_id = ?", groupID).
		Updates(map[string]interface{}{
			"concurrent_group_id": nil,
			"group_pin_code":      nil,
		}).Error
}

// ---------- QueueSession ----------

type QueueSessionStats struct {
	Total      int `json:"total"`
	Waiting    int `json:"waiting"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
}

type QueueSessionClassroomBasic struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Building string `json:"building"`
	Floor    string `json:"floor"`
}

type QueueSessionAssignmentBasic struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	MaxScore string `json:"max_score"`
}

type QueueSessionAttendanceBasic struct {
	ID    uint   `json:"id"`
	Title string `json:"title"`
}

type QueueSessionCreatorBasic struct {
	ID       uint   `json:"id"`
	FullName string `json:"full_name"`
}

type QueueSessionConcurrentPartner struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	CourseID     string  `json:"course_id"`
	CourseName   string  `json:"course_name"`
	Status       string  `json:"status"`
	GroupPinCode *string `json:"group_pin_code,omitempty"`
}

type QueueSessionListItem struct {
	QueueSession            models.QueueSession            `json:"-"`
	Classroom               *QueueSessionClassroomBasic    `json:"classroom"`
	LinkedAssignment        *QueueSessionAssignmentBasic   `json:"linkedAssignment"`
	LinkedAttendanceSession *QueueSessionAttendanceBasic   `json:"linkedAttendanceSession"`
	Creator                 *QueueSessionCreatorBasic      `json:"creator"`
	Stats                   QueueSessionStats              `json:"stats"`
	ConcurrentPartner       *QueueSessionConcurrentPartner `json:"concurrent_partner,omitempty"`
}

type QueueBookingSubItemScoreInput struct {
	SubItemID uint
	Score     float64
}

func GetQueueSessions(courseID string, status string) ([]QueueSessionListItem, error) {
	var sessions []models.QueueSession
	query := config.DB.Where("course_id = ?", courseID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Order("created_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return []QueueSessionListItem{}, nil
	}

	sessionIDs := make([]string, 0, len(sessions))
	classroomIDsMap := map[string]struct{}{}
	assignmentIDsMap := map[uint]struct{}{}
	attendanceIDsMap := map[uint]struct{}{}
	creatorIDsMap := map[uint]struct{}{}
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.ID)
		classroomIDsMap[session.ClassroomID] = struct{}{}
		if session.LinkedAssignmentID != nil {
			assignmentIDsMap[*session.LinkedAssignmentID] = struct{}{}
		}
		if session.LinkedAttendanceSessionID != nil {
			attendanceIDsMap[*session.LinkedAttendanceSessionID] = struct{}{}
		}
		if session.CreatedBy != nil {
			creatorIDsMap[*session.CreatedBy] = struct{}{}
		}
	}

	classroomIDs := make([]string, 0, len(classroomIDsMap))
	for classroomID := range classroomIDsMap {
		classroomIDs = append(classroomIDs, classroomID)
	}
	assignmentIDs := make([]uint, 0, len(assignmentIDsMap))
	for assignmentID := range assignmentIDsMap {
		assignmentIDs = append(assignmentIDs, assignmentID)
	}
	attendanceIDs := make([]uint, 0, len(attendanceIDsMap))
	for attendanceID := range attendanceIDsMap {
		attendanceIDs = append(attendanceIDs, attendanceID)
	}
	creatorIDs := make([]uint, 0, len(creatorIDsMap))
	for creatorID := range creatorIDsMap {
		creatorIDs = append(creatorIDs, creatorID)
	}

	classroomMap := map[string]*QueueSessionClassroomBasic{}
	if len(classroomIDs) > 0 {
		var classrooms []models.Classroom
		if err := config.DB.Select("id", "name", "building", "floor").Where("id IN ?", classroomIDs).Find(&classrooms).Error; err != nil {
			return nil, err
		}
		for _, classroom := range classrooms {
			entry := QueueSessionClassroomBasic{
				ID:       classroom.ID,
				Name:     classroom.Name,
				Building: classroom.Building,
				Floor:    classroom.Floor,
			}
			classroomMap[classroom.ID] = &entry
		}
	}

	assignmentMap := map[uint]*QueueSessionAssignmentBasic{}
	if len(assignmentIDs) > 0 {
		var assignments []models.Assignment
		if err := config.DB.Select("id", "name", "max_score").Where("id IN ?", assignmentIDs).Find(&assignments).Error; err != nil {
			return nil, err
		}
		for _, assignment := range assignments {
			entry := QueueSessionAssignmentBasic{
				ID:       assignment.ID,
				Name:     assignment.Name,
				MaxScore: fmt.Sprintf("%.2f", assignment.MaxScore),
			}
			assignmentMap[assignment.ID] = &entry
		}
	}

	attendanceMap := map[uint]*QueueSessionAttendanceBasic{}
	if len(attendanceIDs) > 0 {
		var attendances []models.AttendanceSession
		if err := config.DB.Select("id", "title").Where("id IN ?", attendanceIDs).Find(&attendances).Error; err != nil {
			return nil, err
		}
		for _, attendance := range attendances {
			entry := QueueSessionAttendanceBasic{
				ID:    attendance.ID,
				Title: attendance.Title,
			}
			attendanceMap[attendance.ID] = &entry
		}
	}

	creatorMap := map[uint]*QueueSessionCreatorBasic{}
	if len(creatorIDs) > 0 {
		var users []models.User
		if err := config.DB.Select("id", "full_name").Where("id IN ?", creatorIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			entry := QueueSessionCreatorBasic{
				ID:       user.ID,
				FullName: user.FullName,
			}
			creatorMap[user.ID] = &entry
		}
	}

	type statRow struct {
		QueueSessionID string `gorm:"column:queue_session_id"`
		Status         string `gorm:"column:status"`
		Count          int    `gorm:"column:count"`
	}
	statsMap := map[string]QueueSessionStats{}
	var statRows []statRow
	if err := config.DB.Raw(`
		SELECT queue_session_id, status, COUNT(id) AS count
		FROM queue_bookings
		WHERE queue_session_id IN ?
		GROUP BY queue_session_id, status
	`, sessionIDs).Scan(&statRows).Error; err != nil {
		return nil, err
	}
	for _, row := range statRows {
		stats := statsMap[row.QueueSessionID]
		stats.Total += row.Count
		switch row.Status {
		case "waiting":
			stats.Waiting = row.Count
		case "in_progress":
			stats.InProgress = row.Count
		case "completed":
			stats.Completed = row.Count
		}
		statsMap[row.QueueSessionID] = stats
	}

	// Build concurrent partner map: group_id → partner session info
	concurrentPartnerMap := map[string]*QueueSessionConcurrentPartner{}
	type partnerRow struct {
		ConcurrentGroupID string  `gorm:"column:concurrent_group_id"`
		ID                string  `gorm:"column:id"`
		Title             string  `gorm:"column:title"`
		CourseID          string  `gorm:"column:course_id"`
		CourseName        string  `gorm:"column:course_name"`
		Status            string  `gorm:"column:status"`
		GroupPinCode      *string `gorm:"column:group_pin_code"`
	}
	groupIDsMap := map[string]struct{}{}
	for _, s := range sessions {
		if s.ConcurrentGroupID != nil {
			groupIDsMap[*s.ConcurrentGroupID] = struct{}{}
		}
	}
	if len(groupIDsMap) > 0 {
		groupIDs := make([]string, 0, len(groupIDsMap))
		for gid := range groupIDsMap {
			groupIDs = append(groupIDs, gid)
		}
		var partnerRows []partnerRow
		config.DB.Table("queue_sessions qs").
			Select("qs.concurrent_group_id, qs.id, qs.title, qs.course_id, c.name AS course_name, qs.status, qs.group_pin_code").
			Joins("JOIN courses c ON c.id = qs.course_id").
			Where("qs.concurrent_group_id IN ? AND qs.id NOT IN ?", groupIDs, sessionIDs).
			Scan(&partnerRows)
		for _, row := range partnerRows {
			p := row
			concurrentPartnerMap[p.ConcurrentGroupID] = &QueueSessionConcurrentPartner{
				ID:           p.ID,
				Title:        p.Title,
				CourseID:     p.CourseID,
				CourseName:   p.CourseName,
				Status:       p.Status,
				GroupPinCode: p.GroupPinCode,
			}
		}
	}

	result := make([]QueueSessionListItem, len(sessions))
	for i, session := range sessions {
		var linkedAssignment *QueueSessionAssignmentBasic
		if session.LinkedAssignmentID != nil {
			linkedAssignment = assignmentMap[*session.LinkedAssignmentID]
		}
		var linkedAttendance *QueueSessionAttendanceBasic
		if session.LinkedAttendanceSessionID != nil {
			linkedAttendance = attendanceMap[*session.LinkedAttendanceSessionID]
		}
		var creator *QueueSessionCreatorBasic
		if session.CreatedBy != nil {
			creator = creatorMap[*session.CreatedBy]
		}
		var concurrentPartner *QueueSessionConcurrentPartner
		if session.ConcurrentGroupID != nil {
			concurrentPartner = concurrentPartnerMap[*session.ConcurrentGroupID]
		}
		result[i] = QueueSessionListItem{
			QueueSession:            session,
			Classroom:               classroomMap[session.ClassroomID],
			LinkedAssignment:        linkedAssignment,
			LinkedAttendanceSession: linkedAttendance,
			Creator:                 creator,
			Stats:                   statsMap[session.ID],
			ConcurrentPartner:       concurrentPartner,
		}
	}

	return result, nil
}

func GetQueueSessionByID(id string) (*models.QueueSession, error) {
	var s models.QueueSession
	if err := config.DB.First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func CreateQueueSession(s *models.QueueSession) error {
	id, err := utils.GenerateNanoID(21)
	if err != nil {
		return err
	}
	s.ID = id
	pin, err := generateUniqueQueuePIN()
	if err != nil {
		return err
	}
	s.PinCode = pin
	s.NextQueueNumber = 1
	s.Status = "draft"
	return config.DB.Create(s).Error
}

func UpdateQueueSession(s *models.QueueSession) error {
	return config.DB.Save(s).Error
}

func DeleteQueueSession(id string) error {
	return config.DB.Delete(&models.QueueSession{}, "id = ?", id).Error
}

func StartQueueSession(id string, classroomID string) error {
	db := config.DB
	now := time.Now()
	newPIN, err := generateUniqueQueuePIN()
	if err != nil {
		return err
	}
	if err := db.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":                 "active",
		"start_time":             now,
		"pin_code":               newPIN,
		"projector_last_seen_at": now,
	}).Error; err != nil {
		return err
	}

	// Create desk status records for all enabled desks in classroom
	var desks []models.Desk
	if err := db.Where("classroom_id = ? AND is_enabled = true", classroomID).Find(&desks).Error; err != nil {
		return err
	}
	for _, desk := range desks {
		ds := models.QueueDeskStatus{
			QueueSessionID: id,
			DeskID:         desk.ID,
			GradingStatus:  "not_started",
			HelpStatus:     "none",
		}
		db.Where("queue_session_id = ? AND desk_id = ?", id, desk.ID).FirstOrCreate(&ds)
	}
	return nil
}

func PauseQueueSession(id string) error {
	now := time.Now()
	return config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":                 "paused",
		"projector_last_seen_at": now,
	}).Error
}

func CloseQueueSession(id string) error {
	now := time.Now()
	return config.DB.Transaction(func(tx *gorm.DB) error {
		// On close, remove from concurrent group so the partner session runs independently.
		var closing models.QueueSession
		if err := tx.Select("concurrent_group_id").Where("id = ?", id).First(&closing).Error; err == nil && closing.ConcurrentGroupID != nil {
			tx.Model(&models.QueueSession{}).Where("concurrent_group_id = ?", *closing.ConcurrentGroupID).Updates(map[string]interface{}{
				"concurrent_group_id": nil,
				"group_pin_code":      nil,
			})
		}

		if err := tx.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":                 "closed",
			"end_time":               now,
			"projector_last_seen_at": nil,
		}).Error; err != nil {
			return err
		}

		return forceWorkersOfflineBySession(tx, id, now)
	})
}

func forceWorkersOfflineBySession(tx *gorm.DB, sessionID string, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("tx is nil")
	}

	return tx.Model(&models.QueueWorker{}).
		Where("queue_session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"status":                     "offline",
			"current_booking_id":         nil,
			"offer_paused_until":         nil,
			"consecutive_offer_timeouts": 0,
			"last_active_at":             now,
		}).Error
}

func ResumeQueueSession(id string) error {
	newPIN, err := generateUniqueQueuePIN()
	if err != nil {
		return err
	}
	now := time.Now()
	return config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":                 "active",
		"pin_code":               newPIN,
		"projector_last_seen_at": now,
	}).Error
}

func TouchQueueSessionProjectorHeartbeat(id string) (*models.QueueSession, error) {
	var session models.QueueSession
	if err := config.DB.First(&session, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if session.Status == "closed" {
		return &session, gorm.ErrRecordNotFound
	}
	if session.Status != "active" && session.Status != "paused" {
		return &session, nil
	}

	now := time.Now()
	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Update("projector_last_seen_at", now).Error; err != nil {
		return nil, err
	}
	session.ProjectorLastSeenAt = &now
	return &session, nil
}

// ---------- QueueBooking ----------

type BookingInput struct {
	StudentID   uint
	DeskID      string
	DeskNumber  int
	BookingType string // grading | help
	Note        string
	BookingIP   string
	UserAgent   string
	Device      string
	IsLate      bool
	LateReason  string
}

func CreateBooking(sessionID string, input BookingInput) (*models.QueueBooking, error) {
	var booking *models.QueueBooking
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		queueNumber, err := reserveNextQueueNumber(tx, sessionID)
		if err != nil {
			return err
		}

		booking = &models.QueueBooking{
			QueueSessionID:   sessionID,
			StudentID:        input.StudentID,
			DeskID:           input.DeskID,
			DeskNumber:       input.DeskNumber,
			BookingType:      input.BookingType,
			QueueNumber:      queueNumber,
			Note:             input.Note,
			BookingIP:        input.BookingIP,
			BookingUserAgent: input.UserAgent,
			BookingDevice:    input.Device,
			IsLateBooking:    input.IsLate,
			LateReason:       input.LateReason,
			Status:           "waiting",
		}
		if err := tx.Create(booking).Error; err != nil {
			if isActiveQueueBookingConflict(err) {
				return fmt.Errorf("student already has an active booking")
			}
			return err
		}

		// Help bookings can queue up on the same desk, so avoid rewriting the same desk-status row
		// for every new waiting request. Grading still needs explicit single-booking desk state.
		if input.BookingType == "grading" {
			if err := updateDeskStatus(tx, sessionID, input.DeskID, input.BookingType, "waiting", booking.ID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return booking, nil
}

func reserveNextQueueNumber(tx *gorm.DB, sessionID string) (int, error) {
	type queueNumberReservation struct {
		QueueNumber int `gorm:"column:queue_number"`
	}

	var reservation queueNumberReservation
	result := tx.Raw(`
		UPDATE queue_sessions
		SET next_queue_number = GREATEST(next_queue_number, 1) + 1
		WHERE id = ? AND status = 'active'
		RETURNING GREATEST(next_queue_number - 1, 1) AS queue_number
	`, sessionID).Scan(&reservation)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		return reservation.QueueNumber, nil
	}

	var session models.QueueSession
	if err := tx.Select("id", "status").First(&session, "id = ?", sessionID).Error; err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("session is not active")
}

func isActiveQueueBookingConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == "uq_queue_bookings_session_student_active"
}

func updateDeskStatus(db *gorm.DB, sessionID, deskID, bookingType, status string, bookingID uint) error {
	updates := map[string]interface{}{}
	if bookingType == "grading" {
		updates["grading_status"] = status
		if bookingID > 0 {
			updates["grading_booking_id"] = bookingID
		} else {
			updates["grading_booking_id"] = nil
		}
	} else {
		updates["help_status"] = status
		if bookingID > 0 {
			updates["help_booking_id"] = bookingID
		} else {
			updates["help_booking_id"] = nil
		}
	}
	return db.Model(&models.QueueDeskStatus{}).Where("queue_session_id = ? AND desk_id = ?", sessionID, deskID).Updates(updates).Error
}

func SyncHelpDeskStatus(sessionID, deskID string) error {
	return syncHelpDeskStatus(config.DB, sessionID, deskID)
}

func syncHelpDeskStatus(db *gorm.DB, sessionID, deskID string) error {
	var activeBooking models.QueueBooking
	err := db.Where("queue_session_id = ? AND desk_id = ? AND booking_type = ? AND status = ?", sessionID, deskID, "help", "in_progress").
		Order("updated_at DESC, id DESC").
		First(&activeBooking).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.Where("queue_session_id = ? AND desk_id = ? AND booking_type = ? AND status = ?", sessionID, deskID, "help", "waiting").
			Order("created_at ASC, id ASC").
			First(&activeBooking).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return updateDeskStatus(db, sessionID, deskID, "help", "none", 0)
	}
	if err != nil {
		return err
	}

	return updateDeskStatus(db, sessionID, deskID, "help", activeBooking.Status, activeBooking.ID)
}

func GetBookingByID(id uint) (*models.QueueBooking, error) {
	var b models.QueueBooking
	if err := config.DB.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func GetBookingsBySession(sessionID string) ([]models.QueueBooking, error) {
	var bookings []models.QueueBooking
	err := config.DB.Where("queue_session_id = ?", sessionID).Order("queue_number ASC").Find(&bookings).Error
	return bookings, err
}

func GetWorkersBySession(sessionID string) ([]models.QueueWorker, error) {
	var workers []models.QueueWorker
	err := config.DB.Where("queue_session_id = ?", sessionID).Order("user_id ASC").Find(&workers).Error
	return workers, err
}

func GetStudentActiveBooking(sessionID string, studentID uint) (*models.QueueBooking, error) {
	var b models.QueueBooking
	err := config.DB.Where("queue_session_id = ? AND student_id = ? AND status IN ('waiting','in_progress')", sessionID, studentID).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func CancelBooking(bookingID uint, studentID uint) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		var b models.QueueBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, bookingID).Error; err != nil {
			return err
		}
		if b.StudentID != studentID {
			return fmt.Errorf("unauthorized")
		}
		if b.Status != "waiting" {
			return fmt.Errorf("cannot cancel booking in status: %s", b.Status)
		}

		now := time.Now()
		updates := map[string]interface{}{
			"status":     "cancelled",
			"updated_at": now,
		}
		if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
			return err
		}

		if b.BookingType == "grading" {
			return updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0)
		}
		return syncHelpDeskStatus(tx, b.QueueSessionID, b.DeskID)
	})
}

func resetDeskStatus(bookingType string) string {
	if bookingType == "grading" {
		return "not_started"
	}
	return "none"
}

func countEligibleOnlineWorkers(tx *gorm.DB, sessionID, bookingType string, now time.Time) (int64, error) {
	query := tx.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND status = ? AND current_booking_id IS NULL AND (offer_paused_until IS NULL OR offer_paused_until <= ?)", sessionID, "online", now)

	if bookingType == "grading" {
		query = query.Where("accept_grading = ?", true)
	} else {
		query = query.Where("accept_help = ?", true)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// minLoadAmongEligibleWorkers returns the lowest completed-task count among workers
// currently free and eligible for the given booking type. Used only as a fairness
// signal in AssignNextWaitingBookingToWorker - never to pick who gets assigned.
func minLoadAmongEligibleWorkers(tx *gorm.DB, sessionID, bookingType string, now time.Time) (int64, error) {
	query := tx.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND status = ? AND current_booking_id IS NULL AND (offer_paused_until IS NULL OR offer_paused_until <= ?)", sessionID, "online", now)

	if bookingType == "grading" {
		query = query.Where("accept_grading = ?", true)
	} else {
		query = query.Where("accept_help = ?", true)
	}

	var minLoad int64
	if err := query.Select("COALESCE(MIN(total_grading_completed + total_help_completed), 0)").Scan(&minLoad).Error; err != nil {
		return 0, err
	}
	return minLoad, nil
}

// ---------- Worker ----------

func WorkerJoin(sessionID string, userID uint, acceptGrading, acceptHelp bool) (*models.QueueWorker, error) {
	var w models.QueueWorker
	err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionID, userID).First(&w).Error
	now := time.Now()
	if err != nil {
		// Create new
		w = models.QueueWorker{
			QueueSessionID:   sessionID,
			UserID:           userID,
			AcceptGrading:    acceptGrading,
			AcceptHelp:       acceptHelp,
			Status:           "online",
			OfferPausedUntil: nil,
			LastActiveAt:     &now,
		}
		return &w, config.DB.Create(&w).Error
	}
	w.Status = "online"
	w.AcceptGrading = acceptGrading
	w.AcceptHelp = acceptHelp
	w.OfferPausedUntil = nil
	w.ConsecutiveOfferTimeouts = 0
	w.LastActiveAt = &now
	return &w, config.DB.Save(&w).Error
}

func GetWorkerBySessionUser(sessionID string, userID uint) (*models.QueueWorker, error) {
	var worker models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionID, userID).First(&worker).Error; err != nil {
		return nil, err
	}
	return &worker, nil
}

func WorkerUpdateStatus(sessionID string, userID uint, status string) error {
	now := time.Now()
	return config.DB.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND user_id = ?", sessionID, userID).
		Updates(map[string]interface{}{"status": status, "last_active_at": now}).Error
}

// WorkerJoinMirrorGroup ensures a QueueWorker row exists for every session in
// the same concurrent group as sessionID. Call after WorkerJoin.
func WorkerJoinMirrorGroup(sessionID string, userID uint, acceptGrading, acceptHelp bool) error {
	groupIDs, err := GetConcurrentSessionIDs(sessionID)
	if err != nil || len(groupIDs) <= 1 {
		return nil
	}
	now := time.Now()
	for _, gid := range groupIDs {
		if gid == sessionID {
			continue
		}
		var w models.QueueWorker
		err := config.DB.Where("queue_session_id = ? AND user_id = ?", gid, userID).First(&w).Error
		if err != nil {
			// Create mirror row
			mirror := models.QueueWorker{
				QueueSessionID:   gid,
				UserID:           userID,
				AcceptGrading:    acceptGrading,
				AcceptHelp:       acceptHelp,
				Status:           "online",
				OfferPausedUntil: nil,
				LastActiveAt:     &now,
			}
			if createErr := config.DB.Create(&mirror).Error; createErr != nil {
				return createErr
			}
		} else {
			w.Status = "online"
			w.AcceptGrading = acceptGrading
			w.AcceptHelp = acceptHelp
			w.OfferPausedUntil = nil
			w.ConsecutiveOfferTimeouts = 0
			w.LastActiveAt = &now
			if saveErr := config.DB.Save(&w).Error; saveErr != nil {
				return saveErr
			}
		}
	}
	return nil
}

// WorkerOfflineMirrorGroup sets offline status for a worker across all sessions in the group.
func WorkerOfflineMirrorGroup(sessionID string, userID uint, newStatus string) {
	groupIDs, err := GetConcurrentSessionIDs(sessionID)
	if err != nil || len(groupIDs) <= 1 {
		return
	}
	now := time.Now()
	for _, gid := range groupIDs {
		if gid == sessionID {
			continue
		}
		config.DB.Model(&models.QueueWorker{}).
			Where("queue_session_id = ? AND user_id = ?", gid, userID).
			Updates(map[string]interface{}{"status": newStatus, "last_active_at": now})
	}
}

func assignBookingOfferToWorker(tx *gorm.DB, booking *models.QueueBooking, worker *models.QueueWorker, now time.Time) error {
	if booking == nil || worker == nil {
		return fmt.Errorf("invalid booking or worker")
	}

	expiresAt := now.Add(queueOfferTimeoutSeconds)
	bookingUpdates := map[string]interface{}{
		"assigned_worker_id":      worker.UserID,
		"assigned_at":             now,
		"offer_expires_at":        expiresAt,
		"started_at":              nil,
		"status":                  "waiting",
		"last_offer_worker_id":    worker.UserID,
		"last_offer_timed_out_at": nil,
		"updated_at":              now,
	}
	if err := tx.Model(&models.QueueBooking{}).Where("id = ?", booking.ID).Updates(bookingUpdates).Error; err != nil {
		return err
	}

	if err := tx.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND user_id = ?", booking.QueueSessionID, worker.UserID).
		Updates(map[string]interface{}{"status": "busy", "current_booking_id": booking.ID, "last_active_at": now}).Error; err != nil {
		return err
	}

	return updateDeskStatus(tx, booking.QueueSessionID, booking.DeskID, booking.BookingType, "waiting", booking.ID)
}

func assignBookingDirectToWorker(tx *gorm.DB, booking *models.QueueBooking, worker *models.QueueWorker, now time.Time) error {
	if booking == nil || worker == nil {
		return fmt.Errorf("invalid booking or worker")
	}

	bookingUpdates := map[string]interface{}{
		"assigned_worker_id":      worker.UserID,
		"assigned_at":             now,
		"offer_expires_at":        nil,
		"started_at":              now,
		"status":                  "in_progress",
		"last_offer_worker_id":    worker.UserID,
		"last_offer_timed_out_at": nil,
		"updated_at":              now,
	}
	if err := tx.Model(&models.QueueBooking{}).Where("id = ?", booking.ID).Updates(bookingUpdates).Error; err != nil {
		return err
	}

	if err := tx.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND user_id = ?", booking.QueueSessionID, worker.UserID).
		Updates(map[string]interface{}{
			"status":                     "busy",
			"current_booking_id":         booking.ID,
			"last_active_at":             now,
			"offer_accept_count":         gorm.Expr("offer_accept_count + 1"),
			"consecutive_offer_timeouts": 0,
		}).Error; err != nil {
		return err
	}

	return updateDeskStatus(tx, booking.QueueSessionID, booking.DeskID, booking.BookingType, "in_progress", booking.ID)
}

func assignTimedOutBookingToOtherWorker(tx *gorm.DB, booking *models.QueueBooking, excludeWorkerID uint, now time.Time) error {
	if booking == nil {
		return nil
	}

	query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("queue_session_id = ? AND user_id <> ? AND status = ? AND current_booking_id IS NULL AND (offer_paused_until IS NULL OR offer_paused_until <= ?)", booking.QueueSessionID, excludeWorkerID, "online", now)
	if booking.BookingType == "grading" {
		query = query.Where("accept_grading = ?", true)
	} else {
		query = query.Where("accept_help = ?", true)
	}

	var candidate models.QueueWorker
	if err := query.Order("(total_grading_completed + total_help_completed) ASC, user_id ASC").First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	return assignBookingOfferToWorker(tx, booking, &candidate, now)
}

// ProcessQueueOfferTimeouts requeues expired offers and immediately tries to route them to another ready worker.
func ProcessQueueOfferTimeouts(sessionID string) (int, error) {
	now := time.Now()
	processedCount := 0

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var expired []models.QueueBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("queue_session_id = ? AND status = ? AND assigned_worker_id IS NOT NULL AND offer_expires_at IS NOT NULL AND offer_expires_at <= ?", sessionID, "waiting", now).
			Order("offer_expires_at ASC, id ASC").
			Find(&expired).Error; err != nil {
			return err
		}

		for _, booking := range expired {
			if booking.AssignedWorkerID == nil {
				continue
			}
			workerID := *booking.AssignedWorkerID

			var worker models.QueueWorker
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("queue_session_id = ? AND user_id = ?", sessionID, workerID).
				First(&worker).Error; err != nil {
				return err
			}

			workerStatus := "online"
			workerUpdates := map[string]interface{}{
				"status":                     workerStatus,
				"current_booking_id":         nil,
				"offer_timeout_count":        gorm.Expr("offer_timeout_count + 1"),
				"consecutive_offer_timeouts": gorm.Expr("consecutive_offer_timeouts + 1"),
				"last_active_at":             now,
			}
			if worker.Status == "paused" {
				workerStatus = "paused"
				workerUpdates["status"] = workerStatus
			}

			if worker.Status != "paused" && worker.ConsecutiveOfferTimeouts+1 >= queueOfferPauseThreshold {
				workerUpdates["status"] = "offline"
				workerUpdates["offer_paused_until"] = now.Add(queueOfferPauseDuration)
				workerUpdates["consecutive_offer_timeouts"] = 0
			}

			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", sessionID, workerID).
				Updates(workerUpdates).Error; err != nil {
				return err
			}

			if err := tx.Model(&models.QueueBooking{}).
				Where("id = ?", booking.ID).
				Updates(map[string]interface{}{
					"assigned_worker_id":      nil,
					"assigned_at":             nil,
					"offer_expires_at":        nil,
					"last_offer_worker_id":    workerID,
					"last_offer_timed_out_at": now,
					"timeout_count":           gorm.Expr("timeout_count + 1"),
					"updated_at":              now,
				}).Error; err != nil {
				return err
			}

			requeued := booking
			requeued.AssignedWorkerID = nil
			requeued.AssignedAt = nil
			requeued.OfferExpiresAt = nil
			requeued.LastOfferWorkerID = &workerID
			timedOutAt := now
			requeued.LastOfferTimedOutAt = &timedOutAt

			if err := assignTimedOutBookingToOtherWorker(tx, &requeued, workerID, now); err != nil {
				return err
			}

			processedCount++
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return processedCount, nil
}

// AssignNextWaitingBookingToWorker attempts to atomically assign the next matching waiting booking
// to the specified worker. It returns (booking, assignedNow, error).
// assignedNow is true only when this call newly assigned a booking.
func AssignNextWaitingBookingToWorker(sessionID string, workerID uint) (*models.QueueBooking, bool, error) {
	if _, err := ProcessQueueOfferTimeouts(sessionID); err != nil {
		return nil, false, err
	}

	var bookingResult models.QueueBooking
	assignedNow := false

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		var worker models.QueueWorker
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("queue_session_id = ? AND user_id = ?", sessionID, workerID).
			First(&worker).Error; err != nil {
			return err
		}

		if worker.OfferPausedUntil != nil && worker.OfferPausedUntil.After(now) {
			return nil
		}
		if worker.OfferPausedUntil != nil && worker.OfferPausedUntil.Before(now) && worker.Status == "offline" {
			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", sessionID, workerID).
				Updates(map[string]interface{}{"status": "online", "offer_paused_until": nil, "consecutive_offer_timeouts": 0, "last_active_at": now}).Error; err != nil {
				return err
			}
			worker.Status = "online"
			worker.OfferPausedUntil = nil
		}

		// Collect all session IDs in the concurrent group (may be just [sessionID])
		groupSessionIDs, _ := GetConcurrentSessionIDs(sessionID)

		// If worker already has an assigned active booking (in any grouped session), return it.
		var existing models.QueueBooking
		existingErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("queue_session_id IN ? AND assigned_worker_id = ? AND status IN ?", groupSessionIDs, workerID, []string{"waiting", "in_progress"}).
			Order("updated_at DESC, id DESC").
			First(&existing).Error
		if existingErr == nil {
			bookingResult = existing
			assignedNow = false
			return nil
		}
		if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}

		if worker.Status != "online" {
			return nil
		}

		bookingTypes := make([]string, 0, 2)
		if worker.AcceptGrading {
			bookingTypes = append(bookingTypes, "grading")
		}
		if worker.AcceptHelp {
			bookingTypes = append(bookingTypes, "help")
		}
		if len(bookingTypes) == 0 {
			return nil
		}

		var waiting models.QueueBooking
		waitingErr := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("queue_session_id IN ? AND status = ? AND assigned_worker_id IS NULL AND booking_type IN ?", groupSessionIDs, "waiting", bookingTypes).
			Where("last_offer_worker_id IS NULL OR last_offer_worker_id <> ? OR last_offer_timed_out_at IS NULL OR last_offer_timed_out_at < ?", workerID, now.Add(-queueOfferReassignGraceWindow)).
			Order("queue_number ASC, created_at ASC, id ASC").
			First(&waiting).Error
		if errors.Is(waitingErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if waitingErr != nil {
			return waitingErr
		}

		eligibleWorkers, err := countEligibleOnlineWorkers(tx, waiting.QueueSessionID, waiting.BookingType, now)
		if err != nil {
			return err
		}

		// Fairness gate: only within a short grace window after the booking
		// started waiting, and only when a genuinely less-loaded free worker
		// exists right now. This never reassigns the booking to anyone else -
		// it only declines *this* worker's own request so a less-loaded worker
		// can claim it on their own next poll. After the grace window elapses,
		// whoever asks gets it, so a booking can never be stuck indefinitely.
		if eligibleWorkers > 1 && now.Sub(waiting.CreatedAt) < queueFairnessGraceWindow {
			minLoad, loadErr := minLoadAmongEligibleWorkers(tx, waiting.QueueSessionID, waiting.BookingType, now)
			if loadErr != nil {
				return loadErr
			}
			workerLoad := int64(worker.TotalGradingCompleted + worker.TotalHelpCompleted)
			if workerLoad-minLoad >= queueFairnessLoadMargin {
				return nil
			}
		}

		// If the booking belongs to a different session in the group, use that session's worker row.
		assignWorker := &worker
		if waiting.QueueSessionID != sessionID {
			var groupWorker models.QueueWorker
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("queue_session_id = ? AND user_id = ?", waiting.QueueSessionID, workerID).
				First(&groupWorker).Error; err != nil {
				// Mirror worker row does not exist yet (edge case); use original
				assignWorker = &worker
			} else {
				assignWorker = &groupWorker
			}
		}

		if eligibleWorkers == 1 {
			err = assignBookingDirectToWorker(tx, &waiting, assignWorker, now)
		} else {
			err = assignBookingOfferToWorker(tx, &waiting, assignWorker, now)
		}
		if err != nil {
			return err
		}

		if err := tx.First(&bookingResult, waiting.ID).Error; err != nil {
			return err
		}

		assignedNow = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	if bookingResult.ID == 0 {
		return nil, false, nil
	}

	return &bookingResult, assignedNow, nil
}

func CompleteBookingWithScores(bookingID uint, workerID uint, score *float64, scoreComment, workerNote string, subItemScores []QueueBookingSubItemScoreInput) (*models.QueueBooking, error) {
	var updated models.QueueBooking

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var b models.QueueBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, bookingID).Error; err != nil {
			return err
		}

		var worker models.QueueWorker
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).First(&worker).Error; err != nil {
			return fmt.Errorf("worker not registered for this queue session")
		}
		if b.Status != "in_progress" {
			return fmt.Errorf("booking is not in progress")
		}
		if b.AssignedWorkerID == nil || *b.AssignedWorkerID != workerID {
			return fmt.Errorf("booking is not assigned to this worker")
		}

		now := time.Now()
		deskStatusAfterComplete := resetDeskStatus(b.BookingType)

		if b.BookingType == "grading" {
			var session models.QueueSession
			if err := tx.Select("id", "linked_assignment_id").Where("id = ?", b.QueueSessionID).First(&session).Error; err != nil {
				return err
			}

			if session.LinkedAssignmentID != nil {
				var subItems []models.AssignmentSubItem
				if err := tx.Where("assignment_id = ?", *session.LinkedAssignmentID).Order("order_index ASC, id ASC").Find(&subItems).Error; err != nil {
					return err
				}

				if len(subItems) > 0 {
					if len(subItemScores) == 0 {
						return fmt.Errorf("at least one sub-item score is required")
					}

					maxScoreBySubItemID := make(map[uint]float64, len(subItems))
					for _, subItem := range subItems {
						maxScoreBySubItemID[subItem.ID] = subItem.MaxScore
					}

					seenSubItemIDs := make(map[uint]struct{}, len(subItemScores))
					subItemIDs := make([]uint, 0, len(subItemScores))
					for _, item := range subItemScores {
						maxScore, ok := maxScoreBySubItemID[item.SubItemID]
						if !ok {
							return fmt.Errorf("invalid sub_item_id %d", item.SubItemID)
						}
						if err := utils.ValidateScoreValue(item.Score, maxScore); err != nil {
							return fmt.Errorf("score for sub_item_id %d: %w", item.SubItemID, err)
						}
						if _, duplicated := seenSubItemIDs[item.SubItemID]; duplicated {
							return fmt.Errorf("duplicate sub_item_id %d", item.SubItemID)
						}
						seenSubItemIDs[item.SubItemID] = struct{}{}
						subItemIDs = append(subItemIDs, item.SubItemID)
					}

					var lockedScores []models.Score
					if err := tx.Where("assignment_id = ? AND student_id = ? AND sub_item_id IN ? AND status = ?", *session.LinkedAssignmentID, b.StudentID, subItemIDs, "graded").Find(&lockedScores).Error; err != nil {
						return err
					}
					if len(lockedScores) > 0 {
						return fmt.Errorf("some sub-items have already been graded")
					}

					for _, item := range subItemScores {
						subItemID := item.SubItemID
						scoreRow := models.Score{
							AssignmentID: *session.LinkedAssignmentID,
							StudentID:    &b.StudentID,
							SubItemID:    &subItemID,
							Score:        item.Score,
							Comment:      scoreComment,
							GradedBy:     &workerID,
							GradedAt:     &now,
							Status:       "graded",
						}
						if err := tx.Create(&scoreRow).Error; err != nil {
							return err
						}
					}

					var gradedSubItemCount int64
					if err := tx.Model(&models.Score{}).
						Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NOT NULL AND status = ?", *session.LinkedAssignmentID, b.StudentID, "graded").
						Distinct("sub_item_id").
						Count(&gradedSubItemCount).Error; err != nil {
						return err
					}
					if gradedSubItemCount == int64(len(subItems)) {
						deskStatusAfterComplete = "completed"
					}
				} else {
					if score == nil {
						return fmt.Errorf("score is required")
					}
					var assignment models.Assignment
					if err := tx.Select("id", "max_score").First(&assignment, *session.LinkedAssignmentID).Error; err != nil {
						return err
					}
					if err := utils.ValidateScoreValue(*score, assignment.MaxScore); err != nil {
						return err
					}

					var existingScore models.Score
					err := tx.Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NULL", *session.LinkedAssignmentID, b.StudentID).First(&existingScore).Error
					if err == nil {
						return fmt.Errorf("score has already been graded")
					}
					if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}

					scoreRow := models.Score{
						AssignmentID: *session.LinkedAssignmentID,
						StudentID:    &b.StudentID,
						Score:        *score,
						Comment:      scoreComment,
						GradedBy:     &workerID,
						GradedAt:     &now,
						Status:       "graded",
					}
					if err := tx.Create(&scoreRow).Error; err != nil {
						return err
					}
					deskStatusAfterComplete = "completed"
				}
			}
		}

		updates := map[string]interface{}{
			"status":        "completed",
			"completed_at":  now,
			"score":         score,
			"score_comment": scoreComment,
			"worker_note":   workerNote,
			"updated_at":    now,
		}
		if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
			return err
		}

		workerUpdates := map[string]interface{}{"current_booking_id": nil, "last_active_at": now}
		if worker.Status == "paused" {
			workerUpdates["status"] = "offline"
		} else {
			workerUpdates["status"] = "online"
		}
		if b.BookingType == "grading" {
			workerUpdates["total_grading_completed"] = gorm.Expr("total_grading_completed + 1")
		} else {
			workerUpdates["total_help_completed"] = gorm.Expr("total_help_completed + 1")
		}
		if err := tx.Model(&models.QueueWorker{}).
			Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
			Updates(workerUpdates).Error; err != nil {
			return err
		}

		if b.BookingType == "grading" {
			gradingBookingRef := uint(0)
			if deskStatusAfterComplete == "completed" {
				gradingBookingRef = b.ID
			}
			if err := updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, deskStatusAfterComplete, gradingBookingRef); err != nil {
				return err
			}
		} else if err := syncHelpDeskStatus(tx, b.QueueSessionID, b.DeskID); err != nil {
			return err
		}

		if err := tx.First(&updated, b.ID).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// Worker starts / completes a booking
func WorkerUpdateBooking(bookingID uint, workerID uint, action string, score *float64, workerNote string) (*models.QueueBooking, error) {
	if action != "start" && action != "complete" && action != "no_show" && action != "reject" {
		return nil, fmt.Errorf("invalid action")
	}

	var updated models.QueueBooking
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var b models.QueueBooking
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&b, bookingID).Error; err != nil {
			return err
		}

		var worker models.QueueWorker
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).First(&worker).Error; err != nil {
			return fmt.Errorf("worker not registered for this queue session")
		}

		now := time.Now()
		needsHelpDeskSync := false

		switch action {
		case "start":
			if b.Status != "waiting" {
				return fmt.Errorf("booking is not waiting")
			}
			if b.AssignedWorkerID == nil || *b.AssignedWorkerID != workerID {
				return fmt.Errorf("booking is not assigned to this worker")
			}
			if b.OfferExpiresAt != nil && now.After(*b.OfferExpiresAt) {
				return fmt.Errorf("offer expired")
			}

			updates := map[string]interface{}{
				"status":             "in_progress",
				"assigned_worker_id": workerID,
				"assigned_at":        now,
				"offer_expires_at":   nil,
				"started_at":         now,
				"updated_at":         now,
			}
			if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
				return err
			}
			if err := updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, "in_progress", b.ID); err != nil {
				return err
			}
			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(map[string]interface{}{"status": "busy", "current_booking_id": b.ID, "last_active_at": now, "offer_accept_count": gorm.Expr("offer_accept_count + 1"), "consecutive_offer_timeouts": 0}).Error; err != nil {
				return err
			}

		case "reject":
			if b.Status != "waiting" {
				return fmt.Errorf("booking is not waiting")
			}
			if b.AssignedWorkerID == nil || *b.AssignedWorkerID != workerID {
				return fmt.Errorf("booking is not assigned to this worker")
			}

			updates := map[string]interface{}{
				"assigned_worker_id":      nil,
				"assigned_at":             nil,
				"offer_expires_at":        nil,
				"last_offer_worker_id":    workerID,
				"last_offer_timed_out_at": nil,
				"reject_count":            gorm.Expr("reject_count + 1"),
				"worker_note":             workerNote,
				"updated_at":              now,
			}
			if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
				return err
			}

			workerUpdates := map[string]interface{}{
				"current_booking_id": nil,
				"last_active_at":     now,
				"offer_reject_count": gorm.Expr("offer_reject_count + 1"),
			}
			if worker.Status == "paused" {
				workerUpdates["status"] = "paused"
			} else {
				workerUpdates["status"] = "online"
			}
			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(workerUpdates).Error; err != nil {
				return err
			}

			if err := updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, "waiting", b.ID); err != nil {
				return err
			}

		case "complete":
			if b.Status != "in_progress" {
				return fmt.Errorf("booking is not in progress")
			}
			if b.AssignedWorkerID == nil || *b.AssignedWorkerID != workerID {
				return fmt.Errorf("booking is not assigned to this worker")
			}

			updates := map[string]interface{}{
				"status":       "completed",
				"completed_at": now,
				"score":        score,
				"worker_note":  workerNote,
				"updated_at":   now,
			}
			if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
				return err
			}

			workerUpdates := map[string]interface{}{"current_booking_id": nil, "last_active_at": now}
			if worker.Status == "paused" {
				workerUpdates["status"] = "offline"
			} else {
				workerUpdates["status"] = "online"
			}
			if b.BookingType == "grading" {
				workerUpdates["total_grading_completed"] = gorm.Expr("total_grading_completed + 1")
			} else {
				workerUpdates["total_help_completed"] = gorm.Expr("total_help_completed + 1")
			}
			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(workerUpdates).Error; err != nil {
				return err
			}

			if b.BookingType == "grading" {
				if err := updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0); err != nil {
					return err
				}
			} else {
				needsHelpDeskSync = true
			}

		case "no_show":
			if b.Status != "in_progress" {
				return fmt.Errorf("booking is not in progress")
			}
			if b.AssignedWorkerID == nil || *b.AssignedWorkerID != workerID {
				return fmt.Errorf("booking is not assigned to this worker")
			}

			updates := map[string]interface{}{
				"status":       "no_show",
				"completed_at": now,
				"worker_note":  workerNote,
				"updated_at":   now,
			}
			if err := tx.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(updates).Error; err != nil {
				return err
			}

			workerUpdates := map[string]interface{}{"current_booking_id": nil, "last_active_at": now}
			if worker.Status == "paused" {
				workerUpdates["status"] = "offline"
			} else {
				workerUpdates["status"] = "online"
			}
			if err := tx.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(workerUpdates).Error; err != nil {
				return err
			}

			if b.BookingType == "grading" {
				if err := updateDeskStatus(tx, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0); err != nil {
					return err
				}
			} else {
				needsHelpDeskSync = true
			}
		}

		if needsHelpDeskSync {
			if err := syncHelpDeskStatus(tx, b.QueueSessionID, b.DeskID); err != nil {
				return err
			}
		}

		if err := tx.First(&updated, b.ID).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

// VerifySessionPIN checks if PIN matches an active session
func VerifySessionPIN(sessionID string, pin string) bool {
	var s models.QueueSession
	err := config.DB.First(&s, "id = ?", sessionID).Error
	return err == nil && s.Status == "active" && s.PinCode == pin
}

func RegenerateQueueSessionPIN(sessionID string) (string, error) {
	pin, err := generateUniqueQueuePIN()
	if err != nil {
		return "", err
	}

	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", sessionID).Update("pin_code", pin).Error; err != nil {
		return "", err
	}

	return pin, nil
}

// ---------- Auto-close stale sessions ----------

// AutoClosedQueueSession is returned by AutoCloseStaleQueueSessions for each
// session that was force-closed so the caller can broadcast realtime events.
type AutoClosedQueueSession struct {
	SessionID        string
	CourseID         string
	CancelledWaiting int // number of waiting bookings cancelled
}

// AutoCloseStaleQueueSessions closes every queue session whose status is
// 'active' or 'paused' and whose start_time is on a calendar day BEFORE
// today (in Asia/Bangkok time).
//
// Safety rules:
//   - Bookings with status='waiting'   → cancelled  (was in the queue but queue never processed them)
//   - Bookings with status='in_progress' → left alone (TA is currently reviewing; they finish naturally)
//   - Session status → 'closed', end_time = now
func AutoCloseStaleQueueSessions(loc *time.Location) ([]AutoClosedQueueSession, error) {
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	// Midnight of today in the given timezone expressed as UTC for the DB query
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// Find stale sessions: started before today and still open
	var sessions []models.QueueSession
	if err := config.DB.
		Where("status IN ('active','paused') AND start_time < ?", todayMidnight).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	var results []AutoClosedQueueSession
	endTime := now.UTC()

	for _, s := range sessions {
		cancelled := 0
		err := config.DB.Transaction(func(tx *gorm.DB) error {
			// Cancel waiting bookings (not in_progress – those finish naturally)
			res := tx.Model(&models.QueueBooking{}).
				Where("queue_session_id = ? AND status = 'waiting'", s.ID).
				Update("status", "cancelled")
			if res.Error != nil {
				return res.Error
			}
			cancelled = int(res.RowsAffected)

			if err := tx.Model(&models.QueueSession{}).
				Where("id = ?", s.ID).
				Updates(map[string]interface{}{
					"status":   "closed",
					"end_time": endTime,
				}).Error; err != nil {
				return err
			}

			return forceWorkersOfflineBySession(tx, s.ID, endTime)
		})
		if err != nil {
			return results, err
		}

		results = append(results, AutoClosedQueueSession{
			SessionID:        s.ID,
			CourseID:         s.CourseID,
			CancelledWaiting: cancelled,
		})
	}

	return results, nil
}

// AutoCloseAbandonedPausedQueueSessions closes queue sessions that are paused
// and have not reported projector heartbeat within the given timeout.
func AutoCloseAbandonedPausedQueueSessions(timeout time.Duration) ([]AutoClosedQueueSession, error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	cutoff := time.Now().Add(-timeout)

	var sessions []models.QueueSession
	if err := config.DB.
		Where("status = 'paused' AND COALESCE(projector_last_seen_at, updated_at) < ?", cutoff).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	var results []AutoClosedQueueSession
	endTime := time.Now().UTC()

	for _, s := range sessions {
		cancelled := 0
		err := config.DB.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&models.QueueBooking{}).
				Where("queue_session_id = ? AND status = 'waiting'", s.ID).
				Update("status", "cancelled")
			if res.Error != nil {
				return res.Error
			}
			cancelled = int(res.RowsAffected)

			if err := tx.Model(&models.QueueSession{}).
				Where("id = ? AND status = 'paused'", s.ID).
				Updates(map[string]interface{}{
					"status":                 "closed",
					"end_time":               endTime,
					"projector_last_seen_at": nil,
				}).Error; err != nil {
				return err
			}

			return forceWorkersOfflineBySession(tx, s.ID, endTime)
		})
		if err != nil {
			return results, err
		}

		results = append(results, AutoClosedQueueSession{
			SessionID:        s.ID,
			CourseID:         s.CourseID,
			CancelledWaiting: cancelled,
		})
	}

	return results, nil
}

// ---------- DeskStatus ----------

func GetDeskStatuses(sessionID string) ([]models.QueueDeskStatus, error) {
	var statuses []models.QueueDeskStatus
	err := config.DB.Where("queue_session_id = ?", sessionID).Find(&statuses).Error
	return statuses, err
}
