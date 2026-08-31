package repositories

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/observability"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AttendancePublicError struct {
	Code       string
	Title      string
	Message    string
	HTTPStatus int
}

func (e *AttendancePublicError) Error() string {
	return e.Message
}

func newAttendancePublicError(code string, httpStatus int, title string, message string) *AttendancePublicError {
	return &AttendancePublicError{
		Code:       code,
		Title:      title,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

var (
	ErrAttendanceStudentNotEligiblePublic = newAttendancePublicError(
		"ATTENDANCE_STUDENT_NOT_ELIGIBLE",
		403,
		"ไม่สามารถเช็กชื่อได้",
		"คุณไม่ได้อยู่ในกลุ่มเรียนที่เปิดรับการเช็กชื่อรอบนี้",
	)
	ErrAttendanceSessionNotFoundPublic = newAttendancePublicError(
		"ATTENDANCE_SESSION_NOT_FOUND",
		404,
		"ไม่พบรอบการเช็กชื่อ",
		"ไม่พบรอบการเช็กชื่อนี้ หรือรอบนี้ถูกลบไปแล้ว",
	)
	ErrAttendanceSessionNotStartedPublic = newAttendancePublicError(
		"ATTENDANCE_SESSION_NOT_STARTED",
		400,
		"ยังไม่ถึงเวลาเช็กชื่อ",
		"รอบการเช็กชื่อนี้ยังไม่เปิด กรุณารอให้ถึงเวลาแล้วลองใหม่อีกครั้ง",
	)
	ErrAttendanceSessionClosedPublic = newAttendancePublicError(
		"ATTENDANCE_SESSION_CLOSED",
		400,
		"หมดเวลาเช็กชื่อแล้ว",
		"รอบการเช็กชื่อนี้ปิดรับแล้ว จึงไม่สามารถเช็กชื่อได้",
	)
	ErrAttendancePINRequiredPublic = newAttendancePublicError(
		"ATTENDANCE_PIN_REQUIRED",
		400,
		"กรุณากรอกรหัส PIN",
		"ต้องกรอกรหัส PIN ของรอบเช็กชื่อนี้ก่อนดำเนินการต่อ",
	)
	ErrAttendanceInvalidPINPublic = newAttendancePublicError(
		"ATTENDANCE_INVALID_PIN",
		400,
		"รหัส PIN ไม่ถูกต้อง",
		"รหัส PIN นี้ไม่ตรงกับรอบเช็กชื่อที่เปิดอยู่ กรุณาตรวจสอบแล้วลองใหม่",
	)
	ErrAttendanceLocationRequiredPublic = newAttendancePublicError(
		"ATTENDANCE_LOCATION_REQUIRED",
		400,
		"ต้องอนุญาตตำแหน่งก่อน",
		"รอบนี้ต้องใช้ตำแหน่งในการเช็กชื่อ กรุณาอนุญาตการเข้าถึงตำแหน่งแล้วลองใหม่",
	)
	ErrAttendanceSessionLocationNotConfiguredPublic = newAttendancePublicError(
		"ATTENDANCE_SESSION_LOCATION_NOT_CONFIGURED",
		400,
		"รอบเช็กชื่อยังไม่พร้อม",
		"รอบเช็กชื่อนี้ยังไม่ได้กำหนดตำแหน่งอ้างอิงสำหรับตรวจสอบ",
	)
	ErrAttendanceStudentNotFoundPublic = newAttendancePublicError(
		"ATTENDANCE_STUDENT_NOT_FOUND",
		404,
		"ไม่พบข้อมูลนักศึกษา",
		"ไม่พบบัญชีนักศึกษาที่ใช้เช็กชื่อในระบบ กรุณาติดต่อผู้สอน",
	)
	ErrAttendanceCourseNotRegisteredPublic = newAttendancePublicError(
		"ATTENDANCE_COURSE_NOT_REGISTERED",
		404,
		"ไม่พบสิทธิ์เช็กชื่อในรายวิชานี้",
		"บัญชีของคุณไม่ได้ลงทะเบียนอยู่ในรายวิชานี้ จึงไม่สามารถเช็กชื่อได้",
	)
)

func NewAttendanceOutsideAllowedAreaError(distance int) *AttendancePublicError {
	return newAttendancePublicError(
		"ATTENDANCE_OUTSIDE_ALLOWED_AREA",
		400,
		"อยู่นอกพื้นที่เช็กชื่อ",
		fmt.Sprintf("คุณอยู่นอกพื้นที่ที่อนุญาตสำหรับเช็กชื่อ โดยอยู่ห่างจากจุดที่กำหนด %d เมตร", distance),
	)
}

// ============================================================
// Attendance Session
// ============================================================

type AttendanceSessionWithStats struct {
	models.AttendanceSession
	SectionIDs []uint          `json:"course_section_ids"`
	Stats      AttendanceStats `json:"stats"`
}

type AttendanceStats struct {
	Present   int `json:"present"`
	Late      int `json:"late"`
	Leave     int `json:"leave"`
	Absent    int `json:"absent"`
	CheckedIn int `json:"checked_in"`
	Total     int `json:"total"`
}

type AttendanceRecordWithStudent struct {
	models.AttendanceRecord
	Student   AttendanceStudentBasic `json:"student"`
	SectionNo string                 `json:"section_no"`
}

type AttendanceSectionBasic struct {
	ID        uint   `json:"id"`
	SectionNo string `json:"section_no"`
}

type AttendanceCourseBasic struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Year     int    `json:"year"`
	Semester int    `json:"semester"`
}

type AttendanceCreatorBasic struct {
	ID       uint   `json:"id"`
	FullName string `json:"full_name"`
}

type AttendanceStudentBasic struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
}

type AttendanceCheckInResult struct {
	Status           string    `json:"status"`
	CheckInTime      time.Time `json:"check_in_time"`
	LocationVerified bool      `json:"location_verified"`
	DistanceMeters   *int      `json:"distance_meters,omitempty"`
	IsDuplicate      bool      `json:"is_duplicate"`
}

type AttendanceStudentSessionStatus struct {
	AlreadyCheckedIn bool
	Status           string
	CheckInTime      *time.Time
}

type AttendanceSessionInfo struct {
	ID                   uint                    `json:"id"`
	Title                string                  `json:"title"`
	SessionType          string                  `json:"session_type"`
	CheckLocation        bool                    `json:"check_location"`
	AutoRotatePin        bool                    `json:"auto_rotate_pin"`
	PinMode              string                  `json:"pin_mode"`
	// PinCode is for privileged consumers only — the instructor live view and
	// the paired classroom display. GetSessionInfoHandler blanks it before
	// answering the public check-in route: the rotating PIN is what forces a
	// student to actually be in the room reading the projector, so serving it
	// over an unauthenticated API would nullify the device, campus-network and
	// canonical-domain guards that sit in front of check-in. omitempty keeps it
	// off the wire entirely rather than sending an empty string.
	PinCode string `json:"pin_code,omitempty"`
	// PinIssued lets a caller that is not allowed to see the code still tell
	// "no PIN generated yet" apart from "PIN withheld".
	PinIssued            bool                    `json:"pin_issued"`
	PinIssuedAt          *time.Time              `json:"pin_issued_at,omitempty"`
	PinRotatesAt         *time.Time              `json:"pin_rotates_at,omitempty"`
	LateThresholdMinutes int                     `json:"late_threshold_minutes"`
	LateThresholdTime    string                  `json:"late_threshold_time"`
	StartTime            time.Time               `json:"start_time"`
	EndTime              time.Time               `json:"end_time"`
	Status               string                  `json:"status"`
	Course               *AttendanceCourseBasic  `json:"course"`
	Section              *AttendanceSectionBasic `json:"section"`
}

type AttendanceSessionDetail struct {
	models.AttendanceSession
	CourseSectionIDs []uint                        `json:"course_section_ids"`
	Section          *AttendanceSectionBasic       `json:"section"`
	Course           *AttendanceCourseBasic        `json:"course"`
	Creator          *AttendanceCreatorBasic       `json:"creator"`
	Records          []AttendanceRecordWithStudent `json:"records"`
	Stats            AttendanceSessionDetailStats  `json:"stats"`
}

type AttendanceSessionDetailStats struct {
	TotalStudents int `json:"total_students"`
	Present       int `json:"present"`
	Late          int `json:"late"`
	Leave         int `json:"leave"`
	Absent        int `json:"absent"`
	CheckedIn     int `json:"checked_in"`
	NotCheckedIn  int `json:"not_checked_in"`
}

type AttendancePinStateChange struct {
	SessionID     uint
	CourseID      string
	Status        string
	PinCode       string
	PinIssuedAt   *time.Time
	PinRotatesAt  *time.Time
	Rotated       bool
	Released      bool
	StatusChanged bool
}

func ComputeSessionStatus(s models.AttendanceSession) string {
	now := time.Now()
	if now.Before(s.StartTime) {
		return "draft"
	}
	if now.After(s.EndTime) {
		return "closed"
	}
	return "active"
}

func GeneratePIN() string {
	b := make([]byte, 3)
	rand.Read(b)
	n := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", n%1000000)
}

func attendancePinRotationWindow() time.Duration {
	return time.Duration(observability.AttendancePinRotationMinutes()) * time.Minute
}

func attendancePinGraceWindow() time.Duration {
	return time.Duration(observability.AttendancePinGraceSeconds()) * time.Second
}

func nextDistinctAttendancePIN(current string) string {
	pin := GeneratePIN()
	for pin == current {
		pin = GeneratePIN()
	}
	return pin
}

func ConfiguredAttendancePinMode(autoRotate bool) string {
	if autoRotate {
		return "rotating"
	}
	return "static"
}

func computeAttendanceLateThreshold(startTime time.Time, lateThresholdTime string, lateThresholdMinutes int) time.Time {
	minutes := lateThresholdMinutes
	if minutes < 0 {
		minutes = 0
	}
	fallback := startTime.Add(time.Duration(minutes) * time.Minute)

	raw := strings.TrimSpace(lateThresholdTime)
	if raw == "" {
		return fallback
	}

	var parsed time.Time
	var err error
	for _, layout := range []string{"15:04:05", "15:04"} {
		parsed, err = time.ParseInLocation(layout, raw, startTime.Location())
		if err == nil {
			return time.Date(startTime.Year(), startTime.Month(), startTime.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, startTime.Location())
		}
	}

	return fallback
}

func generateUniqueAttendancePIN(tx *gorm.DB, sessionID uint, current string) string {
	for attempt := 0; attempt < 32; attempt++ {
		pin := nextDistinctAttendancePIN(current)
		var matches int64
		err := tx.Model(&models.AttendanceSession{}).
			Where("id <> ? AND status = 'active' AND (pin_code = ? OR previous_pin_code = ?)", sessionID, pin, pin).
			Count(&matches).Error
		if err == nil && matches == 0 {
			return pin
		}
	}
	return nextDistinctAttendancePIN(current)
}

func applyAttendancePinState(tx *gorm.DB, session *models.AttendanceSession, now time.Time) (AttendancePinStateChange, error) {
	change := AttendancePinStateChange{
		SessionID: session.ID,
		CourseID:  session.CourseID,
		Status:    ComputeSessionStatus(*session),
	}

	updates := map[string]interface{}{}
	isStaticMode := session.PinMode == "static" || (session.PinMode == "" && (session.AutoRotatePin == nil || !*session.AutoRotatePin))
	if session.Status != change.Status {
		updates["status"] = change.Status
		change.StatusChanged = true
	}
	session.Status = change.Status

	if change.Status != "active" {
		if change.Status == "draft" && isStaticMode {
			if strings.TrimSpace(session.PinCode) == "" {
				nextPin := generateUniqueAttendancePIN(tx, session.ID, "")
				updates["pin_code"] = nextPin
				session.PinCode = nextPin
			}
			if session.PinIssuedAt == nil {
				issuedAt := now
				updates["pin_issued_at"] = issuedAt
				session.PinIssuedAt = &issuedAt
			}
			if session.PreviousPinCode != "" || session.PinGraceUntil != nil || session.PinRotatesAt != nil {
				updates["previous_pin_code"] = ""
				updates["pin_grace_until"] = nil
				updates["pin_rotates_at"] = nil
				session.PreviousPinCode = ""
				session.PinGraceUntil = nil
				session.PinRotatesAt = nil
			}
		} else if session.PinCode != "" || session.PreviousPinCode != "" || session.PinIssuedAt != nil || session.PinGraceUntil != nil || session.PinRotatesAt != nil {
			updates["pin_code"] = ""
			updates["previous_pin_code"] = ""
			updates["pin_issued_at"] = nil
			updates["pin_grace_until"] = nil
			updates["pin_rotates_at"] = nil
			change.Released = true
			session.PinCode = ""
			session.PreviousPinCode = ""
			session.PinIssuedAt = nil
			session.PinGraceUntil = nil
			session.PinRotatesAt = nil
		}
	} else {
		rotationWindow := attendancePinRotationWindow()
		graceWindow := attendancePinGraceWindow()
		autoRotate := (session.AutoRotatePin != nil && *session.AutoRotatePin) && observability.AttendancePinAutoRotateEnabled()

		if session.PreviousPinCode != "" && (session.PinGraceUntil == nil || !session.PinGraceUntil.After(now)) {
			updates["previous_pin_code"] = ""
			updates["pin_grace_until"] = nil
			session.PreviousPinCode = ""
			session.PinGraceUntil = nil
		}

		needsIssue := strings.TrimSpace(session.PinCode) == ""
		needsRotate := autoRotate && !needsIssue && (session.PinRotatesAt == nil || !session.PinRotatesAt.After(now))

		if needsIssue || needsRotate {
			nextPin := generateUniqueAttendancePIN(tx, session.ID, session.PinCode)
			issuedAt := now
			updates["pin_code"] = nextPin
			updates["pin_issued_at"] = issuedAt
			if autoRotate {
				rotatesAt := now.Add(rotationWindow)
				updates["pin_rotates_at"] = rotatesAt
				session.PinRotatesAt = &rotatesAt
			} else {
				updates["pin_rotates_at"] = nil
				session.PinRotatesAt = nil
			}

			if needsRotate && session.PinCode != "" {
				graceUntil := now.Add(graceWindow)
				updates["previous_pin_code"] = session.PinCode
				updates["pin_grace_until"] = graceUntil
				session.PreviousPinCode = session.PinCode
				session.PinGraceUntil = &graceUntil
				change.Rotated = true
				observability.RecordAttendancePinRotation()
			} else {
				updates["previous_pin_code"] = ""
				updates["pin_grace_until"] = nil
				session.PreviousPinCode = ""
				session.PinGraceUntil = nil
			}

			session.PinCode = nextPin
			session.PinIssuedAt = &issuedAt
		} else {
			if !autoRotate && (session.PreviousPinCode != "" || session.PinGraceUntil != nil || session.PinRotatesAt != nil) {
				updates["previous_pin_code"] = ""
				updates["pin_grace_until"] = nil
				updates["pin_rotates_at"] = nil
				session.PreviousPinCode = ""
				session.PinGraceUntil = nil
				session.PinRotatesAt = nil
			}
			if session.PinIssuedAt == nil {
				issuedAt := now
				updates["pin_issued_at"] = issuedAt
				session.PinIssuedAt = &issuedAt
			}
			if autoRotate && session.PinRotatesAt == nil {
				anchor := now
				if session.PinIssuedAt != nil {
					anchor = *session.PinIssuedAt
				}
				rotatesAt := anchor.Add(rotationWindow)
				updates["pin_rotates_at"] = rotatesAt
				session.PinRotatesAt = &rotatesAt
			}
		}
	}

	if len(updates) > 0 {
		if err := tx.Model(&models.AttendanceSession{}).Where("id = ?", session.ID).Updates(updates).Error; err != nil {
			return change, err
		}
	}

	change.PinCode = session.PinCode
	change.PinIssuedAt = session.PinIssuedAt
	change.PinRotatesAt = session.PinRotatesAt
	return change, nil
}

func syncAttendanceSessionPinState(db *gorm.DB, sessionID uint) (*models.AttendanceSession, AttendancePinStateChange, error) {
	var session models.AttendanceSession
	var change AttendancePinStateChange
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}
		var err error
		change, err = applyAttendancePinState(tx, &session, time.Now())
		return err
	})
	if err != nil {
		return nil, change, err
	}
	return &session, change, nil
}

func RefreshAttendanceSessionPinState(sessionID uint) (*models.AttendanceSession, AttendancePinStateChange, error) {
	return syncAttendanceSessionPinState(config.DB, sessionID)
}

func MaintainAttendanceSessionPins(now time.Time) ([]AttendancePinStateChange, error) {
	var candidates []models.AttendanceSession
	if err := config.DB.
		Where("pin_code <> '' OR previous_pin_code <> '' OR (start_time <= ? AND end_time > ?)", now, now).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	changes := make([]AttendancePinStateChange, 0, len(candidates))
	for _, candidate := range candidates {
		session := candidate
		var change AttendancePinStateChange
		if err := config.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, candidate.ID).Error; err != nil {
				return err
			}
			var err error
			change, err = applyAttendancePinState(tx, &session, now)
			return err
		}); err != nil {
			return nil, err
		}

		if change.Rotated || change.Released || change.StatusChanged {
			changes = append(changes, change)
		}
	}

	return changes, nil
}

func calculateDistanceMeters(lat1 float64, lng1 float64, lat2 float64, lng2 float64) int {
	const earthRadiusMeters = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLng := (lng2 - lng1) * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return int(math.Round(earthRadiusMeters * c))
}

func countAttendanceTargetStudents(courseID string, sectionIDs []uint) (int, error) {
	type countRow struct {
		Total int `gorm:"column:total"`
	}

	var row countRow
	if len(sectionIDs) > 0 {
		if err := config.DB.Raw(`
			SELECT COUNT(DISTINCT student_id) AS total
			FROM course_section_students
			WHERE course_section_id IN ?
		`, sectionIDs).Scan(&row).Error; err != nil {
			return 0, err
		}
		return row.Total, nil
	}

	if err := config.DB.Raw(`
		SELECT COUNT(DISTINCT css.student_id) AS total
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ?
	`, courseID).Scan(&row).Error; err != nil {
		return 0, err
	}
	return row.Total, nil
}

func attendanceSessionSectionIDsWithDB(db *gorm.DB, session *models.AttendanceSession) ([]uint, error) {
	type sectionRow struct {
		CourseSectionID uint `gorm:"column:course_section_id"`
	}

	var sectionRows []sectionRow
	if err := db.Raw(`SELECT course_section_id FROM attendance_session_sections WHERE attendance_session_id = ?`, session.ID).Scan(&sectionRows).Error; err != nil {
		return nil, err
	}

	sectionIDs := make([]uint, 0, len(sectionRows))
	for _, row := range sectionRows {
		if row.CourseSectionID > 0 {
			sectionIDs = append(sectionIDs, row.CourseSectionID)
		}
	}
	if len(sectionIDs) == 0 && session.CourseSectionID != nil && *session.CourseSectionID > 0 {
		sectionIDs = append(sectionIDs, *session.CourseSectionID)
	}

	return sectionIDs, nil
}

func attendanceStudentEligibleWithDB(db *gorm.DB, courseID string, sectionIDs []uint, studentID uint) (bool, error) {
	type countRow struct {
		Total int `gorm:"column:total"`
	}

	var row countRow
	if len(sectionIDs) > 0 {
		if err := db.Raw(`
			SELECT COUNT(*) AS total
			FROM course_section_students
			WHERE student_id = ? AND course_section_id IN ?
		`, studentID, sectionIDs).Scan(&row).Error; err != nil {
			return false, err
		}
		return row.Total > 0, nil
	}

	if err := db.Raw(`
		SELECT COUNT(*) AS total
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE css.student_id = ? AND cs.course_id = ?
	`, studentID, courseID).Scan(&row).Error; err != nil {
		return false, err
	}
	return row.Total > 0, nil
}

func dedupeAttendanceRecordsWithDB(db *gorm.DB, sessionID uint, studentID *uint) error {
	type duplicateGroup struct {
		StudentID uint `gorm:"column:student_id"`
	}

	query := db.Table("attendance_records").
		Select("student_id").
		Where("attendance_session_id = ?", sessionID)
	if studentID != nil {
		query = query.Where("student_id = ?", *studentID)
	}

	var groups []duplicateGroup
	if err := query.Group("student_id").Having("COUNT(*) > 1").Scan(&groups).Error; err != nil {
		return err
	}

	for _, group := range groups {
		var records []models.AttendanceRecord
		if err := db.Where("attendance_session_id = ? AND student_id = ?", sessionID, group.StudentID).
			Order(clause.Expr{SQL: "CASE WHEN status <> 'absent' THEN 0 ELSE 1 END"}).
			Order("check_in_time DESC NULLS LAST").
			Order("updated_at DESC").
			Order("id DESC").
			Find(&records).Error; err != nil {
			return err
		}
		if len(records) <= 1 {
			continue
		}

		deleteIDs := make([]uint, 0, len(records)-1)
		for _, record := range records[1:] {
			deleteIDs = append(deleteIDs, record.ID)
		}
		if len(deleteIDs) == 0 {
			continue
		}
		if err := db.Where("id IN ?", deleteIDs).Delete(&models.AttendanceRecord{}).Error; err != nil {
			return err
		}
	}

	return nil
}

func backfillAttendanceRecordsWithDB(db *gorm.DB, session *models.AttendanceSession, sectionIDs []uint) error {
	type idRow struct {
		StudentID uint `gorm:"column:student_id"`
	}

	if err := dedupeAttendanceRecordsWithDB(db, session.ID, nil); err != nil {
		return err
	}

	var targetRows []idRow
	if len(sectionIDs) > 0 {
		if err := db.Raw(`SELECT DISTINCT student_id FROM course_section_students WHERE course_section_id IN ?`, sectionIDs).Scan(&targetRows).Error; err != nil {
			return err
		}
	} else {
		if err := db.Raw(`
			SELECT DISTINCT css.student_id
			FROM course_section_students css
			JOIN course_sections cs ON cs.id = css.course_section_id
			WHERE cs.course_id = ?
		`, session.CourseID).Scan(&targetRows).Error; err != nil {
			return err
		}
	}

	if len(targetRows) == 0 {
		return nil
	}

	var existingRows []idRow
	if err := db.Raw(`SELECT student_id FROM attendance_records WHERE attendance_session_id = ?`, session.ID).Scan(&existingRows).Error; err != nil {
		return err
	}

	existing := make(map[uint]bool, len(existingRows))
	for _, row := range existingRows {
		existing[row.StudentID] = true
	}

	now := time.Now()
	newRecords := make([]models.AttendanceRecord, 0)
	for _, row := range targetRows {
		if row.StudentID == 0 || existing[row.StudentID] {
			continue
		}
		newRecords = append(newRecords, models.AttendanceRecord{
			AttendanceSessionID: session.ID,
			StudentID:           row.StudentID,
			Status:              "absent",
			CreatedAt:           now,
			UpdatedAt:           now,
		})
	}

	if len(newRecords) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "attendance_session_id"}, {Name: "student_id"}},
		DoNothing: true,
	}).Create(&newRecords).Error
}

func ensureAttendanceRecordInTx(tx *gorm.DB, session *models.AttendanceSession, studentID uint) (*models.AttendanceRecord, error) {
	sectionIDs, err := attendanceSessionSectionIDsWithDB(tx, session)
	if err != nil {
		return nil, err
	}

	allowed, err := attendanceStudentEligibleWithDB(tx, session.CourseID, sectionIDs, studentID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAttendanceStudentNotEligiblePublic
	}

	if err := dedupeAttendanceRecordsWithDB(tx, session.ID, &studentID); err != nil {
		return nil, err
	}

	var record models.AttendanceRecord
	if err := tx.Where("attendance_session_id = ? AND student_id = ?", session.ID, studentID).First(&record).Error; err == nil {
		return &record, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := time.Now()
	record = models.AttendanceRecord{
		AttendanceSessionID: session.ID,
		StudentID:           studentID,
		Status:              "absent",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "attendance_session_id"}, {Name: "student_id"}},
		DoNothing: true,
	}).Create(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == 0 {
		if err := tx.Where("attendance_session_id = ? AND student_id = ?", session.ID, studentID).First(&record).Error; err != nil {
			return nil, err
		}
	}
	return &record, nil
}

func EnsureAttendanceRecordForStudent(sessionID uint, studentID uint) (*models.AttendanceRecord, error) {
	tx := config.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}

	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()

	var session models.AttendanceSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	record, err := ensureAttendanceRecordInTx(tx, &session, studentID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return record, nil
}

func GetAttendanceStudentSessionStatus(sessionID uint, studentID uint) (*AttendanceStudentSessionStatus, error) {
	db := config.DB

	var session models.AttendanceSession
	if err := db.Select("id", "course_id", "course_section_id").First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttendanceSessionNotFoundPublic
		}
		return nil, err
	}

	sectionIDs, err := attendanceSessionSectionIDsWithDB(db, &session)
	if err != nil {
		return nil, err
	}

	allowed, err := attendanceStudentEligibleWithDB(db, session.CourseID, sectionIDs, studentID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAttendanceStudentNotEligiblePublic
	}

	status := &AttendanceStudentSessionStatus{}
	var record models.AttendanceRecord
	if err := db.Select("status", "check_in_time").Where("attendance_session_id = ? AND student_id = ?", sessionID, studentID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status, nil
		}
		return nil, err
	}

	if record.Status != "absent" && record.CheckInTime != nil {
		status.AlreadyCheckedIn = true
		status.Status = record.Status
		status.CheckInTime = record.CheckInTime
	}

	return status, nil
}

func GetAttendanceSessions(courseID string, status string) ([]AttendanceSessionWithStats, error) {
	db := config.DB
	var sessions []models.AttendanceSession
	q := db.Where("course_id = ?", courseID)
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	if err := q.Order("created_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}

	sessionIDs := make([]uint, len(sessions))
	for i, s := range sessions {
		sessionIDs[i] = s.ID
	}

	// Batch stats
	type statRow struct {
		AttendanceSessionID uint
		Status              string
		Count               int64
	}
	var statRows []statRow
	if len(sessionIDs) > 0 {
		db.Raw(`SELECT attendance_session_id, status, COUNT(*) as count FROM attendance_records WHERE attendance_session_id IN ? GROUP BY attendance_session_id, status`, sessionIDs).Scan(&statRows)
	}
	statsMap := map[uint]*AttendanceStats{}
	for _, id := range sessionIDs {
		statsMap[id] = &AttendanceStats{}
	}
	for _, r := range statRows {
		s := statsMap[r.AttendanceSessionID]
		if s == nil {
			continue
		}
		switch r.Status {
		case "present":
			s.Present = int(r.Count)
		case "late":
			s.Late = int(r.Count)
		case "leave":
			s.Leave = int(r.Count)
		case "absent":
			s.Absent = int(r.Count)
		}
		s.Total += int(r.Count)
		if r.Status != "absent" {
			s.CheckedIn += int(r.Count)
		}
	}

	// Batch section IDs
	type sectionRow struct {
		AttendanceSessionID uint
		CourseSectionID     uint
	}
	var sectionRows []sectionRow
	if len(sessionIDs) > 0 {
		db.Raw(`SELECT attendance_session_id, course_section_id FROM attendance_session_sections WHERE attendance_session_id IN ?`, sessionIDs).Scan(&sectionRows)
	}
	sectionMap := map[uint][]uint{}
	for _, r := range sectionRows {
		sectionMap[r.AttendanceSessionID] = append(sectionMap[r.AttendanceSessionID], r.CourseSectionID)
	}

	result := make([]AttendanceSessionWithStats, len(sessions))
	for i, s := range sessions {
		syncedSession, _, err := syncAttendanceSessionPinState(db, s.ID)
		if err == nil && syncedSession != nil {
			s = *syncedSession
		}
		stats := AttendanceStats{}
		if st := statsMap[s.ID]; st != nil {
			stats = *st
		}
		result[i] = AttendanceSessionWithStats{
			AttendanceSession: s,
			SectionIDs:        sectionMap[s.ID],
			Stats:             stats,
		}
	}
	return result, nil
}

func GetAttendanceSession(id uint) (*AttendanceSessionDetail, error) {
	db := config.DB
	session, _, err := syncAttendanceSessionPinState(db, id)
	if err != nil {
		return nil, err
	}

	sectionIDs, err := attendanceSessionSectionIDsWithDB(db, session)
	if err != nil {
		return nil, err
	}
	if err := backfillAttendanceRecordsWithDB(db, session, sectionIDs); err != nil {
		return nil, err
	}

	var section *AttendanceSectionBasic
	sectionLookupID := uint(0)
	if session.CourseSectionID != nil && *session.CourseSectionID > 0 {
		sectionLookupID = *session.CourseSectionID
	} else if len(sectionIDs) == 1 {
		sectionLookupID = sectionIDs[0]
	}
	if sectionLookupID > 0 {
		var sectionRow AttendanceSectionBasic
		if err := db.Raw(`SELECT id, section_no FROM course_sections WHERE id = ?`, sectionLookupID).Scan(&sectionRow).Error; err == nil && sectionRow.ID != 0 {
			section = &sectionRow
		}
	}

	var course *AttendanceCourseBasic
	if strings.TrimSpace(session.CourseID) != "" {
		var courseRow AttendanceCourseBasic
		if err := db.Raw(`SELECT id, code, name, year, semester FROM courses WHERE id = ?`, session.CourseID).Scan(&courseRow).Error; err == nil && courseRow.ID != "" {
			course = &courseRow
		}
	}

	var creator *AttendanceCreatorBasic
	if session.CreatedBy != nil && *session.CreatedBy > 0 {
		var creatorRow AttendanceCreatorBasic
		if err := db.Raw(`SELECT id, full_name FROM users WHERE id = ?`, *session.CreatedBy).Scan(&creatorRow).Error; err == nil && creatorRow.ID != 0 {
			creator = &creatorRow
		}
	}

	totalStudents, err := countAttendanceTargetStudents(session.CourseID, sectionIDs)
	if err != nil {
		return nil, err
	}

	// Get records with student info joined
	type recordStudentRow struct {
		// AttendanceRecord fields
		RecordID            uint       `gorm:"column:record_id"`
		AttendanceSessionID uint       `gorm:"column:attendance_session_id"`
		StudentIDFK         uint       `gorm:"column:student_id_fk"`
		CheckInTime         *time.Time `gorm:"column:check_in_time"`
		RecordStatus        string     `gorm:"column:record_status"`
		GoogleEmail         *string    `gorm:"column:google_email"`
		GoogleID            *string    `gorm:"column:google_id"`
		PinVerified         bool       `gorm:"column:pin_verified"`
		LocationVerified    bool       `gorm:"column:location_verified"`
		Note                *string    `gorm:"column:record_note"`
		LocationLat         *float64   `gorm:"column:location_lat"`
		LocationLng         *float64   `gorm:"column:location_lng"`
		DistanceMeters      *int       `gorm:"column:distance_meters"`
		UpdatedBy           *uint      `gorm:"column:updated_by"`
		RecordCreatedAt     time.Time  `gorm:"column:record_created_at"`
		RecordUpdatedAt     time.Time  `gorm:"column:record_updated_at"`
		// Student fields
		StuID        uint   `gorm:"column:stu_id"`
		StuStudentID string `gorm:"column:stu_student_id"`
		StuFullName  string `gorm:"column:stu_full_name"`
		StuEmail     string `gorm:"column:stu_email"`
	}

	var rows []recordStudentRow
	db.Raw(`
		SELECT
			ar.id as record_id,
			ar.attendance_session_id,
			ar.student_id as student_id_fk,
			ar.check_in_time,
			ar.status as record_status,
			ar.google_email,
			ar.google_id,
			ar.pin_verified,
			ar.location_verified,
			ar.note as record_note,
			ar.location_lat,
			ar.location_lng,
			ar.distance_meters,
			ar.updated_by,
			ar.created_at as record_created_at,
			ar.updated_at as record_updated_at,
			s.id as stu_id,
			s.student_id as stu_student_id,
			s.full_name as stu_full_name,
			s.email as stu_email
		FROM attendance_records ar
		LEFT JOIN students s ON s.id = ar.student_id
		WHERE ar.attendance_session_id = ?
		ORDER BY ar.check_in_time DESC NULLS LAST, ar.id ASC
	`, id).Scan(&rows)

	type studentSectionRow struct {
		StudentID uint   `gorm:"column:student_id"`
		SectionNo string `gorm:"column:section_no"`
	}

	var sectionRows []studentSectionRow
	sectionQuery := db.Raw(`
		SELECT css.student_id, cs.section_no
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ?
		ORDER BY cs.section_no ASC, css.student_id ASC
	`, session.CourseID)
	if len(sectionIDs) > 0 {
		sectionQuery = db.Raw(`
			SELECT css.student_id, cs.section_no
			FROM course_section_students css
			JOIN course_sections cs ON cs.id = css.course_section_id
			WHERE css.course_section_id IN ?
			ORDER BY cs.section_no ASC, css.student_id ASC
		`, sectionIDs)
	}
	sectionQuery.Scan(&sectionRows)

	studentSections := make(map[uint][]string)
	for _, row := range sectionRows {
		if row.StudentID == 0 || row.SectionNo == "" {
			continue
		}
		studentSections[row.StudentID] = append(studentSections[row.StudentID], row.SectionNo)
	}

	records := make([]AttendanceRecordWithStudent, len(rows))
	stats := AttendanceSessionDetailStats{TotalStudents: totalStudents}
	for i, r := range rows {
		googleEmail := ""
		if r.GoogleEmail != nil {
			googleEmail = *r.GoogleEmail
		}
		googleID := ""
		if r.GoogleID != nil {
			googleID = *r.GoogleID
		}
		note := ""
		if r.Note != nil {
			note = *r.Note
		}
		records[i] = AttendanceRecordWithStudent{
			AttendanceRecord: models.AttendanceRecord{
				ID:                  r.RecordID,
				AttendanceSessionID: r.AttendanceSessionID,
				StudentID:           r.StudentIDFK,
				CheckInTime:         r.CheckInTime,
				Status:              r.RecordStatus,
				GoogleEmail:         googleEmail,
				GoogleID:            googleID,
				PinVerified:         r.PinVerified,
				LocationVerified:    r.LocationVerified,
				Note:                note,
				LocationLat:         r.LocationLat,
				LocationLng:         r.LocationLng,
				DistanceMeters:      r.DistanceMeters,
				UpdatedBy:           r.UpdatedBy,
				CreatedAt:           r.RecordCreatedAt,
				UpdatedAt:           r.RecordUpdatedAt,
			},
			Student: AttendanceStudentBasic{
				ID:        r.StuID,
				StudentID: r.StuStudentID,
				FullName:  r.StuFullName,
				Email:     r.StuEmail,
			},
			SectionNo: strings.Join(studentSections[r.StudentIDFK], ", "),
		}
		switch r.RecordStatus {
		case "present":
			stats.Present++
			stats.CheckedIn++
		case "late":
			stats.Late++
			stats.CheckedIn++
		case "leave":
			stats.Leave++
			stats.CheckedIn++
		case "absent":
			stats.Absent++
		}
	}
	if stats.TotalStudents < stats.CheckedIn {
		stats.NotCheckedIn = 0
	} else {
		stats.NotCheckedIn = stats.TotalStudents - stats.CheckedIn
	}

	detail := &AttendanceSessionDetail{
		AttendanceSession: *session,
		CourseSectionIDs:  sectionIDs,
		Section:           section,
		Course:            course,
		Creator:           creator,
		Records:           records,
		Stats:             stats,
	}

	if resolvedSession, _, err := ResolveAttendanceSessionPinState(context.Background(), id, true); err == nil && resolvedSession != nil {
		detail.AttendanceSession = *resolvedSession
	}

	return detail, nil
}

func CreateAttendanceSession(session *models.AttendanceSession, sectionIDs []uint) error {
	db := config.DB
	autoRotate := session.AutoRotatePin != nil && *session.AutoRotatePin
	session.PinMode = ConfiguredAttendancePinMode(autoRotate)
	if autoRotate {
		session.PinCode = ""
		session.PinIssuedAt = nil
	} else {
		session.PinCode = strings.TrimSpace(session.PinCode)
		if session.PinCode == "" {
			session.PinCode = GeneratePIN()
		}
		if session.PinIssuedAt == nil {
			now := time.Now()
			session.PinIssuedAt = &now
		}
	}
	session.PreviousPinCode = ""
	session.PinGraceUntil = nil
	session.PinRotatesAt = nil
	session.Status = "draft"
	if err := db.Create(session).Error; err != nil {
		return err
	}

	// Create section links
	if len(sectionIDs) > 0 {
		links := make([]models.AttendanceSessionSection, len(sectionIDs))
		for i, sid := range sectionIDs {
			links[i] = models.AttendanceSessionSection{
				AttendanceSessionID: session.ID,
				CourseSectionID:     sid,
				CreatedAt:           time.Now(),
			}
		}
		db.Create(&links)
	}

	// Pre-create absent records for students in target sections
	var studentIDs []uint
	if len(sectionIDs) > 0 {
		type idRow struct{ StudentID uint }
		var rows []idRow
		db.Raw(`SELECT DISTINCT student_id FROM course_section_students WHERE course_section_id IN ?`, sectionIDs).Scan(&rows)
		for _, r := range rows {
			studentIDs = append(studentIDs, r.StudentID)
		}
	} else {
		// All sections in the course
		type idRow struct{ StudentID uint }
		var rows []idRow
		db.Raw(`SELECT DISTINCT css.student_id FROM course_section_students css JOIN course_sections cs ON cs.id = css.course_section_id WHERE cs.course_id = ?`, session.CourseID).Scan(&rows)
		for _, r := range rows {
			studentIDs = append(studentIDs, r.StudentID)
		}
	}

	if len(studentIDs) > 0 {
		records := make([]models.AttendanceRecord, len(studentIDs))
		for i, sid := range studentIDs {
			records[i] = models.AttendanceRecord{
				AttendanceSessionID: session.ID,
				StudentID:           sid,
				Status:              "absent",
				CreatedAt:           time.Now(),
			}
		}
		db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "attendance_session_id"}, {Name: "student_id"}},
			DoNothing: true,
		}).Create(&records)
	}
	return nil
}

func UpdateAttendanceSession(session *models.AttendanceSession) error {
	return config.DB.Save(session).Error
}

func DeleteAttendanceSession(id uint) error {
	db := config.DB
	db.Where("attendance_session_id = ?", id).Delete(&models.AttendanceRecord{})
	db.Where("attendance_session_id = ?", id).Delete(&models.AttendanceSessionSection{})
	return db.Where("id = ?", id).Delete(&models.AttendanceSession{}).Error
}

// ============================================================
// Attendance Record management
// ============================================================

func UpdateAttendanceRecord(sessionID uint, studentID uint, status string, note string, updatedBy uint) error {
	_, err := UpdateAttendanceRecordReturningPrevious(sessionID, studentID, status, note, updatedBy)
	return err
}

// AttendanceRecordSnapshot is a record's state before an instructor edited it.
//
// A student with no record yet is materialised as "absent" by
// ensureAttendanceRecordInTx before the edit lands, and that is reported as the
// previous status rather than as an empty one: absent is exactly what the
// student counted as up to that moment.
type AttendanceRecordSnapshot struct {
	Status string
	Note   string
}

// UpdateAttendanceRecordReturningPrevious changes a record and reports what it
// held before. The previous state is read inside the same locked transaction as
// the write, so it is always the state that was actually replaced.
func UpdateAttendanceRecordReturningPrevious(sessionID uint, studentID uint, status string, note string, updatedBy uint) (AttendanceRecordSnapshot, error) {
	var previous AttendanceRecordSnapshot

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}

		record, err := ensureAttendanceRecordInTx(tx, &session, studentID)
		if err != nil {
			return err
		}
		if record != nil {
			previous = AttendanceRecordSnapshot{Status: record.Status, Note: record.Note}
		}

		sectionIDs, err := attendanceSessionSectionIDsWithDB(tx, &session)
		if err != nil {
			return err
		}
		if err := backfillAttendanceRecordsWithDB(tx, &session, sectionIDs); err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&models.AttendanceRecord{}).
			Where("id = ?", record.ID).
			Updates(map[string]interface{}{
				"status": status,
				"check_in_time": func() *time.Time {
					if status != "absent" {
						return &now
					}
					return nil
				}(),
				"note":       note,
				"updated_by": updatedBy,
				"updated_at": now,
			}).Error
	})

	return previous, err
}

func BulkUpdateAttendanceRecords(sessionID uint, updates []AttendanceRecordUpdate, updatedBy uint) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}

		now := time.Now()
		for _, u := range updates {
			record, err := ensureAttendanceRecordInTx(tx, &session, u.StudentID)
			if err != nil {
				return err
			}

			var checkInTime *time.Time
			if u.Status != "absent" {
				checkInTime = &now
			}

			if err := tx.Model(&models.AttendanceRecord{}).
				Where("id = ?", record.ID).
				Updates(map[string]interface{}{
					"status":        u.Status,
					"check_in_time": checkInTime,
					"note":          u.Note,
					"updated_by":    updatedBy,
					"updated_at":    now,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type AttendanceRecordUpdate struct {
	StudentID uint
	Status    string
	Note      string
}

// Student check-in (public)
func StudentCheckIn(sessionID uint, studentID uint, pin string, lat *float64, lng *float64, googleEmail string, googleID string, clientRequestID string) (*AttendanceCheckInResult, error) {
	startedAt := time.Now()
	observability.RecordAttendanceCheckInAttempt()
	normalizedRequestID := strings.TrimSpace(clientRequestID)

	if normalizedRequestID != "" {
		cacheCtx, cacheCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		cachedResult, cacheErr := getAttendanceCheckInCachedResult(cacheCtx, sessionID, studentID, normalizedRequestID)
		cacheCancel()
		if cacheErr == nil && cachedResult != nil {
			if !cachedResult.IsDuplicate {
				cachedResult.IsDuplicate = true
			}
			observability.RecordAttendanceCheckInDuplicate(time.Since(startedAt))
			return cachedResult, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	lookupSessionID, err := LookupAttendanceSessionIDByPIN(ctx, pin)
	if err != nil {
		observability.RecordAttendanceWrongPin(time.Since(startedAt))
		// A PIN that matches no open session is a wrong PIN, and must surface as
		// the public wrong-PIN error like every other PIN failure in this
		// function. Returning the bare sentinel let handlers fall through to
		// their generic branch, so students who typed a rotated-out PIN were
		// shown the raw English "attendance invalid pin" under the title
		// "เช็กชื่อไม่สำเร็จ", with the audit log recording the catch-all
		// ATTENDANCE_ERROR. Nothing told them to re-read the projector, so they
		// retried the same dead PIN instead — 42 failures across 19 students in
		// one session on 2026-08-31.
		if errors.Is(err, ErrAttendanceInvalidPIN) {
			return nil, ErrAttendanceInvalidPINPublic
		}
		return nil, err
	}
	if lookupSessionID != sessionID {
		observability.RecordAttendanceWrongPin(time.Since(startedAt))
		return nil, ErrAttendanceInvalidPINPublic
	}

	db := config.DB
	var result AttendanceCheckInResult
	err = db.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.First(&session, sessionID).Error; err != nil {
			return ErrAttendanceSessionNotFoundPublic
		}

		record, err := ensureAttendanceRecordInTx(tx, &session, studentID)
		if err != nil {
			return err
		}

		now := time.Now()
		if now.Before(session.StartTime) {
			return ErrAttendanceSessionNotStartedPublic
		}
		if now.After(session.EndTime) || session.Status != "active" {
			return ErrAttendanceSessionClosedPublic
		}

		providedPin := strings.TrimSpace(pin)
		if providedPin == "" {
			return ErrAttendancePINRequiredPublic
		}
		pinHash := attendancePinHash(providedPin)
		pinAccepted := pinHash == session.CurrentPinHash
		if !pinAccepted && session.PreviousPinHash != "" && session.PinGraceUntil != nil && session.PinGraceUntil.After(now) {
			pinAccepted = pinHash == session.PreviousPinHash
		}
		if !pinAccepted {
			return ErrAttendanceInvalidPINPublic
		}

		var distanceMeters *int
		locationVerified := false
		if session.CheckLocation {
			if lat == nil || lng == nil {
				return ErrAttendanceLocationRequiredPublic
			}
			if session.LocationLat == nil || session.LocationLng == nil {
				return ErrAttendanceSessionLocationNotConfiguredPublic
			}

			distance := calculateDistanceMeters(*session.LocationLat, *session.LocationLng, *lat, *lng)
			distanceMeters = &distance
			if session.RadiusMeters > 0 && distance > session.RadiusMeters {
				return NewAttendanceOutsideAllowedAreaError(distance)
			}
			locationVerified = true
		}

		status := "present"
		lateTime := computeAttendanceLateThreshold(session.StartTime, session.LateThresholdTime, session.LateThresholdMinutes)
		if now.After(lateTime) {
			status = "late"
		}

		checkInTime := now
		updates := map[string]interface{}{
			"status":            status,
			"check_in_time":     &checkInTime,
			"pin_verified":      true,
			"google_email":      googleEmail,
			"google_id":         googleID,
			"updated_at":        now,
			"location_verified": locationVerified,
			"distance_meters":   distanceMeters,
		}
		if lat != nil {
			updates["location_lat"] = *lat
			updates["location_lng"] = *lng
		}

		if record.CheckInTime != nil && record.Status != "absent" {
			result = AttendanceCheckInResult{
				Status:           record.Status,
				CheckInTime:      *record.CheckInTime,
				LocationVerified: record.LocationVerified,
				DistanceMeters:   record.DistanceMeters,
				IsDuplicate:      true,
			}
			observability.RecordAttendanceCheckInDuplicate(time.Since(startedAt))
			return nil
		}

		updateResult := tx.Model(&models.AttendanceRecord{}).
			Where("id = ? AND (check_in_time IS NULL OR status = 'absent')", record.ID).
			Updates(updates)
		if updateResult.RowsAffected == 0 {
			var latest models.AttendanceRecord
			if err := tx.Where("id = ?", record.ID).First(&latest).Error; err != nil {
				return err
			}
			if latest.CheckInTime != nil && latest.Status != "absent" {
				result = AttendanceCheckInResult{
					Status:           latest.Status,
					CheckInTime:      *latest.CheckInTime,
					LocationVerified: latest.LocationVerified,
					DistanceMeters:   latest.DistanceMeters,
					IsDuplicate:      true,
				}
				observability.RecordAttendanceCheckInDuplicate(time.Since(startedAt))
				return nil
			}
			return ErrAttendanceCourseNotRegisteredPublic
		}
		if updateResult.Error != nil {
			return updateResult.Error
		}

		result = AttendanceCheckInResult{
			Status:           status,
			CheckInTime:      checkInTime,
			LocationVerified: locationVerified,
			DistanceMeters:   distanceMeters,
			IsDuplicate:      false,
		}
		return nil
	})
	if err != nil {
		observability.RecordAttendanceCheckInFailure(time.Since(startedAt))
		return nil, err
	}

	if normalizedRequestID != "" {
		setAttendanceCheckInCachedResultAsync(sessionID, studentID, normalizedRequestID, &result)
	}

	observability.RecordAttendanceCheckInSuccess(time.Since(startedAt))
	return &result, nil
}

// GetAttendanceSessionType returns just the session_type column, for callers
// (like the campus network guard) that only need to decide whether a
// session is exempt (online) without paying for the full GetSessionInfo/PIN
// state resolution.
func GetAttendanceSessionType(sessionID uint) (string, error) {
	return GetAttendanceSessionTypeCtx(context.Background(), sessionID)
}

// GetAttendanceSessionTypeCtx is GetAttendanceSessionType with a caller-supplied
// context. The campus network guard needs the deadline: it now fails a check-in
// closed when this lookup errors, so an unbounded query here would turn a slow
// database into a hung request instead of a fast, retryable 503.
func GetAttendanceSessionTypeCtx(ctx context.Context, sessionID uint) (string, error) {
	var sessionType string
	err := config.DB.WithContext(ctx).Model(&models.AttendanceSession{}).
		Select("session_type").
		Where("id = ?", sessionID).
		Take(&sessionType).Error
	if err != nil {
		return "", err
	}
	return sessionType, nil
}

func GetSessionInfo(sessionID uint) (*AttendanceSessionInfo, error) {
	session, _, err := ResolveAttendanceSessionPinState(context.Background(), sessionID, true)
	if err != nil {
		return nil, err
	}

	var course *AttendanceCourseBasic
	if strings.TrimSpace(session.CourseID) != "" {
		var courseRow AttendanceCourseBasic
		if err := config.DB.Raw(`SELECT id, code, name, year, semester FROM courses WHERE id = ?`, session.CourseID).Scan(&courseRow).Error; err == nil && courseRow.ID != "" {
			course = &courseRow
		}
	}

	var section *AttendanceSectionBasic
	if session.CourseSectionID != nil && *session.CourseSectionID > 0 {
		var sectionRow AttendanceSectionBasic
		if err := config.DB.Raw(`SELECT id, section_no FROM course_sections WHERE id = ?`, *session.CourseSectionID).Scan(&sectionRow).Error; err == nil && sectionRow.ID != 0 {
			section = &sectionRow
		}
	}

	return &AttendanceSessionInfo{
		ID:                   session.ID,
		Title:                session.Title,
		SessionType:          session.SessionType,
		CheckLocation:        session.CheckLocation,
		AutoRotatePin:        session.AutoRotatePin != nil && *session.AutoRotatePin,
		PinMode:              session.PinMode,
		PinCode:              session.PinCode,
		PinIssued:            strings.TrimSpace(session.PinCode) != "",
		PinIssuedAt:          session.PinIssuedAt,
		PinRotatesAt:         session.PinRotatesAt,
		LateThresholdMinutes: session.LateThresholdMinutes,
		LateThresholdTime:    session.LateThresholdTime,
		StartTime:            session.StartTime,
		EndTime:              session.EndTime,
		Status:               session.Status,
		Course:               course,
		Section:              section,
	}, nil
}

// ---------------------------------------------------------------------------
// Course-wide attendance report (TOR 3.9.2)
// ---------------------------------------------------------------------------

type AttendanceCourseSessionRow struct {
	ID    uint            `json:"id"`
	Title string          `json:"title"`
	Date  time.Time       `json:"date"`
	Stats AttendanceStats `json:"stats"`
	Rate  float64         `json:"attendance_rate"`
}

type AttendanceCourseStudentRow struct {
	StudentID  uint    `json:"student_id"`
	StudentNo  string  `json:"student_no"`
	FullName   string  `json:"full_name"`
	Present    int     `json:"present"`
	Late       int     `json:"late"`
	Leave      int     `json:"leave"`
	Absent     int     `json:"absent"`
	TotalMarks int     `json:"total_marks"`
	Rate       float64 `json:"attendance_rate"`
}

type AttendanceCourseSummary struct {
	CourseID      string                       `json:"course_id"`
	TotalSessions int                          `json:"total_sessions"`
	TotalStudents int                          `json:"total_students"`
	Overall       AttendanceStats              `json:"overall"`
	OverallRate   float64                      `json:"overall_attendance_rate"`
	BySession     []AttendanceCourseSessionRow `json:"by_session"`
	ByStudent     []AttendanceCourseStudentRow `json:"by_student"`
}

// GetAttendanceCourseSummary aggregates attendance across every session in a course,
// producing a per-session and per-student breakdown for the course-wide attendance report.
func GetAttendanceCourseSummary(courseID string) (*AttendanceCourseSummary, error) {
	db := config.DB

	sessions, err := GetAttendanceSessions(courseID, "")
	if err != nil {
		return nil, err
	}

	totalStudents, err := countAttendanceTargetStudents(courseID, nil)
	if err != nil {
		return nil, err
	}

	summary := &AttendanceCourseSummary{
		CourseID:      courseID,
		TotalSessions: len(sessions),
		TotalStudents: totalStudents,
		BySession:     make([]AttendanceCourseSessionRow, 0, len(sessions)),
		ByStudent:     make([]AttendanceCourseStudentRow, 0),
	}

	sessionIDs := make([]uint, 0, len(sessions))
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.ID)

		summary.Overall.Present += s.Stats.Present
		summary.Overall.Late += s.Stats.Late
		summary.Overall.Leave += s.Stats.Leave
		summary.Overall.Absent += s.Stats.Absent
		summary.Overall.Total += s.Stats.Total
		summary.Overall.CheckedIn += s.Stats.CheckedIn

		rate := 0.0
		eligible := s.Stats.Total
		if eligible > 0 {
			rate = float64(s.Stats.CheckedIn) / float64(eligible) * 100
		}
		summary.BySession = append(summary.BySession, AttendanceCourseSessionRow{
			ID:    s.ID,
			Title: s.Title,
			Date:  s.StartTime,
			Stats: s.Stats,
			Rate:  math.Round(rate*100) / 100,
		})
	}

	if summary.Overall.Total > 0 {
		summary.OverallRate = math.Round(float64(summary.Overall.CheckedIn)/float64(summary.Overall.Total)*100*100) / 100
	}

	if len(sessionIDs) == 0 {
		return summary, nil
	}

	type studentStatRow struct {
		StudentID uint
		StudentNo string
		FullName  string
		Status    string
		Count     int64
	}
	var rows []studentStatRow
	if err := db.Raw(`
		SELECT ar.student_id AS student_id, st.student_id AS student_no, st.full_name AS full_name, ar.status AS status, COUNT(*) AS count
		FROM attendance_records ar
		JOIN students st ON st.id = ar.student_id
		WHERE ar.attendance_session_id IN ?
		GROUP BY ar.student_id, st.student_id, st.full_name, ar.status
	`, sessionIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}

	studentMap := map[uint]*AttendanceCourseStudentRow{}
	order := make([]uint, 0)
	for _, r := range rows {
		row, ok := studentMap[r.StudentID]
		if !ok {
			row = &AttendanceCourseStudentRow{StudentID: r.StudentID, StudentNo: r.StudentNo, FullName: r.FullName}
			studentMap[r.StudentID] = row
			order = append(order, r.StudentID)
		}
		switch r.Status {
		case "present":
			row.Present = int(r.Count)
		case "late":
			row.Late = int(r.Count)
		case "leave":
			row.Leave = int(r.Count)
		case "absent":
			row.Absent = int(r.Count)
		}
		row.TotalMarks += int(r.Count)
	}

	for _, sid := range order {
		row := studentMap[sid]
		checkedIn := row.Present + row.Late + row.Leave
		if row.TotalMarks > 0 {
			row.Rate = math.Round(float64(checkedIn)/float64(row.TotalMarks)*100*100) / 100
		}
		summary.ByStudent = append(summary.ByStudent, *row)
	}

	return summary, nil
}
