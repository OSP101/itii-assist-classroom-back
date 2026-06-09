package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// =============================================================================
// GET /api/students?page=&limit=&search=&status=&sortBy=&sortOrder=
// =============================================================================

func GetStudentsHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	result, err := repositories.GetStudents(repositories.StudentListParams{
		Page:      page,
		Limit:     limit,
		Search:    c.Query("search"),
		Status:    c.Query("status"),
		SortBy:    c.Query("sortBy", "created_at"),
		SortOrder: c.Query("sortOrder", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลนักศึกษาไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"students": result.Students,
			"pagination": fiber.Map{
				"currentPage":  result.Page,
				"totalPages":   result.TotalPages,
				"totalItems":   result.Total,
				"itemsPerPage": result.Limit,
				"hasMore":      result.Page < result.TotalPages,
			},
		},
	})
}

// =============================================================================
// GET /api/students/stats
// =============================================================================

func GetStudentStatsHandler(c fiber.Ctx) error {
	stats, err := repositories.GetStudentStats()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงสถิติไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"total": stats.Total,
			"byStatus": fiber.Map{
				"active":   stats.Active,
				"inactive": stats.Inactive,
			},
		},
	})
}

// =============================================================================
// GET /api/students/:id
// =============================================================================

func GetStudentByIDHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	student, err := repositories.FindStudentByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}
	return c.JSON(fiber.Map{"success": true, "data": student})
}

// =============================================================================
// GET /api/students/lookup/:student_id  (public — นักศึกษาตรวจสอบด้วยตัวเอง)
// =============================================================================

func LookupStudentHandler(c fiber.Ctx) error {
	sid := c.Params("student_id")
	if sid == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรหัสนักศึกษา"})
	}

	result, err := repositories.LookupStudentScores(sid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data":    result,
	})
}

func GetMyStudentCourseHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายวิชา"})
	}

	studentID, ok := middlewares.GetStudentID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล session นักศึกษา"})
	}
	student, err := repositories.FindStudentByID(studentID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}

	if !repositories.IsStudentInCourse(courseID, student.ID) {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชานี้ในบัญชีนักศึกษา"})
	}

	result, err := repositories.LookupStudentCourseScores(student.StudentID, courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดข้อมูลรายวิชาไม่สำเร็จ"})
	}

	queueSessions, err := repositories.GetQueueSessions(courseID, "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดข้อมูลคิวไม่สำเร็จ"})
	}

	queuePayload := make([]fiber.Map, 0)
	for _, session := range queueSessions {
		if session.QueueSession.Status != "active" && session.QueueSession.Status != "paused" {
			continue
		}

		var myBooking fiber.Map
		booking, bookingErr := repositories.GetStudentActiveBooking(session.QueueSession.ID, student.ID)
		if bookingErr == nil {
			myBooking = fiber.Map{
				"id":           booking.ID,
				"queue_number": booking.QueueNumber,
				"booking_type": booking.BookingType,
				"status":       booking.Status,
				"desk_id":      booking.DeskID,
				"desk_number":  booking.DeskNumber,
				"note":         booking.Note,
				"assigned_at":  booking.AssignedAt,
				"started_at":   booking.StartedAt,
				"created_at":   booking.CreatedAt,
			}
		} else if !errors.Is(bookingErr, gorm.ErrRecordNotFound) {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดข้อมูลการจองคิวไม่สำเร็จ"})
		}

		queuePayload = append(queuePayload, fiber.Map{
			"id":                           session.QueueSession.ID,
			"title":                        session.QueueSession.Title,
			"description":                  session.QueueSession.Description,
			"status":                       session.QueueSession.Status,
			"require_attendance":           session.QueueSession.RequireAttendance,
			"linked_assignment_id":         session.QueueSession.LinkedAssignmentID,
			"linked_attendance_session_id": session.QueueSession.LinkedAttendanceSessionID,
			"cutoff_at":                    session.QueueSession.CutoffAt,
			"cutoff_note":                  session.QueueSession.CutoffNote,
			"classroom":                    session.Classroom,
			"linkedAssignment":             session.LinkedAssignment,
			"linkedAttendanceSession":      session.LinkedAttendanceSession,
			"stats":                        session.Stats,
			"my_booking":                   myBooking,
		})
	}

	if len(result.Courses) > 0 {
		course := result.Courses[0]
		return c.JSON(fiber.Map{
			"success": true,
			"data": fiber.Map{
				"student": result.Student,
				"course":  course,
				"queue": fiber.Map{
					"sessions": queuePayload,
				},
			},
		})
	}

	return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลรายวิชาสำหรับนักศึกษา"})
}

// =============================================================================
// POST /api/students/search-by-ids  (Admin, Instructor, TA)
// =============================================================================

type SearchByIDsInput struct {
	StudentIDsCamel []string `json:"studentIds"`
	StudentIDsSnake []string `json:"student_ids"`
}

func SearchStudentsByIDsHandler(c fiber.Ctx) error {
	var input SearchByIDsInput
	if err := c.Bind().JSON(&input); err != nil || (len(input.StudentIDsCamel) == 0 && len(input.StudentIDsSnake) == 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาส่ง studentIds"})
	}

	studentIDs := input.StudentIDsCamel
	if len(studentIDs) == 0 {
		studentIDs = input.StudentIDsSnake
	}
	students, err := repositories.FindStudentsByStudentIDs(studentIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ค้นหาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": students})
}

// =============================================================================
// POST /api/students  (Admin only)
// =============================================================================

type CreateStudentInput struct {
	StudentID string          `json:"student_id"`
	FullName  string          `json:"full_name"`
	Email     string          `json:"email"`
	Extra     json.RawMessage `json:"extra"`
}

func CreateStudentHandler(c fiber.Ctx) error {
	var input CreateStudentInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.StudentID == "" || input.FullName == "" || input.Email == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสนักศึกษา ชื่อ-นามสกุล และอีเมล"})
	}
	if repositories.IsStudentIDExists(input.StudentID, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสนักศึกษานี้มีอยู่ในระบบแล้ว"})
	}
	if repositories.IsStudentEmailExists(input.Email, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
	}

	student := models.Student{
		StudentID: input.StudentID,
		FullName:  input.FullName,
		Email:     input.Email,
		IsActive:  true,
	}
	if len(input.Extra) > 0 && string(input.Extra) != "null" {
		student.Extra = datatypes.JSON(input.Extra)
	}

	if err := repositories.CreateStudent(&student); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างข้อมูลนักศึกษาไม่สำเร็จ"})
	}
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "สร้างข้อมูลนักศึกษาสำเร็จ",
		"data":    student,
	})
}

// =============================================================================
// POST /api/students/import  (Admin only)
// =============================================================================

type ImportStudentRow struct {
	StudentID string          `json:"student_id"`
	FullName  string          `json:"full_name"`
	Email     string          `json:"email"`
	Extra     json.RawMessage `json:"extra"`
}

type ImportStudentsInput struct {
	Students []ImportStudentRow `json:"students"`
}

func ImportStudentsHandler(c fiber.Ctx) error {
	var input ImportStudentsInput
	if err := c.Bind().JSON(&input); err != nil || len(input.Students) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาส่งข้อมูลนักศึกษาที่ต้องการนำเข้า"})
	}

	rows := make([]repositories.ImportRow, 0, len(input.Students))
	for _, s := range input.Students {
		row := repositories.ImportRow{
			StudentID: s.StudentID,
			FullName:  s.FullName,
			Email:     s.Email,
		}
		if len(s.Extra) > 0 && string(s.Extra) != "null" {
			row.Extra = []byte(s.Extra)
		}
		rows = append(rows, row)
	}

	result, err := repositories.ImportStudents(rows)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "นำเข้าข้อมูลไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("เพิ่มใหม่ %d คน, ซ้ำ %d คน, ล้มเหลว %d รายการ",
			result.Created, result.Skipped, result.Failed),
		"data": fiber.Map{
			"created":    result.Created,
			"skipped":    result.Skipped,
			"failed":     result.Failed,
			"duplicates": result.Duplicates,
			"errors":     result.Errors,
		},
	})
}

// =============================================================================
// PUT /api/students/:id  (Admin only)
// =============================================================================

type UpdateStudentInput struct {
	StudentID string          `json:"student_id"`
	FullName  string          `json:"full_name"`
	Email     string          `json:"email"`
	Extra     json.RawMessage `json:"extra"`
	IsActive  *bool           `json:"is_active"`
}

func UpdateStudentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	var input UpdateStudentInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.StudentID == "" || input.FullName == "" || input.Email == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกรหัสนักศึกษา ชื่อ-นามสกุล และอีเมล"})
	}

	student, err := repositories.FindStudentByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}

	if input.StudentID != student.StudentID && repositories.IsStudentIDExists(input.StudentID, student.ID) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสนักศึกษานี้มีอยู่ในระบบแล้ว"})
	}
	if input.Email != student.Email && repositories.IsStudentEmailExists(input.Email, student.ID) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
	}

	student.StudentID = input.StudentID
	student.FullName = input.FullName
	student.Email = input.Email
	if len(input.Extra) > 0 && string(input.Extra) != "null" {
		student.Extra = datatypes.JSON(input.Extra)
	}
	if input.IsActive != nil {
		student.IsActive = *input.IsActive
	}

	if err := repositories.UpdateStudent(student); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "อัปเดตข้อมูลไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"message": "อัปเดตข้อมูลนักศึกษาสำเร็จ",
		"data":    student,
	})
}

// =============================================================================
// PATCH /api/students/:id/status  (Admin only)
// =============================================================================

func ToggleStudentStatusHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	student, err := repositories.ToggleStudentStatus(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}

	msg := "ปิดใช้งานนักศึกษาสำเร็จ"
	if student.IsActive {
		msg = "เปิดใช้งานนักศึกษาสำเร็จ"
	}
	return c.JSON(fiber.Map{"success": true, "message": msg, "data": student})
}

// =============================================================================
// DELETE /api/students/:id  (Admin only)
// =============================================================================

func DeleteStudentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	if _, err := repositories.FindStudentByID(uint(id)); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}

	if err := repositories.DeleteStudent(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบข้อมูลนักศึกษาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "ลบข้อมูลนักศึกษาสำเร็จ"})
}
