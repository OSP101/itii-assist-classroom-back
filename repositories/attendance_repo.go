package repositories

import (
	"crypto/rand"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"math"
	"strings"
	"time"
)

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
	Student AttendanceStudentBasic `json:"student"`
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
}

type AttendanceSessionInfo struct {
	ID            uint                    `json:"id"`
	Title         string                  `json:"title"`
	SessionType   string                  `json:"session_type"`
	CheckLocation bool                    `json:"check_location"`
	StartTime     time.Time               `json:"start_time"`
	EndTime       time.Time               `json:"end_time"`
	Status        string                  `json:"status"`
	Course        *AttendanceCourseBasic  `json:"course"`
	Section       *AttendanceSectionBasic `json:"section"`
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
		stats := AttendanceStats{}
		if st := statsMap[s.ID]; st != nil {
			stats = *st
		}
		// Compute status dynamically from time
		s.Status = ComputeSessionStatus(s)
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
	var session models.AttendanceSession
	if err := db.First(&session, id).Error; err != nil {
		return nil, err
	}

	// Compute status dynamically
	session.Status = ComputeSessionStatus(session)

	// Get section IDs
	type sectionRow struct{ CourseSectionID uint }
	var sectionRows []sectionRow
	db.Raw(`SELECT course_section_id FROM attendance_session_sections WHERE attendance_session_id = ?`, id).Scan(&sectionRows)
	sectionIDs := make([]uint, len(sectionRows))
	for i, r := range sectionRows {
		sectionIDs[i] = r.CourseSectionID
	}
	if len(sectionIDs) == 0 && session.CourseSectionID != nil && *session.CourseSectionID > 0 {
		sectionIDs = []uint{*session.CourseSectionID}
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

	return &AttendanceSessionDetail{
		AttendanceSession: session,
		CourseSectionIDs:  sectionIDs,
		Section:           section,
		Course:            course,
		Creator:           creator,
		Records:           records,
		Stats:             stats,
	}, nil
}

func CreateAttendanceSession(session *models.AttendanceSession, sectionIDs []uint) error {
	db := config.DB
	if strings.TrimSpace(session.PinCode) == "" {
		session.PinCode = GeneratePIN()
	}
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
		db.Create(&records)
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
	now := time.Now()
	result := config.DB.Model(&models.AttendanceRecord{}).
		Where("attendance_session_id = ? AND student_id = ?", sessionID, studentID).
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
		})
	return result.Error
}

func BulkUpdateAttendanceRecords(sessionID uint, updates []AttendanceRecordUpdate, updatedBy uint) error {
	db := config.DB
	now := time.Now()
	for _, u := range updates {
		var checkInTime *time.Time
		if u.Status != "absent" {
			checkInTime = &now
		}
		db.Model(&models.AttendanceRecord{}).
			Where("attendance_session_id = ? AND student_id = ?", sessionID, u.StudentID).
			Updates(map[string]interface{}{
				"status":        u.Status,
				"check_in_time": checkInTime,
				"note":          u.Note,
				"updated_by":    updatedBy,
				"updated_at":    now,
			})
	}
	return nil
}

type AttendanceRecordUpdate struct {
	StudentID uint
	Status    string
	Note      string
}

// Student check-in (public)
func StudentCheckIn(sessionID uint, studentID uint, pin string, lat *float64, lng *float64, googleEmail string, googleID string) (*AttendanceCheckInResult, error) {
	db := config.DB
	var session models.AttendanceSession
	if err := db.First(&session, sessionID).Error; err != nil {
		return nil, fmt.Errorf("ไม่พบ session")
	}

	// Verify PIN
	if strings.TrimSpace(pin) != session.PinCode {
		return nil, fmt.Errorf("รหัส PIN ไม่ถูกต้อง")
	}

	// Check time window
	now := time.Now()
	if now.Before(session.StartTime) {
		return nil, fmt.Errorf("ยังไม่ถึงเวลาเช็คชื่อ")
	}
	if now.After(session.EndTime) {
		return nil, fmt.Errorf("หมดเวลาเช็คชื่อแล้ว")
	}

	var distanceMeters *int
	locationVerified := false
	if session.CheckLocation {
		if lat == nil || lng == nil {
			return nil, fmt.Errorf("กรุณาอนุญาตการเข้าถึงตำแหน่ง")
		}
		if session.LocationLat == nil || session.LocationLng == nil {
			return nil, fmt.Errorf("session นี้ยังไม่ได้กำหนดตำแหน่งสำหรับเช็คชื่อ")
		}

		distance := calculateDistanceMeters(*session.LocationLat, *session.LocationLng, *lat, *lng)
		distanceMeters = &distance
		if session.RadiusMeters > 0 && distance > session.RadiusMeters {
			return nil, fmt.Errorf("คุณอยู่นอกพื้นที่ที่กำหนด (ห่าง %d เมตร)", distance)
		}
		locationVerified = true
	}

	// Determine status (present or late)
	status := "present"
	lateTime := session.StartTime.Add(time.Duration(session.LateThresholdMinutes) * time.Minute)
	if strings.TrimSpace(session.LateThresholdTime) != "" {
		parts := strings.Split(session.LateThresholdTime, ":")
		if len(parts) >= 2 {
			hours := 0
			minutes := 0
			seconds := 0
			fmt.Sscanf(parts[0], "%d", &hours)
			fmt.Sscanf(parts[1], "%d", &minutes)
			if len(parts) > 2 {
				fmt.Sscanf(parts[2], "%d", &seconds)
			}
			lateTime = time.Date(session.StartTime.Year(), session.StartTime.Month(), session.StartTime.Day(), hours, minutes, seconds, 0, session.StartTime.Location())
		}
	}
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

	result := db.Model(&models.AttendanceRecord{}).
		Where("attendance_session_id = ? AND student_id = ?", sessionID, studentID).
		Updates(updates)
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("คุณไม่ได้ลงทะเบียนในรายวิชานี้")
	}
	if result.Error != nil {
		return nil, result.Error
	}

	return &AttendanceCheckInResult{
		Status:           status,
		CheckInTime:      checkInTime,
		LocationVerified: locationVerified,
		DistanceMeters:   distanceMeters,
	}, nil
}

func GetSessionInfo(sessionID uint) (*AttendanceSessionInfo, error) {
	var session models.AttendanceSession
	if err := config.DB.First(&session, sessionID).Error; err != nil {
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
		ID:            session.ID,
		Title:         session.Title,
		SessionType:   session.SessionType,
		CheckLocation: session.CheckLocation,
		StartTime:     session.StartTime,
		EndTime:       session.EndTime,
		Status:        ComputeSessionStatus(session),
		Course:        course,
		Section:       section,
	}, nil
}
