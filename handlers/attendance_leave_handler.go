package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"itii-assist/utils"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

// =============================================================================
// คำขอลา (attendance leave requests)
// =============================================================================

const (
	leaveEvidenceMaxFiles    = 5
	leaveEvidenceMaxFileSize = 5 * 1024 * 1024
	// เก็บใต้ uploads/private ซึ่งถูกกันไม่ให้ static เสิร์ฟ (ดู cmd/api/main.go)
	leaveEvidenceDir = "uploads/private/leave-evidence"
)

var thaiWeekdays = []string{"อาทิตย์", "จันทร์", "อังคาร", "พุธ", "พฤหัสบดี", "ศุกร์", "เสาร์"}
var thaiMonthsShort = []string{"", "ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}

func formatThaiDate(t time.Time) string {
	local := t.In(repositories.LeaveLocation())
	return fmt.Sprintf("%s %d %s %d", thaiWeekdays[int(local.Weekday())], local.Day(), thaiMonthsShort[int(local.Month())], local.Year()+543)
}

func formatThaiTimeRange(start, end *time.Time) string {
	if start == nil {
		return ""
	}
	loc := repositories.LeaveLocation()
	s := start.In(loc).Format("15:04")
	if end == nil {
		return s
	}
	return s + " ถึง " + end.In(loc).Format("15:04")
}

func leaveRequestErrorResponse(c fiber.Ctx, err error) error {
	var ve *repositories.LeaveRequestValidationError
	itemIndex := -1
	detail := ""
	if errors.As(err, &ve) {
		itemIndex = ve.ItemIndex
		detail = ve.Detail
	}
	respond := func(status int, code, message string) error {
		payload := fiber.Map{"success": false, "message": message, "code": code}
		if itemIndex >= 0 {
			payload["item_index"] = itemIndex
		}
		if detail != "" {
			payload["detail"] = detail
		}
		return c.Status(status).JSON(payload)
	}
	switch {
	case errors.Is(err, repositories.ErrLeaveRequestDisabled):
		return respond(403, "leave_disabled", "รายวิชานี้ปิดรับคำขอลาผ่านระบบ")
	case errors.Is(err, repositories.ErrLeaveRequestNotFound):
		return respond(404, "not_found", "ไม่พบคำขอลา")
	case errors.Is(err, repositories.ErrLeaveRequestNotPending):
		return respond(409, "not_pending", "คำขอนี้ถูกพิจารณาไปแล้ว ยกเลิกไม่ได้")
	case errors.Is(err, repositories.ErrLeaveRequestNotStudent):
		return respond(403, "not_in_course", "คุณไม่ได้อยู่ในรายวิชานี้")
	case errors.Is(err, repositories.ErrLeaveRequestInvalidType):
		return respond(400, "invalid_type", "ประเภทการลาไม่ถูกต้อง")
	case errors.Is(err, repositories.ErrLeaveRequestNoItems):
		return respond(400, "no_items", "กรุณาเลือกวันหรือคาบเรียนที่ต้องการลาอย่างน้อย 1 รายการ")
	case errors.Is(err, repositories.ErrLeaveRequestTooManyPending):
		return respond(429, "too_many_pending", "คุณมีคำขอลาที่รอพิจารณาอยู่ครบจำนวนแล้ว กรุณารอผลก่อนส่งใหม่")
	case errors.Is(err, repositories.ErrLeaveRequestEvidence):
		return respond(400, "evidence_required", "การลาประเภทนี้ต้องแนบหลักฐาน")
	case errors.Is(err, repositories.ErrLeaveRequestDuplicate):
		return respond(409, "duplicate", "วันหรือคาบที่เลือกมีคำขอลาอยู่แล้ว")
	case errors.Is(err, repositories.ErrLeaveRequestOutOfWindow):
		return respond(400, "out_of_window", "วันที่เลือกอยู่นอกช่วงที่รายวิชาอนุญาตให้ขอลา")
	case errors.Is(err, repositories.ErrLeaveRequestSessionInvalid):
		return respond(400, "session_invalid", "คาบเรียนที่เลือกไม่ถูกต้องหรือคุณไม่ได้อยู่ในกลุ่มเรียนนั้น")
	case errors.Is(err, repositories.ErrLeaveRequestAlreadyPresent):
		return respond(409, "already_present", "คาบนี้คุณเช็กชื่อเข้าเรียนแล้ว ไม่ต้องขอลา")
	case errors.Is(err, repositories.ErrLeaveRequestReasonRequired):
		return respond(400, "reason_required", "กรุณาระบุเหตุผลการลา")
	case errors.Is(err, repositories.ErrLeaveRequestNotReviewable):
		return respond(409, "not_reviewable", "คำขอนี้ถูกพิจารณาหรือยกเลิกไปแล้ว")
	case errors.Is(err, repositories.ErrLeaveRequestNotRevocable):
		return respond(409, "not_revocable", "ถอนการอนุมัติได้เฉพาะคำขอที่อนุมัติแล้วเท่านั้น")
	}
	log.Printf("event=leave_request_error err=%v", err)
	return respond(500, "internal", "ดำเนินการไม่สำเร็จ กรุณาลองใหม่")
}

// -----------------------------------------------------------------------------
// หลักฐาน
// -----------------------------------------------------------------------------

func saveLeaveEvidenceFiles(courseID string, files []*multipart.FileHeader) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > leaveEvidenceMaxFiles {
		return nil, fmt.Errorf("แนบหลักฐานได้สูงสุด %d ไฟล์", leaveEvidenceMaxFiles)
	}
	dir := filepath.Join(leaveEvidenceDir, courseID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	saved := make([]string, 0, len(files))
	cleanup := func() {
		for _, name := range saved {
			_ = os.Remove(filepath.Join(leaveEvidenceDir, name))
		}
	}
	for _, fh := range files {
		if fh.Size > leaveEvidenceMaxFileSize {
			cleanup()
			return nil, fmt.Errorf("ไฟล์ %s ใหญ่เกิน 5 MB", fh.Filename)
		}
		f, err := fh.Open()
		if err != nil {
			cleanup()
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(f, leaveEvidenceMaxFileSize+1))
		f.Close()
		if err != nil {
			cleanup()
			return nil, err
		}
		if len(content) > leaveEvidenceMaxFileSize {
			cleanup()
			return nil, fmt.Errorf("ไฟล์ %s ใหญ่เกิน 5 MB", fh.Filename)
		}
		// ตรวจชนิดจากเนื้อไฟล์จริง ไม่เชื่อ header ที่ client ส่งมา
		detected := http.DetectContentType(content)
		ext := ""
		switch {
		case strings.HasPrefix(detected, "image/jpeg"):
			ext = ".jpg"
		case strings.HasPrefix(detected, "image/png"):
			ext = ".png"
		case strings.HasPrefix(detected, "image/gif"):
			ext = ".gif"
		case strings.HasPrefix(detected, "image/webp"):
			ext = ".webp"
		case strings.HasPrefix(detected, "application/pdf"):
			ext = ".pdf"
		default:
			cleanup()
			return nil, fmt.Errorf("ไฟล์ %s ต้องเป็นรูปภาพ (JPG, PNG, GIF, WebP) หรือ PDF", fh.Filename)
		}
		if strings.HasPrefix(detected, "image/") && !strings.HasPrefix(detected, "image/gif") {
			if resized, ok := utils.ProcessUploadedImage(content, 2000, 2000, 85); ok {
				content = resized
				ext = ".jpg"
			}
		}
		id, err := utils.GenerateNanoID(21)
		if err != nil {
			cleanup()
			return nil, err
		}
		name := id + ext
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		saved = append(saved, filepath.ToSlash(filepath.Join(courseID, name)))
	}
	return saved, nil
}

func deleteLeaveEvidenceFiles(names []string) {
	for _, name := range names {
		clean := filepath.Clean(name)
		if strings.HasPrefix(clean, "..") {
			continue
		}
		_ = os.Remove(filepath.Join(leaveEvidenceDir, clean))
	}
}

// serveLeaveEvidence ส่งไฟล์หลักฐานเมื่อผ่านการเช็กสิทธิ์แล้ว (file ต้องอยู่ในรายการของคำขอ)
func serveLeaveEvidence(c fiber.Ctx, view *repositories.LeaveRequestView, file string) error {
	file = strings.TrimSpace(file)
	found := ""
	for _, e := range view.EvidenceList {
		if filepath.Base(e) == file {
			found = e
			break
		}
	}
	if found == "" {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบไฟล์หลักฐาน"})
	}
	path := filepath.Join(leaveEvidenceDir, filepath.Clean(found))
	if _, err := os.Stat(path); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบไฟล์หลักฐาน"})
	}
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(strings.ToLower(path), ".pdf") {
		c.Set("Content-Type", "application/pdf")
		c.Set("Content-Disposition", "inline; filename=\""+file+"\"")
	}
	return c.SendFile(path)
}

// -----------------------------------------------------------------------------
// ฝั่งนักศึกษา
// -----------------------------------------------------------------------------

func requireLeaveStudent(c fiber.Ctx, courseID string) (*models.Student, error) {
	studentID, ok := middlewares.GetStudentID(c)
	if !ok {
		return nil, c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล session นักศึกษา"})
	}
	student, err := repositories.FindStudentByID(studentID)
	if err != nil {
		return nil, c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}
	if !repositories.IsStudentInCourse(courseID, student.ID) {
		return nil, c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชานี้ในบัญชีนักศึกษา"})
	}
	return student, nil
}

// GET /api/students/me/courses/:courseId/leave-requests/context
func GetStudentLeaveContextHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	settings, course, err := repositories.GetLeaveCourseSettings(courseID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชา"})
	}
	sessions, err := repositories.ListLeaveEligibleSessions(courseID, student.ID, settings)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดรายการคาบเรียนไม่สำเร็จ"})
	}
	today := time.Now().In(repositories.LeaveLocation())
	from := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, repositories.LeaveLocation()).AddDate(0, 0, -settings.BackdateDays)
	to := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, repositories.LeaveLocation()).AddDate(0, 0, settings.AdvanceDays)
	var pendingCount int64
	config.DB.Model(&models.AttendanceLeaveRequest{}).Where("course_id = ? AND student_id = ? AND status = ?", courseID, student.ID, repositories.LeaveStatusPending).Count(&pendingCount)
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"settings":      settings,
		"course_active": course.IsActive,
		"window":        fiber.Map{"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "today": today.Format("2006-01-02")},
		"sessions":      sessions,
		"pending_count": pendingCount,
		"evidence_required_types": fiber.Map{
			"sick":     repositories.LeaveEvidenceRequired(settings.EvidencePolicy, repositories.LeaveTypeSick),
			"personal": repositories.LeaveEvidenceRequired(settings.EvidencePolicy, repositories.LeaveTypePersonal),
			"official": repositories.LeaveEvidenceRequired(settings.EvidencePolicy, repositories.LeaveTypeOfficial),
			"other":    repositories.LeaveEvidenceRequired(settings.EvidencePolicy, repositories.LeaveTypeOther),
		},
		"limits": fiber.Map{"max_files": leaveEvidenceMaxFiles, "max_file_size": leaveEvidenceMaxFileSize},
	}})
}

// GET /api/students/me/courses/:courseId/leave-requests
func GetMyLeaveRequestsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	views, err := repositories.GetStudentLeaveRequests(courseID, student.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดคำขอลาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": views})
}

// GET /api/students/me/courses/:courseId/leave-requests/:id
func GetMyLeaveRequestHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	view, err := repositories.GetLeaveRequestByID(uint(id))
	if err != nil || view.StudentID != student.ID || view.CourseID != courseID {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบคำขอลา"})
	}
	return c.JSON(fiber.Map{"success": true, "data": view})
}

// POST /api/students/me/courses/:courseId/leave-requests  (multipart/form-data)
// fields: leave_type, reason, items (JSON: [{"session_id":1} | {"date":"2026-09-20"}]), evidence[] files
func CreateLeaveRequestHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	if !courseActiveForLeave(courseID) {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "รายวิชานี้ปิดแล้ว ไม่รับคำขอลา"})
	}

	leaveType := strings.TrimSpace(c.FormValue("leave_type"))
	reason := strings.TrimSpace(c.FormValue("reason"))
	if len(reason) > 2000 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "เหตุผลยาวเกิน 2000 ตัวอักษร"})
	}
	itemsRaw := strings.TrimSpace(c.FormValue("items"))
	type itemInput struct {
		SessionID *uint  `json:"session_id"`
		Date      string `json:"date"`
	}
	var itemInputs []itemInput
	if itemsRaw != "" {
		if err := json.Unmarshal([]byte(itemsRaw), &itemInputs); err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "รูปแบบรายการวันลาไม่ถูกต้อง"})
		}
	}
	if len(itemInputs) == 0 {
		return leaveRequestErrorResponse(c, repositories.ErrLeaveRequestNoItems)
	}
	if len(itemInputs) > 30 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ขอลาได้สูงสุด 30 รายการต่อคำขอ"})
	}
	items := make([]repositories.LeaveRequestItemInput, 0, len(itemInputs))
	for _, it := range itemInputs {
		items = append(items, repositories.LeaveRequestItemInput{SessionID: it.SessionID, Date: it.Date})
	}

	// ตรวจเงื่อนไขเบา ๆ ก่อนเซฟไฟล์ จะได้ไม่เหลือไฟล์ขยะเมื่อคำขอไม่ผ่าน
	settings, _, err := repositories.GetLeaveCourseSettings(courseID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชา"})
	}
	if !settings.Enabled {
		return leaveRequestErrorResponse(c, repositories.ErrLeaveRequestDisabled)
	}
	if !repositories.IsValidLeaveType(leaveType) {
		return leaveRequestErrorResponse(c, repositories.ErrLeaveRequestInvalidType)
	}
	if reason == "" {
		return leaveRequestErrorResponse(c, repositories.ErrLeaveRequestReasonRequired)
	}

	var files []*multipart.FileHeader
	if strings.HasPrefix(strings.ToLower(c.Get("Content-Type")), "multipart/form-data") {
		if form, err := c.MultipartForm(); err == nil && form != nil {
			files = form.File["evidence"]
			if len(files) == 0 {
				files = form.File["evidence[]"]
			}
		}
	}
	if repositories.LeaveEvidenceRequired(settings.EvidencePolicy, leaveType) && len(files) == 0 {
		return leaveRequestErrorResponse(c, repositories.ErrLeaveRequestEvidence)
	}
	evidence, err := saveLeaveEvidenceFiles(courseID, files)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	created, err := repositories.CreateLeaveRequest(repositories.CreateLeaveRequestInput{
		CourseID:    courseID,
		StudentID:   student.ID,
		LeaveType:   leaveType,
		Reason:      reason,
		Evidence:    evidence,
		Items:       items,
		SubmittedIP: c.IP(),
	})
	if err != nil {
		deleteLeaveEvidenceFiles(evidence)
		return leaveRequestErrorResponse(c, err)
	}

	view, err := repositories.GetLeaveRequestByID(created.ID)
	if err != nil {
		return c.Status(201).JSON(fiber.Map{"success": true, "data": created})
	}
	writeStudentLeaveActivity(c, courseID, student, "leave_request_submitted", view)
	go notifyLeaveRequestSubmitted(view)
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "ส่งคำขอลาแล้ว รอผู้สอนพิจารณา", "data": view})
}

// DELETE /api/students/me/courses/:courseId/leave-requests/:id
func CancelMyLeaveRequestHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	request, err := repositories.CancelLeaveRequest(uint(id), student.ID)
	if err != nil {
		return leaveRequestErrorResponse(c, err)
	}
	if view, err := repositories.GetLeaveRequestByID(request.ID); err == nil {
		writeStudentLeaveActivity(c, courseID, student, "leave_request_cancelled", view)
	}
	return c.JSON(fiber.Map{"success": true, "message": "ยกเลิกคำขอลาแล้ว", "data": request})
}

// GET /api/students/me/courses/:courseId/leave-requests/:id/evidence/:file
func GetMyLeaveEvidenceHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	student, errResp := requireLeaveStudent(c, courseID)
	if errResp != nil {
		return errResp
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	view, err := repositories.GetLeaveRequestByID(uint(id))
	if err != nil || view.StudentID != student.ID || view.CourseID != courseID {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบคำขอลา"})
	}
	return serveLeaveEvidence(c, view, c.Params("file"))
}

func courseActiveForLeave(courseID string) bool {
	var course models.Course
	if err := config.DB.Select("is_active").First(&course, "id = ?", courseID).Error; err != nil {
		return false
	}
	return course.IsActive
}

// writeStudentLeaveActivity เขียน course activity log แบบ actor เป็นนักศึกษา (ไม่มี user account)
func writeStudentLeaveActivity(c fiber.Ctx, courseID string, student *models.Student, action string, view *repositories.LeaveRequestView) {
	detail := leaveActivityDetail(view)
	payload, _ := json.Marshal(detail)
	entry := models.CourseActivityLog{
		CourseID:    courseID,
		ActorUserID: 0,
		ActorEmail:  student.Email,
		ActorRole:   "student",
		Action:      action,
		Category:    "attendance",
		TargetType:  "leave_request",
		TargetID:    strconv.Itoa(int(view.ID)),
		TargetName:  fmt.Sprintf("%s %s", student.StudentID, student.FullName),
		Detail:      datatypes.JSON(payload),
		IPAddress:   c.IP(),
		UserAgent:   string(c.Request().Header.UserAgent()),
		CreatedAt:   time.Now(),
	}
	_ = config.DB.Create(&entry).Error
}

func leaveActivityDetail(view *repositories.LeaveRequestView) fiber.Map {
	dates := make([]string, 0, len(view.Items))
	sessionIDs := make([]uint, 0, len(view.Items))
	for _, it := range view.Items {
		dates = append(dates, it.LeaveDateString)
		if it.AttendanceSessionID != nil {
			sessionIDs = append(sessionIDs, *it.AttendanceSessionID)
		}
	}
	return fiber.Map{
		"leave_request_id": view.ID,
		"student_id":       view.StudentID,
		"leave_type":       view.LeaveType,
		"status":           view.Status,
		"dates":            dates,
		"session_ids":      sessionIDs,
		"evidence_count":   len(view.EvidenceList),
	}
}

// -----------------------------------------------------------------------------
// ฝั่งผู้สอน / TA
// -----------------------------------------------------------------------------

// GET /api/attendance/leave-requests?course_id=&status=&student_id=&limit=&offset=
func ListLeaveRequestsHandler(c fiber.Ctx) error {
	courseID := strings.TrimSpace(c.Query("course_id"))
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายวิชา"})
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	studentID, _ := strconv.ParseUint(c.Query("student_id", "0"), 10, 64)
	views, total, err := repositories.ListCourseLeaveRequests(repositories.LeaveRequestListFilter{
		CourseID:  courseID,
		Status:    strings.TrimSpace(c.Query("status", "")),
		StudentID: uint(studentID),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดคำขอลาไม่สำเร็จ"})
	}
	counts, _ := repositories.CountCourseLeaveRequests(courseID)
	settings, _, _ := repositories.GetLeaveCourseSettings(courseID)
	return c.JSON(fiber.Map{"success": true, "data": views, "meta": fiber.Map{"total": total, "limit": limit, "offset": offset, "counts": counts, "settings": settings}})
}

// GET /api/attendance/leave-requests/count?course_id=
func CountLeaveRequestsHandler(c fiber.Ctx) error {
	courseID := strings.TrimSpace(c.Query("course_id"))
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายวิชา"})
	}
	counts, err := repositories.CountCourseLeaveRequests(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "นับคำขอลาไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": counts})
}

// GET /api/attendance/leave-requests/:id
func GetLeaveRequestHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	view, err := repositories.GetLeaveRequestByID(uint(id))
	if err != nil {
		return leaveRequestErrorResponse(c, err)
	}
	history, _ := repositories.GetAttendanceStudentHistoryInCourse(view.CourseID, view.StudentID, 50)
	return c.JSON(fiber.Map{"success": true, "data": view, "meta": fiber.Map{"student_history": history}})
}

// GET /api/attendance/leave-requests/:id/evidence/:file
func GetLeaveEvidenceHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	view, err := repositories.GetLeaveRequestByID(uint(id))
	if err != nil {
		return leaveRequestErrorResponse(c, err)
	}
	return serveLeaveEvidence(c, view, c.Params("file"))
}

type leaveReviewInput struct {
	Approved *bool  `json:"approved"`
	Comment  string `json:"comment"`
	Items    []struct {
		ItemID   uint   `json:"item_id"`
		Approved bool   `json:"approved"`
		Comment  string `json:"comment"`
	} `json:"items"`
}

// POST /api/attendance/leave-requests/:id/review
// body: {approved: true|false, comment} หรือ {items: [{item_id, approved, comment}], comment} สำหรับอนุมัติบางวัน
func (h *AttendanceHandler) ReviewLeaveRequest(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	var input leaveReviewInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.Approved == nil && len(input.Items) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุผลการพิจารณา"})
	}
	if len(strings.TrimSpace(input.Comment)) > 2000 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ความเห็นยาวเกิน 2000 ตัวอักษร"})
	}
	reviewerID := c.Locals("user_id").(uint)
	approveAll := input.Approved != nil && *input.Approved
	decisions := make([]repositories.LeaveItemDecision, 0, len(input.Items))
	for _, it := range input.Items {
		decisions = append(decisions, repositories.LeaveItemDecision{ItemID: it.ItemID, Approved: it.Approved, Comment: it.Comment})
	}
	result, err := repositories.ReviewLeaveRequest(uint(id), reviewerID, approveAll, decisions, input.Comment)
	if err != nil {
		return leaveRequestErrorResponse(c, err)
	}
	view, err := repositories.GetLeaveRequestByID(uint(id))
	if err != nil {
		return c.JSON(fiber.Map{"success": true, "data": result.Request})
	}
	h.afterLeaveReview(c, reviewerID, view, result)
	return c.JSON(fiber.Map{"success": true, "message": "บันทึกผลการพิจารณาแล้ว", "data": view, "meta": fiber.Map{
		"approved_items": result.ApprovedItems,
		"rejected_items": result.RejectedItems,
		"applied_items":  result.AppliedItems,
		"awaiting_items": result.AwaitingItems,
		"superseded":     result.Superseded,
	}})
}

func (h *AttendanceHandler) afterLeaveReview(c fiber.Ctx, reviewerID uint, view *repositories.LeaveRequestView, result *repositories.LeaveReviewResult) {
	detail := leaveActivityDetail(view)
	detail["review_comment"] = view.ReviewComment
	detail["approved_items"] = result.ApprovedItems
	detail["rejected_items"] = result.RejectedItems
	detail["applied_items"] = result.AppliedItems
	detail["awaiting_items"] = result.AwaitingItems
	targetName := ""
	if view.Student != nil {
		targetName = fmt.Sprintf("%s %s", view.Student.StudentID, view.Student.FullName)
	}
	logCourseActivity(c, view.CourseID, reviewerID, "leave_request_reviewed", "attendance", "leave_request", view.ID, targetName, detail)
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    view.CourseID,
		ActorUserID: reviewerID,
		Action:      "attendance.leave_request.reviewed",
		TargetType:  "leave_request",
		TargetID:    strconv.Itoa(int(view.ID)),
		Description: fmt.Sprintf("Leave request %d reviewed: %s", view.ID, view.Status),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	for _, it := range view.Items {
		if it.AttendanceSessionID != nil && (it.ItemStatus == repositories.LeaveItemApplied || it.ItemStatus == repositories.LeaveItemSuperseded) {
			emitAttendanceRecordUpdated(*it.AttendanceSessionID, view.StudentID)
		}
	}
	go notifyLeaveRequestReviewed(view)
}

// POST /api/attendance/leave-requests/batch-review  body: {ids: [], approved: bool, comment}
func (h *AttendanceHandler) BatchReviewLeaveRequests(c fiber.Ctx) error {
	var input struct {
		IDs      []uint `json:"ids"`
		Approved *bool  `json:"approved"`
		Comment  string `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.Approved == nil || len(input.IDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if len(input.IDs) > 100 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "พิจารณาได้สูงสุด 100 คำขอต่อครั้ง"})
	}
	reviewerID := c.Locals("user_id").(uint)
	userRole, _ := c.Locals("user_role").(string)
	// ทุกคำขอต้องอยู่ในวิชาที่ผู้ตรวจมีสิทธิ์ (route ตรวจได้แค่วิชาแรก จึงต้องเช็กซ้ำรายใบ)
	processed := 0
	failed := make([]fiber.Map, 0)
	for _, id := range input.IDs {
		courseID, err := repositories.GetCourseIDByLeaveRequestID(id)
		if err != nil {
			failed = append(failed, fiber.Map{"id": id, "reason": "not_found"})
			continue
		}
		allowed, err := repositories.HasCoursePermission(courseID, reviewerID, userRole, repositories.PermissionReviewLeaveRequests)
		if err != nil || !allowed {
			failed = append(failed, fiber.Map{"id": id, "reason": "forbidden"})
			continue
		}
		result, err := repositories.ReviewLeaveRequest(id, reviewerID, *input.Approved, nil, input.Comment)
		if err != nil {
			failed = append(failed, fiber.Map{"id": id, "reason": err.Error()})
			continue
		}
		processed++
		if view, err := repositories.GetLeaveRequestByID(id); err == nil {
			h.afterLeaveReview(c, reviewerID, view, result)
		}
	}
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("พิจารณาแล้ว %d คำขอ", processed), "data": fiber.Map{"processed": processed, "failed": failed}})
}

// POST /api/attendance/leave-requests/:id/revoke  body: {comment}
func (h *AttendanceHandler) RevokeLeaveRequest(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสคำขอไม่ถูกต้อง"})
	}
	var input struct {
		Comment string `json:"comment"`
	}
	_ = c.Bind().JSON(&input)
	if strings.TrimSpace(input.Comment) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุเหตุผลในการถอนการอนุมัติ"})
	}
	reviewerID := c.Locals("user_id").(uint)
	request, restored, err := repositories.RevokeLeaveRequest(uint(id), reviewerID, input.Comment)
	if err != nil {
		return leaveRequestErrorResponse(c, err)
	}
	view, err := repositories.GetLeaveRequestByID(request.ID)
	if err == nil {
		detail := leaveActivityDetail(view)
		detail["review_comment"] = view.ReviewComment
		detail["restored_records"] = restored
		targetName := ""
		if view.Student != nil {
			targetName = fmt.Sprintf("%s %s", view.Student.StudentID, view.Student.FullName)
		}
		logCourseActivity(c, view.CourseID, reviewerID, "leave_request_revoked", "attendance", "leave_request", view.ID, targetName, detail)
		reqID, _, ip := services.ExtractMeta(c)
		h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
			CourseID:    view.CourseID,
			ActorUserID: reviewerID,
			Action:      "attendance.leave_request.revoked",
			TargetType:  "leave_request",
			TargetID:    strconv.Itoa(int(view.ID)),
			Description: fmt.Sprintf("Leave request %d revoked, %d records restored", view.ID, restored),
			RequestID:   reqID,
			IPAddress:   ip,
		})
		for _, it := range view.Items {
			if it.AttendanceSessionID != nil {
				emitAttendanceRecordUpdated(*it.AttendanceSessionID, view.StudentID)
			}
		}
		go notifyLeaveRequestReviewed(view)
	}
	return c.JSON(fiber.Map{"success": true, "message": "ถอนการอนุมัติแล้ว", "data": view, "meta": fiber.Map{"restored_records": restored}})
}

// GET /api/attendance/:id/records/:recordId/history
func GetAttendanceRecordHistoryHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	recordID, err := strconv.ParseUint(c.Params("recordId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid record ID"})
	}
	var record models.AttendanceRecord
	if err := config.DB.Where("id = ? AND attendance_session_id = ?", uint(recordID), uint(sessionID)).First(&record).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance record not found"})
	}
	rows, err := repositories.GetAttendanceRecordHistory(record.ID, 100)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "โหลดประวัติไม่สำเร็จ"})
	}
	// แนบชื่อผู้กระทำ (user) ให้ครบ
	userIDs := make([]uint, 0)
	for _, r := range rows {
		if r.ActorType == repositories.AttendanceActorUser && r.ActorID != nil {
			userIDs = append(userIDs, *r.ActorID)
		}
	}
	names := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := config.DB.Select("id, full_name, username").Where("id IN ?", userIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				if strings.TrimSpace(u.FullName) != "" {
					names[u.ID] = u.FullName
				} else {
					names[u.ID] = u.Username
				}
			}
		}
	}
	items := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		actorName := ""
		if r.ActorType == repositories.AttendanceActorUser && r.ActorID != nil {
			actorName = names[*r.ActorID]
		}
		items = append(items, fiber.Map{
			"id":               r.ID,
			"from_status":      r.FromStatus,
			"to_status":        r.ToStatus,
			"source":           r.Source,
			"actor_type":       r.ActorType,
			"actor_id":         r.ActorID,
			"actor_name":       actorName,
			"leave_request_id": r.LeaveRequestID,
			"note":             r.Note,
			"created_at":       r.CreatedAt,
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"record": record, "history": items}})
}

// -----------------------------------------------------------------------------
// แจ้งเตือน
// -----------------------------------------------------------------------------

func leaveEmailItems(view *repositories.LeaveRequestView) []services.LeaveEmailItem {
	out := make([]services.LeaveEmailItem, 0, len(view.Items))
	for _, it := range view.Items {
		day, _ := time.ParseInLocation("2006-01-02", it.LeaveDateString, repositories.LeaveLocation())
		sessionText := ""
		if it.SessionTitle != "" {
			sessionText = it.SessionTitle
			if tr := formatThaiTimeRange(it.SessionStart, it.SessionEnd); tr != "" {
				sessionText += " " + tr
			}
		}
		result := it.ItemStatus
		switch it.ItemStatus {
		case repositories.LeaveItemApplied, repositories.LeaveItemApproved:
			result = "approved"
		case repositories.LeaveItemAwaitingSession:
			result = "awaiting"
		}
		out = append(out, services.LeaveEmailItem{DateText: formatThaiDate(day), SessionText: sessionText, Result: result})
	}
	return out
}

func notifyLeaveRequestSubmitted(view *repositories.LeaveRequestView) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("event=leave_notify_submitted_panic err=%v", r)
		}
	}()
	reviewerIDs, err := repositories.GetLeaveRequestReviewerUserIDs(view.CourseID)
	if err != nil || len(reviewerIDs) == 0 {
		return
	}
	var users []models.User
	if err := config.DB.Where("id IN ? AND is_active = true", reviewerIDs).Find(&users).Error; err != nil {
		return
	}
	studentName, studentCode := "นักศึกษา", ""
	if view.Student != nil {
		studentName, studentCode = view.Student.FullName, view.Student.StudentID
	}
	courseName := view.CourseName
	if courseName == "" {
		courseName = view.CourseID
	}
	typeLabel := services.LeaveTypeLabelTH(view.LeaveType)
	link := "/classroom/" + view.CourseID + "?tab=attendance&view=leave"
	title := fmt.Sprintf("คำขอ%sใหม่: %s", typeLabel, studentName)
	message := fmt.Sprintf("%s %s ส่งคำขอ%s %d วัน ในวิชา %s", studentCode, studentName, typeLabel, len(view.Items), courseName)
	data := buildNotifData(view.CourseID, strconv.Itoa(int(view.ID)), "leave_request", studentName)
	items := leaveEmailItems(view)
	for i := range users {
		u := users[i]
		createNotificationForUser(u.ID, view.CourseID, "leave_request_submitted", title, message, link, data)
		if strings.TrimSpace(u.Email) == "" {
			continue
		}
		name := strings.TrimSpace(u.FullName)
		if name == "" {
			name = u.Username
		}
		if err := services.SendLeaveRequestSubmittedEmail(u.Email, name, courseName, studentName, studentCode, view.LeaveType, view.Reason, items, len(view.EvidenceList), services.LeaveRequestReviewURL(view.CourseID)); err != nil {
			services.LogEmailDeliveryError("leave_request_submitted", err)
		}
	}
}

func notifyLeaveRequestReviewed(view *repositories.LeaveRequestView) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("event=leave_notify_reviewed_panic err=%v", r)
		}
	}()
	if view.Student == nil || strings.TrimSpace(view.Student.Email) == "" {
		return
	}
	courseName := view.CourseName
	if courseName == "" {
		courseName = view.CourseID
	}
	if err := services.SendLeaveRequestReviewedEmail(view.Student.Email, view.Student.FullName, courseName, view.LeaveType, view.Status, view.ReviewComment, leaveEmailItems(view), services.StudentLeaveRequestURL(view.CourseID)); err != nil {
		services.LogEmailDeliveryError("leave_request_reviewed", err)
	}
}

// RunLeaveRequestPendingReminder ส่งเมลเตือนผู้สอนสำหรับคำขอที่ค้างเกิน olderThan (เรียกจาก ticker วันละครั้ง)
func RunLeaveRequestPendingReminder(olderThan time.Duration) {
	grouped, err := repositories.PendingLeaveRequestsOlderThan(time.Now().Add(-olderThan))
	if err != nil || len(grouped) == 0 {
		return
	}
	for courseID, requests := range grouped {
		reviewerIDs, err := repositories.GetLeaveRequestReviewerUserIDs(courseID)
		if err != nil || len(reviewerIDs) == 0 {
			continue
		}
		var course models.Course
		courseName := courseID
		if err := config.DB.Select("name").First(&course, "id = ?", courseID).Error; err == nil && course.Name != "" {
			courseName = course.Name
		}
		oldest := requests[0].CreatedAt
		oldestDays := int(time.Since(oldest).Hours() / 24)
		var users []models.User
		if err := config.DB.Where("id IN ? AND is_active = true AND email <> ''", reviewerIDs).Find(&users).Error; err != nil {
			continue
		}
		for _, u := range users {
			name := strings.TrimSpace(u.FullName)
			if name == "" {
				name = u.Username
			}
			if err := services.SendLeaveRequestPendingReminderEmail(u.Email, name, courseName, len(requests), oldestDays, services.LeaveRequestReviewURL(courseID)); err != nil {
				services.LogEmailDeliveryError("leave_request_pending_reminder", err)
			}
		}
	}
}
