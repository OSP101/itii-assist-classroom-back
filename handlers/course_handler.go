package handlers

import (
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// CourseHandler — struct-based handler with audit logger
type CourseHandler struct {
	auditLogger *services.AuditLogger
}

func NewCourseHandler(auditLogger *services.AuditLogger) *CourseHandler {
	return &CourseHandler{auditLogger: auditLogger}
}

// =============================================================================
// GET /api/courses/instructors
// =============================================================================

func GetInstructorsListHandler(c fiber.Ctx) error {
	users, err := repositories.GetUsersByRole("instructor")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลอาจารย์ไม่สำเร็จ"})
	}
	result := make([]fiber.Map, len(users))
	for i, u := range users {
		result[i] = fiber.Map{
			"id": u.ID, "full_name": u.FullName,
			"email": u.Email, "username": u.Username, "avatar": u.Avatar,
		}
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// =============================================================================
// GET /api/courses/tas-list
// =============================================================================

func GetTAsListHandler(c fiber.Ctx) error {
	courseID := strings.TrimSpace(c.Query("course_id"))
	if courseID == "" {
		courseID = strings.TrimSpace(c.Query("courseId"))
	}
	if courseID == "" {
		courseID = strings.TrimSpace(c.Query("courseID"))
	}

	var (
		users []models.User
		err   error
	)
	if courseID != "" {
		users, err = repositories.GetAvailableTAs(courseID)
	} else {
		users, err = repositories.GetUsersByRole("ta")
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูล TA ไม่สำเร็จ"})
	}
	result := make([]fiber.Map, len(users))
	for i, u := range users {
		result[i] = fiber.Map{
			"id": u.ID, "full_name": u.FullName,
			"email": u.Email, "username": u.Username, "avatar": u.Avatar,
		}
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

func extractBulkUserIDs(body []byte) ([]uint, error) {
	var payload struct {
		UserIDsSnake []uint `json:"user_ids"`
		UserIDsCamel []uint `json:"userIds"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if len(payload.UserIDsSnake) > 0 {
			return uniqueCourseUserIDs(payload.UserIDsSnake), nil
		}
		if len(payload.UserIDsCamel) > 0 {
			return uniqueCourseUserIDs(payload.UserIDsCamel), nil
		}
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	for _, key := range []string{"user_ids", "userIds"} {
		value, exists := raw[key]
		if !exists {
			continue
		}

		items, ok := value.([]any)
		if !ok {
			continue
		}

		result := make([]uint, 0, len(items))
		for _, item := range items {
			switch typed := item.(type) {
			case float64:
				if typed > 0 {
					result = append(result, uint(typed))
				}
			case string:
				parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
				if err == nil && parsed > 0 {
					result = append(result, uint(parsed))
				}
			case map[string]any:
				for _, nestedKey := range []string{"id", "user_id", "userId"} {
					nestedValue, nestedExists := typed[nestedKey]
					if !nestedExists {
						continue
					}
					switch nestedTyped := nestedValue.(type) {
					case float64:
						if nestedTyped > 0 {
							result = append(result, uint(nestedTyped))
						}
					case string:
						parsed, err := strconv.ParseUint(strings.TrimSpace(nestedTyped), 10, 64)
						if err == nil && parsed > 0 {
							result = append(result, uint(parsed))
						}
					}
					break
				}
			}
		}

		return uniqueCourseUserIDs(result), nil
	}

	return []uint{}, nil
}

func uniqueCourseUserIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// =============================================================================
// GET /api/courses/my-courses
// =============================================================================

func GetMyCoursesHandler(c fiber.Ctx) error {
	role := c.Locals("user_role").(string)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "12"))
	year, _ := strconv.Atoi(c.Query("year", "0"))
	semester, _ := strconv.Atoi(c.Query("semester", "0"))

	// Student sessions carry student_id, not user_id
	var resolvedUserID uint
	if role == "student" {
		sid, ok := middlewares.GetStudentID(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล session นักศึกษา"})
		}
		resolvedUserID = sid
	} else {
		uid, ok := middlewares.GetUserID(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล session ผู้ใช้"})
		}
		resolvedUserID = uid
	}

	result, err := repositories.GetMyCourses(resolvedUserID, role, repositories.CourseListParams{
		Page: page, Limit: limit,
		Search:    c.Query("search"),
		Year:      year,
		Semester:  semester,
		Status:    c.Query("status"),
		SortBy:    c.Query("sortBy", "created_at"),
		SortOrder: c.Query("sortOrder", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลรายวิชาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"courses": result.Courses,
			"pagination": fiber.Map{
				"currentPage":  result.CurrentPage,
				"totalPages":   result.TotalPages,
				"totalItems":   result.Total,
				"itemsPerPage": result.PerPage,
				"hasMore":      result.CurrentPage < result.TotalPages,
			},
		},
	})
}

// =============================================================================
// GET /api/courses/my-courses/stats
// =============================================================================

func GetMyCoursesStatsHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	role := c.Locals("user_role").(string)

	stats, err := repositories.GetMyCourseStats(userID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงสถิติรายวิชาของฉันไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// =============================================================================
// GET /api/courses/stats
// =============================================================================

func GetCourseStatsHandler(c fiber.Ctx) error {
	stats, err := repositories.GetCourseStats()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงสถิติรายวิชาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// =============================================================================
// GET /api/courses/conflicts?search=&year=&semester=&limit=
// =============================================================================

func GetCourseConflictsHandler(c fiber.Ctx) error {
	year, _ := strconv.Atoi(strings.TrimSpace(c.Query("year", "0")))
	semester, _ := strconv.Atoi(strings.TrimSpace(c.Query("semester", "0")))
	limit, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit", "50")))

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	result, err := repositories.GetCourseActivationConflicts(repositories.CourseConflictParams{
		Search:   strings.TrimSpace(c.Query("search")),
		Year:     year,
		Semester: semester,
		Limit:    limit,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลความขัดแย้งรายวิชาไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{"success": true, "data": result})
}

// =============================================================================
// GET /api/courses?page=&limit=&search=&year=&semester=&status=
// =============================================================================

func GetCoursesHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	year, _ := strconv.Atoi(c.Query("year", "0"))
	semester, _ := strconv.Atoi(c.Query("semester", "0"))

	result, err := repositories.GetCourses(repositories.CourseListParams{
		Page:      page,
		Limit:     limit,
		Search:    c.Query("search"),
		Year:      year,
		Semester:  semester,
		Status:    c.Query("status"),
		SortBy:    c.Query("sortBy", "created_at"),
		SortOrder: c.Query("sortOrder", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลรายวิชาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"courses": result.Courses,
			"pagination": fiber.Map{
				"currentPage":  result.CurrentPage,
				"totalPages":   result.TotalPages,
				"totalItems":   result.Total,
				"itemsPerPage": result.PerPage,
				"hasMore":      result.CurrentPage < result.TotalPages,
			},
		},
	})
}

// =============================================================================
// GET /api/courses/:id
// =============================================================================

func GetCourseByIDHandler(c fiber.Ctx) error {
	id := c.Params("id")
	detail, err := repositories.FindCourseByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลรายวิชา"})
	}
	return c.JSON(fiber.Map{"success": true, "data": detail})
}

// =============================================================================
// POST /api/courses
// =============================================================================

func CreateCourseHandler(c fiber.Ctx) error {
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	var input struct {
		Code               string `json:"code"`
		Name               string `json:"name"`
		Year               int    `json:"year"`
		Semester           int    `json:"semester"`
		Description        string `json:"description"`
		Image              string `json:"image"`
		AttentionThreshold *int   `json:"attention_threshold"`
		InstructorIDs      []uint `json:"instructor_ids"`
		InstructorID       *uint  `json:"instructor_id"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	if strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.Name) == "" || input.Year == 0 || input.Semester == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกข้อมูลที่จำเป็น (รหัสวิชา, ชื่อวิชา, ปีการศึกษา, ภาคเรียน)"})
	}

	// Check duplicate active course
	if repositories.IsActiveCourseExists(input.Code, input.Year, input.Semester, "") {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รายวิชานี้มีเปิดใช้งานอยู่แล้ว (รหัส-ปี-ภาคเรียน ซ้ำ) กรุณาปิดใช้งานรายวิชาเดิมก่อน"})
	}

	// Build instructor list
	var instructorIDs []uint
	if len(input.InstructorIDs) > 0 {
		instructorIDs = input.InstructorIDs
	} else if input.InstructorID != nil {
		instructorIDs = []uint{*input.InstructorID}
	}

	// Auto-add self if instructor
	if actorRole == "instructor" {
		found := false
		for _, uid := range instructorIDs {
			if uid == actorID {
				found = true
				break
			}
		}
		if !found {
			instructorIDs = append([]uint{actorID}, instructorIDs...)
		}
	}

	// Generate NanoID
	courseID, err := utils.GenerateNanoID(21)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถสร้างรหัสรายวิชาได้"})
	}

	threshold := 60
	if input.AttentionThreshold != nil {
		threshold = *input.AttentionThreshold
	}

	var primaryInstructorID *uint
	if len(instructorIDs) > 0 {
		primaryInstructorID = &instructorIDs[0]
	}

	course := models.Course{
		ID:                 courseID,
		Code:               strings.TrimSpace(input.Code),
		Name:               strings.TrimSpace(input.Name),
		Year:               input.Year,
		Semester:           input.Semester,
		InstructorID:       primaryInstructorID,
		Description:        input.Description,
		Image:              input.Image,
		IsActive:           true,
		AttentionThreshold: threshold,
	}

	if err := repositories.CreateCourse(&course); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างรายวิชาไม่สำเร็จ"})
	}

	// Add instructors
	if len(instructorIDs) > 0 {
		repositories.ReplaceAllCourseInstructors(courseID, instructorIDs)
	}

	logCourseActivity(c, courseID, actorID, "create_course", "course", "course", courseID, course.Name, fiber.Map{
		"code":           course.Code,
		"year":           course.Year,
		"semester":       course.Semester,
		"instructor_ids": instructorIDs,
	})

	detail, _ := repositories.FindCourseByID(courseID)
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "สร้างรายวิชาสำเร็จ",
		"data":    detail,
	})
}

// =============================================================================
// PUT /api/courses/:id
// =============================================================================

func UpdateCourseHandler(c fiber.Ctx) error {
	id := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	course, err := repositories.FindCourseByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลรายวิชา"})
	}

	// Access check for instructor
	if actorRole == "instructor" && !repositories.IsUserCourseInstructor(id, actorID) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์แก้ไขรายวิชานี้"})
	}

	var input struct {
		Code               string `json:"code"`
		Name               string `json:"name"`
		Year               *int   `json:"year"`
		Semester           *int   `json:"semester"`
		Description        string `json:"description"`
		Image              string `json:"image"`
		IsActive           *bool  `json:"is_active"`
		AttentionThreshold *int   `json:"attention_threshold"`
		InstructorIDs      []uint `json:"instructor_ids"`
		InstructorID       *uint  `json:"instructor_id"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	// Duplicate check
	checkCode := course.Code
	checkYear := course.Year
	checkSemester := course.Semester
	if input.Code != "" {
		checkCode = input.Code
	}
	if input.Year != nil {
		checkYear = *input.Year
	}
	if input.Semester != nil {
		checkSemester = *input.Semester
	}
	if repositories.IsActiveCourseExists(checkCode, checkYear, checkSemester, id) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รายวิชานี้มีเปิดใช้งานอยู่แล้ว (รหัส-ปี-ภาคเรียน ซ้ำ)"})
	}

	// Apply updates
	updated := course.Course
	if input.Code != "" {
		updated.Code = strings.TrimSpace(input.Code)
	}
	if input.Name != "" {
		updated.Name = strings.TrimSpace(input.Name)
	}
	if input.Year != nil {
		updated.Year = *input.Year
	}
	if input.Semester != nil {
		updated.Semester = *input.Semester
	}
	if input.Description != "" {
		updated.Description = input.Description
	}
	if input.Image != "" {
		updated.Image = input.Image
	}
	if input.IsActive != nil {
		updated.IsActive = *input.IsActive
	}
	if input.AttentionThreshold != nil {
		updated.AttentionThreshold = *input.AttentionThreshold
	}

	// Update instructors
	shouldUpdateInstructors := len(input.InstructorIDs) > 0 || input.InstructorID != nil
	var instructorIDs []uint
	if len(input.InstructorIDs) > 0 {
		instructorIDs = input.InstructorIDs
	} else if input.InstructorID != nil {
		instructorIDs = []uint{*input.InstructorID}
	}
	if shouldUpdateInstructors && actorRole == "instructor" {
		found := false
		for _, uid := range instructorIDs {
			if uid == actorID {
				found = true
				break
			}
		}
		if !found && repositories.IsUserCourseInstructor(id, actorID) {
			instructorIDs = append([]uint{actorID}, instructorIDs...)
		}
	}

	if shouldUpdateInstructors && len(instructorIDs) > 0 {
		updated.InstructorID = &instructorIDs[0]
		repositories.ReplaceAllCourseInstructors(id, instructorIDs)
	}

	if err := repositories.UpdateCourse(&updated); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "อัปเดตรายวิชาไม่สำเร็จ"})
	}

	logDetail := fiber.Map{
		"code":                updated.Code,
		"year":                updated.Year,
		"semester":            updated.Semester,
		"is_active":           updated.IsActive,
		"attention_threshold": updated.AttentionThreshold,
	}
	if shouldUpdateInstructors {
		logDetail["instructor_ids"] = instructorIDs
	}
	logCourseActivity(c, id, actorID, "update_course", "course", "course", id, updated.Name, logDetail)
	logPrivilegedAdminAction(c, actorID, "update_course", "warn", "courses", id, fiber.Map{
		"target_type": "course",
		"target_snapshot": fiber.Map{
			"id":                  id,
			"code":                updated.Code,
			"name":                updated.Name,
			"year":                updated.Year,
			"semester":            updated.Semester,
			"is_active":           updated.IsActive,
			"attention_threshold": updated.AttentionThreshold,
		},
		"updated_instructors": shouldUpdateInstructors,
	})

	detail, _ := repositories.FindCourseByID(id)
	return c.JSON(fiber.Map{
		"success": true,
		"message": "อัปเดตรายวิชาสำเร็จ",
		"data":    detail,
	})
}

// =============================================================================
// DELETE /api/courses/:id
// =============================================================================

func DeleteCourseHandler(c fiber.Ctx) error {
	id := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	course, err := repositories.FindCourseByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลรายวิชา"})
	}

	if actorRole == "instructor" && !repositories.IsUserCourseInstructor(id, actorID) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์ลบรายวิชานี้"})
	}

	if err := repositories.DeleteCourse(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบรายวิชาไม่สำเร็จ"})
	}
	logCourseActivity(c, id, actorID, "delete_course", "course", "course", id, course.Name, fiber.Map{
		"code":     course.Code,
		"year":     course.Year,
		"semester": course.Semester,
	})
	logPrivilegedAdminAction(c, actorID, "delete_course", "critical", "courses", id, fiber.Map{
		"target_type": "course",
		"target_snapshot": fiber.Map{
			"id":       id,
			"code":     course.Code,
			"name":     course.Name,
			"year":     course.Year,
			"semester": course.Semester,
		},
	})
	return c.JSON(fiber.Map{"success": true, "message": "ลบรายวิชาสำเร็จ"})
}

// =============================================================================
// PATCH /api/courses/:id/toggle-status
// =============================================================================

func ToggleCourseStatusHandler(c fiber.Ctx) error {
	id := c.Params("id")
	actorID := c.Locals("user_id").(uint)

	course, err := repositories.FindCourseByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลรายวิชา"})
	}

	willBeActive := !course.IsActive
	if willBeActive {
		if repositories.IsActiveCourseExists(course.Code, course.Year, course.Semester, id) {
			return c.Status(400).JSON(fiber.Map{
				"success": false,
				"message": "ไม่สามารถเปิดใช้งานได้ มีรายวิชาที่เปิดใช้งานอยู่แล้ว",
			})
		}
	}

	updated, err := repositories.ToggleCourseStatus(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เปลี่ยนสถานะไม่สำเร็จ"})
	}

	msg := "ปิดใช้งานรายวิชาสำเร็จ"
	action := "deactivate_course"
	if updated.IsActive {
		msg = "เปิดใช้งานรายวิชาสำเร็จ"
		action = "activate_course"
	}
	logCourseActivity(c, id, actorID, action, "course", "course", id, updated.Name, fiber.Map{"is_active": updated.IsActive})
	logPrivilegedAdminAction(c, actorID, "toggle_course_status", "warn", "courses", id, fiber.Map{
		"target_type": "course",
		"target_snapshot": fiber.Map{
			"id":        id,
			"code":      updated.Code,
			"name":      updated.Name,
			"is_active": updated.IsActive,
		},
		"applied_action": action,
	})
	return c.JSON(fiber.Map{"success": true, "message": msg, "data": updated})
}

// =============================================================================
// Section Management
// =============================================================================

// POST /api/courses/:id/sections
func AddSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	// Access check
	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	var input struct {
		SectionNo string `json:"section_no"`
		Note      string `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.SectionNo) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุหมายเลขกลุ่มเรียน"})
	}

	if repositories.IsSectionExists(courseID, strings.TrimSpace(input.SectionNo), 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กลุ่มเรียนนี้มีอยู่แล้ว"})
	}

	section := models.CourseSection{
		CourseID:  courseID,
		SectionNo: strings.TrimSpace(input.SectionNo),
		Note:      input.Note,
	}
	if err := repositories.CreateSection(&section); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มกลุ่มเรียนไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "add_section", "member", "section", section.ID, section.SectionNo, fiber.Map{"section_no": section.SectionNo})
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "เพิ่มกลุ่มเรียนสำเร็จ", "data": section})
}

// PUT /api/courses/:id/sections/:sectionId
func UpdateSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	section, err := repositories.FindSectionByID(uint(sectionID), courseID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่มเรียน"})
	}

	var input struct {
		SectionNo string `json:"section_no"`
		Note      string `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.SectionNo) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุหมายเลขกลุ่มเรียน"})
	}

	if repositories.IsSectionExists(courseID, strings.TrimSpace(input.SectionNo), uint(sectionID)) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "หมายเลขกลุ่มเรียนนี้มีอยู่แล้ว"})
	}

	section.SectionNo = strings.TrimSpace(input.SectionNo)
	section.Note = input.Note
	if err := repositories.UpdateSection(section); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "แก้ไขกลุ่มเรียนไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "update_section", "member", "section", sectionID, section.SectionNo, fiber.Map{"section_no": section.SectionNo})
	return c.JSON(fiber.Map{"success": true, "message": "แก้ไขกลุ่มเรียนสำเร็จ", "data": section})
}

// DELETE /api/courses/:id/sections/:sectionId
func RemoveSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	if _, err := repositories.FindSectionByID(uint(sectionID), courseID); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่มเรียน"})
	}

	if err := repositories.DeleteSection(uint(sectionID), courseID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบกลุ่มเรียนไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "remove_section", "member", "section", sectionID, "", nil)
	return c.JSON(fiber.Map{"success": true, "message": "ลบกลุ่มเรียนสำเร็จ"})
}

// =============================================================================
// TA Management
// =============================================================================

// POST /api/courses/:id/tas
func (h *CourseHandler) AddTA(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เพิ่มผู้ช่วยสอนในรายวิชานี้"})
		}
	}

	var input struct {
		UserID uint `json:"user_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.UserID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุ user_id"})
	}

	if repositories.IsUserCourseTA(courseID, input.UserID) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ผู้ช่วยสอนนี้อยู่ในรายวิชาแล้ว"})
	}

	// Cross-role check: reject if this user's email already belongs to an enrolled student
	taUserEmail := repositories.GetUserEmailByID(input.UserID)
	if repositories.IsUserEmailStudentInCourse(courseID, taUserEmail) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ผู้ใช้นี้ลงทะเบียนเป็นนักศึกษาในรายวิชานี้อยู่แล้ว ไม่สามารถเพิ่มเป็นผู้ช่วยสอนได้"})
	}

	if err := repositories.AddCourseTA(courseID, input.UserID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มผู้ช่วยสอนไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "add_ta", "member", "user", input.UserID, "", nil)
	logPrivilegedAdminAction(c, actorID, "add_course_ta", "warn", "course_members", courseID, fiber.Map{
		"target_type": "course_member",
		"course_id":   courseID,
		"member_role": "ta",
		"target_snapshot": fiber.Map{
			"user_id": input.UserID,
			"role":    "ta",
		},
	})
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    courseID,
		ActorUserID: actorID,
		Action:      services.ActionCourseTAAdded,
		TargetType:  "ta",
		TargetID:    strconv.Itoa(int(input.UserID)),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "เพิ่มผู้ช่วยสอนสำเร็จ"})
}

// POST /api/courses/:id/tas/create-account
func (h *CourseHandler) CreateTAAccount(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์สร้างบัญชีผู้ช่วยสอนในรายวิชานี้"})
		}
	}

	var input struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Avatar   string `json:"avatar"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	input.Username = strings.TrimSpace(input.Username)
	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.TrimSpace(input.Email)
	input.Avatar = strings.TrimSpace(input.Avatar)

	if input.Username == "" || input.FullName == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอกชื่อผู้ใช้และชื่อ-นามสกุล"})
	}
	if repositories.IsActiveUsernameExists(input.Username, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ชื่อผู้ใช้นี้มีอยู่ในระบบแล้ว"})
	}
	if input.Email != "" && repositories.IsActiveEmailExists(input.Email, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
	}
	if repositories.IsUserEmailStudentInCourse(courseID, input.Email) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้ถูกใช้โดยนักศึกษาในรายวิชานี้แล้ว"})
	}

	generatedPassword := generatePassword(12)
	hashedPassword, err := utils.HashPassword(generatedPassword)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
	}

	newUser := models.User{
		Username:           input.Username,
		PasswordHash:       hashedPassword,
		Role:               "ta",
		FullName:           input.FullName,
		Email:              input.Email,
		IsActive:           true,
		Provider:           "local",
		Avatar:             input.Avatar,
		MustChangePassword: true,
	}

	if err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&newUser).Error; err != nil {
			return err
		}
		return tx.Create(&models.CourseTA{
			CourseID:    courseID,
			UserID:      newUser.ID,
			Permissions: repositories.EncodeCourseMemberPermissions("ta", nil),
			AssignedAt:  time.Now(),
		}).Error
	}); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างบัญชีผู้ช่วยสอนไม่สำเร็จ"})
	}

	logCourseActivity(c, courseID, actorID, "add_ta", "member", "user", newUser.ID, "", fiber.Map{"created_account": true})
	logPrivilegedAdminAction(c, actorID, "create_course_ta_account", "warn", "course_members", courseID, fiber.Map{
		"target_type": "course_member",
		"course_id":   courseID,
		"member_role": "ta",
		"target_snapshot": fiber.Map{
			"user_id":   newUser.ID,
			"username":  newUser.Username,
			"full_name": newUser.FullName,
			"email":     newUser.Email,
			"role":      "ta",
		},
	})
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    courseID,
		ActorUserID: actorID,
		Action:      services.ActionCourseTAAdded,
		TargetType:  "ta",
		TargetID:    strconv.Itoa(int(newUser.ID)),
		RequestID:   reqID,
		IPAddress:   ip,
	})

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "สร้างบัญชีและเพิ่มเป็นผู้ช่วยสอนสำเร็จ",
		"data": fiber.Map{
			"user": safeUser(&newUser),
			"credentials": fiber.Map{
				"username": newUser.Username,
				"password": generatedPassword,
			},
		},
	})
}

// POST /api/courses/:id/tas/bulk
func BulkAddTAsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เพิ่มผู้ช่วยสอนในรายวิชานี้"})
		}
	}

	rawBody := append([]byte(nil), c.Body()...)
	userIDs, err := extractBulkUserIDs(rawBody)
	log.Printf("[courses] bulk add TAs request course=%s actor=%d body=%s parsed_user_ids=%v", courseID, actorID, string(rawBody), userIDs)
	if err != nil || len(userIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายชื่อผู้ช่วยสอน"})
	}

	addedUsers, skipped, err := repositories.BulkAddCourseTAs(courseID, userIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มผู้ช่วยสอนไม่สำเร็จ"})
	}
	if len(addedUsers) == 0 {
		message := "ไม่พบผู้ช่วยสอนที่ระบุ"
		if skipped > 0 {
			message = "ผู้ช่วยสอนที่เลือกทั้งหมดอยู่ในรายวิชาแล้ว"
		}
		return c.Status(400).JSON(fiber.Map{"success": false, "message": message})
	}
	log.Printf("[courses] bulk add TAs result course=%s actor=%d added=%d skipped=%d added_user_ids=%v", courseID, actorID, len(addedUsers), skipped, userIDs)
	logCourseActivity(c, courseID, actorID, "bulk_add_tas", "member", "course", courseID, "", fiber.Map{"added": len(addedUsers), "skipped": skipped})
	logPrivilegedAdminAction(c, actorID, "bulk_add_course_tas", "warn", "course_members", courseID, fiber.Map{
		"target_type":   "course_member_bulk",
		"course_id":     courseID,
		"member_role":   "ta",
		"user_ids":      userIDs,
		"added_count":   len(addedUsers),
		"skipped_count": skipped,
	})
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "เพิ่มผู้ช่วยสอน " + strconv.Itoa(len(addedUsers)) + " คนสำเร็จ",
		"data": fiber.Map{
			"added":   addedUsers,
			"skipped": skipped,
		},
	})
}

// DELETE /api/courses/:id/tas/:userId
func (h *CourseHandler) RemoveTA(c fiber.Ctx) error {
	courseID := c.Params("id")
	userID, _ := strconv.ParseUint(c.Params("userId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if uint(userID) == actorID {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถนำตัวเองออกจากรายวิชาได้"})
	}

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์นำผู้ช่วยสอนออก"})
		}
	}

	if !repositories.RemoveCourseTA(courseID, uint(userID)) {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ช่วยสอนในรายวิชานี้"})
	}
	logCourseActivity(c, courseID, actorID, "remove_ta", "member", "user", userID, "", nil)
	logPrivilegedAdminAction(c, actorID, "remove_course_ta", "warn", "course_members", courseID, fiber.Map{
		"target_type": "course_member",
		"course_id":   courseID,
		"member_role": "ta",
		"target_snapshot": fiber.Map{
			"user_id": uint(userID),
			"role":    "ta",
		},
	})
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    courseID,
		ActorUserID: actorID,
		Action:      services.ActionCourseTARemoved,
		TargetType:  "ta",
		TargetID:    strconv.FormatUint(userID, 10),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.JSON(fiber.Map{"success": true, "message": "นำผู้ช่วยสอนออกสำเร็จ"})
}

// =============================================================================
// Instructor Management
// =============================================================================

// POST /api/courses/:id/instructors
func AddInstructorHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole == "ta" {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "ผู้ช่วยสอนไม่สามารถเพิ่มอาจารย์ได้"})
	}

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เพิ่มอาจารย์ในรายวิชานี้"})
		}
	}

	var input struct {
		UserID uint `json:"user_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.UserID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุ user_id"})
	}

	if repositories.IsUserCourseInstructor(courseID, input.UserID) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อาจารย์นี้อยู่ในรายวิชาแล้ว"})
	}

	if err := repositories.AddCourseInstructor(courseID, input.UserID, false); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มอาจารย์ไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "add_instructor", "member", "user", input.UserID, "", nil)
	logPrivilegedAdminAction(c, actorID, "add_course_instructor", "warn", "course_members", courseID, fiber.Map{
		"target_type": "course_member",
		"course_id":   courseID,
		"member_role": "instructor",
		"target_snapshot": fiber.Map{
			"user_id": input.UserID,
			"role":    "instructor",
		},
	})
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "เพิ่มอาจารย์สำเร็จ"})
}

// POST /api/courses/:id/instructors/bulk
func BulkAddInstructorsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole == "ta" {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "ผู้ช่วยสอนไม่สามารถเพิ่มอาจารย์ได้"})
	}

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เพิ่มอาจารย์ในรายวิชานี้"})
		}
	}

	var input struct {
		UserIDs []uint `json:"user_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.UserIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายชื่ออาจารย์"})
	}

	addedUsers, skipped, err := repositories.BulkAddCourseInstructors(courseID, input.UserIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มอาจารย์ไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "bulk_add_instructors", "member", "course", courseID, "", fiber.Map{"added": len(addedUsers), "skipped": skipped})
	logPrivilegedAdminAction(c, actorID, "bulk_add_course_instructors", "warn", "course_members", courseID, fiber.Map{
		"target_type":   "course_member_bulk",
		"course_id":     courseID,
		"member_role":   "instructor",
		"user_ids":      input.UserIDs,
		"added_count":   len(addedUsers),
		"skipped_count": skipped,
	})
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "เพิ่มอาจารย์ " + strconv.Itoa(len(addedUsers)) + " คนสำเร็จ",
		"data": fiber.Map{
			"added":   addedUsers,
			"skipped": skipped,
		},
	})
}

// DELETE /api/courses/:id/instructors/:userId
func RemoveInstructorHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	userID, _ := strconv.ParseUint(c.Params("userId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole != "admin" {
		hasPermission, err := repositories.HasCoursePermission(courseID, actorID, actorRole, repositories.PermissionManagePeople)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
		}
		if !hasPermission {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์นำอาจารย์ออก"})
		}
	}

	// Prevent removing self (non-admin)
	if actorRole != "admin" && uint(userID) == actorID {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถนำตัวเองออกจากรายวิชาได้"})
	}

	// Check last instructor
	if repositories.GetCourseInstructorCount(courseID) <= 1 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถนำอาจารย์คนสุดท้ายออกได้"})
	}

	ok, err := repositories.RemoveCourseInstructor(courseID, uint(userID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "นำอาจารย์ออกไม่สำเร็จ"})
	}
	if !ok {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบอาจารย์ในรายวิชานี้"})
	}
	logCourseActivity(c, courseID, actorID, "remove_instructor", "member", "user", userID, "", nil)
	logPrivilegedAdminAction(c, actorID, "remove_course_instructor", "warn", "course_members", courseID, fiber.Map{
		"target_type": "course_member",
		"course_id":   courseID,
		"member_role": "instructor",
		"target_snapshot": fiber.Map{
			"user_id": uint(userID),
			"role":    "instructor",
		},
	})
	return c.JSON(fiber.Map{"success": true, "message": "นำอาจารย์ออกสำเร็จ"})
}

// PATCH /api/courses/:id/tas/:userId/permissions
func UpdateTAPermissionsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	userID, _ := strconv.ParseUint(c.Params("userId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if actorRole == "ta" && uint(userID) == actorID {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "ผู้ช่วยสอนไม่สามารถแก้สิทธิ์ของตัวเองได้"})
	}

	var input struct {
		Permissions repositories.CourseMemberPermissions `json:"permissions"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลสิทธิ์ไม่ถูกต้อง"})
	}

	if err := repositories.UpdateCourseTAPermissions(courseID, uint(userID), &input.Permissions); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ช่วยสอนในรายวิชานี้"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกสิทธิ์ผู้ช่วยสอนไม่สำเร็จ"})
	}
	logPrivilegedAdminAction(c, actorID, "update_course_ta_permissions", "critical", "course_member_permissions", courseID, fiber.Map{
		"target_type": "course_permission",
		"course_id":   courseID,
		"member_role": "ta",
		"target_snapshot": fiber.Map{
			"user_id":     uint(userID),
			"permissions": input.Permissions,
		},
	})

	return c.JSON(fiber.Map{"success": true, "message": "อัปเดตสิทธิ์ผู้ช่วยสอนสำเร็จ"})
}

// PATCH /api/courses/:id/instructors/:userId/permissions
func UpdateInstructorPermissionsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	userID, _ := strconv.ParseUint(c.Params("userId"), 10, 64)
	actorID := c.Locals("user_id").(uint)

	var input struct {
		Permissions repositories.CourseMemberPermissions `json:"permissions"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลสิทธิ์ไม่ถูกต้อง"})
	}

	if err := repositories.UpdateCourseInstructorPermissions(courseID, uint(userID), &input.Permissions); err != nil {
		if errors.Is(err, repositories.ErrPrimaryInstructorPermissionsImmutable) {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "อาจารย์เจ้าของวิชาต้องมีสิทธิ์เต็มเสมอ"})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบอาจารย์ในรายวิชานี้"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกสิทธิ์อาจารย์ไม่สำเร็จ"})
	}
	logPrivilegedAdminAction(c, actorID, "update_course_instructor_permissions", "critical", "course_member_permissions", courseID, fiber.Map{
		"target_type": "course_permission",
		"course_id":   courseID,
		"member_role": "instructor",
		"target_snapshot": fiber.Map{
			"user_id":     uint(userID),
			"permissions": input.Permissions,
		},
	})

	return c.JSON(fiber.Map{"success": true, "message": "อัปเดตสิทธิ์อาจารย์สำเร็จ"})
}

// =============================================================================
// Section Student Management
// =============================================================================

// GET /api/courses/:id/sections/:sectionId/students
func GetSectionStudentsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	if _, err := repositories.FindSectionByID(uint(sectionID), courseID); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่มเรียน"})
	}

	students, err := repositories.GetSectionStudents(uint(sectionID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลนักศึกษาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": students})
}

// POST /api/courses/:id/sections/:sectionId/students
func AddStudentToSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	if _, err := repositories.FindSectionByID(uint(sectionID), courseID); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่มเรียน"})
	}

	var input struct {
		StudentID uint `json:"student_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.StudentID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุ student_id"})
	}

	if repositories.IsStudentInCourse(courseID, input.StudentID) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "นักศึกษานี้อยู่ในรายวิชานี้แล้ว"})
	}

	// Cross-role check: reject if this student's email belongs to a TA in the same course
	studentEmail := repositories.GetStudentEmailByID(input.StudentID)
	if repositories.IsStudentEmailCourseTA(courseID, studentEmail) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "นักศึกษานี้เป็นผู้ช่วยสอนในรายวิชานี้อยู่แล้ว ไม่สามารถลงทะเบียนเป็นนักศึกษาได้"})
	}

	if err := repositories.AddStudentToSection(uint(sectionID), input.StudentID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มนักศึกษาไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "add_student", "member", "student", input.StudentID, "", fiber.Map{"section_id": sectionID})
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "เพิ่มนักศึกษาสำเร็จ"})
}

// POST /api/courses/:id/sections/:sectionId/students/bulk
func BulkAddStudentsToSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	if _, err := repositories.FindSectionByID(uint(sectionID), courseID); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่มเรียน"})
	}

	var input struct {
		StudentIDs []uint `json:"student_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.StudentIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายชื่อนักศึกษา"})
	}

	added, skipped, err := repositories.BulkAddStudentsToSection(courseID, uint(sectionID), input.StudentIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มนักศึกษาไม่สำเร็จ"})
	}
	logCourseActivity(c, courseID, actorID, "bulk_add_students", "member", "section", sectionID, "", fiber.Map{"added": added, "skipped": skipped})
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "เพิ่มนักศึกษาสำเร็จ",
		"data":    fiber.Map{"added": added, "skipped": skipped},
	})
}

// DELETE /api/courses/:id/sections/:sectionId/students/:studentId
func (h *CourseHandler) RemoveStudentFromSection(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	studentID, _ := strconv.ParseUint(c.Params("studentId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	removed, restoreUntil, err := repositories.ArchiveAndRemoveStudentFromSection(uint(sectionID), uint(studentID), actorID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "นำนักศึกษาออกไม่สำเร็จ"})
	}

	if !removed {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบนักศึกษาในกลุ่มเรียนนี้"})
	}
	logCourseActivity(c, courseID, actorID, "remove_student", "member", "student", studentID, "", fiber.Map{"section_id": sectionID})
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    courseID,
		ActorUserID: actorID,
		Action:      services.ActionCourseStudentRemoved,
		TargetType:  "student",
		TargetID:    strconv.FormatUint(studentID, 10),
		RequestID:   reqID,
		IPAddress:   ip,
	})

	return c.JSON(fiber.Map{
		"success": true,
		"message": "นำนักศึกษาออกสำเร็จ (กู้คืนได้ภายใน 10 วัน)",
		"data": fiber.Map{
			"restore_until": restoreUntil,
		},
	})
}

// GET /api/courses/:id/students/removed
func GetRemovedStudentsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	var sectionIDPtr *uint
	if sectionIDRaw := strings.TrimSpace(c.Query("section_id")); sectionIDRaw != "" {
		parsed, err := strconv.ParseUint(sectionIDRaw, 10, 64)
		if err != nil || parsed == 0 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "section_id ไม่ถูกต้อง"})
		}
		parsedSectionID := uint(parsed)
		sectionIDPtr = &parsedSectionID
	}

	rows, err := repositories.GetRestorableRemovedStudents(courseID, sectionIDPtr)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงรายการกู้คืนไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{"success": true, "data": rows})
}

// POST /api/courses/:id/sections/:sectionId/students/:studentId/restore
func RestoreStudentToSectionHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	sectionID, _ := strconv.ParseUint(c.Params("sectionId"), 10, 64)
	studentID, _ := strconv.ParseUint(c.Params("studentId"), 10, 64)
	actorID := c.Locals("user_id").(uint)
	actorRole := c.Locals("user_role").(string)

	if !hasCourseAccess(courseID, actorID, actorRole) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
	}

	restored, err := repositories.RestoreStudentToSection(uint(sectionID), uint(studentID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "กู้คืนนักศึกษาไม่สำเร็จ"})
	}
	if !restored {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายการที่สามารถกู้คืนได้ หรือพ้นกำหนดเวลาแล้ว"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "กู้คืนนักศึกษาสำเร็จ"})
}

// =============================================================================
// GET /api/courses/:id/overview
// =============================================================================

func GetCourseOverviewHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	overview, err := repositories.GetCourseOverview(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูล overview ไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": overview})
}

// =============================================================================
// helpers
// =============================================================================

func hasCourseAccess(courseID string, userID uint, role string) bool {
	if role == "admin" {
		return true
	}
	if role == "instructor" && repositories.IsUserCourseInstructor(courseID, userID) {
		return true
	}
	if repositories.IsUserCourseTA(courseID, userID) {
		return true
	}
	return false
}

// =============================================================================
// PATCH /api/courses/bulk-toggle (admin only)
// =============================================================================

func BulkToggleCourseStatusHandler(c fiber.Ctx) error {
	actorID := c.Locals("user_id").(uint)

	var input struct {
		CourseIDs []string `json:"course_ids"`
		Action    string   `json:"action"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if len(input.CourseIDs) == 0 || len(input.CourseIDs) > 50 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_ids ต้องมี 1-50 รายการ"})
	}
	if input.Action != "enable" && input.Action != "disable" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "action ต้องเป็น 'enable' หรือ 'disable'"})
	}

	enable := input.Action == "enable"
	toggled, skipped, err := repositories.BulkToggleCourseStatus(input.CourseIDs, enable)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เปลี่ยนสถานะไม่สำเร็จ"})
	}

	action := "deactivate_course"
	if enable {
		action = "activate_course"
	}
	for _, course := range toggled {
		logCourseActivity(c, course.ID, actorID, action, "course", "course", course.ID, course.Name, fiber.Map{"is_active": enable})
	}
	logPrivilegedAdminAction(c, actorID, "bulk_toggle_course_status", "warn", "courses", strings.Join(input.CourseIDs, ","), fiber.Map{
		"target_type":    "course_bulk",
		"course_ids":     input.CourseIDs,
		"applied_action": input.Action,
		"toggled_count":  len(toggled),
		"skipped_count":  len(skipped),
	})

	msg := "ปิดใช้งานสำเร็จ"
	if enable {
		msg = "เปิดใช้งานสำเร็จ"
	}
	return c.JSON(fiber.Map{
		"success": true,
		"toggled": len(toggled),
		"skipped": len(skipped),
		"message": msg,
	})
}

// =============================================================================
// DELETE /api/courses/bulk (admin only)
// =============================================================================

func BulkDeleteCoursesHandler(c fiber.Ctx) error {
	actorID := c.Locals("user_id").(uint)

	var input struct {
		CourseIDs []string `json:"course_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if len(input.CourseIDs) == 0 || len(input.CourseIDs) > 50 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_ids ต้องมี 1-50 รายการ"})
	}

	deleted := 0
	for _, id := range input.CourseIDs {
		course, err := repositories.FindCourseByID(id)
		if err != nil {
			continue
		}
		if err := repositories.DeleteCourse(id); err != nil {
			log.Printf("bulk delete course %s: %v", id, err)
			continue
		}
		logCourseActivity(c, id, actorID, "delete_course", "course", "course", id, course.Name, fiber.Map{
			"code":     course.Code,
			"year":     course.Year,
			"semester": course.Semester,
		})
		deleted++
	}
	logPrivilegedAdminAction(c, actorID, "bulk_delete_courses", "critical", "courses", strings.Join(input.CourseIDs, ","), fiber.Map{
		"target_type":   "course_bulk",
		"course_ids":    input.CourseIDs,
		"deleted_count": deleted,
	})
	return c.JSON(fiber.Map{"success": true, "deleted": deleted})
}

// =============================================================================
// GET /api/courses/export (admin only)
// =============================================================================

func ExportCoursesCSVHandler(c fiber.Ctx) error {
	actorID := c.Locals("user_id").(uint)
	year, _ := strconv.Atoi(c.Query("year", "0"))
	semester, _ := strconv.Atoi(c.Query("semester", "0"))

	rows, err := repositories.GetCoursesForExport(repositories.CourseListParams{
		Search:   strings.TrimSpace(c.Query("search")),
		Year:     year,
		Semester: semester,
		Status:   c.Query("status"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลไม่สำเร็จ"})
	}

	var sb strings.Builder
	sb.WriteString("code,name,year,semester,is_active,instructor,sections_count,created_at\n")
	for _, r := range rows {
		sb.WriteString(csvField(r.Code))
		sb.WriteByte(',')
		sb.WriteString(csvField(r.Name))
		sb.WriteByte(',')
		sb.WriteString(strconv.Itoa(r.Year))
		sb.WriteByte(',')
		sb.WriteString(strconv.Itoa(r.Semester))
		sb.WriteByte(',')
		sb.WriteString(strconv.FormatBool(r.IsActive))
		sb.WriteByte(',')
		sb.WriteString(csvField(r.InstructorNames))
		sb.WriteByte(',')
		sb.WriteString(strconv.FormatInt(r.SectionsCount, 10))
		sb.WriteByte(',')
		sb.WriteString(r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
		sb.WriteByte('\n')
	}

	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="courses_export.csv"`)
	logPrivilegedAdminAction(c, actorID, "export_courses_csv", "info", "courses", "export", fiber.Map{
		"target_type": "course_export",
		"filters": fiber.Map{
			"search":   strings.TrimSpace(c.Query("search")),
			"year":     year,
			"semester": semester,
			"status":   c.Query("status"),
		},
		"row_count": len(rows),
	})
	return c.SendString(sb.String())
}

func csvField(s string) string {
	if strings.ContainsAny(s, "\",\n\r") {
		s = strings.ReplaceAll(s, `"`, `""`)
		return `"` + s + `"`
	}
	return s
}
