package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/services"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	ua "github.com/mileusna/useragent"
	"gorm.io/gorm"
)

var queueRejectReasonOrder = []string{
	"busy_with_student",
	"consulting_instructor",
	"bathroom_break",
	"technical_issue",
	"temporary_unavailable",
	"other",
}

func normalizeQueueRejectReason(reason string) string {
	normalized := strings.TrimSpace(strings.ToLower(reason))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")

	switch normalized {
	case "busy_explaining":
		return "busy_with_student"
	case "teacher_consultation":
		return "consulting_instructor"
	case "restroom_break":
		return "bathroom_break"
	case "temp_unavailable":
		return "temporary_unavailable"
	}

	for _, allowed := range queueRejectReasonOrder {
		if normalized == allowed {
			return normalized
		}
	}

	return ""
}

func queueRejectReasonLabels(reasonCode string) (string, string) {
	switch normalizeQueueRejectReason(reasonCode) {
	case "busy_with_student":
		return "ติดช่วยเหลือนักศึกษาคนอื่น", "Busy helping another student"
	case "consulting_instructor":
		return "กำลังคุยกับอาจารย์", "Consulting instructor"
	case "bathroom_break":
		return "ติดภารกิจส่วนตัวชั่วคราว", "Temporary personal break"
	case "technical_issue":
		return "มีปัญหาทางเทคนิค", "Technical issue"
	case "temporary_unavailable":
		return "ไม่สะดวกรับงานชั่วคราว", "Temporarily unavailable"
	default:
		return "เหตุผลอื่น", "Other"
	}
}

// QueueHandler â€” struct-based handler with audit logger
type QueueHandler struct {
	auditLogger *services.AuditLogger
}

func NewQueueHandler(auditLogger *services.AuditLogger) *QueueHandler {
	return &QueueHandler{auditLogger: auditLogger}
}

// GET /api/courses/:courseId/queue/sessions
func GetQueueSessionsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	result, err := repositories.GetQueueSessions(courseID, strings.TrimSpace(c.Query("status")))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch queue sessions"})
	}
	payload := make([]fiber.Map, len(result))
	for i, item := range result {
		payload[i] = fiber.Map{
			"id":                           item.QueueSession.ID,
			"course_id":                    item.QueueSession.CourseID,
			"classroom_id":                 item.QueueSession.ClassroomID,
			"title":                        item.QueueSession.Title,
			"description":                  item.QueueSession.Description,
			"pin_code":                     item.QueueSession.PinCode,
			"linked_assignment_id":         item.QueueSession.LinkedAssignmentID,
			"require_attendance":           item.QueueSession.RequireAttendance,
			"linked_attendance_session_id": item.QueueSession.LinkedAttendanceSessionID,
			"is_cutoff_enabled":            item.QueueSession.IsCutoffEnabled,
			"cutoff_at":                    item.QueueSession.CutoffAt,
			"cutoff_note":                  item.QueueSession.CutoffNote,
			"status":                       item.QueueSession.Status,
			"start_time":                   item.QueueSession.StartTime,
			"end_time":                     item.QueueSession.EndTime,
			"created_by":                   item.QueueSession.CreatedBy,
			"created_at":                   item.QueueSession.CreatedAt,
			"updated_at":                   item.QueueSession.UpdatedAt,
			"classroom":                    item.Classroom,
			"linkedAssignment":             item.LinkedAssignment,
			"linkedAttendanceSession":      item.LinkedAttendanceSession,
			"creator":                      item.Creator,
			"stats": fiber.Map{
				"total":       item.Stats.Total,
				"waiting":     item.Stats.Waiting,
				"in_progress": item.Stats.InProgress,
				"completed":   item.Stats.Completed,
			},
		}
	}
	return c.JSON(fiber.Map{"success": true, "data": payload})
}

// POST /api/courses/:courseId/queue/sessions
func CreateQueueSessionHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	if err := queueEnsureCourseWritable(c, courseID); err != nil {
		return err
	}

	var input struct {
		ClassroomID               string     `json:"classroom_id"`
		Title                     string     `json:"title"`
		Description               string     `json:"description"`
		LinkedAssignmentID        *uint      `json:"linked_assignment_id"`
		RequireAttendance         bool       `json:"require_attendance"`
		LinkedAttendanceSessionID *uint      `json:"linked_attendance_session_id"`
		IsCutoffEnabled           bool       `json:"is_cutoff_enabled"`
		CutoffAt                  *time.Time `json:"cutoff_at"`
		CutoffNote                string     `json:"cutoff_note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.ClassroomID == "" || input.Title == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "classroom_id and title are required"})
	}
	if input.IsCutoffEnabled && input.CutoffAt == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "cutoff_at is required when is_cutoff_enabled=true"})
	}

	userID := c.Locals("user_id").(uint)
	session := models.QueueSession{
		CourseID:                  courseID,
		ClassroomID:               input.ClassroomID,
		Title:                     input.Title,
		Description:               input.Description,
		LinkedAssignmentID:        input.LinkedAssignmentID,
		RequireAttendance:         input.RequireAttendance,
		LinkedAttendanceSessionID: input.LinkedAttendanceSessionID,
		IsCutoffEnabled:           input.IsCutoffEnabled,
		CutoffAt:                  input.CutoffAt,
		CutoffNote:                strings.TrimSpace(input.CutoffNote),
		CreatedBy:                 &userID,
	}
	if err := repositories.CreateQueueSession(&session); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create queue session"})
	}
	logCourseActivity(c, courseID, userID, "create_queue_session", "queue", "queue_session", session.ID, session.Title, fiber.Map{"classroom_id": session.ClassroomID, "linked_assignment_id": session.LinkedAssignmentID, "require_attendance": session.RequireAttendance})
	go createNotificationsForCourseMembers(courseID, userID, "queue_created", "สร้างคิว: "+session.Title, "มีการสร้างคิวใหม่ในวิชา", "/classroom/"+courseID+"/queue", buildNotifData(courseID, session.ID, "queue_session", ""))
	return c.Status(201).JSON(fiber.Map{"success": true, "data": session})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId
func GetQueueSessionHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Queue session not found"})
	}

	payload := fiber.Map{
		"id":                           session.ID,
		"course_id":                    session.CourseID,
		"classroom_id":                 session.ClassroomID,
		"title":                        session.Title,
		"description":                  session.Description,
		"pin_code":                     session.PinCode,
		"linked_assignment_id":         session.LinkedAssignmentID,
		"require_attendance":           session.RequireAttendance,
		"linked_attendance_session_id": session.LinkedAttendanceSessionID,
		"is_cutoff_enabled":            session.IsCutoffEnabled,
		"cutoff_at":                    session.CutoffAt,
		"cutoff_note":                  session.CutoffNote,
		"status":                       session.Status,
		"start_time":                   session.StartTime,
		"end_time":                     session.EndTime,
		"created_by":                   session.CreatedBy,
		"created_at":                   session.CreatedAt,
		"updated_at":                   session.UpdatedAt,
	}

	if session.LinkedAssignmentID != nil {
		if assignment, subItems, loadErr := loadQueueAssignmentWithSubItems(*session.LinkedAssignmentID); loadErr == nil && assignment != nil {
			payload["linkedAssignment"] = fiber.Map{
				"id":              assignment.ID,
				"name":            assignment.Name,
				"max_score":       assignment.MaxScore,
				"assignment_type": assignment.AssignmentType,
				"subItems":        subItems,
			}
		}
	}

	if session.LinkedAttendanceSessionID != nil {
		var attendanceSession struct {
			ID    uint   `gorm:"column:id"`
			Title string `gorm:"column:title"`
		}
		if err := config.DB.Table("attendance_sessions").Select("id", "title").Where("id = ?", *session.LinkedAttendanceSessionID).Take(&attendanceSession).Error; err == nil {
			payload["linkedAttendanceSession"] = fiber.Map{
				"id":    attendanceSession.ID,
				"title": attendanceSession.Title,
			}
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": payload})
}

// PUT /api/courses/:courseId/queue/sessions/:sessionId
func UpdateQueueSessionHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	actorID := c.Locals("user_id").(uint)

	var input struct {
		Title                     *string    `json:"title"`
		Description               *string    `json:"description"`
		LinkedAssignmentID        *uint      `json:"linked_assignment_id"`
		RequireAttendance         *bool      `json:"require_attendance"`
		LinkedAttendanceSessionID *uint      `json:"linked_attendance_session_id"`
		IsCutoffEnabled           *bool      `json:"is_cutoff_enabled"`
		CutoffAt                  *time.Time `json:"cutoff_at"`
		CutoffNote                *string    `json:"cutoff_note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.Title != nil {
		session.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		session.Description = *input.Description
	}
	session.LinkedAssignmentID = input.LinkedAssignmentID
	if input.RequireAttendance != nil {
		session.RequireAttendance = *input.RequireAttendance
	}
	session.LinkedAttendanceSessionID = input.LinkedAttendanceSessionID
	if !session.RequireAttendance {
		session.LinkedAttendanceSessionID = nil
	}
	if input.IsCutoffEnabled != nil {
		session.IsCutoffEnabled = *input.IsCutoffEnabled
		if !*input.IsCutoffEnabled {
			session.CutoffAt = nil
			session.CutoffNote = ""
		}
	}
	if input.CutoffAt != nil {
		session.CutoffAt = input.CutoffAt
	}
	if input.CutoffNote != nil {
		session.CutoffNote = strings.TrimSpace(*input.CutoffNote)
	}
	if session.IsCutoffEnabled && session.CutoffAt == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "cutoff_at is required when is_cutoff_enabled=true"})
	}
	if err := repositories.UpdateQueueSession(session); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update session"})
	}
	logCourseActivity(c, session.CourseID, actorID, "update_queue_session", "queue", "queue_session", session.ID, session.Title, fiber.Map{"description": session.Description, "linked_assignment_id": session.LinkedAssignmentID, "require_attendance": session.RequireAttendance, "linked_attendance_session_id": session.LinkedAttendanceSessionID})
	go createNotificationsForCourseMembers(session.CourseID, actorID, "queue_updated", "แก้ไขคิว: "+session.Title, "มีการแก้ไขคิวในวิชา", "/classroom/"+session.CourseID+"/queue", buildNotifData(session.CourseID, session.ID, "queue_session", ""))
	return c.JSON(fiber.Map{"success": true, "data": session})
}

// DELETE /api/courses/:courseId/queue/sessions/:sessionId
func DeleteQueueSessionHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	actorID := c.Locals("user_id").(uint)
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if err := repositories.DeleteQueueSession(sessionID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete session"})
	}
	logCourseActivity(c, session.CourseID, actorID, "delete_queue_session", "queue", "queue_session", session.ID, session.Title, nil)
	return c.JSON(fiber.Map{"success": true, "message": "Queue session deleted"})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/start
func (h *QueueHandler) StartQueueSession(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if err := repositories.StartQueueSession(sessionID, session.ClassroomID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to start session"})
	}
	actorID := c.Locals("user_id").(uint)
	logCourseActivity(c, session.CourseID, actorID, "start_queue_session", "queue", "queue_session", session.ID, session.Title, nil)
	go createNotificationsForCourseMembers(session.CourseID, actorID, "queue_opened", "เปิดคิว: "+session.Title, "มีการเปิดคิวในวิชา", "/classroom/"+session.CourseID+"/queue", buildNotifData(session.CourseID, session.ID, "queue_session", ""))
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    session.CourseID,
		ActorUserID: actorID,
		Action:      services.ActionQueueSessionOpened,
		TargetType:  "queue_session",
		TargetID:    session.ID,
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.JSON(fiber.Map{"success": true, "message": "Session started"})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/pause
func PauseQueueSessionHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if err := repositories.PauseQueueSession(sessionID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to pause session"})
	}
	logCourseActivity(c, session.CourseID, c.Locals("user_id").(uint), "pause_queue_session", "queue", "queue_session", session.ID, session.Title, nil)
	return c.JSON(fiber.Map{"success": true, "message": "Session paused"})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/resume
func ResumeQueueSessionHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if err := repositories.ResumeQueueSession(sessionID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resume session"})
	}
	logCourseActivity(c, session.CourseID, c.Locals("user_id").(uint), "resume_queue_session", "queue", "queue_session", session.ID, session.Title, nil)
	return c.JSON(fiber.Map{"success": true, "message": "Session resumed"})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/close
func (h *QueueHandler) CloseQueueSession(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if err := repositories.CloseQueueSession(sessionID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to close session"})
	}
	actorID := c.Locals("user_id").(uint)
	logCourseActivity(c, session.CourseID, actorID, "close_queue_session", "queue", "queue_session", session.ID, session.Title, nil)
	go createNotificationsForCourseMembers(session.CourseID, actorID, "queue_closed", "ปิดคิว: "+session.Title, "มีการปิดคิวในวิชา", "/classroom/"+session.CourseID+"/queue", buildNotifData(session.CourseID, session.ID, "queue_session", ""))
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    session.CourseID,
		ActorUserID: actorID,
		Action:      services.ActionQueueSessionClosed,
		TargetType:  "queue_session",
		TargetID:    session.ID,
		RequestID:   reqID,
		IPAddress:   ip,
	})
	updatedSession, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Session closed but failed to reload session"})
	}
	realtime.EmitToQueue(updatedSession.ID, "session-status-changed", fiber.Map{
		"status":    updatedSession.Status,
		"session":   updatedSession,
		"timestamp": time.Now().UnixMilli(),
	})
	return c.JSON(fiber.Map{"success": true, "message": "Session closed", "data": updatedSession})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/verify-pin
func VerifyQueuePINHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	var input struct {
		PIN string `json:"pin"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	ok := repositories.VerifySessionPIN(sessionID, input.PIN)
	if !ok {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid PIN or session not active"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "PIN verified"})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/bookings
func (h *QueueHandler) CreateBooking(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	var input struct {
		StudentID   uint   `json:"student_id"`
		DeskID      string `json:"desk_id"`
		DeskNumber  int    `json:"desk_number"`
		BookingType string `json:"booking_type"`
		Note        string `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.StudentID == 0 || input.DeskID == "" || input.BookingType == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "student_id, desk_id, booking_type required"})
	}

	booking, err := repositories.CreateBooking(sessionID, repositories.BookingInput{
		StudentID:   input.StudentID,
		DeskID:      input.DeskID,
		DeskNumber:  input.DeskNumber,
		BookingType: input.BookingType,
		Note:        input.Note,
		IsLate:      queueBookingIsAfterCutoff(session, time.Now()),
		LateReason:  queueBookingLateReason(session),
	})
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	actorID := c.Locals("user_id").(uint)
	logCourseActivity(c, session.CourseID, actorID, "create_queue_booking", "queue", "queue_booking", booking.ID, session.Title, fiber.Map{"queue_session_id": booking.QueueSessionID, "student_id": booking.StudentID, "desk_number": booking.DeskNumber, "booking_type": booking.BookingType})
	emitQueueBookingChanged(sessionID, "new-booking", booking)
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    session.CourseID,
		ActorUserID: actorID,
		Action:      services.ActionQueueBookingCreated,
		TargetType:  "queue_booking",
		TargetID:    strconv.Itoa(int(booking.ID)),
		Description: fmt.Sprintf("Student %d booked desk %s", input.StudentID, input.DeskID),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.Status(201).JSON(fiber.Map{"success": true, "data": booking})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/bookings
func GetBookingsHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	bookings, err := repositories.GetBookingsBySession(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch bookings"})
	}
	return c.JSON(fiber.Map{"success": true, "data": bookings})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/report
func GetQueueSessionReportHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	sessionID := c.Params("sessionId")

	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil || session.CourseID != courseID {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	bookings, err := repositories.GetBookingsBySession(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch bookings"})
	}

	workers, err := repositories.GetWorkersBySession(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch workers"})
	}

	studentIDs := make([]uint, 0)
	studentSeen := map[uint]struct{}{}
	workerIDs := make([]uint, 0)
	workerSeen := map[uint]struct{}{}
	for _, booking := range bookings {
		if _, ok := studentSeen[booking.StudentID]; !ok {
			studentSeen[booking.StudentID] = struct{}{}
			studentIDs = append(studentIDs, booking.StudentID)
		}
		if booking.AssignedWorkerID != nil {
			if _, ok := workerSeen[*booking.AssignedWorkerID]; !ok {
				workerSeen[*booking.AssignedWorkerID] = struct{}{}
				workerIDs = append(workerIDs, *booking.AssignedWorkerID)
			}
		}
	}
	for _, worker := range workers {
		if _, ok := workerSeen[worker.UserID]; !ok {
			workerSeen[worker.UserID] = struct{}{}
			workerIDs = append(workerIDs, worker.UserID)
		}
	}

	studentMap := map[uint]models.Student{}
	if len(studentIDs) > 0 {
		var students []models.Student
		if err := config.DB.Select("id", "student_id", "full_name").Where("id IN ?", studentIDs).Find(&students).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch students"})
		}
		for _, student := range students {
			studentMap[student.ID] = student
		}
	}

	workerMap := map[uint]models.User{}
	if len(workerIDs) > 0 {
		var users []models.User
		if err := config.DB.Select("id", "full_name").Where("id IN ?", workerIDs).Find(&users).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch workers"})
		}
		for _, user := range users {
			workerMap[user.ID] = user
		}
	}

	type queueWorkerActivityLog struct {
		ActorUserID uint      `gorm:"column:actor_user_id"`
		Action      string    `gorm:"column:action"`
		CreatedAt   time.Time `gorm:"column:created_at"`
	}
	type queueWorkerActivitySummary struct {
		OpenedCount         int
		ClosedCount         int
		FirstOpenedAt       *time.Time
		LastOpenedAt        *time.Time
		LastClosedAt        *time.Time
		TotalActiveDuration time.Duration
		CurrentOpenedAt     *time.Time
	}

	workerActivity := map[uint]*queueWorkerActivitySummary{}
	if len(workerIDs) > 0 {
		var activityLogs []queueWorkerActivityLog
		if err := config.DB.Model(&models.CourseActivityLog{}).
			Select("actor_user_id", "action", "created_at").
			Where("course_id = ? AND target_type = ? AND target_id = ? AND action IN ? AND actor_user_id IN ?", courseID, "queue_session", sessionID, []string{"join_queue_worker", "leave_queue_worker"}, workerIDs).
			Order("created_at ASC").
			Scan(&activityLogs).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch worker activity"})
		}

		for _, logEntry := range activityLogs {
			summary, ok := workerActivity[logEntry.ActorUserID]
			if !ok {
				summary = &queueWorkerActivitySummary{}
				workerActivity[logEntry.ActorUserID] = summary
			}

			switch logEntry.Action {
			case "join_queue_worker":
				summary.OpenedCount++
				if summary.FirstOpenedAt == nil {
					openedAt := logEntry.CreatedAt
					summary.FirstOpenedAt = &openedAt
				}
				openedAt := logEntry.CreatedAt
				summary.LastOpenedAt = &openedAt
				if summary.CurrentOpenedAt == nil {
					summary.CurrentOpenedAt = &openedAt
				}
			case "leave_queue_worker":
				summary.ClosedCount++
				closedAt := logEntry.CreatedAt
				summary.LastClosedAt = &closedAt
				if summary.CurrentOpenedAt != nil && summary.CurrentOpenedAt.Before(closedAt) {
					summary.TotalActiveDuration += closedAt.Sub(*summary.CurrentOpenedAt)
					summary.CurrentOpenedAt = nil
				}
			}
		}
	}

	bookingReports := make([]fiber.Map, 0, len(bookings))
	for _, booking := range bookings {
		waitDuration := ""
		serviceDuration := ""

		waitUntil := booking.StartedAt
		if waitUntil == nil {
			waitUntil = booking.AssignedAt
		}
		if waitUntil == nil {
			waitUntil = booking.CompletedAt
		}
		if waitUntil != nil && booking.CreatedAt.Before(*waitUntil) {
			waitDuration = waitUntil.Sub(booking.CreatedAt).String()
		}
		if booking.StartedAt != nil && booking.CompletedAt != nil && booking.StartedAt.Before(*booking.CompletedAt) {
			serviceDuration = booking.CompletedAt.Sub(*booking.StartedAt).String()
		}

		student := studentMap[booking.StudentID]
		var workerInfo fiber.Map
		if booking.AssignedWorkerID != nil {
			if user, ok := workerMap[*booking.AssignedWorkerID]; ok {
				workerInfo = fiber.Map{"id": user.ID, "full_name": user.FullName}
			} else {
				workerInfo = fiber.Map{"id": *booking.AssignedWorkerID, "full_name": ""}
			}
		}

		bookingReports = append(bookingReports, fiber.Map{
			"id":         booking.ID,
			"student_id": booking.StudentID,
			"student": fiber.Map{
				"id":         student.ID,
				"student_id": student.StudentID,
				"full_name":  student.FullName,
			},
			"desk_id":            booking.DeskID,
			"desk_number":        booking.DeskNumber,
			"booking_type":       booking.BookingType,
			"queue_number":       booking.QueueNumber,
			"status":             booking.Status,
			"assigned_worker_id": booking.AssignedWorkerID,
			"assigned_worker":    workerInfo,
			"assigned_at":        booking.AssignedAt,
			"started_at":         booking.StartedAt,
			"completed_at":       booking.CompletedAt,
			"created_at":         booking.CreatedAt,
			"wait_duration":      waitDuration,
			"service_duration":   serviceDuration,
			"booking_ip":         booking.BookingIP,
			"booking_user_agent": booking.BookingUserAgent,
			"booking_device":     booking.BookingDevice,
			"timeout_count":      booking.TimeoutCount,
			"reject_count":       booking.RejectCount,
			"score":              booking.Score,
			"score_comment":      booking.ScoreComment,
			"worker_note":        booking.WorkerNote,
		})
	}

	totalCompleted := 0
	for _, worker := range workers {
		totalCompleted += worker.TotalGradingCompleted + worker.TotalHelpCompleted
	}

	workerStats := make([]fiber.Map, 0, len(workers))
	for _, worker := range workers {
		completed := worker.TotalGradingCompleted + worker.TotalHelpCompleted
		percent := 0.0
		if totalCompleted > 0 {
			percent = float64(completed) * 100 / float64(totalCompleted)
		}
		user := workerMap[worker.UserID]
		activity := workerActivity[worker.UserID]
		openedCount := 0
		closedCount := 0
		var firstOpenedAt *time.Time
		var lastOpenedAt *time.Time
		var lastClosedAt *time.Time
		totalActiveDuration := ""
		if activity != nil {
			openedCount = activity.OpenedCount
			closedCount = activity.ClosedCount
			firstOpenedAt = activity.FirstOpenedAt
			lastOpenedAt = activity.LastOpenedAt
			lastClosedAt = activity.LastClosedAt
			if activity.TotalActiveDuration > 0 {
				totalActiveDuration = activity.TotalActiveDuration.String()
			}
		}
		totalOffers := worker.OfferAcceptCount + worker.OfferRejectCount + worker.OfferTimeoutCount
		offerAcceptRate := 0.0
		if totalOffers > 0 {
			offerAcceptRate = float64(worker.OfferAcceptCount) * 100 / float64(totalOffers)
		}

		workerStats = append(workerStats, fiber.Map{
			"user_id":               worker.UserID,
			"full_name":             user.FullName,
			"total_completed":       completed,
			"grading_completed":     worker.TotalGradingCompleted,
			"help_completed":        worker.TotalHelpCompleted,
			"percent":               percent,
			"opened_count":          openedCount,
			"closed_count":          closedCount,
			"first_opened_at":       firstOpenedAt,
			"last_opened_at":        lastOpenedAt,
			"last_closed_at":        lastClosedAt,
			"total_active_duration": totalActiveDuration,
			"offer_accept_count":    worker.OfferAcceptCount,
			"offer_reject_count":    worker.OfferRejectCount,
			"offer_timeout_count":   worker.OfferTimeoutCount,
			"offer_total_count":     totalOffers,
			"offer_accept_rate":     offerAcceptRate,
			"offer_paused_until":    worker.OfferPausedUntil,
		})
	}

	bookingIDFilters := make([]string, 0, len(bookings))
	for _, booking := range bookings {
		bookingIDFilters = append(bookingIDFilters, strconv.Itoa(int(booking.ID)))
	}

	rejectReasonCounts := map[string]int{}
	if len(bookingIDFilters) > 0 {
		type queueBookingActionLog struct {
			Detail json.RawMessage `gorm:"column:detail"`
		}

		var actionLogs []queueBookingActionLog
		if err := config.DB.Model(&models.CourseActivityLog{}).
			Select("detail").
			Where("course_id = ? AND action = ? AND target_type = ? AND target_id IN ?", courseID, "update_queue_booking", "queue_booking", bookingIDFilters).
			Order("created_at ASC").
			Scan(&actionLogs).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to aggregate reject reasons"})
		}

		for _, logEntry := range actionLogs {
			if len(logEntry.Detail) == 0 {
				continue
			}

			var payload struct {
				Action       string `json:"action"`
				RejectReason string `json:"reject_reason"`
				WorkerNote   string `json:"worker_note"`
			}
			if err := json.Unmarshal(logEntry.Detail, &payload); err != nil {
				continue
			}
			if payload.Action != "reject" {
				continue
			}

			reasonCode := normalizeQueueRejectReason(payload.RejectReason)
			if reasonCode == "" {
				reasonCode = normalizeQueueRejectReason(payload.WorkerNote)
			}
			if reasonCode == "" {
				reasonCode = "other"
			}

			rejectReasonCounts[reasonCode]++
		}
	}

	rejectReasonStats := make([]fiber.Map, 0, len(queueRejectReasonOrder))
	for _, reasonCode := range queueRejectReasonOrder {
		count := rejectReasonCounts[reasonCode]
		if count == 0 {
			continue
		}
		labelTH, labelEN := queueRejectReasonLabels(reasonCode)
		rejectReasonStats = append(rejectReasonStats, fiber.Map{
			"code":     reasonCode,
			"label_th": labelTH,
			"label_en": labelEN,
			"count":    count,
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session": fiber.Map{
				"id":    session.ID,
				"title": session.Title,
			},
			"bookings":            bookingReports,
			"worker_stats":        workerStats,
			"reject_reason_stats": rejectReasonStats,
		},
	})
}

func queueBookingDeviceLabel(userAgent string) string {
	parsed := ua.Parse(strings.TrimSpace(userAgent))
	deviceType := "Desktop"
	if parsed.Mobile {
		deviceType = "Mobile"
	} else if parsed.Tablet {
		deviceType = "Tablet"
	} else if parsed.Bot {
		deviceType = "Bot"
	}

	parts := make([]string, 0, 3)
	parts = append(parts, deviceType)
	if parsed.Name != "" {
		parts = append(parts, parsed.Name)
	}
	if parsed.OS != "" {
		parts = append(parts, parsed.OS)
	}

	return strings.Join(parts, " / ")
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/bookings/student/:studentId
func GetStudentBookingHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	studentIDStr := c.Params("studentId")
	studentID, err := strconv.ParseUint(studentIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid student ID"})
	}
	booking, err := repositories.GetStudentActiveBooking(sessionID, uint(studentID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No active booking found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": booking})
}

// DELETE /api/courses/:courseId/queue/sessions/:sessionId/bookings/:bookingId
func CancelBookingHandler(c fiber.Ctx) error {
	bookingIDStr := c.Params("bookingId")
	bookingID, err := strconv.ParseUint(bookingIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid booking ID"})
	}
	studentID, _ := middlewares.GetStudentID(c)
	booking, _ := repositories.GetBookingByID(uint(bookingID))
	if booking != nil {
		if session, sessionErr := repositories.GetQueueSessionByID(booking.QueueSessionID); sessionErr == nil {
			if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
				return err
			}
		}
	}
	if err := repositories.CancelBooking(uint(bookingID), studentID); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	if booking != nil {
		if session, sessionErr := repositories.GetQueueSessionByID(booking.QueueSessionID); sessionErr == nil {
			logCourseActivity(c, session.CourseID, studentID, "cancel_queue_booking", "queue", "queue_booking", booking.ID, session.Title, fiber.Map{"booking_type": booking.BookingType, "desk_number": booking.DeskNumber})
		}
		emitQueueBookingChanged(booking.QueueSessionID, "booking-cancelled", booking)
	}
	return c.JSON(fiber.Map{"success": true, "message": "Booking cancelled"})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/desks
func GetDeskStatusesHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	statuses, err := repositories.GetDeskStatuses(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch desk statuses"})
	}
	return c.JSON(fiber.Map{"success": true, "data": statuses})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/worker/join
func WorkerJoinHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}

	var input struct {
		AcceptGrading bool `json:"accept_grading"`
		AcceptHelp    bool `json:"accept_help"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	userID := c.Locals("user_id").(uint)
	worker, err := repositories.WorkerJoin(sessionID, userID, input.AcceptGrading, input.AcceptHelp)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to join as worker"})
	}

	assignedBooking, assignErr := tryAssignNextBookingAndEmit(sessionID, userID)
	if assignErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to join as worker"})
	}

	var assignedBookingPayload interface{}
	if assignedBooking != nil {
		payload, payloadErr := buildWorkerBookingPayload(assignedBooking)
		if payloadErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to join as worker"})
		}
		assignedBookingPayload = payload
	}

	logCourseActivity(c, session.CourseID, userID, "join_queue_worker", "queue", "queue_session", session.ID, session.Title, fiber.Map{"accept_grading": input.AcceptGrading, "accept_help": input.AcceptHelp})
	realtime.EmitToQueue(sessionID, "worker-joined", fiber.Map{"worker": worker, "timestamp": time.Now().UnixMilli()})
	return c.JSON(fiber.Map{"success": true, "data": worker, "assignedBooking": assignedBookingPayload})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/workers
func GetWorkersHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	workers, err := repositories.GetWorkersBySession(sessionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch workers"})
	}
	return c.JSON(fiber.Map{"success": true, "data": workers})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/workers/leave
func WorkerLeaveHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	userID := c.Locals("user_id").(uint)
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}

	worker, err := repositories.GetWorkerBySessionUser(sessionID, userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Worker not found"})
	}

	var activeBooking models.QueueBooking
	hasActiveBooking := config.DB.Where("queue_session_id = ? AND assigned_worker_id = ? AND status IN ?", sessionID, userID, []string{"waiting", "in_progress"}).First(&activeBooking).Error == nil

	newStatus := "offline"
	message := "ออกจากการรับงานสำเร็จ"
	if hasActiveBooking {
		newStatus = "paused"
		message = "หยุดรับงานใหม่แล้ว กรุณาทำงานปัจจุบันให้เสร็จ"
	}

	now := time.Now()
	if err := config.DB.Model(&models.QueueWorker{}).
		Where("id = ?", worker.ID).
		Updates(map[string]interface{}{"status": newStatus, "last_active_at": now}).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to leave worker session"})
	}

	logCourseActivity(c, session.CourseID, userID, "leave_queue_worker", "queue", "queue_session", session.ID, session.Title, fiber.Map{"status": newStatus})
	realtime.EmitToQueue(sessionID, "worker-left", fiber.Map{"worker_id": userID, "status": newStatus, "timestamp": time.Now().UnixMilli()})

	return c.JSON(fiber.Map{"success": true, "message": message, "data": fiber.Map{"status": newStatus}})
}

// GET /api/courses/:courseId/queue/sessions/:sessionId/workers/current-booking
func GetWorkerCurrentBookingHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	userID := c.Locals("user_id").(uint)
	if _, timeoutErr := repositories.ProcessQueueOfferTimeouts(sessionID); timeoutErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch current booking"})
	}
	session, err := repositories.GetQueueSessionByID(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	courseExists, isCourseActive, activeErr := repositories.GetCourseActiveState(session.CourseID)
	if activeErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch current booking"})
	}
	if !courseExists {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Course not found"})
	}

	worker, _ := repositories.GetWorkerBySessionUser(sessionID, userID)
	if worker == nil {
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"worker":         nil,
				"currentBooking": nil,
			},
		})
	}

	var booking models.QueueBooking
	bookingErr := config.DB.Where("queue_session_id = ? AND assigned_worker_id = ? AND status IN ?", sessionID, userID, []string{"waiting", "in_progress"}).Order("updated_at DESC, id DESC").First(&booking).Error
	if bookingErr != nil && !errors.Is(bookingErr, gorm.ErrRecordNotFound) {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch current booking"})
	}

	if errors.Is(bookingErr, gorm.ErrRecordNotFound) && isCourseActive {
		assignedBooking, assignErr := tryAssignNextBookingAndEmit(sessionID, userID)
		if assignErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch current booking"})
		}
		if assignedBooking != nil {
			booking = *assignedBooking
			bookingErr = nil
		}
	}

	var bookingPayload interface{}
	if bookingErr == nil {
		payload, payloadErr := buildWorkerBookingPayload(&booking)
		if payloadErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch current booking"})
		}
		bookingPayload = payload
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"worker":         worker,
			"currentBooking": bookingPayload,
		},
	})
}

// PUT /api/courses/:courseId/queue/sessions/:sessionId/bookings/:bookingId/action
func WorkerBookingActionHandler(c fiber.Ctx) error {
	bookingIDStr := c.Params("bookingId")
	bookingID, err := strconv.ParseUint(bookingIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid booking ID"})
	}

	var input struct {
		Action       string   `json:"action"` // start, complete, no_show, reject
		Score        *float64 `json:"score"`
		RejectReason string   `json:"reject_reason"`
		WorkerNote   string   `json:"worker_note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	existingBooking, err := repositories.GetBookingByID(uint(bookingID))
	if err != nil || existingBooking == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Booking not found"})
	}
	if session, sessionErr := repositories.GetQueueSessionByID(existingBooking.QueueSessionID); sessionErr == nil {
		if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
			return err
		}
	}

	workerID := c.Locals("user_id").(uint)
	if input.Action == "reject" {
		reasonCode := normalizeQueueRejectReason(input.RejectReason)
		if reasonCode == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid reject_reason"})
		}
		input.RejectReason = reasonCode
		input.WorkerNote = reasonCode
	}

	booking, err2 := repositories.WorkerUpdateBooking(uint(bookingID), workerID, input.Action, input.Score, input.WorkerNote)
	if err2 != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update booking"})
	}
	if session, sessionErr := repositories.GetQueueSessionByID(booking.QueueSessionID); sessionErr == nil {
		logCourseActivity(c, session.CourseID, workerID, "update_queue_booking", "queue", "queue_booking", booking.ID, session.Title, fiber.Map{"action": input.Action, "score": input.Score, "booking_type": booking.BookingType, "reject_reason": input.RejectReason, "worker_note": input.WorkerNote})
	}
	emitQueueActionChanged(booking, input.Action)

	var nextBooking *models.QueueBooking
	if input.Action == "complete" || input.Action == "no_show" || input.Action == "reject" {
		nextBooking, err = tryAssignNextBookingAndEmit(booking.QueueSessionID, workerID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update booking"})
		}
	}

	var nextBookingPayload interface{}
	if nextBooking != nil {
		payload, payloadErr := buildWorkerBookingPayload(nextBooking)
		if payloadErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update booking"})
		}
		nextBookingPayload = payload
	}

	currentBookingPayload, currentPayloadErr := buildWorkerBookingPayload(booking)
	if currentPayloadErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update booking"})
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"booking": currentBookingPayload, "next_booking": nextBookingPayload}})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/bookings/:bookingId/complete
func CompleteQueueBookingCompatHandler(c fiber.Ctx) error {
	bookingID, err := strconv.ParseUint(c.Params("bookingId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid booking ID"})
	}

	var input struct {
		Score         *float64 `json:"score"`
		ScoreComment  string   `json:"score_comment"`
		WorkerNote    string   `json:"worker_note"`
		SubItemScores []struct {
			SubItemID uint    `json:"sub_item_id"`
			Score     float64 `json:"score"`
		} `json:"sub_item_scores"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	existingBooking, lookupErr := repositories.GetBookingByID(uint(bookingID))
	if lookupErr != nil || existingBooking == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Booking not found"})
	}
	if session, sessionErr := repositories.GetQueueSessionByID(existingBooking.QueueSessionID); sessionErr == nil {
		if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
			return err
		}
	}

	workerID := c.Locals("user_id").(uint)
	subItemScores := make([]repositories.QueueBookingSubItemScoreInput, 0, len(input.SubItemScores))
	for _, item := range input.SubItemScores {
		subItemScores = append(subItemScores, repositories.QueueBookingSubItemScoreInput{
			SubItemID: item.SubItemID,
			Score:     item.Score,
		})
	}

	booking, err := repositories.CompleteBookingWithScores(uint(bookingID), workerID, input.Score, input.ScoreComment, input.WorkerNote, subItemScores)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if session, sessionErr := repositories.GetQueueSessionByID(booking.QueueSessionID); sessionErr == nil {
		logCourseActivity(c, session.CourseID, workerID, "complete_queue_booking", "queue", "queue_booking", booking.ID, session.Title, fiber.Map{"booking_type": booking.BookingType, "score": input.Score})
	}
	emitQueueBookingChanged(booking.QueueSessionID, "booking-completed", booking)
	realtime.EmitToBooking(booking.ID, "your-booking-completed", fiber.Map{"booking": booking, "timestamp": time.Now().UnixMilli()})
	realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})
	nextBooking, assignErr := tryAssignNextBookingAndEmit(booking.QueueSessionID, workerID)
	if assignErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to complete booking"})
	}

	var nextBookingPayload interface{}
	if nextBooking != nil {
		payload, payloadErr := buildWorkerBookingPayload(nextBooking)
		if payloadErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to complete booking"})
		}
		nextBookingPayload = payload
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"booking": booking, "next_booking": nextBookingPayload}})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/bookings/:bookingId/skip
func SkipQueueBookingCompatHandler(c fiber.Ctx) error {
	bookingID, err := strconv.ParseUint(c.Params("bookingId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid booking ID"})
	}

	var input struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	existingBooking, lookupErr := repositories.GetBookingByID(uint(bookingID))
	if lookupErr != nil || existingBooking == nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Booking not found"})
	}
	if session, sessionErr := repositories.GetQueueSessionByID(existingBooking.QueueSessionID); sessionErr == nil {
		if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
			return err
		}
	}

	workerID := c.Locals("user_id").(uint)
	booking, err := repositories.WorkerUpdateBooking(uint(bookingID), workerID, "no_show", nil, input.Reason)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to skip booking"})
	}

	if session, sessionErr := repositories.GetQueueSessionByID(booking.QueueSessionID); sessionErr == nil {
		logCourseActivity(c, session.CourseID, workerID, "skip_queue_booking", "queue", "queue_booking", booking.ID, session.Title, fiber.Map{"booking_type": booking.BookingType, "reason": input.Reason})
	}
	emitQueueBookingChanged(booking.QueueSessionID, "booking-skipped", booking)
	realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})
	nextBooking, assignErr := tryAssignNextBookingAndEmit(booking.QueueSessionID, workerID)
	if assignErr != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to skip booking"})
	}

	var nextBookingPayload interface{}
	if nextBooking != nil {
		payload, payloadErr := buildWorkerBookingPayload(nextBooking)
		if payloadErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to skip booking"})
		}
		nextBookingPayload = payload
	}

	return c.JSON(fiber.Map{"success": true, "message": "ข้ามคิวสำเร็จ", "data": fiber.Map{"booking": booking, "next_booking": nextBookingPayload}})
}

func queueLegacyError(c fiber.Ctx, status int, message string, extras ...fiber.Map) error {
	errorBody := fiber.Map{"message": message}
	if len(extras) > 0 {
		for key, value := range extras[0] {
			errorBody[key] = value
		}
	}

	return c.Status(status).JSON(fiber.Map{
		"success": false,
		"error":   errorBody,
	})
}

func queueCourseScopeMatches(c fiber.Ctx, courseID string) bool {
	courseParam := c.Params("courseId")
	return courseParam == "" || courseParam == courseID
}

func queueOptionalActorID(c fiber.Ctx) (uint, bool) {
	actor := c.Locals("user_id")
	if actor == nil {
		return 0, false
	}

	switch typed := actor.(type) {
	case uint:
		return typed, typed != 0
	case int:
		if typed > 0 {
			return uint(typed), true
		}
	case int64:
		if typed > 0 {
			return uint(typed), true
		}
	case float64:
		if typed > 0 {
			return uint(typed), true
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil && parsed > 0 {
			return uint(parsed), true
		}
	}

	return 0, false
}

func loadQueueSessionByPIN(pinCode string, statuses ...string) (*models.QueueSession, error) {
	var session models.QueueSession
	query := config.DB.Where("pin_code = ?", pinCode)
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if err := query.First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func loadQueueCourse(courseID string) (*models.Course, error) {
	var course models.Course
	if err := config.DB.First(&course, "id = ?", courseID).Error; err != nil {
		return nil, err
	}
	return &course, nil
}

func queueEnsureCourseWritable(c fiber.Ctx, courseID string) error {
	courseExists, isActive, err := repositories.GetCourseActiveState(courseID)
	if err != nil {
		return queueLegacyError(c, 500, "ไม่สามารถตรวจสอบสถานะรายวิชาได้")
	}
	if !courseExists {
		return queueLegacyError(c, 404, "ไม่พบรายวิชา")
	}
	if !isActive {
		return queueLegacyError(c, 403, "รายวิชานี้ถูกปิดแล้วและอนุญาตให้ดูข้อมูลได้อย่างเดียว")
	}
	return nil
}

func loadQueueClassroom(classroomID string) (*models.Classroom, error) {
	var classroom models.Classroom
	if err := config.DB.First(&classroom, "id = ?", classroomID).Error; err != nil {
		return nil, err
	}
	return &classroom, nil
}

func loadStudentByCode(studentCode string) (*models.Student, error) {
	var student models.Student
	if err := config.DB.Where("student_id = ?", studentCode).First(&student).Error; err != nil {
		return nil, err
	}
	return &student, nil
}

func isStudentEnrolledInCourse(courseID string, studentID uint) (bool, error) {
	var count int64
	err := config.DB.Table("course_section_students AS css").
		Joins("JOIN course_sections AS cs ON cs.id = css.course_section_id").
		Where("css.student_id = ? AND cs.course_id = ?", studentID, courseID).
		Count(&count).Error
	return count > 0, err
}

func hasQueueAttendanceEligibility(session *models.QueueSession, studentID uint) (bool, error) {
	if !session.RequireAttendance || session.LinkedAttendanceSessionID == nil {
		return true, nil
	}

	var count int64
	err := config.DB.Model(&models.AttendanceRecord{}).
		Where("attendance_session_id = ? AND student_id = ? AND status IN ?", *session.LinkedAttendanceSessionID, studentID, []string{"present", "late"}).
		Count(&count).Error
	return count > 0, err
}

func loadQueueAssignmentWithSubItems(assignmentID uint) (*models.Assignment, []models.AssignmentSubItem, error) {
	var assignment models.Assignment
	if err := config.DB.First(&assignment, assignmentID).Error; err != nil {
		return nil, nil, err
	}

	var subItems []models.AssignmentSubItem
	if err := config.DB.Where("assignment_id = ?", assignmentID).Order("order_index ASC, id ASC").Find(&subItems).Error; err != nil {
		return nil, nil, err
	}

	return &assignment, subItems, nil
}

func loadQueueDeskForStudent(classroomID string, deskNumber int) (*models.Desk, error) {
	var desk models.Desk
	err := config.DB.Where("classroom_id = ? AND number = ? AND is_enabled = ? AND type <> ?", classroomID, deskNumber, true, "teacher").First(&desk).Error
	if err != nil {
		return nil, err
	}
	return &desk, nil
}

func loadQueueDeskForValidation(classroomID string, deskNumber int) (*models.Desk, error) {
	var desk models.Desk
	err := config.DB.Where("classroom_id = ? AND number = ? AND is_enabled = ?", classroomID, deskNumber, true).First(&desk).Error
	if err != nil {
		return nil, err
	}
	return &desk, nil
}

func loadQueueDeskByNumber(classroomID string, deskNumber int) (*models.Desk, error) {
	var desk models.Desk
	err := config.DB.Where("classroom_id = ? AND number = ?", classroomID, deskNumber).First(&desk).Error
	if err != nil {
		return nil, err
	}
	return &desk, nil
}

func loadQueueDeskStatus(sessionID string, deskID string) (*models.QueueDeskStatus, error) {
	var status models.QueueDeskStatus
	err := config.DB.Where("queue_session_id = ? AND desk_id = ?", sessionID, deskID).First(&status).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &status, nil
}

func loadQueueZoneForDesk(classroomID string, desk *models.Desk) (*models.Zone, error) {
	var zone models.Zone
	err := config.DB.
		Where("classroom_id = ? AND ? >= x AND ? <= x + width AND ? >= y AND ? <= y + height", classroomID, desk.X, desk.X, desk.Y, desk.Y).
		Order("created_at ASC").
		First(&zone).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

type queueBookingValidationMode string

const (
	queueBookingValidationPreview queueBookingValidationMode = "preview"
	queueBookingValidationCreate  queueBookingValidationMode = "create"
)

type queueLegacyDeskNumber int

func (deskNumber *queueLegacyDeskNumber) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*deskNumber = 0
		return nil
	}

	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return err
		}
		raw = strings.TrimSpace(unquoted)
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return err
	}

	*deskNumber = queueLegacyDeskNumber(value)
	return nil
}

func queueLegacyBookingStudentResponse(student *models.Student) interface{} {
	if student == nil {
		return nil
	}

	return fiber.Map{
		"id":         student.ID,
		"student_id": student.StudentID,
		"full_name":  student.FullName,
	}
}

func queueLegacyBookingDeskResponse(desk *models.Desk) interface{} {
	if desk == nil {
		return nil
	}

	return fiber.Map{
		"id":     desk.ID,
		"number": desk.Number,
		"type":   desk.Type,
	}
}

func queueLegacyCreatedBookingResponse(booking *models.QueueBooking, sessionTitle string) fiber.Map {
	data := fiber.Map{
		"id":               booking.ID,
		"queue_session_id": booking.QueueSessionID,
		"student_id":       booking.StudentID,
		"desk_id":          booking.DeskID,
		"desk_number":      strconv.Itoa(booking.DeskNumber),
		"booking_type":     booking.BookingType,
		"queue_number":     booking.QueueNumber,
		"is_late_booking":  booking.IsLateBooking,
		"late_reason":      booking.LateReason,
		"status":           booking.Status,
		"updated_at":       booking.UpdatedAt,
		"created_at":       booking.CreatedAt,
		"session_title":    sessionTitle,
	}

	if strings.TrimSpace(booking.Note) != "" {
		data["note"] = booking.Note
	}

	return data
}

func queueBookingIsAfterCutoff(session *models.QueueSession, now time.Time) bool {
	if session == nil || !session.IsCutoffEnabled || session.CutoffAt == nil {
		return false
	}
	return now.After(*session.CutoffAt)
}

func queueBookingLateReason(session *models.QueueSession) string {
	if session == nil || !session.IsCutoffEnabled || session.CutoffAt == nil {
		return ""
	}
	if strings.TrimSpace(session.CutoffNote) != "" {
		return strings.TrimSpace(session.CutoffNote)
	}
	return fmt.Sprintf("จองหลัง cutoff เวลา %s", session.CutoffAt.Format("15:04"))
}

func queueDeskBookingPriority(status string) int {
	switch status {
	case "in_progress":
		return 2
	case "waiting":
		return 1
	default:
		return 0
	}
}

func shouldReplaceDeskBooking(current models.QueueBooking, candidate models.QueueBooking) bool {
	currentPriority := queueDeskBookingPriority(current.Status)
	candidatePriority := queueDeskBookingPriority(candidate.Status)
	if candidatePriority != currentPriority {
		return candidatePriority > currentPriority
	}
	if candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.ID > current.ID
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}

func loadActiveQueueBookingForStudent(sessionID string, studentID uint) (*models.QueueBooking, error) {
	var booking models.QueueBooking
	err := config.DB.Where("queue_session_id = ? AND student_id = ? AND status IN ?", sessionID, studentID, []string{"waiting", "in_progress"}).First(&booking).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func loadGradedAssignmentState(assignmentID uint, studentID uint) ([]uint, *models.Score, error) {
	var gradedSubItemIDs []uint
	if err := config.DB.Model(&models.Score{}).
		Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NOT NULL AND status = ?", assignmentID, studentID, "graded").
		Pluck("sub_item_id", &gradedSubItemIDs).Error; err != nil {
		return nil, nil, err
	}

	var singleScore models.Score
	err := config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NULL AND status = ?", assignmentID, studentID, "graded").First(&singleScore).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return gradedSubItemIDs, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	return gradedSubItemIDs, &singleScore, nil
}

func isQueueDeskFullyGraded(sessionID string, deskID string, assignmentID uint, subItems []models.AssignmentSubItem) (bool, error) {
	if len(subItems) == 0 {
		return true, nil
	}

	var studentIDs []uint
	if err := config.DB.Model(&models.QueueBooking{}).
		Where("queue_session_id = ? AND desk_id = ? AND booking_type = ? AND status = ?", sessionID, deskID, "grading", "completed").
		Distinct().
		Pluck("student_id", &studentIDs).Error; err != nil {
		return false, err
	}

	if len(studentIDs) == 0 {
		return true, nil
	}

	allSubItemIDs := make(map[uint]struct{}, len(subItems))
	for _, subItem := range subItems {
		allSubItemIDs[subItem.ID] = struct{}{}
	}

	for _, studentID := range studentIDs {
		var gradedIDs []uint
		if err := config.DB.Model(&models.Score{}).
			Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NOT NULL AND status = ?", assignmentID, studentID, "graded").
			Pluck("sub_item_id", &gradedIDs).Error; err != nil {
			return false, err
		}

		gradedSet := make(map[uint]struct{}, len(gradedIDs))
		for _, gradedID := range gradedIDs {
			gradedSet[gradedID] = struct{}{}
		}

		for subItemID := range allSubItemIDs {
			if _, ok := gradedSet[subItemID]; !ok {
				return false, nil
			}
		}
	}

	return true, nil
}

func loadLatestCompletedGradingBookingForDesk(sessionID string, deskID string) (*models.QueueBooking, error) {
	var booking models.QueueBooking
	err := config.DB.
		Where("queue_session_id = ? AND desk_id = ? AND booking_type = ? AND status = ?", sessionID, deskID, "grading", "completed").
		Order("completed_at DESC NULLS LAST, updated_at DESC, id DESC").
		First(&booking).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func isStudentFullyGradedForQueueAssignment(assignmentID uint, studentID uint, subItems []models.AssignmentSubItem) (bool, error) {
	gradedSubItemIDs, singleScore, err := loadGradedAssignmentState(assignmentID, studentID)
	if err != nil {
		return false, err
	}

	if len(subItems) == 0 {
		return singleScore != nil, nil
	}

	gradedSet := make(map[uint]struct{}, len(gradedSubItemIDs))
	for _, gradedID := range gradedSubItemIDs {
		gradedSet[gradedID] = struct{}{}
	}

	for _, subItem := range subItems {
		if _, ok := gradedSet[subItem.ID]; !ok {
			return false, nil
		}
	}

	return true, nil
}

func getStudentGradingRebookBlockReason(session *models.QueueSession, student *models.Student) (string, error) {
	if session == nil || student == nil || session.LinkedAssignmentID == nil {
		return "", nil
	}

	assignment, subItems, err := loadQueueAssignmentWithSubItems(*session.LinkedAssignmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	fullyGraded, err := isStudentFullyGradedForQueueAssignment(assignment.ID, student.ID, subItems)
	if err != nil {
		return "", err
	}
	if !fullyGraded {
		return "", nil
	}

	return "คุณได้รับการตรวจงานครบแล้ว ไม่สามารถจองคิวตรวจงานได้อีก", nil
}

func validateQueueBookingCompatibility(session *models.QueueSession, studentCode string, deskNumber int, bookingType string, mode queueBookingValidationMode) (*models.Student, *models.Desk, *models.QueueBooking, []fiber.Map, []fiber.Map, error) {
	var validationErrors []fiber.Map
	var warnings []fiber.Map

	var student *models.Student
	var desk *models.Desk
	var existingBooking *models.QueueBooking

	var assignment *models.Assignment
	var subItems []models.AssignmentSubItem
	if bookingType == "grading" && session.LinkedAssignmentID != nil {
		var err error
		assignment, subItems, err = loadQueueAssignmentWithSubItems(*session.LinkedAssignmentID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, nil, err
		}
	}

	if studentCode != "" {
		loadedStudent, err := loadStudentByCode(studentCode)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			validationErrors = append(validationErrors, fiber.Map{
				"field":   "student_id",
				"message": "ไม่พบรหัสนักศึกษานี้ในระบบ",
			})
		} else if err != nil {
			return nil, nil, nil, nil, nil, err
		} else {
			student = loadedStudent

			enrolled, err := isStudentEnrolledInCourse(session.CourseID, student.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			if !enrolled {
				validationErrors = append(validationErrors, fiber.Map{
					"field":   "student_id",
					"message": fmt.Sprintf("รหัสนักศึกษา %s ไม่ได้ลงทะเบียนในรายวิชานี้", studentCode),
				})
			}

			attendanceAllowed, err := hasQueueAttendanceEligibility(session, student.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			if !attendanceAllowed {
				message := "นักศึกษายังไม่ได้เช็คชื่อในรอบการเรียนนี้"
				if mode == queueBookingValidationCreate {
					message = "กรุณาเช็คชื่อก่อนจองคิว"
				}
				validationErrors = append(validationErrors, fiber.Map{
					"field":   "student_id",
					"message": message,
				})
			}

			existing, err := loadActiveQueueBookingForStudent(session.ID, student.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			if existing != nil {
				existingBooking = existing
				warnings = append(warnings, fiber.Map{
					"field":   "student_id",
					"message": fmt.Sprintf("นักศึกษามีการจองคิวอยู่แล้ว (คิวที่ %d)", existing.QueueNumber),
					"existing_booking": fiber.Map{
						"id":           existing.ID,
						"queue_number": existing.QueueNumber,
						"booking_type": existing.BookingType,
						"status":       existing.Status,
					},
				})
			}

			if assignment != nil {
				gradedSubItemIDs, singleScore, err := loadGradedAssignmentState(assignment.ID, student.ID)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}

				if len(subItems) > 0 {
					gradedSet := make(map[uint]struct{}, len(gradedSubItemIDs))
					for _, gradedID := range gradedSubItemIDs {
						gradedSet[gradedID] = struct{}{}
					}

					allGraded := true
					for _, subItem := range subItems {
						if _, ok := gradedSet[subItem.ID]; !ok {
							allGraded = false
							break
						}
					}

					if allGraded {
						message := "นักศึกษาได้รับการตรวจครบทุกข้อแล้ว ไม่สามารถจองคิวตรวจงานได้อีก"
						if mode == queueBookingValidationCreate {
							message = "คุณได้รับการตรวจครบทุกข้อแล้ว ไม่สามารถจองคิวตรวจงานได้อีก"
						}
						validationErrors = append(validationErrors, fiber.Map{
							"field":   "student_id",
							"message": message,
						})
					} else if len(gradedSubItemIDs) > 0 {
						warningMessage := fmt.Sprintf("นักศึกษาได้รับการตรวจไปแล้ว %d/%d ข้อ", len(gradedSubItemIDs), len(subItems))
						if mode == queueBookingValidationPreview {
							remainingCount := len(subItems) - len(gradedSubItemIDs)
							warningMessage = fmt.Sprintf("นักศึกษาได้รับการตรวจไปแล้ว %d/%d ข้อ (เหลืออีก %d ข้อ)", len(gradedSubItemIDs), len(subItems), remainingCount)
						}
						warnings = append(warnings, fiber.Map{
							"field":   "student_id",
							"message": warningMessage,
						})
					}
				} else if singleScore != nil {
					message := fmt.Sprintf("นักศึกษาได้รับการตรวจงานแล้ว (%.2f คะแนน) ไม่สามารถจองคิวตรวจงานได้อีก", singleScore.Score)
					if mode == queueBookingValidationCreate {
						message = "คุณได้รับการตรวจงานแล้ว ไม่สามารถจองคิวตรวจงานได้อีก"
					}
					validationErrors = append(validationErrors, fiber.Map{
						"field":   "student_id",
						"message": message,
					})
				}
			}
		}
	}

	if deskNumber > 0 {
		if mode == queueBookingValidationPreview {
			loadedDesk, err := loadQueueDeskForValidation(session.ClassroomID, deskNumber)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				existingDesk, existingErr := loadQueueDeskByNumber(session.ClassroomID, deskNumber)
				if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
					return nil, nil, nil, nil, nil, existingErr
				}

				message := fmt.Sprintf("ไม่พบโต๊ะหมายเลข %d ในห้องนี้", deskNumber)
				if existingDesk != nil && !existingDesk.IsEnabled {
					message = fmt.Sprintf("โต๊ะหมายเลข %d ถูกปิดใช้งาน", deskNumber)
				}

				validationErrors = append(validationErrors, fiber.Map{
					"field":   "desk_number",
					"message": message,
				})
			} else if err != nil {
				return nil, nil, nil, nil, nil, err
			} else {
				desk = loadedDesk

				deskStatus, err := loadQueueDeskStatus(session.ID, desk.ID)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}

				if deskStatus != nil && bookingType == "grading" {
					if deskStatus.GradingStatus == "completed" {
						completedBooking, err := loadLatestCompletedGradingBookingForDesk(session.ID, desk.ID)
						if err != nil {
							return nil, nil, nil, nil, nil, err
						}

						lockedToCurrentStudent := student != nil && completedBooking != nil && completedBooking.StudentID == student.ID
						deskFullyGraded := assignment == nil || len(subItems) == 0
						if completedBooking != nil && assignment != nil {
							deskFullyGraded, err = isStudentFullyGradedForQueueAssignment(assignment.ID, completedBooking.StudentID, subItems)
							if err != nil {
								return nil, nil, nil, nil, nil, err
							}
						}

						switch {
						case completedBooking == nil:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": fmt.Sprintf("โต๊ะหมายเลข %d ถูกล็อกหลังการตรวจสำเร็จแล้ว", deskNumber),
							})
						case lockedToCurrentStudent && !deskFullyGraded:
							warnings = append(warnings, fiber.Map{
								"field":   "desk_number",
								"message": fmt.Sprintf("โต๊ะหมายเลข %d ถูกล็อกไว้สำหรับนักศึกษาคนเดิมจนกว่าจะตรวจครบทุกข้อ", deskNumber),
							})
						case deskFullyGraded:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": fmt.Sprintf("โต๊ะหมายเลข %d ได้รับการตรวจครบแล้วและถูกล็อกไว้", deskNumber),
							})
						default:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": fmt.Sprintf("โต๊ะหมายเลข %d ถูกล็อกไว้สำหรับนักศึกษาคนเดิมที่ยังตรวจไม่ครบ", deskNumber),
							})
						}
					} else if deskStatus.GradingStatus == "waiting" || deskStatus.GradingStatus == "in_progress" {
						validationErrors = append(validationErrors, fiber.Map{
							"field":   "desk_number",
							"message": fmt.Sprintf("โต๊ะหมายเลข %d มีการจองตรวจงานอยู่แล้ว", deskNumber),
						})
					}
				}
			}
		} else {
			loadedDesk, err := loadQueueDeskForStudent(session.ClassroomID, deskNumber)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				validationErrors = append(validationErrors, fiber.Map{
					"field":   "desk_number",
					"message": "ไม่พบโต๊ะหมายเลขนี้ (โต๊ะอาจารย์ไม่สามารถจองได้)",
				})
			} else if err != nil {
				return nil, nil, nil, nil, nil, err
			} else {
				desk = loadedDesk

				deskStatus, err := loadQueueDeskStatus(session.ID, desk.ID)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}

				if deskStatus != nil && bookingType == "grading" {
					if deskStatus.GradingStatus == "waiting" || deskStatus.GradingStatus == "in_progress" {
						validationErrors = append(validationErrors, fiber.Map{
							"field":   "desk_number",
							"message": "โต๊ะนี้มีการจองตรวจงานอยู่แล้ว",
						})
					} else if deskStatus.GradingStatus == "completed" {
						completedBooking, err := loadLatestCompletedGradingBookingForDesk(session.ID, desk.ID)
						if err != nil {
							return nil, nil, nil, nil, nil, err
						}

						lockedToCurrentStudent := student != nil && completedBooking != nil && completedBooking.StudentID == student.ID
						deskFullyGraded := assignment == nil || len(subItems) == 0
						if completedBooking != nil && assignment != nil {
							deskFullyGraded, err = isStudentFullyGradedForQueueAssignment(assignment.ID, completedBooking.StudentID, subItems)
							if err != nil {
								return nil, nil, nil, nil, nil, err
							}
						}

						switch {
						case completedBooking == nil:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": "โต๊ะนี้ถูกล็อกหลังการตรวจสำเร็จแล้ว",
							})
						case lockedToCurrentStudent && !deskFullyGraded:
							warnings = append(warnings, fiber.Map{
								"field":   "desk_number",
								"message": "โต๊ะนี้ถูกล็อกไว้สำหรับคุณจนกว่าจะตรวจครบทุกข้อ",
							})
						case deskFullyGraded:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": "โต๊ะนี้ได้รับการตรวจครบทุกข้อแล้วและถูกล็อกไว้",
							})
						default:
							validationErrors = append(validationErrors, fiber.Map{
								"field":   "desk_number",
								"message": "โต๊ะนี้ถูกล็อกไว้สำหรับนักศึกษาคนเดิมที่ยังตรวจไม่ครบ",
							})
						}
					}
				}
			}
		}
	}

	return student, desk, existingBooking, validationErrors, warnings, nil
}

func buildQueueScoreDetails(booking *models.QueueBooking, session *models.QueueSession) (fiber.Map, error) {
	if booking.Status != "completed" || session.LinkedAssignmentID == nil {
		return nil, nil
	}

	assignment, subItems, err := loadQueueAssignmentWithSubItems(*session.LinkedAssignmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if len(subItems) > 0 {
		var scores []models.Score
		if err := config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NOT NULL", assignment.ID, booking.StudentID).Order("sub_item_id ASC").Find(&scores).Error; err != nil {
			return nil, err
		}
		if len(scores) == 0 {
			return nil, nil
		}

		subItemMap := make(map[uint]models.AssignmentSubItem, len(subItems))
		totalMaxScore := 0.0
		for _, subItem := range subItems {
			subItemMap[subItem.ID] = subItem
			totalMaxScore += subItem.MaxScore
		}

		graderNames := map[uint]string{}
		graderIDs := make([]uint, 0)
		graderSet := map[uint]struct{}{}
		for _, score := range scores {
			if score.GradedBy != nil {
				if _, ok := graderSet[*score.GradedBy]; !ok {
					graderSet[*score.GradedBy] = struct{}{}
					graderIDs = append(graderIDs, *score.GradedBy)
				}
			}
		}

		if len(graderIDs) > 0 {
			var graders []models.User
			if err := config.DB.Where("id IN ?", graderIDs).Find(&graders).Error; err != nil {
				return nil, err
			}
			for _, grader := range graders {
				graderNames[grader.ID] = grader.FullName
			}
		}

		totalScore := 0.0
		items := make([]fiber.Map, 0, len(scores))
		for _, score := range scores {
			totalScore += score.Score
			item := fiber.Map{
				"id":        score.SubItemID,
				"score":     score.Score,
				"graded_at": score.GradedAt,
				"graded_by": nil,
				"max_score": nil,
				"name":      nil,
			}
			if score.SubItemID != nil {
				if subItem, ok := subItemMap[*score.SubItemID]; ok {
					item["id"] = subItem.ID
					item["name"] = subItem.Name
					item["max_score"] = subItem.MaxScore
				}
			}
			if score.GradedBy != nil {
				item["graded_by"] = graderNames[*score.GradedBy]
			}
			items = append(items, item)
		}

		return fiber.Map{
			"type":            "sub_items",
			"assignment_name": assignment.Name,
			"sub_items":       items,
			"total_score":     totalScore,
			"total_max_score": totalMaxScore,
		}, nil
	}

	var score models.Score
	err = config.DB.Where("assignment_id = ? AND student_id = ? AND sub_item_id IS NULL", assignment.ID, booking.StudentID).First(&score).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var graderName *string
	if score.GradedBy != nil {
		var grader models.User
		if err := config.DB.First(&grader, *score.GradedBy).Error; err == nil {
			graderName = &grader.FullName
		}
	}

	return fiber.Map{
		"type":            "single",
		"assignment_name": assignment.Name,
		"score":           score.Score,
		"max_score":       assignment.MaxScore,
		"graded_by":       graderName,
		"graded_at":       score.GradedAt,
		"comment":         score.Comment,
	}, nil
}

func updateQueueSessionStatusCompat(session *models.QueueSession, targetStatus string, allowDraft bool) error {
	validTransitions := map[string][]string{
		"draft":  {"active"},
		"active": {"paused", "closed"},
		"paused": {"active", "closed"},
		"closed": {},
	}

	allowedTargets, ok := validTransitions[session.Status]
	if !ok {
		return fmt.Errorf("ไม่สามารถเปลี่ยนสถานะจาก %s เป็น %s", session.Status, targetStatus)
	}

	transitionAllowed := false
	for _, candidate := range allowedTargets {
		if candidate == targetStatus {
			transitionAllowed = true
			break
		}
	}

	if !transitionAllowed {
		return fmt.Errorf("ไม่สามารถเปลี่ยนสถานะจาก %s เป็น %s", session.Status, targetStatus)
	}

	switch targetStatus {
	case "active":
		// Block start/resume if another session is already active in the same classroom.
		if err := repositories.CheckActiveQueueSessionForClassroom(session.ClassroomID, session.ID); err != nil {
			return err
		}
		if session.Status == "draft" {
			if !allowDraft {
				return fmt.Errorf("ไม่สามารถเปลี่ยนสถานะจาก %s เป็น %s", session.Status, targetStatus)
			}
			return repositories.StartQueueSession(session.ID, session.ClassroomID)
		}
		return repositories.ResumeQueueSession(session.ID)
	case "paused":
		return repositories.PauseQueueSession(session.ID)
	case "closed":
		return repositories.CloseQueueSession(session.ID)
	default:
		return fmt.Errorf("สถานะ %s ไม่รองรับ", targetStatus)
	}
}

// POST /api/queue/verify-pin
func VerifyQueuePINPublicHandler(c fiber.Ctx) error {
	var input struct {
		PinCode string `json:"pin_code"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.PinCode == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := loadQueueSessionByPIN(input.PinCode, "active", "paused")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}
	if session.Status == "paused" {
		return queueLegacyError(c, 400, "ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่", fiber.Map{"code": "SESSION_PAUSED"})
	}

	course, err := loadQueueCourse(session.CourseID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 500, err.Error())
	}

	classroom, err := loadQueueClassroom(session.ClassroomID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 500, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id":         session.ID,
			"title":              session.Title,
			"course":             course,
			"classroom":          classroom,
			"require_attendance": session.RequireAttendance,
			"is_cutoff_enabled":  session.IsCutoffEnabled,
			"cutoff_at":          session.CutoffAt,
			"cutoff_note":        session.CutoffNote,
		},
	})
}

// POST /api/queue/validate
func ValidateQueueBookingInfoPublicHandler(c fiber.Ctx) error {
	var input struct {
		PinCode     string                `json:"pin_code"`
		StudentCode string                `json:"student_id"`
		DeskNumber  queueLegacyDeskNumber `json:"desk_number"`
		BookingType string                `json:"booking_type"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.PinCode == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := loadQueueSessionByPIN(input.PinCode, "active", "paused")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}
	if session.Status == "paused" {
		return queueLegacyError(c, 400, "ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่", fiber.Map{"code": "SESSION_PAUSED"})
	}

	student, desk, _, validationErrors, warnings, err := validateQueueBookingCompatibility(session, input.StudentCode, int(input.DeskNumber), input.BookingType, queueBookingValidationPreview)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	gradingAllowed := true
	gradingReason := ""
	if reason, err := getStudentGradingRebookBlockReason(session, student); err != nil {
		return queueLegacyError(c, 500, err.Error())
	} else if reason != "" {
		gradingAllowed = false
		gradingReason = reason
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"valid":                   len(validationErrors) == 0,
			"errors":                  validationErrors,
			"warnings":                warnings,
			"student":                 queueLegacyBookingStudentResponse(student),
			"desk":                    queueLegacyBookingDeskResponse(desk),
			"is_cutoff_enabled":       session.IsCutoffEnabled,
			"cutoff_at":               session.CutoffAt,
			"cutoff_note":             session.CutoffNote,
			"is_late_booking_preview": queueBookingIsAfterCutoff(session, time.Now()),
			"late_reason_preview":     queueBookingLateReason(session),
			"booking_type_availability": fiber.Map{
				"grading": fiber.Map{
					"allowed": gradingAllowed,
					"reason":  gradingReason,
				},
				"help": fiber.Map{
					"allowed": true,
					"reason":  "",
				},
			},
		},
	})
}

// POST /api/queue/check-existing
func CheckExistingQueueBookingPublicHandler(c fiber.Ctx) error {
	var input struct {
		PinCode     string `json:"pin_code"`
		StudentCode string `json:"student_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.PinCode == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := loadQueueSessionByPIN(input.PinCode, "active")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว")
	}

	student, err := loadStudentByCode(input.StudentCode)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"has_booking": false}})
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	booking, err := loadActiveQueueBookingForStudent(session.ID, student.ID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if booking == nil {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"has_booking": false}})
	}

	var waitingAhead int64
	if err := config.DB.Model(&models.QueueBooking{}).
		Where("queue_session_id = ? AND status = ? AND created_at < ?", session.ID, "waiting", booking.CreatedAt).
		Count(&waitingAhead).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"has_booking": true,
			"booking": fiber.Map{
				"id":                booking.ID,
				"queue_number":      booking.QueueNumber,
				"booking_type":      booking.BookingType,
				"desk_number":       booking.DeskNumber,
				"status":            booking.Status,
				"position_in_queue": waitingAhead,
			},
		},
	})
}

// POST /api/queue/bookings
func CreateQueueBookingPublicHandler(c fiber.Ctx) error {
	var input struct {
		PinCode     string                `json:"pin_code"`
		StudentCode string                `json:"student_id"`
		DeskNumber  queueLegacyDeskNumber `json:"desk_number"`
		BookingType string                `json:"booking_type"`
		Note        string                `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.PinCode == "" || input.StudentCode == "" || int(input.DeskNumber) == 0 || input.BookingType == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := loadQueueSessionByPIN(input.PinCode, "active", "paused")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบการจองคิวที่เปิดอยู่ หรือ PIN ไม่ถูกต้อง")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบการจองคิวที่เปิดอยู่ หรือ PIN ไม่ถูกต้อง")
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}
	if session.Status == "paused" {
		return queueLegacyError(c, 400, "ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่", fiber.Map{"code": "SESSION_PAUSED"})
	}

	student, desk, existingBooking, validationErrors, _, err := validateQueueBookingCompatibility(session, input.StudentCode, int(input.DeskNumber), input.BookingType, queueBookingValidationCreate)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if existingBooking != nil {
		return queueLegacyError(c, 400, "คุณมีคิวที่รออยู่แล้ว กรุณารอให้ TA ดำเนินการเสร็จก่อน")
	}
	if len(validationErrors) > 0 {
		message, _ := validationErrors[0]["message"].(string)
		if message == "" {
			message = "ข้อมูลไม่ถูกต้อง"
		}
		status := 400
		if message == "ไม่พบรหัสนักศึกษานี้ในระบบ" || message == "ไม่พบโต๊ะหมายเลขนี้ (โต๊ะอาจารย์ไม่สามารถจองได้)" {
			status = 404
		}
		return queueLegacyError(c, status, message)
	}
	if student == nil || desk == nil {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	booking, err := repositories.CreateBooking(session.ID, repositories.BookingInput{
		StudentID:   student.ID,
		DeskID:      desk.ID,
		DeskNumber:  int(input.DeskNumber),
		BookingType: input.BookingType,
		Note:        input.Note,
		BookingIP:   strings.TrimSpace(c.IP()),
		UserAgent:   strings.TrimSpace(c.Get("User-Agent")),
		Device:      queueBookingDeviceLabel(c.Get("User-Agent")),
		IsLate:      queueBookingIsAfterCutoff(session, time.Now()),
		LateReason:  queueBookingLateReason(session),
	})
	if err != nil {
		return queueLegacyError(c, 400, err.Error())
	}
	emitQueueBookingChanged(session.ID, "new-booking", booking)

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"data":    queueLegacyCreatedBookingResponse(booking, session.Title),
	})
}

// GET /api/queue/bookings/:bookingId/status
func GetQueueBookingStatusPublicHandler(c fiber.Ctx) error {
	bookingID, err := strconv.ParseUint(c.Params("bookingId"), 10, 64)
	if err != nil {
		return queueLegacyError(c, 400, "รหัสการจองไม่ถูกต้อง")
	}

	booking, err := repositories.GetBookingByID(uint(bookingID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบข้อมูลการจอง")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	session, err := repositories.GetQueueSessionByID(booking.QueueSessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบข้อมูลการจอง")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบข้อมูลการจอง")
	}

	var student models.Student
	if err := config.DB.First(&student, booking.StudentID).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var desk models.Desk
	if err := config.DB.First(&desk, "id = ?", booking.DeskID).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var assignedWorker *models.User
	if booking.AssignedWorkerID != nil {
		var worker models.User
		if err := config.DB.First(&worker, *booking.AssignedWorkerID).Error; err == nil {
			assignedWorker = &worker
		}
	}

	var waitingAhead int64
	if err := config.DB.Model(&models.QueueBooking{}).
		Where("queue_session_id = ? AND status = ? AND created_at < ?", booking.QueueSessionID, "waiting", booking.CreatedAt).
		Count(&waitingAhead).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	scoreDetails, err := buildQueueScoreDetails(booking, session)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	zone, err := loadQueueZoneForDesk(session.ClassroomID, &desk)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	data := fiber.Map{
		"id":                 booking.ID,
		"queue_session_id":   booking.QueueSessionID,
		"student_id":         booking.StudentID,
		"desk_id":            booking.DeskID,
		"desk_number":        booking.DeskNumber,
		"booking_type":       booking.BookingType,
		"queue_number":       booking.QueueNumber,
		"is_late_booking":    booking.IsLateBooking,
		"late_reason":        booking.LateReason,
		"note":               booking.Note,
		"status":             booking.Status,
		"assigned_worker_id": booking.AssignedWorkerID,
		"assigned_at":        booking.AssignedAt,
		"started_at":         booking.StartedAt,
		"completed_at":       booking.CompletedAt,
		"score":              booking.Score,
		"worker_note":        booking.WorkerNote,
		"created_at":         booking.CreatedAt,
		"updated_at":         booking.UpdatedAt,
		"session": fiber.Map{
			"id":                   session.ID,
			"title":                session.Title,
			"status":               session.Status,
			"linked_assignment_id": session.LinkedAssignmentID,
			"is_cutoff_enabled":    session.IsCutoffEnabled,
			"cutoff_at":            session.CutoffAt,
			"cutoff_note":          session.CutoffNote,
			"classroom_id":         session.ClassroomID,
		},
		"student": fiber.Map{
			"id":         student.ID,
			"student_id": student.StudentID,
			"full_name":  student.FullName,
		},
		"desk": fiber.Map{
			"id":     desk.ID,
			"number": desk.Number,
			"x":      desk.X,
			"y":      desk.Y,
		},
		"position_in_queue": waitingAhead + 1,
		"score_details":     scoreDetails,
		"zone":              zone,
	}

	if assignedWorker != nil && booking.Status != "waiting" {
		data["assignedWorker"] = fiber.Map{
			"id":        assignedWorker.ID,
			"full_name": assignedWorker.FullName,
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": data})
}

// POST /api/queue/bookings/:bookingId/cancel
func CancelQueueBookingPublicHandler(c fiber.Ctx) error {
	bookingID, err := strconv.ParseUint(c.Params("bookingId"), 10, 64)
	if err != nil {
		return queueLegacyError(c, 400, "รหัสการจองไม่ถูกต้อง")
	}

	booking, err := repositories.GetBookingByID(uint(bookingID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบข้อมูลการจอง")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	session, err := repositories.GetQueueSessionByID(booking.QueueSessionID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 500, err.Error())
	}
	if session != nil && !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบข้อมูลการจอง")
	}
	if session != nil {
		if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
			return err
		}
	}

	if booking.Status != "waiting" {
		return queueLegacyError(c, 400, "ไม่สามารถยกเลิกได้ เนื่องจากถึงคิวแล้วหรือดำเนินการเสร็จสิ้นแล้ว")
	}

	now := time.Now()
	updates := map[string]interface{}{
		"status":       "cancelled",
		"completed_at": &now,
	}
	if err := config.DB.Model(&models.QueueBooking{}).Where("id = ?", booking.ID).Updates(updates).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	if booking.BookingType == "grading" {
		resetStatus := map[string]interface{}{
			"grading_status":     "not_started",
			"grading_booking_id": nil,
		}
		if err := config.DB.Model(&models.QueueDeskStatus{}).Where("queue_session_id = ? AND desk_id = ?", booking.QueueSessionID, booking.DeskID).Updates(resetStatus).Error; err != nil {
			return queueLegacyError(c, 500, err.Error())
		}
	} else {
		if err := repositories.SyncHelpDeskStatus(booking.QueueSessionID, booking.DeskID); err != nil {
			return queueLegacyError(c, 500, err.Error())
		}
	}

	booking.Status = "cancelled"
	booking.CompletedAt = &now
	emitQueueBookingChanged(booking.QueueSessionID, "booking-cancelled", booking)
	realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})

	return c.JSON(fiber.Map{
		"success": true,
		"message": "ยกเลิกการจองสำเร็จ",
		"data":    booking,
	})
}

// GET /api/queue/sessions/:sessionId/desk-statuses
func GetQueueDeskStatusesPublicHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}

	classroom, err := loadQueueClassroom(session.ClassroomID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var desks []models.Desk
	if err := config.DB.Where("classroom_id = ? AND (is_enabled = ? OR type = ?)", session.ClassroomID, true, "teacher").Order("number ASC").Find(&desks).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var deskStatuses []models.QueueDeskStatus
	if err := config.DB.Where("queue_session_id = ?", sessionID).Find(&deskStatuses).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var activeBookings []models.QueueBooking
	if err := config.DB.Where("queue_session_id = ? AND status IN ? AND desk_id <> ''", sessionID, []string{"waiting", "in_progress"}).Order("created_at DESC").Find(&activeBookings).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	var completedGradingBookings []models.QueueBooking
	if err := config.DB.Where("queue_session_id = ? AND booking_type = ? AND status = ? AND desk_id <> ''", sessionID, "grading", "completed").
		Order("completed_at DESC NULLS LAST, updated_at DESC, id DESC").
		Find(&completedGradingBookings).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	studentIDs := make([]uint, 0)
	studentSet := map[uint]struct{}{}
	for _, booking := range activeBookings {
		if _, ok := studentSet[booking.StudentID]; !ok {
			studentSet[booking.StudentID] = struct{}{}
			studentIDs = append(studentIDs, booking.StudentID)
		}
	}
	for _, booking := range completedGradingBookings {
		if _, ok := studentSet[booking.StudentID]; !ok {
			studentSet[booking.StudentID] = struct{}{}
			studentIDs = append(studentIDs, booking.StudentID)
		}
	}

	studentMap := map[uint]models.Student{}
	if len(studentIDs) > 0 {
		var students []models.Student
		if err := config.DB.Where("id IN ?", studentIDs).Find(&students).Error; err != nil {
			return queueLegacyError(c, 500, err.Error())
		}
		for _, student := range students {
			studentMap[student.ID] = student
		}
	}

	workerIDs := make([]uint, 0)
	workerSet := map[uint]struct{}{}
	for _, booking := range activeBookings {
		if booking.AssignedWorkerID == nil {
			continue
		}
		if _, ok := workerSet[*booking.AssignedWorkerID]; ok {
			continue
		}
		workerSet[*booking.AssignedWorkerID] = struct{}{}
		workerIDs = append(workerIDs, *booking.AssignedWorkerID)
	}
	for _, booking := range completedGradingBookings {
		if booking.AssignedWorkerID == nil {
			continue
		}
		if _, ok := workerSet[*booking.AssignedWorkerID]; ok {
			continue
		}
		workerSet[*booking.AssignedWorkerID] = struct{}{}
		workerIDs = append(workerIDs, *booking.AssignedWorkerID)
	}

	workerMap := map[uint]models.User{}
	if len(workerIDs) > 0 {
		var workers []models.User
		if err := config.DB.Select("id", "full_name", "avatar").Where("id IN ?", workerIDs).Find(&workers).Error; err != nil {
			return queueLegacyError(c, 500, err.Error())
		}
		for _, worker := range workers {
			workerMap[worker.ID] = worker
		}
	}

	selectedDeskBookings := map[string]models.QueueBooking{}
	helpDeskBookings := map[string]models.QueueBooking{}
	gradingDeskBookings := map[string]models.QueueBooking{}
	completedDeskBookings := map[string]models.QueueBooking{}
	for _, booking := range activeBookings {
		if current, exists := selectedDeskBookings[booking.DeskID]; !exists || shouldReplaceDeskBooking(current, booking) {
			selectedDeskBookings[booking.DeskID] = booking
		}

		switch booking.BookingType {
		case "help":
			if current, exists := helpDeskBookings[booking.DeskID]; !exists || shouldReplaceDeskBooking(current, booking) {
				helpDeskBookings[booking.DeskID] = booking
			}
		case "grading":
			if current, exists := gradingDeskBookings[booking.DeskID]; !exists || shouldReplaceDeskBooking(current, booking) {
				gradingDeskBookings[booking.DeskID] = booking
			}
		}
	}
	for _, booking := range completedGradingBookings {
		if _, exists := completedDeskBookings[booking.DeskID]; !exists {
			completedDeskBookings[booking.DeskID] = booking
		}
	}

	bookingMap := map[string]fiber.Map{}
	for deskID, booking := range selectedDeskBookings {
		student := studentMap[booking.StudentID]
		entry := fiber.Map{
			"id":           booking.ID,
			"queue_number": booking.QueueNumber,
			"booking_type": booking.BookingType,
			"status":       booking.Status,
			"student_name": student.FullName,
			"student_code": student.StudentID,
		}
		if booking.Status == "in_progress" && booking.AssignedWorkerID != nil {
			if worker, ok := workerMap[*booking.AssignedWorkerID]; ok {
				entry["assigned_worker"] = fiber.Map{
					"id":        worker.ID,
					"full_name": worker.FullName,
					"avatar":    worker.Avatar,
				}
			}
		}
		bookingMap[deskID] = entry
	}
	for deskID, booking := range completedDeskBookings {
		if _, exists := bookingMap[deskID]; exists {
			continue
		}
		student := studentMap[booking.StudentID]
		entry := fiber.Map{
			"id":           booking.ID,
			"queue_number": booking.QueueNumber,
			"booking_type": booking.BookingType,
			"status":       booking.Status,
			"student_name": student.FullName,
			"student_code": student.StudentID,
		}
		if booking.AssignedWorkerID != nil {
			if worker, ok := workerMap[*booking.AssignedWorkerID]; ok {
				entry["assigned_worker"] = fiber.Map{
					"id":        worker.ID,
					"full_name": worker.FullName,
					"avatar":    worker.Avatar,
				}
			}
		}
		bookingMap[deskID] = entry
	}

	statusMap := map[string]models.QueueDeskStatus{}
	for _, deskStatus := range deskStatuses {
		statusMap[deskStatus.DeskID] = deskStatus
	}

	desksWithStatus := make([]fiber.Map, 0, len(desks))
	for _, desk := range desks {
		status := fiber.Map{
			"grading_status": "not_started",
			"help_status":    "none",
		}
		if deskStatus, ok := statusMap[desk.ID]; ok {
			status = fiber.Map{
				"id":                 deskStatus.ID,
				"queue_session_id":   deskStatus.QueueSessionID,
				"desk_id":            deskStatus.DeskID,
				"grading_status":     deskStatus.GradingStatus,
				"grading_booking_id": deskStatus.GradingBookingID,
				"help_status":        deskStatus.HelpStatus,
				"help_booking_id":    deskStatus.HelpBookingID,
				"updated_at":         deskStatus.UpdatedAt,
			}
			if deskStatus.GradingStatus == "completed" {
				if completedBooking, ok := completedDeskBookings[desk.ID]; ok {
					status["grading_booking_id"] = completedBooking.ID
				} else {
					status["grading_status"] = "not_started"
					status["grading_booking_id"] = nil
				}
			}
		}
		if helpBooking, ok := helpDeskBookings[desk.ID]; ok {
			status["help_status"] = helpBooking.Status
			status["help_booking_id"] = helpBooking.ID
		}
		if gradingBooking, ok := gradingDeskBookings[desk.ID]; ok {
			status["grading_status"] = gradingBooking.Status
			status["grading_booking_id"] = gradingBooking.ID
		}

		desksWithStatus = append(desksWithStatus, fiber.Map{
			"id":           desk.ID,
			"classroom_id": desk.ClassroomID,
			"number":       desk.Number,
			"x":            desk.X,
			"y":            desk.Y,
			"type":         desk.Type,
			"is_enabled":   desk.IsEnabled,
			"created_at":   desk.CreatedAt,
			"updated_at":   desk.UpdatedAt,
			"status":       status,
			"booking":      bookingMap[desk.ID],
		})
	}

	var queueCounts []struct {
		BookingType string
		Count       int64
	}
	if err := config.DB.Model(&models.QueueBooking{}).
		Select("booking_type, COUNT(id) AS count").
		Where("queue_session_id = ? AND status = ?", sessionID, "waiting").
		Group("booking_type").
		Scan(&queueCounts).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	queueStats := fiber.Map{"grading_waiting": int64(0), "help_waiting": int64(0)}
	for _, row := range queueCounts {
		queueStats[fmt.Sprintf("%s_waiting", row.BookingType)] = row.Count
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session": fiber.Map{
				"id":                session.ID,
				"course_id":         session.CourseID,
				"title":             session.Title,
				"pin_code":          session.PinCode,
				"status":            session.Status,
				"is_cutoff_enabled": session.IsCutoffEnabled,
				"cutoff_at":         session.CutoffAt,
				"cutoff_note":       session.CutoffNote,
			},
			"classroom": fiber.Map{
				"id":       classroom.ID,
				"name":     classroom.Name,
				"building": classroom.Building,
			},
			"desks":      desksWithStatus,
			"queueStats": queueStats,
		},
	})
}

// POST /api/queue/sessions/:sessionId/status
func UpdateQueueSessionStatusPublicHandler(c fiber.Ctx) error {
	var input struct {
		Status string `json:"status"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.Status == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}
	if input.Status != "active" && input.Status != "paused" {
		return queueLegacyError(c, 400, "ใช้ได้เฉพาะ active หรือ paused เท่านั้น")
	}

	session, err := repositories.GetQueueSessionByID(c.Params("sessionId"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}

	if err := updateQueueSessionStatusCompat(session, input.Status, false); err != nil {
		var conflictErr *repositories.ClassroomConflictError
		if errors.As(err, &conflictErr) {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"message": conflictErr.Error(),
				"classroom_conflict": fiber.Map{
					"session_id":    conflictErr.SessionID,
					"session_title": conflictErr.SessionTitle,
					"course_id":     conflictErr.CourseID,
					"course_name":   conflictErr.CourseName,
					"started_at":    conflictErr.StartedAt,
				},
			})
		}
		return queueLegacyError(c, 400, err.Error())
	}

	updatedSession, err := repositories.GetQueueSessionByID(session.ID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if actorID, ok := queueOptionalActorID(c); ok {
		logCourseActivity(c, updatedSession.CourseID, actorID, "update_queue_session_status", "queue", "queue_session", updatedSession.ID, updatedSession.Title, fiber.Map{"status": updatedSession.Status, "source": "projector"})
	}
	realtime.EmitToQueue(updatedSession.ID, "session-status-changed", fiber.Map{"status": updatedSession.Status, "session": updatedSession, "timestamp": time.Now().UnixMilli()})

	return c.JSON(fiber.Map{"success": true, "data": updatedSession})
}

// POST /api/queue/sessions/:sessionId/cutoff
func UpdateQueueSessionCutoffPublicHandler(c fiber.Ctx) error {
	var input struct {
		IsCutoffEnabled bool `json:"is_cutoff_enabled"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := repositories.GetQueueSessionByID(c.Params("sessionId"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}

	updates := map[string]interface{}{
		"is_cutoff_enabled": input.IsCutoffEnabled,
	}
	if input.IsCutoffEnabled {
		if session.CutoffAt == nil {
			now := time.Now()
			updates["cutoff_at"] = &now
		}
	} else {
		updates["cutoff_at"] = nil
	}

	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", session.ID).Updates(updates).Error; err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	updatedSession, err := repositories.GetQueueSessionByID(session.ID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}

	realtime.EmitToQueue(updatedSession.ID, "session-cutoff-changed", fiber.Map{
		"session_id":        updatedSession.ID,
		"is_cutoff_enabled": updatedSession.IsCutoffEnabled,
		"cutoff_at":         updatedSession.CutoffAt,
		"timestamp":         time.Now().UnixMilli(),
	})

	return c.JSON(fiber.Map{"success": true, "data": updatedSession})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/status
func UpdateQueueSessionStatusCompatHandler(c fiber.Ctx) error {
	var input struct {
		Status string `json:"status"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.Status == "" {
		return queueLegacyError(c, 400, "ข้อมูลไม่ถูกต้อง")
	}

	session, err := repositories.GetQueueSessionByID(c.Params("sessionId"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err := queueEnsureCourseWritable(c, session.CourseID); err != nil {
		return err
	}

	if err := updateQueueSessionStatusCompat(session, input.Status, true); err != nil {
		var conflictErr *repositories.ClassroomConflictError
		if errors.As(err, &conflictErr) {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"message": conflictErr.Error(),
				"classroom_conflict": fiber.Map{
					"session_id":    conflictErr.SessionID,
					"session_title": conflictErr.SessionTitle,
					"course_id":     conflictErr.CourseID,
					"course_name":   conflictErr.CourseName,
					"started_at":    conflictErr.StartedAt,
				},
			})
		}
		return queueLegacyError(c, 400, err.Error())
	}

	updatedSession, err := repositories.GetQueueSessionByID(session.ID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	realtime.EmitToQueue(updatedSession.ID, "session-status-changed", fiber.Map{"status": updatedSession.Status, "session": updatedSession, "timestamp": time.Now().UnixMilli()})

	return c.JSON(fiber.Map{"success": true, "data": updatedSession})
}

// POST /api/courses/:courseId/queue/sessions/:sessionId/regenerate-pin
func RegenerateQueuePINHandler(c fiber.Ctx) error {
	sessionID := c.Params("sessionId")
	session, err := repositories.GetQueueSessionByID(sessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	if !queueCourseScopeMatches(c, session.CourseID) {
		return queueLegacyError(c, 404, "ไม่พบ Queue Session")
	}

	pinCode, err := repositories.RegenerateQueueSessionPIN(sessionID)
	if err != nil {
		return queueLegacyError(c, 500, err.Error())
	}
	logCourseActivity(c, session.CourseID, c.Locals("user_id").(uint), "regenerate_queue_pin", "queue", "queue_session", session.ID, session.Title, fiber.Map{"regenerated": true})

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"pin_code": pinCode,
		},
	})
}

func emitQueueBookingChanged(sessionID string, event string, booking *models.QueueBooking) {
	if booking == nil {
		return
	}
	payload := fiber.Map{"booking": booking, "booking_id": booking.ID, "timestamp": time.Now().UnixMilli()}
	realtime.EmitToQueue(sessionID, event, payload)
	realtime.EmitToBooking(booking.ID, event, payload)
	realtime.EmitDataUpdate("queue", "update", booking.ID, payload)
}

func emitQueueActionChanged(booking *models.QueueBooking, action string) {
	if booking == nil {
		return
	}
	switch action {
	case "start":
		emitQueueBookingChanged(booking.QueueSessionID, "booking-assigned", booking)
		if booking.AssignedWorkerID != nil {
			realtime.EmitToWorker(*booking.AssignedWorkerID, "booking-assigned", fiber.Map{"booking": booking, "timestamp": time.Now().UnixMilli()})
		}
	case "complete":
		emitQueueBookingChanged(booking.QueueSessionID, "booking-completed", booking)
		realtime.EmitToBooking(booking.ID, "your-booking-completed", fiber.Map{"booking": booking, "timestamp": time.Now().UnixMilli()})
		realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})
	case "no_show":
		emitQueueBookingChanged(booking.QueueSessionID, "booking-skipped", booking)
		realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})
	case "reject":
		emitQueueBookingChanged(booking.QueueSessionID, "booking-requeued", booking)
		realtime.EmitToQueue(booking.QueueSessionID, "queue-position-updated", fiber.Map{"booking_id": booking.ID, "timestamp": time.Now().UnixMilli()})
	default:
		emitQueueBookingChanged(booking.QueueSessionID, "booking-assigned", booking)
	}
}

func tryAssignNextBookingAndEmit(sessionID string, workerID uint) (*models.QueueBooking, error) {
	if _, timeoutErr := repositories.ProcessQueueOfferTimeouts(sessionID); timeoutErr != nil {
		return nil, timeoutErr
	}

	nextBooking, assignedNow, err := repositories.AssignNextWaitingBookingToWorker(sessionID, workerID)
	if err != nil {
		return nil, err
	}
	if assignedNow && nextBooking != nil {
		bookingPayload, payloadErr := buildWorkerBookingPayload(nextBooking)
		if payloadErr != nil {
			return nil, payloadErr
		}
		realtime.EmitToQueue(sessionID, "booking-assigned", fiber.Map{"booking": bookingPayload, "worker_id": workerID, "timestamp": time.Now().UnixMilli()})
		realtime.EmitToBooking(nextBooking.ID, "booking-assigned", fiber.Map{"booking": bookingPayload, "timestamp": time.Now().UnixMilli()})
		realtime.EmitToWorker(workerID, "new-task", fiber.Map{"booking": bookingPayload, "timestamp": time.Now().UnixMilli()})
	}
	return nextBooking, nil
}

func buildWorkerBookingPayload(booking *models.QueueBooking) (fiber.Map, error) {
	if booking == nil {
		return nil, nil
	}

	var student models.Student
	if err := config.DB.Select("id", "student_id", "full_name").Where("id = ?", booking.StudentID).First(&student).Error; err != nil {
		return nil, err
	}

	return fiber.Map{
		"id":                 booking.ID,
		"queue_session_id":   booking.QueueSessionID,
		"student_id":         booking.StudentID,
		"desk_id":            booking.DeskID,
		"desk_number":        booking.DeskNumber,
		"booking_type":       booking.BookingType,
		"queue_number":       booking.QueueNumber,
		"is_late_booking":    booking.IsLateBooking,
		"late_reason":        booking.LateReason,
		"note":               booking.Note,
		"status":             booking.Status,
		"assigned_worker_id": booking.AssignedWorkerID,
		"assigned_at":        booking.AssignedAt,
		"offer_expires_at":   booking.OfferExpiresAt,
		"started_at":         booking.StartedAt,
		"completed_at":       booking.CompletedAt,
		"score":              booking.Score,
		"worker_note":        booking.WorkerNote,
		"created_at":         booking.CreatedAt,
		"updated_at":         booking.UpdatedAt,
		"student": fiber.Map{
			"id":         student.ID,
			"student_id": student.StudentID,
			"full_name":  student.FullName,
		},
	}, nil
}

// GET /api/admin/queue/sessions/active
// Returns all active or paused queue sessions across all courses (admin-only)
func GetActiveQueueSessionsAdminHandler(c fiber.Ctx) error {
	type ActiveSessionRow struct {
		ID                string     `json:"id"`
		Title             string     `json:"title"`
		Status            string     `json:"status"`
		PinCode           string     `json:"pin_code"`
		CourseID          string     `json:"course_id"`
		CourseName        string     `json:"course_name"`
		CourseCode        string     `json:"course_code"`
		ClassroomID       string     `json:"classroom_id"`
		ClassroomName     string     `json:"classroom_name"`
		ClassroomBuilding string     `json:"classroom_building"`
		StartTime         *time.Time `json:"start_time,omitempty"`
		CreatedAt         time.Time  `json:"created_at"`
	}

	var rows []ActiveSessionRow
	if err := config.DB.Raw(`
		SELECT
			qs.id,
			qs.title,
			qs.status,
			qs.pin_code,
			qs.course_id,
			c.name AS course_name,
			c.code AS course_code,
			qs.classroom_id,
			cl.name AS classroom_name,
			cl.building AS classroom_building,
			qs.start_time,
			qs.created_at
		FROM queue_sessions qs
		JOIN courses c ON c.id = qs.course_id
		JOIN classrooms cl ON cl.id = qs.classroom_id
		WHERE qs.status IN ('active', 'paused')
		ORDER BY qs.created_at ASC
	`).Scan(&rows).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fiber.Map{"message": "ไม่สามารถโหลดข้อมูลได้"}})
	}

	if rows == nil {
		rows = []ActiveSessionRow{}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    rows,
	})
}
