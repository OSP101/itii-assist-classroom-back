package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/observability"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/services"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// AttendanceHandler — struct-based handler with audit logger

type AttendanceHandler struct {
	auditLogger *services.AuditLogger
}

func NewAttendanceHandler(auditLogger *services.AuditLogger) *AttendanceHandler {
	return &AttendanceHandler{auditLogger: auditLogger}
}

func uniqueUintValues(values []uint) []uint {
	seen := make(map[uint]bool, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func attendancePublicErrorResponse(err error, fallbackStatus int, fallbackTitle string, fallbackMessage string) (int, fiber.Map) {
	var publicErr *repositories.AttendancePublicError
	if errors.As(err, &publicErr) {
		return publicErr.HTTPStatus, fiber.Map{
			"success": false,
			"code":    publicErr.Code,
			"title":   publicErr.Title,
			"message": publicErr.Message,
		}
	}

	message := fallbackMessage
	if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
		message = trimmed
	}

	if strings.Contains(message, "student is not registered in this attendance session") {
		publicErr = repositories.ErrAttendanceStudentNotEligiblePublic
	} else if strings.Contains(message, "ไม่ได้ลงทะเบียนในรายวิชานี้") {
		publicErr = repositories.ErrAttendanceCourseNotRegisteredPublic
	} else if strings.Contains(message, "ไม่พบข้อมูลนักศึกษา") {
		publicErr = repositories.ErrAttendanceStudentNotFoundPublic
	}

	if publicErr != nil {
		return publicErr.HTTPStatus, fiber.Map{
			"success": false,
			"code":    publicErr.Code,
			"title":   publicErr.Title,
			"message": publicErr.Message,
		}
	}

	return fallbackStatus, fiber.Map{
		"success": false,
		"title":   fallbackTitle,
		"message": message,
	}
}

type googleTokenInfo struct {
	IssuedTo      string `json:"issued_to"`
	Audience      string `json:"aud"`
	UserID        string `json:"user_id"`
	Scope         string `json:"scope"`
	ExpiresIn     string `json:"expires_in"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	AccessType    string `json:"access_type"`
	IssuedAt      string `json:"iat"`
	ExpiresAt     string `json:"exp"`
	Issuer        string `json:"iss"`
	Subject       string `json:"sub"`
	Error         string `json:"error_description"`
}

func normalizeGoogleBool(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	return normalized == "true" || normalized == "1" || normalized == "yes"
}

func verifyGoogleIDToken(ctx context.Context, idToken string) (*googleTokenInfo, error) {
	token := strings.TrimSpace(idToken)
	if token == "" {
		return nil, errors.New("google_token is required")
	}

	googleClientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	if googleClientID == "" {
		return nil, errors.New("google client id is not configured")
	}

	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var info googleTokenInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, errors.New("failed to parse google token response")
	}

	if resp.StatusCode != http.StatusOK {
		if strings.TrimSpace(info.Error) != "" {
			return nil, errors.New(info.Error)
		}
		return nil, errors.New("invalid google token")
	}

	aud := strings.TrimSpace(info.Audience)
	if aud == "" {
		aud = strings.TrimSpace(info.IssuedTo)
	}
	if aud == "" || aud != googleClientID {
		return nil, errors.New("google token audience mismatch")
	}

	iss := strings.TrimSpace(strings.ToLower(info.Issuer))
	if iss != "accounts.google.com" && iss != "https://accounts.google.com" {
		return nil, errors.New("google token issuer mismatch")
	}

	if !normalizeGoogleBool(info.EmailVerified) {
		return nil, errors.New("google email is not verified")
	}

	expiresAt, err := strconv.ParseInt(strings.TrimSpace(info.ExpiresAt), 10, 64)
	if err != nil {
		return nil, errors.New("google token expiry is invalid")
	}
	if time.Unix(expiresAt, 0).Before(time.Now()) {
		return nil, errors.New("google token has expired")
	}

	if strings.TrimSpace(info.Email) == "" || strings.TrimSpace(info.Subject) == "" {
		return nil, errors.New("google token payload is incomplete")
	}

	return &info, nil
}

func authenticatedStudentIDFromContext(c fiber.Ctx) uint {
	raw := c.Locals("student_id")
	if raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case uint:
		return v
	case float64:
		return uint(v)
	default:
		return 0
	}
}

func desiredAttendanceStudentIDs(courseID string, sectionIDs []uint) ([]uint, error) {
	type row struct {
		StudentID uint `gorm:"column:student_id"`
	}
	var rows []row
	if len(sectionIDs) > 0 {
		if err := config.DB.Raw(`SELECT DISTINCT student_id FROM course_section_students WHERE course_section_id IN ?`, sectionIDs).Scan(&rows).Error; err != nil {
			return nil, err
		}
	} else {
		if err := config.DB.Raw(`
			SELECT DISTINCT css.student_id
			FROM course_section_students css
			JOIN course_sections cs ON cs.id = css.course_section_id
			WHERE cs.course_id = ?
		`, courseID).Scan(&rows).Error; err != nil {
			return nil, err
		}
	}

	studentIDs := make([]uint, 0, len(rows))
	for _, item := range rows {
		studentIDs = append(studentIDs, item.StudentID)
	}
	return uniqueUintValues(studentIDs), nil
}

func syncAttendanceSessionTargets(session *models.AttendanceSession, sectionIDs []uint) error {
	tx := config.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Where("attendance_session_id = ?", session.ID).Delete(&models.AttendanceSessionSection{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	if len(sectionIDs) > 0 {
		links := make([]models.AttendanceSessionSection, len(sectionIDs))
		for i, sectionID := range sectionIDs {
			links[i] = models.AttendanceSessionSection{
				AttendanceSessionID: session.ID,
				CourseSectionID:     sectionID,
				CreatedAt:           time.Now(),
			}
		}
		if err := tx.Create(&links).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	desiredStudentIDs, err := desiredAttendanceStudentIDs(session.CourseID, sectionIDs)
	if err != nil {
		tx.Rollback()
		return err
	}

	if len(desiredStudentIDs) > 0 {
		if err := tx.Where("attendance_session_id = ? AND student_id NOT IN ?", session.ID, desiredStudentIDs).Delete(&models.AttendanceRecord{}).Error; err != nil {
			tx.Rollback()
			return err
		}
	} else {
		if err := tx.Where("attendance_session_id = ?", session.ID).Delete(&models.AttendanceRecord{}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	type existingRow struct {
		StudentID uint `gorm:"column:student_id"`
	}
	var existingRows []existingRow
	if err := tx.Raw(`SELECT student_id FROM attendance_records WHERE attendance_session_id = ?`, session.ID).Scan(&existingRows).Error; err != nil {
		tx.Rollback()
		return err
	}

	existing := make(map[uint]bool, len(existingRows))
	for _, row := range existingRows {
		existing[row.StudentID] = true
	}

	newRecords := make([]models.AttendanceRecord, 0)
	for _, studentID := range desiredStudentIDs {
		if existing[studentID] {
			continue
		}
		newRecords = append(newRecords, models.AttendanceRecord{
			AttendanceSessionID: session.ID,
			StudentID:           studentID,
			Status:              "absent",
			CreatedAt:           time.Now(),
		})
	}

	if len(newRecords) > 0 {
		if err := tx.Create(&newRecords).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

func computeLateThresholdTime(startTime time.Time, lateThresholdTime string, lateThresholdMinutes int) time.Time {
	if strings.TrimSpace(lateThresholdTime) != "" {
		parts := strings.Split(lateThresholdTime, ":")
		if len(parts) >= 2 {
			hours, _ := strconv.Atoi(parts[0])
			minutes, _ := strconv.Atoi(parts[1])
			seconds := 0
			if len(parts) > 2 {
				seconds, _ = strconv.Atoi(parts[2])
			}
			return time.Date(startTime.Year(), startTime.Month(), startTime.Day(), hours, minutes, seconds, 0, startTime.Location())
		}
	}
	return startTime.Add(time.Duration(lateThresholdMinutes) * time.Minute)
}

func classifyAttendanceCheckIn(checkIn time.Time, startTime time.Time, endTime time.Time, lateThreshold time.Time) string {
	if checkIn.Before(startTime) || checkIn.After(endTime) {
		return "invalid"
	}
	if checkIn.After(lateThreshold) {
		return "late"
	}
	return "present"
}

func loadStudentsMap(studentIDs []uint) (map[uint]models.Student, error) {
	result := map[uint]models.Student{}
	if len(studentIDs) == 0 {
		return result, nil
	}

	var students []models.Student
	if err := config.DB.Select("id", "student_id", "full_name", "email").Where("id IN ?", uniqueUintValues(studentIDs)).Find(&students).Error; err != nil {
		return nil, err
	}
	for _, student := range students {
		result[student.ID] = student
	}
	return result, nil
}

func nullableAttendanceString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableAttendanceFloatString(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func attendanceSectionResponse(section *repositories.AttendanceSectionBasic) interface{} {
	if section == nil {
		return nil
	}
	return fiber.Map{
		"id":         section.ID,
		"section_no": section.SectionNo,
	}
}

func attendanceCourseResponse(course *repositories.AttendanceCourseBasic) interface{} {
	if course == nil {
		return nil
	}
	return fiber.Map{
		"id":       course.ID,
		"code":     course.Code,
		"name":     course.Name,
		"year":     course.Year,
		"semester": course.Semester,
	}
}

func attendanceCreatorResponse(creator *repositories.AttendanceCreatorBasic) interface{} {
	if creator == nil {
		return nil
	}
	return fiber.Map{
		"id":        creator.ID,
		"full_name": creator.FullName,
	}
}

func attendanceSessionDetailResponse(detail *repositories.AttendanceSessionDetail) fiber.Map {
	records := make([]fiber.Map, len(detail.Records))
	for i, record := range detail.Records {
		records[i] = fiber.Map{
			"id":                    record.ID,
			"attendance_session_id": record.AttendanceSessionID,
			"student_id":            record.StudentID,
			"check_in_time":         record.CheckInTime,
			"status":                record.Status,
			"google_email":          nullableAttendanceString(record.GoogleEmail),
			"google_id":             nullableAttendanceString(record.GoogleID),
			"pin_verified":          record.PinVerified,
			"location_verified":     record.LocationVerified,
			"location_lat":          nullableAttendanceFloatString(record.LocationLat),
			"location_lng":          nullableAttendanceFloatString(record.LocationLng),
			"distance_meters":       record.DistanceMeters,
			"note":                  nullableAttendanceString(record.Note),
			"updated_by":            record.UpdatedBy,
			"created_at":            record.CreatedAt,
			"updated_at":            record.UpdatedAt,
			"student": fiber.Map{
				"id":         record.Student.ID,
				"student_id": record.Student.StudentID,
				"full_name":  record.Student.FullName,
				"email":      record.Student.Email,
			},
		}
	}

	return fiber.Map{
		"id":                     detail.ID,
		"course_id":              detail.CourseID,
		"course_section_id":      detail.CourseSectionID,
		"course_section_ids":     detail.CourseSectionIDs,
		"title":                  detail.Title,
		"auto_rotate_pin":        detail.AutoRotatePin,
		"pin_mode":               detail.PinMode,
		"pin_code":               detail.PinCode,
		"pin_issued_at":          detail.PinIssuedAt,
		"pin_rotates_at":         detail.PinRotatesAt,
		"session_type":           detail.SessionType,
		"check_location":         detail.CheckLocation,
		"location_lat":           nullableAttendanceFloatString(detail.LocationLat),
		"location_lng":           nullableAttendanceFloatString(detail.LocationLng),
		"radius_meters":          detail.RadiusMeters,
		"start_time":             detail.StartTime,
		"end_time":               detail.EndTime,
		"late_threshold_minutes": detail.LateThresholdMinutes,
		"status":                 detail.Status,
		"created_by":             detail.CreatedBy,
		"created_at":             detail.CreatedAt,
		"updated_at":             detail.UpdatedAt,
		"section":                attendanceSectionResponse(detail.Section),
		"course":                 attendanceCourseResponse(detail.Course),
		"creator":                attendanceCreatorResponse(detail.Creator),
		"records":                records,
		"stats": fiber.Map{
			"total_students": detail.Stats.TotalStudents,
			"present":        detail.Stats.Present,
			"late":           detail.Stats.Late,
			"leave":          detail.Stats.Leave,
			"absent":         detail.Stats.Absent,
			"checked_in":     detail.Stats.CheckedIn,
			"not_checked_in": detail.Stats.NotCheckedIn,
		},
	}
}

func attendanceSessionPayload(session models.AttendanceSession, sectionIDs []uint) fiber.Map {
	return fiber.Map{
		"id":                     session.ID,
		"course_id":              session.CourseID,
		"course_section_id":      session.CourseSectionID,
		"course_section_ids":     uniqueUintValues(sectionIDs),
		"title":                  session.Title,
		"auto_rotate_pin":        session.AutoRotatePin,
		"pin_mode":               session.PinMode,
		"pin_code":               session.PinCode,
		"pin_issued_at":          session.PinIssuedAt,
		"pin_rotates_at":         session.PinRotatesAt,
		"session_type":           session.SessionType,
		"check_location":         session.CheckLocation,
		"location_lat":           nullableAttendanceFloatString(session.LocationLat),
		"location_lng":           nullableAttendanceFloatString(session.LocationLng),
		"radius_meters":          session.RadiusMeters,
		"start_time":             session.StartTime,
		"end_time":               session.EndTime,
		"late_threshold_minutes": session.LateThresholdMinutes,
		"late_threshold_time":    nullableAttendanceString(session.LateThresholdTime),
		"status":                 session.Status,
		"created_by":             session.CreatedBy,
		"created_at":             session.CreatedAt,
		"updated_at":             session.UpdatedAt,
	}
}

// GET /api/attendance/check-in/:sessionId/info  (public)
func GetSessionInfoHandler(c fiber.Ctx) error {
	idStr := c.Params("sessionId")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	info, err := repositories.GetSessionInfo(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": info})
}

// POST /api/attendance/verify-pin (public)
func VerifyAttendancePINHandler(c fiber.Ctx) error {
	var input struct {
		PinCode string `json:"pin_code"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.PinCode) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "pin_code is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sessionID, err := repositories.LookupAttendanceSessionIDByPIN(ctx, input.PinCode)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "PIN ไม่ถูกต้อง หรือไม่มีการเปิดเช็คชื่อ"})
	}

	info, err := repositories.GetSessionInfo(sessionID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance session not found"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id":      info.ID,
			"title":           info.Title,
			"status":          info.Status,
			"session_type":    info.SessionType,
			"check_location":  info.CheckLocation,
			"auto_rotate_pin": info.AutoRotatePin,
			"pin_mode":        info.PinMode,
			"course":          info.Course,
			"section":         info.Section,
		},
	})
}

// POST /api/attendance/check-in/:sessionId  (public)
func StudentCheckInHandler(c fiber.Ctx) error {
	idStr := c.Params("sessionId")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	var input struct {
		StudentID   *uint    `json:"student_id"`
		PinCode     string   `json:"pin_code"`
		ClientID    string   `json:"client_request_id"`
		Lat         *float64 `json:"lat"`
		Lng         *float64 `json:"lng"`
		LocationLat *float64 `json:"location_lat"`
		LocationLng *float64 `json:"location_lng"`
		GoogleEmail string   `json:"google_email"`
		GoogleID    string   `json:"google_id"`
		GoogleToken string   `json:"google_token"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	lat := input.LocationLat
	if lat == nil {
		lat = input.Lat
	}
	lng := input.LocationLng
	if lng == nil {
		lng = input.Lng
	}

	studentID := uint(0)
	var student models.Student
	verifiedEmail := ""
	verifiedGoogleID := ""

	if authStudentID := authenticatedStudentIDFromContext(c); authStudentID > 0 {
		studentID = authStudentID
		if err := config.DB.Select("id", "student_id", "full_name", "email").Where("id = ?", studentID).First(&student).Error; err != nil {
			return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
				"success": false,
				"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
				"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
				"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
			})
		}
		verifiedEmail = strings.TrimSpace(strings.ToLower(student.Email))
	} else {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer verifyCancel()
		tokenInfo, verifyErr := verifyGoogleIDToken(verifyCtx, input.GoogleToken)
		if verifyErr != nil {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "Google identity verification failed"})
		}

		verifiedEmail = strings.TrimSpace(strings.ToLower(tokenInfo.Email))
		providedEmail := strings.TrimSpace(strings.ToLower(input.GoogleEmail))
		if providedEmail != "" && providedEmail != verifiedEmail {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "Google account mismatch"})
		}

		verifiedGoogleID = strings.TrimSpace(tokenInfo.Subject)
		providedGoogleID := strings.TrimSpace(input.GoogleID)
		if providedGoogleID != "" && providedGoogleID != verifiedGoogleID {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "Google account mismatch"})
		}

		if err := config.DB.Select("id", "student_id", "full_name", "email").Where("LOWER(email) = LOWER(?)", verifiedEmail).First(&student).Error; err == nil {
			studentID = student.ID
		}
	}

	if input.StudentID != nil && studentID != 0 && *input.StudentID != studentID {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "Student identity mismatch"})
	}
	if studentID == 0 {
		return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
			"success": false,
			"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
			"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
			"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
		})
	}

	result, err := repositories.StudentCheckIn(uint(id), studentID, input.PinCode, lat, lng, verifiedEmail, verifiedGoogleID, input.ClientID)
	if err != nil {
		statusCode, payload := attendancePublicErrorResponse(err, 400, "เช็คชื่อไม่สำเร็จ", "ไม่สามารถเช็คชื่อได้ในขณะนี้")
		return c.Status(statusCode).JSON(payload)
	}
	if student.ID == 0 {
		if err := config.DB.Select("id", "student_id", "full_name", "email").Where("id = ?", studentID).First(&student).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load student after check-in"})
		}
	}

	message := "เช็คชื่อสำเร็จ: มาเรียน"
	if result.Status == "late" {
		message = "เช็คชื่อสำเร็จ: มาสาย"
	}

	recordPayload := fiber.Map{
		"attendance_session_id": id,
		"student_id":            studentID,
		"check_in_time":         result.CheckInTime,
		"status":                result.Status,
		"location_verified":     result.LocationVerified,
		"distance_meters":       result.DistanceMeters,
		"student":               fiber.Map{"id": student.ID, "student_id": student.StudentID, "full_name": student.FullName, "email": student.Email},
	}
	var checkedInRecord models.AttendanceRecord
	if err := config.DB.Where("attendance_session_id = ? AND student_id = ?", uint(id), studentID).First(&checkedInRecord).Error; err == nil {
		recordPayload["id"] = checkedInRecord.ID
		recordPayload["pin_verified"] = checkedInRecord.PinVerified
		recordPayload["google_email"] = nullableAttendanceString(checkedInRecord.GoogleEmail)
		recordPayload["google_id"] = nullableAttendanceString(checkedInRecord.GoogleID)
		recordPayload["location_lat"] = nullableAttendanceFloatString(checkedInRecord.LocationLat)
		recordPayload["location_lng"] = nullableAttendanceFloatString(checkedInRecord.LocationLng)
		recordPayload["note"] = nullableAttendanceString(checkedInRecord.Note)
		recordPayload["updated_by"] = checkedInRecord.UpdatedBy
		recordPayload["created_at"] = checkedInRecord.CreatedAt
		recordPayload["updated_at"] = checkedInRecord.UpdatedAt
	}
	realtime.EmitToInstructor(id, "student-checked-in", fiber.Map{"record": recordPayload})
	realtime.EmitToAttendanceDisplay(id, "student-checked-in", fiber.Map{"record": recordPayload})

	return c.JSON(fiber.Map{
		"success": true,
		"message": message,
		"data": fiber.Map{
			"status":            result.Status,
			"student":           fiber.Map{"id": student.ID, "student_id": student.StudentID, "full_name": student.FullName, "email": student.Email},
			"check_in_time":     result.CheckInTime,
			"location_verified": result.LocationVerified,
			"distance_meters":   result.DistanceMeters,
			"is_duplicate":      result.IsDuplicate,
		},
	})
}

// GET /api/attendance?course_id=&status=
func GetAttendanceSessionsHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	sessions, err := repositories.GetAttendanceSessions(courseID, c.Query("status"))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch sessions"})
	}
	return c.JSON(fiber.Map{"success": true, "data": sessions})
}

// GET /api/attendance/:id
func GetAttendanceSessionHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": attendanceSessionDetailResponse(detail)})
}

// POST /api/attendance
func (h *AttendanceHandler) CreateAttendanceSession(c fiber.Ctx) error {
	var input struct {
		CourseID             string    `json:"course_id"`
		CourseSectionID      *uint     `json:"course_section_id"`
		CourseSectionIDs     []uint    `json:"course_section_ids"`
		SectionIDs           []uint    `json:"section_ids"`
		Title                string    `json:"title"`
		AutoRotatePin        *bool     `json:"auto_rotate_pin"`
		PinCode              string    `json:"pin_code"`
		SessionType          string    `json:"session_type"`
		CheckLocation        bool      `json:"check_location"`
		LocationLat          *float64  `json:"location_lat"`
		LocationLng          *float64  `json:"location_lng"`
		RadiusMeters         int       `json:"radius_meters"`
		StartTime            time.Time `json:"start_time"`
		EndTime              time.Time `json:"end_time"`
		LateThresholdMinutes int       `json:"late_threshold_minutes"`
		LateThresholdTime    string    `json:"late_threshold_time"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.CourseID == "" || input.StartTime.IsZero() || input.EndTime.IsZero() {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id, start_time and end_time are required"})
	}

	sectionIDs := uniqueUintValues(input.CourseSectionIDs)
	if len(sectionIDs) == 0 {
		sectionIDs = uniqueUintValues(input.SectionIDs)
	}
	if len(sectionIDs) == 0 && input.CourseSectionID != nil && *input.CourseSectionID > 0 {
		sectionIDs = []uint{*input.CourseSectionID}
	}

	userID := c.Locals("user_id").(uint)
	pin := input.PinCode
	if pin == "" {
		pin = repositories.GeneratePIN()
	}

	title := input.Title
	if title == "" {
		title = "Attendance"
	}
	sessionType := input.SessionType
	if sessionType == "" {
		sessionType = "lecture"
	}
	radius := input.RadiusMeters
	if radius == 0 {
		radius = 50
	}
	lateThreshold := input.LateThresholdMinutes
	if lateThreshold == 0 {
		lateThreshold = 15
	}
	var legacySectionID *uint
	if len(sectionIDs) == 1 {
		legacySectionID = &sectionIDs[0]
	}

	autoRotatePin := input.AutoRotatePin == nil || *input.AutoRotatePin
	session := models.AttendanceSession{
		CourseID:             input.CourseID,
		CourseSectionID:      legacySectionID,
		Title:                title,
		AutoRotatePin:        &autoRotatePin,
		PinMode:              repositories.ConfiguredAttendancePinMode(autoRotatePin),
		PinCode:              pin,
		SessionType:          sessionType,
		CheckLocation:        input.CheckLocation,
		LocationLat:          input.LocationLat,
		LocationLng:          input.LocationLng,
		RadiusMeters:         radius,
		StartTime:            input.StartTime,
		EndTime:              input.EndTime,
		LateThresholdMinutes: lateThreshold,
		LateThresholdTime:    strings.TrimSpace(input.LateThresholdTime),
		CreatedBy:            &userID,
	}

	if err := repositories.CreateAttendanceSession(&session, sectionIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create session"})
	}
	session.Status = repositories.ComputeSessionStatus(session)
	logCourseActivity(c, input.CourseID, userID, "create_attendance_session", "attendance", "attendance_session", session.ID, session.Title, fiber.Map{
		"course_section_ids": sectionIDs,
		"start_time":         session.StartTime,
		"end_time":           session.EndTime,
		"check_location":     session.CheckLocation,
	})
	go createNotificationsForCourseMembers(
		input.CourseID, userID,
		"attendance_created",
		"Create attendance: "+session.Title,
		"A new attendance session was created in this course",
		"/classroom/"+input.CourseID+"/attendance",
		buildNotifData(input.CourseID, fmt.Sprint(session.ID), "attendance_session", ""),
	)
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    session.CourseID,
		ActorUserID: userID,
		Action:      services.ActionAttendanceSessionCreated,
		TargetType:  "attendance_session",
		TargetID:    strconv.Itoa(int(session.ID)),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.Status(201).JSON(fiber.Map{"success": true, "data": attendanceSessionPayload(session, sectionIDs)})
}

// PUT /api/attendance/:id
func UpdateAttendanceSessionHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)

	var input struct {
		Title                *string    `json:"title"`
		AutoRotatePin        *bool      `json:"auto_rotate_pin"`
		PinCode              *string    `json:"pin_code"`
		SessionType          *string    `json:"session_type"`
		CheckLocation        *bool      `json:"check_location"`
		LocationLat          *float64   `json:"location_lat"`
		LocationLng          *float64   `json:"location_lng"`
		RadiusMeters         *int       `json:"radius_meters"`
		StartTime            *time.Time `json:"start_time"`
		EndTime              *time.Time `json:"end_time"`
		LateThresholdMinutes *int       `json:"late_threshold_minutes"`
		LateThresholdTime    *string    `json:"late_threshold_time"`
		CourseSectionID      *uint      `json:"course_section_id"`
		CourseSectionIDs     *[]uint    `json:"course_section_ids"`
		SectionIDs           *[]uint    `json:"section_ids"`
		RegeneratePin        bool       `json:"regenerate_pin"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	detail, err2 := repositories.GetAttendanceSession(uint(id))
	if err2 != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	session := detail.AttendanceSession
	sectionIDs := detail.CourseSectionIDs
	sectionsProvided := false
	if input.CourseSectionIDs != nil {
		sectionIDs = uniqueUintValues(*input.CourseSectionIDs)
		sectionsProvided = true
	} else if input.SectionIDs != nil {
		sectionIDs = uniqueUintValues(*input.SectionIDs)
		sectionsProvided = true
	} else if input.CourseSectionID != nil {
		sectionIDs = []uint{}
		if *input.CourseSectionID > 0 {
			sectionIDs = []uint{*input.CourseSectionID}
		}
		sectionsProvided = true
	}

	if input.Title != nil {
		session.Title = *input.Title
	}
	if input.AutoRotatePin != nil {
		session.AutoRotatePin = input.AutoRotatePin
		session.PinMode = repositories.ConfiguredAttendancePinMode(*input.AutoRotatePin)
	}
	if input.PinCode != nil && strings.TrimSpace(*input.PinCode) != "" {
		session.PinCode = strings.TrimSpace(*input.PinCode)
		session.PreviousPinCode = ""
		session.PinGraceUntil = nil
		now := time.Now()
		session.PinIssuedAt = &now
		if (session.AutoRotatePin != nil && *session.AutoRotatePin) && observability.AttendancePinAutoRotateEnabled() {
			rotatesAt := now.Add(time.Duration(observability.AttendancePinRotationMinutes()) * time.Minute)
			session.PinRotatesAt = &rotatesAt
		} else {
			session.PinRotatesAt = nil
		}
	}
	if input.RegeneratePin {
		session.PinCode = ""
		session.PreviousPinCode = ""
		session.PinIssuedAt = nil
		session.PinGraceUntil = nil
		session.PinRotatesAt = nil
		observability.RecordAttendancePinManualRefresh()
	}
	if input.SessionType != nil && strings.TrimSpace(*input.SessionType) != "" {
		session.SessionType = strings.TrimSpace(*input.SessionType)
	}
	if input.CheckLocation != nil {
		session.CheckLocation = *input.CheckLocation
		if !*input.CheckLocation {
			session.LocationLat = nil
			session.LocationLng = nil
		}
	}
	if input.LocationLat != nil {
		session.LocationLat = input.LocationLat
	}
	if input.LocationLng != nil {
		session.LocationLng = input.LocationLng
	}
	if input.RadiusMeters != nil && *input.RadiusMeters > 0 {
		session.RadiusMeters = *input.RadiusMeters
	}
	if input.StartTime != nil && !input.StartTime.IsZero() {
		session.StartTime = *input.StartTime
	}
	if input.EndTime != nil && !input.EndTime.IsZero() {
		session.EndTime = *input.EndTime
	}
	if input.LateThresholdMinutes != nil && *input.LateThresholdMinutes > 0 {
		session.LateThresholdMinutes = *input.LateThresholdMinutes
	}
	if input.LateThresholdTime != nil {
		session.LateThresholdTime = strings.TrimSpace(*input.LateThresholdTime)
	}
	if sectionsProvided {
		if len(sectionIDs) == 1 {
			session.CourseSectionID = &sectionIDs[0]
		} else {
			session.CourseSectionID = nil
		}
	}

	if err := repositories.UpdateAttendanceSession(&session); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update session"})
	}
	autoRotateChanged := input.AutoRotatePin != nil
	if refreshedSession, pinChange, err := repositories.RefreshAttendanceSessionPinState(session.ID); err == nil && refreshedSession != nil {
		session = *refreshedSession
		if pinChange.Rotated || pinChange.Released || input.RegeneratePin {
			emitAttendancePinUpdated(session)
		}
	}
	if autoRotateChanged {
		autoRotate := session.AutoRotatePin != nil && *session.AutoRotatePin
		if _, err := repositories.SyncAttendanceRuntimeAutoRotate(c.Context(), session.ID, autoRotate); err != nil {
			log.Printf("event=attendance_runtime_sync_failed session_id=%d err=%v", session.ID, err)
		} else {
			emitAttendancePinUpdated(session)
		}
	}
	if sectionsProvided {
		if err := syncAttendanceSessionTargets(&session, sectionIDs); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update session sections"})
		}
	}
	session.Status = repositories.ComputeSessionStatus(session)
	logCourseActivity(c, session.CourseID, actorID, "update_attendance_session", "attendance", "attendance_session", session.ID, session.Title, fiber.Map{
		"course_section_ids":     sectionIDs,
		"status":                 session.Status,
		"late_threshold_minutes": session.LateThresholdMinutes,
		"late_threshold_time":    session.LateThresholdTime,
		"check_location":         session.CheckLocation,
	})
	return c.JSON(fiber.Map{"success": true, "data": attendanceSessionPayload(session, sectionIDs)})
}

// DELETE /api/attendance/:id
func DeleteAttendanceSessionHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)
	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := repositories.DeleteAttendanceSession(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete session"})
	}
	logCourseActivity(c, detail.CourseID, actorID, "delete_attendance_session", "attendance", "attendance_session", detail.ID, detail.Title, fiber.Map{"course_section_ids": detail.CourseSectionIDs})
	return c.JSON(fiber.Map{"success": true, "message": "Session deleted"})
}

// PATCH /api/attendance/:id/records/:studentId
func (h *AttendanceHandler) UpdateAttendanceRecord(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	studentID, err := strconv.ParseUint(c.Params("studentId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid student ID"})
	}

	var input struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	updatedBy := c.Locals("user_id").(uint)
	detail, err := repositories.GetAttendanceSession(uint(sessionID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}
	if err := repositories.UpdateAttendanceRecord(uint(sessionID), uint(studentID), input.Status, input.Note, updatedBy); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update record"})
	}
	logCourseActivity(c, detail.CourseID, updatedBy, "update_attendance_record", "attendance", "attendance_session", detail.ID, detail.Title, fiber.Map{"student_id": studentID, "status": input.Status})
	reqID, traceID, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    detail.CourseID,
		ActorUserID: updatedBy,
		Action:      services.ActionAttendanceRecordUpdated,
		TargetType:  "attendance_record",
		TargetID:    strconv.Itoa(int(studentID)),
		Description: fmt.Sprintf("Attendance updated for student %d in session %d", studentID, sessionID),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	_ = traceID
	emitAttendanceRecordUpdated(uint(sessionID), uint(studentID))
	return c.JSON(fiber.Map{"success": true, "message": "Record updated"})
}

func (h *AttendanceHandler) UpdateAttendanceRecordByRecordID(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	recordID, err := strconv.ParseUint(c.Params("recordId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid record ID"})
	}

	var input struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	var record models.AttendanceRecord
	if err := config.DB.Where("id = ? AND attendance_session_id = ?", uint(recordID), uint(sessionID)).First(&record).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance record not found"})
	}

	updatedBy := c.Locals("user_id").(uint)
	record.Status = input.Status
	record.Note = input.Note
	record.UpdatedBy = &updatedBy
	record.UpdatedAt = time.Now()
	if err := config.DB.Save(&record).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update record"})
	}

	detail, err := repositories.GetAttendanceSession(uint(sessionID))
	if err == nil {
		logCourseActivity(c, detail.CourseID, updatedBy, "update_attendance_record", "attendance", "attendance_session", detail.ID, detail.Title, fiber.Map{"student_id": record.StudentID, "record_id": record.ID, "status": record.Status})
		reqID, traceID, ip := services.ExtractMeta(c)
		h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
			CourseID:    detail.CourseID,
			ActorUserID: updatedBy,
			Action:      services.ActionAttendanceRecordUpdated,
			TargetType:  "attendance_record",
			TargetID:    strconv.Itoa(int(record.ID)),
			Description: fmt.Sprintf("Attendance updated for student %d in session %d", record.StudentID, sessionID),
			RequestID:   reqID,
			IPAddress:   ip,
		})
		_ = traceID
	}
	emitAttendanceRecordUpdated(uint(sessionID), record.StudentID)

	if detail != nil {
		for _, enrichedRecord := range detail.Records {
			if enrichedRecord.ID == record.ID {
				return c.JSON(fiber.Map{"success": true, "data": enrichedRecord})
			}
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": record})
}

func emitAttendanceRecordUpdated(sessionID uint, studentID uint) {
	detail, err := repositories.GetAttendanceSession(sessionID)
	if err != nil {
		return
	}
	for _, record := range detail.Records {
		if record.StudentID == studentID {
			payload := fiber.Map{"record": fiber.Map{
				"id":                    record.ID,
				"attendance_session_id": record.AttendanceSessionID,
				"student_id":            record.StudentID,
				"check_in_time":         record.CheckInTime,
				"status":                record.Status,
				"google_email":          nullableAttendanceString(record.GoogleEmail),
				"google_id":             nullableAttendanceString(record.GoogleID),
				"pin_verified":          record.PinVerified,
				"location_verified":     record.LocationVerified,
				"location_lat":          nullableAttendanceFloatString(record.LocationLat),
				"location_lng":          nullableAttendanceFloatString(record.LocationLng),
				"distance_meters":       record.DistanceMeters,
				"note":                  nullableAttendanceString(record.Note),
				"section_no":            nullableAttendanceString(record.SectionNo),
				"updated_by":            record.UpdatedBy,
				"created_at":            record.CreatedAt,
				"updated_at":            record.UpdatedAt,
				"student": fiber.Map{
					"id":         record.Student.ID,
					"student_id": record.Student.StudentID,
					"full_name":  record.Student.FullName,
					"email":      record.Student.Email,
				},
			}}
			realtime.EmitToInstructor(sessionID, "attendance-updated", payload)
			realtime.EmitToAttendanceDisplay(sessionID, "attendance-updated", payload)
			return
		}
	}
}

func GetAttendanceRecordsHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	status := c.Query("status")
	if status == "" {
		return c.JSON(fiber.Map{"success": true, "data": detail.Records})
	}

	filtered := make([]repositories.AttendanceRecordWithStudent, 0, len(detail.Records))
	for _, record := range detail.Records {
		if record.Status == status {
			filtered = append(filtered, record)
		}
	}
	return c.JSON(fiber.Map{"success": true, "data": filtered})
}

func ActivateAttendanceSessionHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance session not found"})
	}
	session := detail.AttendanceSession
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := repositories.StartAttendanceSession(ctx, session.ID, c.Get("Idempotency-Key")); err != nil {
		if err == repositories.ErrAttendanceRedisUnavailable {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "Attendance PIN service is temporarily unavailable"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to activate session"})
	}
	if refreshedSession, _, err := repositories.ResolveAttendanceSessionPinState(ctx, session.ID, false); err == nil && refreshedSession != nil {
		session = *refreshedSession
		emitAttendancePinUpdated(session)
	}
	actorUID := c.Locals("user_id").(uint)
	logCourseActivity(c, session.CourseID, actorUID, "activate_attendance_session", "attendance", "attendance_session", session.ID, session.Title, fiber.Map{"status": session.Status})
	go createNotificationsForCourseMembers(
		session.CourseID, actorUID,
		"attendance_started",
		"Start attendance: "+session.Title,
		"Attendance has started in this course",
		"/classroom/"+session.CourseID+"/attendance",
		buildNotifData(session.CourseID, fmt.Sprint(session.ID), "attendance_session", ""),
	)
	return c.JSON(fiber.Map{"success": true, "message": "Session activated", "data": attendanceSessionPayload(session, detail.CourseSectionIDs)})
}

func CloseAttendanceSessionHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance session not found"})
	}
	session := detail.AttendanceSession
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := repositories.CloseAttendanceRuntimeSession(ctx, session.ID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to close session"})
	}
	if refreshedSession, _, err := repositories.ResolveAttendanceSessionPinState(ctx, session.ID, false); err == nil && refreshedSession != nil {
		session = *refreshedSession
	} else {
		session.Status = "closed"
		session.PinCode = ""
	}
	actorCloseUID := c.Locals("user_id").(uint)
	logCourseActivity(c, session.CourseID, actorCloseUID, "close_attendance_session", "attendance", "attendance_session", session.ID, session.Title, fiber.Map{"status": session.Status})
	go createNotificationsForCourseMembers(
		session.CourseID, actorCloseUID,
		"attendance_closed",
		"Close attendance: "+session.Title,
		"Attendance has been closed in this course",
		"/classroom/"+session.CourseID+"/attendance",
		buildNotifData(session.CourseID, fmt.Sprint(session.ID), "attendance_session", ""),
	)
	realtime.EmitToAttendance(session.ID, "session-closed", fiber.Map{"session_id": session.ID})
	realtime.EmitToAttendanceDisplay(session.ID, "session-closed", fiber.Map{"session_id": session.ID})
	emitAttendancePinUpdated(session)
	return c.JSON(fiber.Map{"success": true, "message": "Session closed", "data": attendanceSessionPayload(session, detail.CourseSectionIDs)})
}

func StartAttendanceSessionHandler(c fiber.Ctx) error {
	var input struct {
		AttendanceSessionID uint   `json:"attendance_session_id"`
		IdempotencyKey      string `json:"idempotency_key"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.AttendanceSessionID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "attendance_session_id is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := repositories.StartAttendanceSession(ctx, input.AttendanceSessionID, strings.TrimSpace(firstNonEmpty(input.IdempotencyKey, c.Get("Idempotency-Key"))))
	if err != nil {
		if err == repositories.ErrAttendanceRedisUnavailable {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "Attendance PIN service is temporarily unavailable"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to start attendance session"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id":       result.SessionID,
			"current_pin":      result.CurrentPIN,
			"expires_at":       result.ExpiresAt,
			"next_rotation_at": result.NextRotationAt,
			"mode":             result.State.Mode,
			"status":           result.State.Status,
		},
	})
}

func GetAttendanceSessionPinHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	session, state, err := repositories.ResolveAttendanceSessionPinState(ctx, uint(id), true)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Attendance session not found"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id":       session.ID,
			"status":           session.Status,
			"mode":             session.PinMode,
			"current_pin":      session.PinCode,
			"previous_pin":     session.PreviousPinCode,
			"expires_at":       session.ExpiresAt,
			"pin_issued_at":    session.PinIssuedAt,
			"next_rotation_at": session.PinRotatesAt,
			"next_pin_ready":   state != nil && strings.TrimSpace(state.NextPIN) != "",
		},
	})
}

func RotateAttendanceSessionPinHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	state, err := repositories.RotateAttendanceSessionPIN(ctx, uint(id), "manual_regenerate")
	if err != nil {
		if err == repositories.ErrAttendanceRedisUnavailable {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "Attendance PIN service is temporarily unavailable"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to rotate attendance PIN"})
	}

	session, _, resolveErr := repositories.ResolveAttendanceSessionPinState(ctx, uint(id), false)
	if resolveErr == nil && session != nil {
		emitAttendancePinUpdated(*session)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id":       id,
			"current_pin":      state.CurrentPIN,
			"previous_pin":     state.PreviousPIN,
			"pin_issued_at":    state.PinIssuedAt,
			"next_rotation_at": state.NextRotationAt,
		},
	})
}

func StudentCheckInByPINHandler(c fiber.Ctx) error {
	var input struct {
		PinCode     string   `json:"pin_code"`
		ClientID    string   `json:"client_request_id"`
		GoogleEmail string   `json:"google_email"`
		GoogleID    string   `json:"google_id"`
		GoogleToken string   `json:"google_token"`
		StudentID   *uint    `json:"student_id"`
		LocationLat *float64 `json:"location_lat"`
		LocationLng *float64 `json:"location_lng"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.PinCode) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "pin_code is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sessionID, err := repositories.LookupAttendanceSessionIDByPIN(ctx, input.PinCode)
	if err != nil {
		return c.Status(repositories.ErrAttendanceInvalidPINPublic.HTTPStatus).JSON(fiber.Map{
			"success": false,
			"code":    repositories.ErrAttendanceInvalidPINPublic.Code,
			"title":   repositories.ErrAttendanceInvalidPINPublic.Title,
			"message": repositories.ErrAttendanceInvalidPINPublic.Message,
		})
	}

	verifiedEmail := ""
	verifiedGoogleID := ""
	var student models.Student
	if authStudentID := authenticatedStudentIDFromContext(c); authStudentID > 0 {
		if err := config.DB.Select("id", "email").Where("id = ?", authStudentID).First(&student).Error; err != nil {
			return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
				"success": false,
				"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
				"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
				"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
			})
		}
		verifiedEmail = strings.TrimSpace(strings.ToLower(student.Email))
	} else {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer verifyCancel()
		tokenInfo, verifyErr := verifyGoogleIDToken(verifyCtx, input.GoogleToken)
		if verifyErr != nil {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "Google identity verification failed"})
		}

		verifiedEmail = strings.TrimSpace(strings.ToLower(tokenInfo.Email))
		providedEmail := strings.TrimSpace(strings.ToLower(input.GoogleEmail))
		if providedEmail != "" && providedEmail != verifiedEmail {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "Google account mismatch"})
		}

		verifiedGoogleID = strings.TrimSpace(tokenInfo.Subject)
		providedGoogleID := strings.TrimSpace(input.GoogleID)
		if providedGoogleID != "" && providedGoogleID != verifiedGoogleID {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "Google account mismatch"})
		}

		if err := config.DB.Select("id", "email").Where("LOWER(email) = LOWER(?)", verifiedEmail).First(&student).Error; err != nil {
			return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
				"success": false,
				"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
				"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
				"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
			})
		}
	}

	if input.StudentID != nil && *input.StudentID != student.ID {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "Student identity mismatch"})
	}

	result, err := repositories.StudentCheckIn(sessionID, student.ID, input.PinCode, input.LocationLat, input.LocationLng, verifiedEmail, verifiedGoogleID, input.ClientID)
	if err != nil {
		statusCode, payload := attendancePublicErrorResponse(err, 400, "เช็คชื่อไม่สำเร็จ", "ไม่สามารถเช็คชื่อได้ในขณะนี้")
		return c.Status(statusCode).JSON(payload)
	}

	return c.JSON(fiber.Map{"success": true, "data": result})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// NotifyAttendanceSessionStarted mirrors the course-member notification sent by
// the manual activate endpoint so sessions auto-opened by the background worker
// notify students the same way. actorID 0 = system, so every member is notified.
func NotifyAttendanceSessionStarted(courseID string, sessionID uint, title string) {
	go createNotificationsForCourseMembers(
		courseID, 0,
		"attendance_started",
		"Start attendance: "+title,
		"Attendance has started in this course",
		"/classroom/"+courseID+"/attendance",
		buildNotifData(courseID, fmt.Sprint(sessionID), "attendance_session", ""),
	)
}

func emitAttendancePinUpdated(session models.AttendanceSession) {
	payload := fiber.Map{
		"session_id":      session.ID,
		"auto_rotate_pin": session.AutoRotatePin,
		"pin_mode":        session.PinMode,
		"pin_code":        session.PinCode,
		"pin_issued_at":   session.PinIssuedAt,
		"pin_rotates_at":  session.PinRotatesAt,
		"status":          session.Status,
	}
	realtime.EmitToInstructor(session.ID, "attendance-pin-updated", payload)
	realtime.EmitToAttendanceDisplay(session.ID, "attendance-pin-updated", payload)
}

func VerifyStudentHandler(c fiber.Ctx) error {
	var input struct {
		GoogleEmail string `json:"google_email"`
		SessionID   *uint  `json:"session_id"`
		GoogleToken string `json:"google_token"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.SessionID == nil || *input.SessionID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "session_id is required"})
	}

	var student models.Student
	if authStudentID := authenticatedStudentIDFromContext(c); authStudentID > 0 {
		if err := config.DB.Select("id", "student_id", "full_name", "email").Where("id = ?", authStudentID).First(&student).Error; err != nil {
			return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
				"success": false,
				"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
				"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
				"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
			})
		}
	} else {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer verifyCancel()
		tokenInfo, verifyErr := verifyGoogleIDToken(verifyCtx, input.GoogleToken)
		if verifyErr != nil {
			return c.Status(401).JSON(fiber.Map{"success": false, "message": "Google identity verification failed"})
		}

		verifiedEmail := strings.TrimSpace(strings.ToLower(tokenInfo.Email))
		providedEmail := strings.TrimSpace(strings.ToLower(input.GoogleEmail))
		if providedEmail != "" && providedEmail != verifiedEmail {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "Google account mismatch"})
		}

		if err := config.DB.Select("id", "student_id", "full_name", "email").Where("LOWER(email) = LOWER(?)", verifiedEmail).First(&student).Error; err != nil {
			return c.Status(repositories.ErrAttendanceStudentNotFoundPublic.HTTPStatus).JSON(fiber.Map{
				"success": false,
				"code":    repositories.ErrAttendanceStudentNotFoundPublic.Code,
				"title":   repositories.ErrAttendanceStudentNotFoundPublic.Title,
				"message": repositories.ErrAttendanceStudentNotFoundPublic.Message,
			})
		}
	}

	response := fiber.Map{
		"student": fiber.Map{
			"id":         student.ID,
			"student_id": student.StudentID,
			"full_name":  student.FullName,
			"email":      student.Email,
		},
		"already_checked_in": false,
	}

	status, err := repositories.GetAttendanceStudentSessionStatus(*input.SessionID, student.ID)
	if err != nil {
		statusCode, payload := attendancePublicErrorResponse(err, 403, "ไม่สามารถเช็คชื่อได้", "บัญชีนี้ไม่สามารถเช็คชื่อในรอบนี้ได้")
		return c.Status(statusCode).JSON(payload)
	}
	if status != nil && status.AlreadyCheckedIn {
		response["already_checked_in"] = true
		response["status"] = status.Status
		response["check_in_time"] = status.CheckInTime
	}

	return c.JSON(fiber.Map{"success": true, "data": response})
}

func PreviewSectionChangeHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	var input struct {
		CourseSectionIDs []uint `json:"course_section_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_section_ids must be an array"})
	}

	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรอบการเช็คชื่อ"})
	}

	newSectionIDs := uniqueUintValues(input.CourseSectionIDs)
	currentSections := uniqueUintValues(detail.CourseSectionIDs)
	removedSectionIDs := make([]uint, 0)
	newSet := make(map[uint]bool, len(newSectionIDs))
	for _, sectionID := range newSectionIDs {
		newSet[sectionID] = true
	}
	for _, sectionID := range currentSections {
		if !newSet[sectionID] {
			removedSectionIDs = append(removedSectionIDs, sectionID)
		}
	}

	if len(removedSectionIDs) == 0 {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"session_id": id, "session_title": detail.Title, "removed_sections": []fiber.Map{}, "affected_students": []fiber.Map{}, "total_affected": 0, "has_checked_in_students": false}})
	}

	var removedSections []models.CourseSection
	if err := config.DB.Select("id", "section_no").Where("id IN ?", removedSectionIDs).Find(&removedSections).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load removed sections"})
	}

	type enrollmentRow struct {
		StudentID       uint `gorm:"column:student_id"`
		CourseSectionID uint `gorm:"column:course_section_id"`
	}
	var removedEnrollments []enrollmentRow
	if err := config.DB.Raw(`SELECT student_id, course_section_id FROM course_section_students WHERE course_section_id IN ?`, removedSectionIDs).Scan(&removedEnrollments).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to inspect enrollments"})
	}

	studentIDsToCheck := make([]uint, 0, len(removedEnrollments))
	for _, enrollment := range removedEnrollments {
		studentIDsToCheck = append(studentIDsToCheck, enrollment.StudentID)
	}
	studentIDsToCheck = uniqueUintValues(studentIDsToCheck)

	if len(newSectionIDs) > 0 && len(studentIDsToCheck) > 0 {
		var remainingEnrollments []enrollmentRow
		if err := config.DB.Raw(`SELECT student_id, course_section_id FROM course_section_students WHERE course_section_id IN ?`, newSectionIDs).Scan(&remainingEnrollments).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to inspect remaining enrollments"})
		}
		remaining := make(map[uint]bool, len(remainingEnrollments))
		for _, enrollment := range remainingEnrollments {
			remaining[enrollment.StudentID] = true
		}
		filtered := make([]uint, 0, len(studentIDsToCheck))
		for _, studentID := range studentIDsToCheck {
			if !remaining[studentID] {
				filtered = append(filtered, studentID)
			}
		}
		studentIDsToCheck = filtered
	}

	if len(studentIDsToCheck) == 0 {
		sectionsPayload := make([]fiber.Map, len(removedSections))
		for i, section := range removedSections {
			sectionsPayload[i] = fiber.Map{"id": section.ID, "section_no": section.SectionNo}
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"session_id": id, "session_title": detail.Title, "removed_sections": sectionsPayload, "affected_students": []fiber.Map{}, "total_affected": 0, "has_checked_in_students": false}})
	}

	var records []models.AttendanceRecord
	if err := config.DB.Where("attendance_session_id = ? AND student_id IN ? AND status <> ?", uint(id), studentIDsToCheck, "absent").Find(&records).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to inspect attendance records"})
	}

	studentMap, err := loadStudentsMap(studentIDsToCheck)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}
	sectionNoMap := make(map[uint]string, len(removedSections))
	for _, section := range removedSections {
		sectionNoMap[section.ID] = section.SectionNo
	}
	studentSections := make(map[uint][]string)
	for _, enrollment := range removedEnrollments {
		if sectionNo, ok := sectionNoMap[enrollment.CourseSectionID]; ok {
			studentSections[enrollment.StudentID] = append(studentSections[enrollment.StudentID], sectionNo)
		}
	}

	affectedStudents := make([]fiber.Map, len(records))
	for i, record := range records {
		student := studentMap[record.StudentID]
		affectedStudents[i] = fiber.Map{
			"record_id":     record.ID,
			"student_id":    student.StudentID,
			"student_name":  student.FullName,
			"status":        record.Status,
			"check_in_time": record.CheckInTime,
			"section_no":    strings.Join(studentSections[record.StudentID], ", "),
		}
	}

	sectionsPayload := make([]fiber.Map, len(removedSections))
	for i, section := range removedSections {
		sectionsPayload[i] = fiber.Map{"id": section.ID, "section_no": section.SectionNo}
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"session_id": id, "session_title": detail.Title, "removed_sections": sectionsPayload, "affected_students": affectedStudents, "total_affected": len(affectedStudents), "has_checked_in_students": len(affectedStudents) > 0}})
}

func PreviewTimeChangeHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	var input struct {
		StartTime            time.Time `json:"start_time"`
		EndTime              time.Time `json:"end_time"`
		LateThresholdTime    string    `json:"late_threshold_time"`
		LateThresholdMinutes *int      `json:"late_threshold_minutes"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.StartTime.IsZero() || input.EndTime.IsZero() {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "start_time and end_time are required"})
	}

	var session models.AttendanceSession
	if err := config.DB.First(&session, uint(id)).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรอบการเช็คชื่อ"})
	}

	var records []models.AttendanceRecord
	if err := config.DB.Where("attendance_session_id = ? AND check_in_time IS NOT NULL AND status <> ?", uint(id), "leave").Find(&records).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load attendance records"})
	}

	studentIDs := make([]uint, 0, len(records))
	for _, record := range records {
		studentIDs = append(studentIDs, record.StudentID)
	}
	studentMap, err := loadStudentsMap(studentIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}

	oldLate := computeLateThresholdTime(session.StartTime, session.LateThresholdTime, session.LateThresholdMinutes)
	newLateMinutes := session.LateThresholdMinutes
	if input.LateThresholdMinutes != nil {
		newLateMinutes = *input.LateThresholdMinutes
	}
	newLate := computeLateThresholdTime(input.StartTime, input.LateThresholdTime, newLateMinutes)

	summary := fiber.Map{
		"total_checked_in":    len(records),
		"will_be_invalidated": 0,
		"present_to_late":     0,
		"late_to_present":     0,
		"unchanged":           0,
		"already_invalid":     0,
		"recovered":           0,
	}
	changes := make([]fiber.Map, 0)

	for _, record := range records {
		if record.CheckInTime == nil {
			continue
		}
		oldStatus := classifyAttendanceCheckIn(*record.CheckInTime, session.StartTime, session.EndTime, oldLate)
		newStatus := classifyAttendanceCheckIn(*record.CheckInTime, input.StartTime, input.EndTime, newLate)

		changeType := "unchanged"
		switch {
		case oldStatus == "invalid" && newStatus == "invalid":
			changeType = "already_invalid"
			summary["already_invalid"] = summary["already_invalid"].(int) + 1
		case oldStatus != "invalid" && newStatus == "invalid":
			changeType = "will_be_invalidated"
			summary["will_be_invalidated"] = summary["will_be_invalidated"].(int) + 1
		case oldStatus == "invalid" && newStatus != "invalid":
			changeType = "recovered"
			summary["recovered"] = summary["recovered"].(int) + 1
		case oldStatus == "present" && newStatus == "late":
			changeType = "present_to_late"
			summary["present_to_late"] = summary["present_to_late"].(int) + 1
		case oldStatus == "late" && newStatus == "present":
			changeType = "late_to_present"
			summary["late_to_present"] = summary["late_to_present"].(int) + 1
		default:
			summary["unchanged"] = summary["unchanged"].(int) + 1
		}

		if changeType != "unchanged" {
			student := studentMap[record.StudentID]
			changes = append(changes, fiber.Map{
				"record_id":     record.ID,
				"student_id":    student.StudentID,
				"student_name":  student.FullName,
				"check_in_time": record.CheckInTime,
				"old_status":    oldStatus,
				"new_status":    newStatus,
				"change_type":   changeType,
			})
		}
	}

	hasDestructiveChanges := summary["will_be_invalidated"].(int) > 0
	hasAnyImpact := hasDestructiveChanges || summary["present_to_late"].(int) > 0 || summary["late_to_present"].(int) > 0 || summary["recovered"].(int) > 0

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"session_id":    id,
		"session_title": session.Title,
		"summary":       summary,
		"changes":       changes,
		"timeChanges": fiber.Map{
			"start_time":     fiber.Map{"old": session.StartTime, "new": input.StartTime, "changed": !session.StartTime.Equal(input.StartTime)},
			"end_time":       fiber.Map{"old": session.EndTime, "new": input.EndTime, "changed": !session.EndTime.Equal(input.EndTime)},
			"late_threshold": fiber.Map{"old": oldLate.Format(time.RFC3339), "new": newLate.Format(time.RFC3339), "changed": !oldLate.Equal(newLate)},
		},
		"hasDestructiveChanges": hasDestructiveChanges,
		"hasAnyImpact":          hasAnyImpact,
	}})
}

func ApplyTimeChangeHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	var input struct {
		StartTime            *time.Time `json:"start_time"`
		EndTime              *time.Time `json:"end_time"`
		LateThresholdTime    *string    `json:"late_threshold_time"`
		LateThresholdMinutes *int       `json:"late_threshold_minutes"`
		RegeneratePin        bool       `json:"regenerate_pin"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	detail, err := repositories.GetAttendanceSession(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรอบการเช็คชื่อ"})
	}
	session := detail.AttendanceSession
	newStart := session.StartTime
	if input.StartTime != nil && !input.StartTime.IsZero() {
		newStart = *input.StartTime
	}
	newEnd := session.EndTime
	if input.EndTime != nil && !input.EndTime.IsZero() {
		newEnd = *input.EndTime
	}
	newLateTime := session.LateThresholdTime
	if input.LateThresholdTime != nil {
		newLateTime = strings.TrimSpace(*input.LateThresholdTime)
	}
	newLateMinutes := session.LateThresholdMinutes
	if input.LateThresholdMinutes != nil && *input.LateThresholdMinutes > 0 {
		newLateMinutes = *input.LateThresholdMinutes
	}
	newLate := computeLateThresholdTime(newStart, newLateTime, newLateMinutes)

	var records []models.AttendanceRecord
	if err := config.DB.Where("attendance_session_id = ? AND check_in_time IS NOT NULL AND status <> ?", uint(id), "leave").Find(&records).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load attendance records"})
	}

	tx := config.DB.Begin()
	if tx.Error != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to start transaction"})
	}

	updatedBy := c.Locals("user_id").(uint)
	session.StartTime = newStart
	session.EndTime = newEnd
	session.LateThresholdTime = newLateTime
	session.LateThresholdMinutes = newLateMinutes
	if input.RegeneratePin {
		session.PinCode = repositories.GeneratePIN()
	}
	if err := tx.Save(&session).Error; err != nil {
		tx.Rollback()
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update session"})
	}

	invalidated := 0
	presentToLate := 0
	lateToPresent := 0
	recovered := 0
	unchanged := 0
	auditDetails := make([]fiber.Map, 0)
	studentIDs := make([]uint, 0, len(records))
	for _, record := range records {
		studentIDs = append(studentIDs, record.StudentID)
	}
	studentMap, err := loadStudentsMap(studentIDs)
	if err != nil {
		tx.Rollback()
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}

	for _, record := range records {
		if record.CheckInTime == nil {
			continue
		}
		newStatus := classifyAttendanceCheckIn(*record.CheckInTime, newStart, newEnd, newLate)
		dbStatus := newStatus
		if newStatus == "invalid" {
			dbStatus = "absent"
		}
		if dbStatus == record.Status {
			unchanged++
			continue
		}

		note := "[ระบบ] สถานะถูกอัปเดตหลังปรับเวลาเช็คชื่อ"
		if newStatus == "invalid" {
			note = "[ระบบ] สถานะเปลี่ยนเป็นขาด เนื่องจากเวลาเช็คชื่ออยู่นอกช่วงเวลาใหม่"
		}
		if err := tx.Model(&models.AttendanceRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{"status": dbStatus, "updated_by": updatedBy, "note": note, "updated_at": time.Now()}).Error; err != nil {
			tx.Rollback()
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to re-evaluate attendance records"})
		}

		switch {
		case newStatus == "invalid":
			invalidated++
		case record.Status == "present" && dbStatus == "late":
			presentToLate++
		case record.Status == "late" && dbStatus == "present":
			lateToPresent++
		case record.Status == "absent" && (dbStatus == "present" || dbStatus == "late"):
			recovered++
		}

		student := studentMap[record.StudentID]
		auditDetails = append(auditDetails, fiber.Map{"record_id": record.ID, "student_id": student.StudentID, "student_name": student.FullName, "check_in_time": record.CheckInTime, "old_status": record.Status, "new_status": dbStatus})
	}

	if err := tx.Commit().Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to commit attendance changes"})
	}

	session.Status = repositories.ComputeSessionStatus(session)
	logCourseActivity(c, session.CourseID, updatedBy, "apply_attendance_time_change", "attendance", "attendance_session", session.ID, session.Title, fiber.Map{"invalidated": invalidated, "present_to_late": presentToLate, "late_to_present": lateToPresent, "recovered": recovered, "unchanged": unchanged})
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"session": attendanceSessionPayload(session, detail.CourseSectionIDs), "impact": fiber.Map{"total_records": len(records), "invalidated": invalidated, "present_to_late": presentToLate, "late_to_present": lateToPresent, "recovered": recovered, "unchanged": unchanged, "details": auditDetails}}})
}

// POST /api/attendance/:id/records/bulk
func BulkUpdateAttendanceRecordsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	detail, err := repositories.GetAttendanceSession(uint(sessionID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	var input struct {
		Updates []repositories.AttendanceRecordUpdate `json:"updates"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	updatedBy := c.Locals("user_id").(uint)
	if err := repositories.BulkUpdateAttendanceRecords(uint(sessionID), input.Updates, updatedBy); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to bulk update records"})
	}
	logCourseActivity(c, detail.CourseID, updatedBy, "bulk_update_attendance_records", "attendance", "attendance_session", detail.ID, detail.Title, fiber.Map{"count": len(input.Updates)})
	return c.JSON(fiber.Map{"success": true, "message": "Records updated"})
}
