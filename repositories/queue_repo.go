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

type QueueSessionListItem struct {
	QueueSession            models.QueueSession          `json:"-"`
	Classroom               *QueueSessionClassroomBasic  `json:"classroom"`
	LinkedAssignment        *QueueSessionAssignmentBasic `json:"linkedAssignment"`
	LinkedAttendanceSession *QueueSessionAttendanceBasic `json:"linkedAttendanceSession"`
	Creator                 *QueueSessionCreatorBasic    `json:"creator"`
	Stats                   QueueSessionStats            `json:"stats"`
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
		result[i] = QueueSessionListItem{
			QueueSession:            session,
			Classroom:               classroomMap[session.ClassroomID],
			LinkedAssignment:        linkedAssignment,
			LinkedAttendanceSession: linkedAttendance,
			Creator:                 creator,
			Stats:                   statsMap[session.ID],
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
	pin, err := generateQueuePIN()
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
	if err := db.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     "active",
		"start_time": now,
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
	return config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Update("status", "paused").Error
}

func CloseQueueSession(id string) error {
	now := time.Now()
	return config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":   "closed",
		"end_time": now,
	}).Error
}

func ResumeQueueSession(id string) error {
	return config.DB.Model(&models.QueueSession{}).Where("id = ?", id).Update("status", "active").Error
}

// ---------- QueueBooking ----------

type BookingInput struct {
	StudentID   uint
	DeskID      string
	DeskNumber  int
	BookingType string // grading | help
	Note        string
}

func CreateBooking(sessionID string, input BookingInput) (*models.QueueBooking, error) {
	var booking *models.QueueBooking
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		queueNumber, err := reserveNextQueueNumber(tx, sessionID)
		if err != nil {
			return err
		}

		booking = &models.QueueBooking{
			QueueSessionID: sessionID,
			StudentID:      input.StudentID,
			DeskID:         input.DeskID,
			DeskNumber:     input.DeskNumber,
			BookingType:    input.BookingType,
			QueueNumber:    queueNumber,
			Note:           input.Note,
			Status:         "waiting",
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

func GetStudentActiveBooking(sessionID string, studentID uint) (*models.QueueBooking, error) {
	var b models.QueueBooking
	err := config.DB.Where("queue_session_id = ? AND student_id = ? AND status IN ('waiting','in_progress')", sessionID, studentID).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func CancelBooking(bookingID uint, studentID uint) error {
	var b models.QueueBooking
	if err := config.DB.First(&b, bookingID).Error; err != nil {
		return err
	}
	if b.StudentID != studentID {
		return fmt.Errorf("unauthorized")
	}
	if b.Status != "waiting" {
		return fmt.Errorf("cannot cancel booking in status: %s", b.Status)
	}
	b.Status = "cancelled"
	if err := config.DB.Save(&b).Error; err != nil {
		return err
	}
	if b.BookingType == "grading" {
		return updateDeskStatus(config.DB, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0)
	}
	return syncHelpDeskStatus(config.DB, b.QueueSessionID, b.DeskID)
	return nil
}

func resetDeskStatus(bookingType string) string {
	if bookingType == "grading" {
		return "not_started"
	}
	return "none"
}

// ---------- Worker ----------

func WorkerJoin(sessionID string, userID uint, acceptGrading, acceptHelp bool) (*models.QueueWorker, error) {
	var w models.QueueWorker
	err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionID, userID).First(&w).Error
	now := time.Now()
	if err != nil {
		// Create new
		w = models.QueueWorker{
			QueueSessionID: sessionID,
			UserID:         userID,
			AcceptGrading:  acceptGrading,
			AcceptHelp:     acceptHelp,
			Status:         "online",
			LastActiveAt:   &now,
		}
		return &w, config.DB.Create(&w).Error
	}
	w.Status = "online"
	w.AcceptGrading = acceptGrading
	w.AcceptHelp = acceptHelp
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

func GetWorkersBySession(sessionID string) ([]models.QueueWorker, error) {
	var workers []models.QueueWorker
	err := config.DB.Where("queue_session_id = ?", sessionID).Find(&workers).Error
	return workers, err
}

// Worker starts / completes a booking
func WorkerUpdateBooking(bookingID uint, workerID uint, action string, score *float64, workerNote string) (*models.QueueBooking, error) {
	var b models.QueueBooking
	if err := config.DB.First(&b, bookingID).Error; err != nil {
		return nil, err
	}

	var worker models.QueueWorker
	workerErr := config.DB.Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).First(&worker).Error
	if workerErr != nil {
		return nil, fmt.Errorf("worker not registered for this queue session")
	}

	now := time.Now()
	needsHelpDeskSync := false
	switch action {
	case "start":
		b.Status = "in_progress"
		b.AssignedWorkerID = &workerID
		b.AssignedAt = &now
		b.StartedAt = &now
		if err := updateDeskStatus(config.DB, b.QueueSessionID, b.DeskID, b.BookingType, "in_progress", b.ID); err != nil {
			return nil, err
		}
		if workerErr == nil {
			config.DB.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(map[string]interface{}{"status": "busy", "current_booking_id": b.ID, "last_active_at": now})
		}
	case "complete":
		b.Status = "completed"
		b.CompletedAt = &now
		b.Score = score
		b.WorkerNote = workerNote
		if workerErr == nil {
			updates := map[string]interface{}{"current_booking_id": nil, "last_active_at": now}
			if worker.Status == "paused" {
				updates["status"] = "offline"
			} else {
				updates["status"] = "online"
			}
			if b.BookingType == "grading" {
				updates["total_grading_completed"] = gorm.Expr("total_grading_completed + 1")
			} else {
				updates["total_help_completed"] = gorm.Expr("total_help_completed + 1")
			}
			config.DB.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(updates)
		}
		if b.BookingType == "grading" {
			if err := updateDeskStatus(config.DB, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0); err != nil {
				return nil, err
			}
		} else {
			needsHelpDeskSync = true
		}
	case "no_show":
		b.Status = "no_show"
		b.CompletedAt = &now
		b.WorkerNote = workerNote
		if workerErr == nil {
			updates := map[string]interface{}{"current_booking_id": nil, "last_active_at": now}
			if worker.Status == "paused" {
				updates["status"] = "offline"
			} else {
				updates["status"] = "online"
			}
			config.DB.Model(&models.QueueWorker{}).
				Where("queue_session_id = ? AND user_id = ?", b.QueueSessionID, workerID).
				Updates(updates)
		}
		if b.BookingType == "grading" {
			if err := updateDeskStatus(config.DB, b.QueueSessionID, b.DeskID, b.BookingType, resetDeskStatus(b.BookingType), 0); err != nil {
				return nil, err
			}
		} else {
			needsHelpDeskSync = true
		}
	}

	if err := config.DB.Save(&b).Error; err != nil {
		return nil, err
	}
	if needsHelpDeskSync {
		if err := syncHelpDeskStatus(config.DB, b.QueueSessionID, b.DeskID); err != nil {
			return nil, err
		}
	}
	return &b, nil
}

// VerifySessionPIN checks if PIN matches an active session
func VerifySessionPIN(sessionID string, pin string) bool {
	var s models.QueueSession
	err := config.DB.First(&s, "id = ?", sessionID).Error
	return err == nil && s.Status == "active" && s.PinCode == pin
}

func RegenerateQueueSessionPIN(sessionID string) (string, error) {
	pin, err := generateQueuePIN()
	if err != nil {
		return "", err
	}

	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", sessionID).Update("pin_code", pin).Error; err != nil {
		return "", err
	}

	return pin, nil
}

// ---------- DeskStatus ----------

func GetDeskStatuses(sessionID string) ([]models.QueueDeskStatus, error) {
	var statuses []models.QueueDeskStatus
	err := config.DB.Where("queue_session_id = ?", sessionID).Find(&statuses).Error
	return statuses, err
}
