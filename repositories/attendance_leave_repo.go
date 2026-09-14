package repositories

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// =============================================================================
// คำขอลา (attendance leave requests)
// =============================================================================

const (
	LeaveTypeSick     = "sick"
	LeaveTypePersonal = "personal"
	LeaveTypeOfficial = "official"
	LeaveTypeOther    = "other"

	LeaveStatusPending           = "pending"
	LeaveStatusApproved          = "approved"
	LeaveStatusPartiallyApproved = "partially_approved"
	LeaveStatusRejected          = "rejected"
	LeaveStatusCancelled         = "cancelled"
	LeaveStatusRevoked           = "revoked"

	LeaveItemPending         = "pending"
	LeaveItemApproved        = "approved" // อนุมัติแล้วแต่ยังไม่ได้ลง record (ใช้ชั่วคราวใน transaction)
	LeaveItemRejected        = "rejected"
	LeaveItemApplied         = "applied"          // ลงสถานะ leave ใน record แล้ว
	LeaveItemAwaitingSession = "awaiting_session" // อนุมัติแล้ว รอ session ของวันนั้นถูกสร้าง
	LeaveItemSuperseded      = "superseded"       // นักศึกษามาเช็กชื่อจริง present ทับ leave
	LeaveItemCancelled       = "cancelled"
	LeaveItemRevoked         = "revoked"

	LeaveEvidencePolicyNone         = "none"
	LeaveEvidencePolicySickOnly     = "sick_only"
	LeaveEvidencePolicySickPersonal = "sick_personal"
	LeaveEvidencePolicyAll          = "all"
)

var (
	ErrLeaveRequestDisabled       = errors.New("leave request disabled")
	ErrLeaveRequestNotFound       = errors.New("leave request not found")
	ErrLeaveRequestNotPending     = errors.New("leave request not pending")
	ErrLeaveRequestNotStudent     = errors.New("student not in course")
	ErrLeaveRequestInvalidType    = errors.New("invalid leave type")
	ErrLeaveRequestNoItems        = errors.New("no leave dates")
	ErrLeaveRequestTooManyPending = errors.New("too many pending leave requests")
	ErrLeaveRequestEvidence       = errors.New("evidence required")
	ErrLeaveRequestDuplicate      = errors.New("duplicate leave date")
	ErrLeaveRequestOutOfWindow    = errors.New("leave date out of window")
	ErrLeaveRequestSessionInvalid = errors.New("session not eligible")
	ErrLeaveRequestAlreadyPresent = errors.New("already present")
	ErrLeaveRequestReasonRequired = errors.New("reason required")
	ErrLeaveRequestNotReviewable  = errors.New("leave request cannot be reviewed")
	ErrLeaveRequestNotRevocable   = errors.New("leave request cannot be revoked")
)

// leaveLocation คือเขตเวลาที่ใช้ตีความ "วัน" ของคาบเรียน
var leaveLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}()

func LeaveLocation() *time.Location { return leaveLocation }

func leaveDateOf(t time.Time) time.Time {
	local := t.In(leaveLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, leaveLocation)
}

func IsValidLeaveType(t string) bool {
	switch t {
	case LeaveTypeSick, LeaveTypePersonal, LeaveTypeOfficial, LeaveTypeOther:
		return true
	}
	return false
}

// LeaveEvidenceRequired ตัดสินจากนโยบายของวิชาว่าประเภทการลานี้ต้องมีหลักฐานไหม
func LeaveEvidenceRequired(policy string, leaveType string) bool {
	switch strings.TrimSpace(policy) {
	case LeaveEvidencePolicyNone:
		return false
	case LeaveEvidencePolicySickOnly:
		return leaveType == LeaveTypeSick
	case LeaveEvidencePolicyAll:
		return true
	default: // sick_personal (ค่าเริ่มต้น)
		return leaveType == LeaveTypeSick || leaveType == LeaveTypePersonal
	}
}

// LeaveCourseSettings ค่าตั้งค่าคำขอลาของวิชา (คืนค่าเริ่มต้นถ้ายังไม่ได้ตั้ง)
type LeaveCourseSettings struct {
	Enabled        bool   `json:"enabled"`
	EvidencePolicy string `json:"evidence_policy"`
	BackdateDays   int    `json:"backdate_days"`
	AdvanceDays    int    `json:"advance_days"`
	MaxPending     int    `json:"max_pending"`
}

func LeaveSettingsFromCourse(course *models.Course) LeaveCourseSettings {
	s := LeaveCourseSettings{Enabled: true, EvidencePolicy: LeaveEvidencePolicySickPersonal, BackdateDays: 7, AdvanceDays: 60, MaxPending: 5}
	if course == nil {
		return s
	}
	if course.LeaveRequestEnabled != nil {
		s.Enabled = *course.LeaveRequestEnabled
	}
	if strings.TrimSpace(course.LeaveEvidencePolicy) != "" {
		s.EvidencePolicy = course.LeaveEvidencePolicy
	}
	if course.LeaveBackdateDays >= 0 {
		s.BackdateDays = course.LeaveBackdateDays
	}
	if course.LeaveAdvanceDays >= 0 {
		s.AdvanceDays = course.LeaveAdvanceDays
	}
	if course.LeaveMaxPending > 0 {
		s.MaxPending = course.LeaveMaxPending
	}
	return s
}

func GetLeaveCourseSettings(courseID string) (LeaveCourseSettings, *models.Course, error) {
	var course models.Course
	if err := config.DB.Select("id, code, name, is_active, leave_request_enabled, leave_evidence_policy, leave_backdate_days, leave_advance_days, leave_max_pending").First(&course, "id = ?", courseID).Error; err != nil {
		return LeaveCourseSettings{}, nil, err
	}
	return LeaveSettingsFromCourse(&course), &course, nil
}

// leaveWindow คืนช่วงวันที่ขอลาได้ [from, to] (วันแรกและวันสุดท้ายรวม)
func leaveWindow(settings LeaveCourseSettings, now time.Time) (time.Time, time.Time) {
	today := leaveDateOf(now)
	from := today.AddDate(0, 0, -settings.BackdateDays)
	to := today.AddDate(0, 0, settings.AdvanceDays)
	return from, to
}

// -----------------------------------------------------------------------------
// รายการคาบที่ขอลาได้ (ฝั่งนักศึกษา)
// -----------------------------------------------------------------------------

type LeaveEligibleSession struct {
	ID            uint      `json:"id"`
	Title         string    `json:"title"`
	SessionType   string    `json:"session_type"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	LeaveDate     string    `json:"leave_date"`
	Status        string    `json:"status"` // draft, active, closed
	RecordStatus  string    `json:"record_status"`
	RecordSource  string    `json:"record_source"`
	SectionNo     string    `json:"section_no"`
	RequestStatus string    `json:"request_status,omitempty"` // สถานะคำขอที่ค้างอยู่ของคาบนี้ ถ้ามี
	RequestID     *uint     `json:"request_id,omitempty"`
	CanRequest    bool      `json:"can_request"`
	BlockReason   string    `json:"block_reason,omitempty"` // present, pending, approved, out_of_window
}

// ListLeaveEligibleSessions คืน session ของวิชาที่นักศึกษามีสิทธิ์ (ตาม section)
// อยู่ในกรอบวันที่ขอลาได้ พร้อมบอกว่าขอได้ไหมเพราะอะไร
func ListLeaveEligibleSessions(courseID string, studentID uint, settings LeaveCourseSettings) ([]LeaveEligibleSession, error) {
	from, to := leaveWindow(settings, time.Now())
	toEnd := to.AddDate(0, 0, 1)

	type row struct {
		ID           uint
		Title        string
		SessionType  string
		StartTime    time.Time
		EndTime      time.Time
		RecordStatus *string
		RecordSource *string
		SectionNo    *string
	}
	var rows []row
	// session ที่ผูก section ไว้: นักศึกษาต้องอยู่ใน section นั้น
	// session ที่ไม่ผูก section: นักศึกษาอยู่ section ไหนก็ได้ในวิชา
	err := config.DB.Raw(`
		SELECT s.id, s.title, s.session_type, s.start_time, s.end_time,
		       r.status AS record_status, r.status_source AS record_source,
		       (SELECT cs.section_no FROM course_section_students css
		          JOIN course_sections cs ON cs.id = css.course_section_id
		         WHERE css.student_id = ? AND cs.course_id = s.course_id
		         ORDER BY cs.id LIMIT 1) AS section_no
		FROM attendance_sessions s
		LEFT JOIN attendance_records r ON r.attendance_session_id = s.id AND r.student_id = ?
		WHERE s.course_id = ?
		  AND s.start_time >= ? AND s.start_time < ?
		  AND (
		    EXISTS (SELECT 1 FROM attendance_session_sections ass
		              JOIN course_section_students css ON css.course_section_id = ass.course_section_id
		             WHERE ass.attendance_session_id = s.id AND css.student_id = ?)
		    OR (NOT EXISTS (SELECT 1 FROM attendance_session_sections ass WHERE ass.attendance_session_id = s.id)
		        AND (s.course_section_id IS NULL
		             OR EXISTS (SELECT 1 FROM course_section_students css WHERE css.course_section_id = s.course_section_id AND css.student_id = ?))
		        AND EXISTS (SELECT 1 FROM course_section_students css JOIN course_sections cs ON cs.id = css.course_section_id WHERE cs.course_id = s.course_id AND css.student_id = ?))
		  )
		ORDER BY s.start_time ASC
	`, studentID, studentID, courseID, from, toEnd, studentID, studentID, studentID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// คำขอที่ค้าง/อนุมัติแล้วของนักศึกษาในวิชานี้ เอามาบอกว่าคาบไหนขอไปแล้ว
	type openItem struct {
		SessionID  *uint
		LeaveDate  time.Time
		ItemStatus string
		RequestID  uint
		ReqStatus  string
	}
	var open []openItem
	if err := config.DB.Raw(`
		SELECT i.attendance_session_id AS session_id, i.leave_date, i.item_status, i.leave_request_id AS request_id, q.status AS req_status
		FROM attendance_leave_request_items i
		JOIN attendance_leave_requests q ON q.id = i.leave_request_id
		WHERE q.course_id = ? AND q.student_id = ?
		  AND i.item_status IN ('pending','approved','applied','awaiting_session')
	`, courseID, studentID).Scan(&open).Error; err != nil {
		return nil, err
	}
	bySession := map[uint]openItem{}
	byDate := map[string]openItem{}
	for _, it := range open {
		if it.SessionID != nil {
			bySession[*it.SessionID] = it
		} else {
			byDate[leaveDateOf(it.LeaveDate).Format("2006-01-02")] = it
		}
	}

	out := make([]LeaveEligibleSession, 0, len(rows))
	for _, r := range rows {
		item := LeaveEligibleSession{
			ID:          r.ID,
			Title:       r.Title,
			SessionType: r.SessionType,
			StartTime:   r.StartTime,
			EndTime:     r.EndTime,
			LeaveDate:   leaveDateOf(r.StartTime).Format("2006-01-02"),
			Status:      ComputeSessionStatus(models.AttendanceSession{StartTime: r.StartTime, EndTime: r.EndTime}),
			CanRequest:  true,
		}
		if r.RecordStatus != nil {
			item.RecordStatus = *r.RecordStatus
		}
		if r.RecordSource != nil {
			item.RecordSource = *r.RecordSource
		}
		if r.SectionNo != nil {
			item.SectionNo = *r.SectionNo
		}
		if item.RecordStatus == "present" {
			item.CanRequest = false
			item.BlockReason = "present"
		}
		if it, ok := bySession[r.ID]; ok {
			item.RequestID = &it.RequestID
			item.RequestStatus = it.ItemStatus
			item.CanRequest = false
			if it.ItemStatus == LeaveItemPending {
				item.BlockReason = "pending"
			} else {
				item.BlockReason = "approved"
			}
		} else if it, ok := byDate[item.LeaveDate]; ok {
			item.RequestID = &it.RequestID
			item.RequestStatus = it.ItemStatus
			item.CanRequest = false
			if it.ItemStatus == LeaveItemPending {
				item.BlockReason = "pending"
			} else {
				item.BlockReason = "approved"
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// สร้างคำขอ
// -----------------------------------------------------------------------------

type LeaveRequestItemInput struct {
	SessionID *uint  // เลือกคาบ
	Date      string // หรือเลือกวัน (YYYY-MM-DD) สำหรับวันที่ยังไม่มีคาบ
}

type CreateLeaveRequestInput struct {
	CourseID    string
	StudentID   uint
	LeaveType   string
	Reason      string
	Evidence    []string
	Items       []LeaveRequestItemInput
	SubmittedIP string
}

// LeaveRequestValidationError บอกว่า item ไหนผิดเพราะอะไร เพื่อให้หน้าจอชี้ได้
type LeaveRequestValidationError struct {
	Err       error
	ItemIndex int
	Detail    string
}

func (e *LeaveRequestValidationError) Error() string { return e.Err.Error() + ": " + e.Detail }
func (e *LeaveRequestValidationError) Unwrap() error { return e.Err }

func CreateLeaveRequest(input CreateLeaveRequestInput) (*models.AttendanceLeaveRequest, error) {
	settings, _, err := GetLeaveCourseSettings(input.CourseID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, ErrLeaveRequestDisabled
	}
	if !IsValidLeaveType(input.LeaveType) {
		return nil, ErrLeaveRequestInvalidType
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrLeaveRequestReasonRequired
	}
	if len(input.Items) == 0 {
		return nil, ErrLeaveRequestNoItems
	}
	if LeaveEvidenceRequired(settings.EvidencePolicy, input.LeaveType) && len(input.Evidence) == 0 {
		return nil, ErrLeaveRequestEvidence
	}
	if !IsStudentInCourse(input.CourseID, input.StudentID) {
		return nil, ErrLeaveRequestNotStudent
	}

	now := time.Now()
	from, to := leaveWindow(settings, now)

	var created models.AttendanceLeaveRequest
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		var pendingCount int64
		if err := tx.Model(&models.AttendanceLeaveRequest{}).
			Where("course_id = ? AND student_id = ? AND status = ?", input.CourseID, input.StudentID, LeaveStatusPending).
			Count(&pendingCount).Error; err != nil {
			return err
		}
		if int(pendingCount) >= settings.MaxPending {
			return ErrLeaveRequestTooManyPending
		}

		// ล็อกคำขอที่ค้างของนักศึกษาคนนี้ กันส่งซ้ำพร้อมกัน
		type openRow struct {
			SessionID *uint
			LeaveDate time.Time
		}
		var openRows []openRow
		if err := tx.Raw(`
			SELECT i.attendance_session_id AS session_id, i.leave_date
			FROM attendance_leave_request_items i
			JOIN attendance_leave_requests q ON q.id = i.leave_request_id
			WHERE q.course_id = ? AND q.student_id = ?
			  AND i.item_status IN ('pending','approved','applied','awaiting_session')
		`+leaveRowLockClause(tx), input.CourseID, input.StudentID).Scan(&openRows).Error; err != nil {
			return err
		}
		openSessions := map[uint]bool{}
		openDates := map[string]bool{}
		for _, r := range openRows {
			if r.SessionID != nil {
				openSessions[*r.SessionID] = true
			}
			openDates[leaveDateOf(r.LeaveDate).Format("2006-01-02")] = true
		}

		items := make([]models.AttendanceLeaveRequestItem, 0, len(input.Items))
		seenSessions := map[uint]bool{}
		seenDates := map[string]bool{}
		for idx, in := range input.Items {
			if in.SessionID != nil && *in.SessionID > 0 {
				sid := *in.SessionID
				if seenSessions[sid] || openSessions[sid] {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestDuplicate, ItemIndex: idx, Detail: fmt.Sprintf("session %d", sid)}
				}
				var session models.AttendanceSession
				if err := tx.First(&session, sid).Error; err != nil || session.CourseID != input.CourseID {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestSessionInvalid, ItemIndex: idx, Detail: "session not in course"}
				}
				sectionIDs, err := attendanceSessionSectionIDsWithDB(tx, &session)
				if err != nil {
					return err
				}
				eligible, err := attendanceStudentEligibleWithDB(tx, session.CourseID, sectionIDs, input.StudentID)
				if err != nil {
					return err
				}
				if !eligible {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestSessionInvalid, ItemIndex: idx, Detail: "not in section"}
				}
				day := leaveDateOf(session.StartTime)
				if day.Before(from) || day.After(to) {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestOutOfWindow, ItemIndex: idx, Detail: day.Format("2006-01-02")}
				}
				var record models.AttendanceRecord
				if err := tx.Where("attendance_session_id = ? AND student_id = ?", sid, input.StudentID).First(&record).Error; err == nil {
					if record.Status == "present" {
						return &LeaveRequestValidationError{Err: ErrLeaveRequestAlreadyPresent, ItemIndex: idx, Detail: fmt.Sprintf("session %d", sid)}
					}
				}
				dayKey := day.Format("2006-01-02")
				if openDates[dayKey] && !openSessions[sid] {
					// มีคำขอแบบ "เลือกวัน" ครอบวันนี้อยู่แล้ว
					return &LeaveRequestValidationError{Err: ErrLeaveRequestDuplicate, ItemIndex: idx, Detail: dayKey}
				}
				seenSessions[sid] = true
				items = append(items, models.AttendanceLeaveRequestItem{
					LeaveDate:           day,
					AttendanceSessionID: &sid,
					ByDate:              false,
					ItemStatus:          LeaveItemPending,
					CreatedAt:           now,
					UpdatedAt:           now,
				})
				continue
			}

			day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(in.Date), leaveLocation)
			if err != nil {
				return &LeaveRequestValidationError{Err: ErrLeaveRequestNoItems, ItemIndex: idx, Detail: "invalid date"}
			}
			dayKey := day.Format("2006-01-02")
			if seenDates[dayKey] || openDates[dayKey] {
				return &LeaveRequestValidationError{Err: ErrLeaveRequestDuplicate, ItemIndex: idx, Detail: dayKey}
			}
			if day.Before(from) || day.After(to) {
				return &LeaveRequestValidationError{Err: ErrLeaveRequestOutOfWindow, ItemIndex: idx, Detail: dayKey}
			}
			// ถ้าวันนั้นมี session ที่นักศึกษามีสิทธิ์อยู่แล้ว ผูกให้เลย (อาจมีหลายคาบ)
			bound, err := leaveSessionsOnDate(tx, input.CourseID, input.StudentID, day)
			if err != nil {
				return err
			}
			if len(bound) == 0 {
				seenDates[dayKey] = true
				items = append(items, models.AttendanceLeaveRequestItem{
					LeaveDate:  day,
					ByDate:     true,
					ItemStatus: LeaveItemPending,
					CreatedAt:  now,
					UpdatedAt:  now,
				})
				continue
			}
			for _, s := range bound {
				if seenSessions[s.ID] || openSessions[s.ID] {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestDuplicate, ItemIndex: idx, Detail: dayKey}
				}
				var record models.AttendanceRecord
				if err := tx.Where("attendance_session_id = ? AND student_id = ?", s.ID, input.StudentID).First(&record).Error; err == nil && record.Status == "present" {
					return &LeaveRequestValidationError{Err: ErrLeaveRequestAlreadyPresent, ItemIndex: idx, Detail: dayKey}
				}
				sid := s.ID
				seenSessions[sid] = true
				items = append(items, models.AttendanceLeaveRequestItem{
					LeaveDate:           day,
					AttendanceSessionID: &sid,
					ByDate:              true,
					ItemStatus:          LeaveItemPending,
					CreatedAt:           now,
					UpdatedAt:           now,
				})
			}
			seenDates[dayKey] = true
		}

		evidence := datatypes.JSON([]byte("[]"))
		if len(input.Evidence) > 0 {
			evidence = stringsJSON(input.Evidence)
		}
		created = models.AttendanceLeaveRequest{
			CourseID:    input.CourseID,
			StudentID:   input.StudentID,
			LeaveType:   input.LeaveType,
			Reason:      strings.TrimSpace(input.Reason),
			Evidence:    evidence,
			Status:      LeaveStatusPending,
			SubmittedIP: input.SubmittedIP,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].LeaveRequestID = created.ID
		}
		return tx.Create(&items).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func stringsJSON(values []string) datatypes.JSON {
	var b strings.Builder
	b.WriteString("[")
	for i, v := range values {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("%q", v))
	}
	b.WriteString("]")
	return datatypes.JSON([]byte(b.String()))
}

// leaveSessionsOnDate คืน session ในวิชาที่อยู่ในวันนั้นและนักศึกษามีสิทธิ์
func leaveSessionsOnDate(tx *gorm.DB, courseID string, studentID uint, day time.Time) ([]models.AttendanceSession, error) {
	start := leaveDateOf(day)
	end := start.AddDate(0, 0, 1)
	var sessions []models.AttendanceSession
	if err := tx.Where("course_id = ? AND start_time >= ? AND start_time < ?", courseID, start, end).Order("start_time ASC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	out := make([]models.AttendanceSession, 0, len(sessions))
	for _, s := range sessions {
		sectionIDs, err := attendanceSessionSectionIDsWithDB(tx, &s)
		if err != nil {
			return nil, err
		}
		ok, err := attendanceStudentEligibleWithDB(tx, s.CourseID, sectionIDs, studentID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// อ่านคำขอ
// -----------------------------------------------------------------------------

type LeaveRequestItemView struct {
	models.AttendanceLeaveRequestItem
	SessionTitle    string     `json:"session_title,omitempty"`
	SessionType     string     `json:"session_type,omitempty"`
	SessionStart    *time.Time `json:"session_start,omitempty"`
	SessionEnd      *time.Time `json:"session_end,omitempty"`
	RecordStatus    string     `json:"record_status,omitempty"`
	RecordSource    string     `json:"record_source,omitempty"`
	LeaveDateString string     `json:"leave_date_string"`
}

type LeaveRequestView struct {
	models.AttendanceLeaveRequest
	Student      *LeaveStudentBasic      `json:"student,omitempty"`
	Reviewer     *AttendanceCreatorBasic `json:"reviewer,omitempty"`
	Items        []LeaveRequestItemView  `json:"items"`
	EvidenceList []string                `json:"evidence_list"`
	CourseName   string                  `json:"course_name,omitempty"`
	CourseCode   string                  `json:"course_code,omitempty"`
}

type LeaveStudentBasic struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	SectionNo string `json:"section_no,omitempty"`
}

func decodeEvidence(raw datatypes.JSON) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func loadLeaveRequestItems(requestIDs []uint) (map[uint][]LeaveRequestItemView, error) {
	result := map[uint][]LeaveRequestItemView{}
	if len(requestIDs) == 0 {
		return result, nil
	}
	type row struct {
		models.AttendanceLeaveRequestItem
		SessionTitle *string
		SessionType  *string
		SessionStart *time.Time
		SessionEnd   *time.Time
		RecordStatus *string
		RecordSource *string
	}
	var rows []row
	err := config.DB.Raw(`
		SELECT i.*, s.title AS session_title, s.session_type, s.start_time AS session_start, s.end_time AS session_end,
		       r.status AS record_status, r.status_source AS record_source
		FROM attendance_leave_request_items i
		LEFT JOIN attendance_sessions s ON s.id = i.attendance_session_id
		LEFT JOIN attendance_leave_requests q ON q.id = i.leave_request_id
		LEFT JOIN attendance_records r ON r.attendance_session_id = i.attendance_session_id AND r.student_id = q.student_id
		WHERE i.leave_request_id IN ?
		ORDER BY i.leave_date ASC, s.start_time ASC, i.id ASC
	`, requestIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		v := LeaveRequestItemView{AttendanceLeaveRequestItem: r.AttendanceLeaveRequestItem, LeaveDateString: leaveDateOf(r.LeaveDate).Format("2006-01-02")}
		if r.SessionTitle != nil {
			v.SessionTitle = *r.SessionTitle
		}
		if r.SessionType != nil {
			v.SessionType = *r.SessionType
		}
		v.SessionStart = r.SessionStart
		v.SessionEnd = r.SessionEnd
		if r.RecordStatus != nil {
			v.RecordStatus = *r.RecordStatus
		}
		if r.RecordSource != nil {
			v.RecordSource = *r.RecordSource
		}
		result[r.LeaveRequestID] = append(result[r.LeaveRequestID], v)
	}
	return result, nil
}

func buildLeaveRequestViews(requests []models.AttendanceLeaveRequest, withStudent bool) ([]LeaveRequestView, error) {
	ids := make([]uint, 0, len(requests))
	studentIDs := make([]uint, 0, len(requests))
	reviewerIDs := make([]uint, 0)
	courseIDs := map[string]bool{}
	for _, r := range requests {
		ids = append(ids, r.ID)
		studentIDs = append(studentIDs, r.StudentID)
		if r.ReviewedBy != nil {
			reviewerIDs = append(reviewerIDs, *r.ReviewedBy)
		}
		courseIDs[r.CourseID] = true
	}
	items, err := loadLeaveRequestItems(ids)
	if err != nil {
		return nil, err
	}

	students := map[uint]LeaveStudentBasic{}
	if withStudent && len(studentIDs) > 0 {
		type srow struct {
			ID        uint
			StudentID string
			FullName  string
			Email     string
		}
		var srows []srow
		if err := config.DB.Raw(`SELECT id, student_id, full_name, email FROM students WHERE id IN ?`, studentIDs).Scan(&srows).Error; err != nil {
			return nil, err
		}
		for _, s := range srows {
			students[s.ID] = LeaveStudentBasic{ID: s.ID, StudentID: s.StudentID, FullName: s.FullName, Email: s.Email}
		}
	}
	reviewers := map[uint]AttendanceCreatorBasic{}
	if len(reviewerIDs) > 0 {
		var users []models.User
		if err := config.DB.Select("id, username, full_name").Where("id IN ?", reviewerIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				reviewers[u.ID] = AttendanceCreatorBasic{ID: u.ID, FullName: u.FullName}
			}
		}
	}
	courses := map[string]models.Course{}
	if len(courseIDs) > 0 {
		keys := make([]string, 0, len(courseIDs))
		for k := range courseIDs {
			keys = append(keys, k)
		}
		var rows []models.Course
		if err := config.DB.Select("id, code, name").Where("id IN ?", keys).Find(&rows).Error; err == nil {
			for _, c := range rows {
				courses[c.ID] = c
			}
		}
	}

	views := make([]LeaveRequestView, 0, len(requests))
	for _, r := range requests {
		v := LeaveRequestView{AttendanceLeaveRequest: r, Items: items[r.ID], EvidenceList: decodeEvidence(r.Evidence)}
		if v.Items == nil {
			v.Items = []LeaveRequestItemView{}
		}
		if s, ok := students[r.StudentID]; ok {
			sc := s
			v.Student = &sc
		}
		if r.ReviewedBy != nil {
			if u, ok := reviewers[*r.ReviewedBy]; ok {
				uc := u
				v.Reviewer = &uc
			}
		}
		if c, ok := courses[r.CourseID]; ok {
			v.CourseName = c.Name
			v.CourseCode = c.Code
		}
		views = append(views, v)
	}
	return views, nil
}

func GetStudentLeaveRequests(courseID string, studentID uint) ([]LeaveRequestView, error) {
	var requests []models.AttendanceLeaveRequest
	if err := config.DB.Where("course_id = ? AND student_id = ?", courseID, studentID).Order("created_at DESC").Limit(100).Find(&requests).Error; err != nil {
		return nil, err
	}
	return buildLeaveRequestViews(requests, false)
}

func GetLeaveRequestByID(id uint) (*LeaveRequestView, error) {
	var request models.AttendanceLeaveRequest
	if err := config.DB.First(&request, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLeaveRequestNotFound
		}
		return nil, err
	}
	views, err := buildLeaveRequestViews([]models.AttendanceLeaveRequest{request}, true)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, ErrLeaveRequestNotFound
	}
	return &views[0], nil
}

type LeaveRequestListFilter struct {
	CourseID  string
	Status    string
	StudentID uint
	Limit     int
	Offset    int
}

func ListCourseLeaveRequests(filter LeaveRequestListFilter) ([]LeaveRequestView, int64, error) {
	q := config.DB.Model(&models.AttendanceLeaveRequest{}).Where("course_id = ?", filter.CourseID)
	if filter.Status != "" && filter.Status != "all" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.StudentID > 0 {
		q = q.Where("student_id = ?", filter.StudentID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var requests []models.AttendanceLeaveRequest
	if err := q.Order("CASE WHEN status = 'pending' THEN 0 ELSE 1 END, created_at DESC").Limit(limit).Offset(filter.Offset).Find(&requests).Error; err != nil {
		return nil, 0, err
	}
	views, err := buildLeaveRequestViews(requests, true)
	return views, total, err
}

type LeaveRequestCounts struct {
	Pending           int64 `json:"pending"`
	Approved          int64 `json:"approved"`
	PartiallyApproved int64 `json:"partially_approved"`
	Rejected          int64 `json:"rejected"`
	Cancelled         int64 `json:"cancelled"`
	Revoked           int64 `json:"revoked"`
	Total             int64 `json:"total"`
}

func CountCourseLeaveRequests(courseID string) (LeaveRequestCounts, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	if err := config.DB.Raw(`SELECT status, COUNT(*) AS count FROM attendance_leave_requests WHERE course_id = ? GROUP BY status`, courseID).Scan(&rows).Error; err != nil {
		return LeaveRequestCounts{}, err
	}
	var c LeaveRequestCounts
	for _, r := range rows {
		c.Total += r.Count
		switch r.Status {
		case LeaveStatusPending:
			c.Pending = r.Count
		case LeaveStatusApproved:
			c.Approved = r.Count
		case LeaveStatusPartiallyApproved:
			c.PartiallyApproved = r.Count
		case LeaveStatusRejected:
			c.Rejected = r.Count
		case LeaveStatusCancelled:
			c.Cancelled = r.Count
		case LeaveStatusRevoked:
			c.Revoked = r.Count
		}
	}
	return c, nil
}

// -----------------------------------------------------------------------------
// ยกเลิก (นักศึกษา)
// -----------------------------------------------------------------------------

func CancelLeaveRequest(id uint, studentID uint) (*models.AttendanceLeaveRequest, error) {
	var request models.AttendanceLeaveRequest
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND student_id = ?", id, studentID).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLeaveRequestNotFound
			}
			return err
		}
		if request.Status != LeaveStatusPending {
			return ErrLeaveRequestNotPending
		}
		now := time.Now()
		if err := tx.Model(&models.AttendanceLeaveRequest{}).Where("id = ?", id).Updates(map[string]interface{}{"status": LeaveStatusCancelled, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AttendanceLeaveRequestItem{}).Where("leave_request_id = ? AND item_status = ?", id, LeaveItemPending).Updates(map[string]interface{}{"item_status": LeaveItemCancelled, "updated_at": now}).Error; err != nil {
			return err
		}
		request.Status = LeaveStatusCancelled
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &request, nil
}

// CancelPendingLeaveRequestsForStudent ใช้ตอนนักศึกษาถูกถอดออกจากวิชา
func CancelPendingLeaveRequestsForStudent(tx *gorm.DB, courseID string, studentID uint) error {
	if tx == nil {
		tx = config.DB
	}
	now := time.Now()
	var ids []uint
	if err := tx.Model(&models.AttendanceLeaveRequest{}).Where("course_id = ? AND student_id = ? AND status = ?", courseID, studentID, LeaveStatusPending).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Model(&models.AttendanceLeaveRequest{}).Where("id IN ?", ids).Updates(map[string]interface{}{"status": LeaveStatusCancelled, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&models.AttendanceLeaveRequestItem{}).Where("leave_request_id IN ? AND item_status = ?", ids, LeaveItemPending).Updates(map[string]interface{}{"item_status": LeaveItemCancelled, "updated_at": now}).Error
}

// -----------------------------------------------------------------------------
// ตรวจ (ผู้สอน/TA)
// -----------------------------------------------------------------------------

type LeaveItemDecision struct {
	ItemID   uint
	Approved bool
	Comment  string
}

type LeaveReviewResult struct {
	Request       models.AttendanceLeaveRequest
	ApprovedItems int
	RejectedItems int
	AppliedItems  int
	AwaitingItems int
	Superseded    int
}

// ReviewLeaveRequest ตัดสินคำขอ ถ้า decisions ว่าง = ตัดสินทุก item ตาม approveAll
// อนุมัติแล้วจะลงสถานะ leave ให้ record ของ session ที่ผูกไว้ทันที (ใน transaction เดียว)
func ReviewLeaveRequest(id uint, reviewerID uint, approveAll bool, decisions []LeaveItemDecision, comment string) (*LeaveReviewResult, error) {
	var result LeaveReviewResult
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var request models.AttendanceLeaveRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLeaveRequestNotFound
			}
			return err
		}
		if request.Status != LeaveStatusPending {
			return ErrLeaveRequestNotReviewable
		}
		var items []models.AttendanceLeaveRequestItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("leave_request_id = ?", id).Order("id ASC").Find(&items).Error; err != nil {
			return err
		}
		decisionMap := map[uint]LeaveItemDecision{}
		for _, d := range decisions {
			decisionMap[d.ItemID] = d
		}

		now := time.Now()
		for i := range items {
			item := &items[i]
			if item.ItemStatus != LeaveItemPending {
				continue
			}
			approved := approveAll
			itemComment := ""
			if d, ok := decisionMap[item.ID]; ok {
				approved = d.Approved
				itemComment = strings.TrimSpace(d.Comment)
			} else if len(decisions) > 0 {
				// ส่ง decisions มาบางส่วน item ที่ไม่ได้ระบุถือว่าปฏิเสธ
				approved = false
			}
			if !approved {
				result.RejectedItems++
				if err := tx.Model(item).Updates(map[string]interface{}{"item_status": LeaveItemRejected, "review_comment": itemComment, "updated_at": now}).Error; err != nil {
					return err
				}
				continue
			}
			result.ApprovedItems++
			if item.AttendanceSessionID == nil {
				result.AwaitingItems++
				if err := tx.Model(item).Updates(map[string]interface{}{"item_status": LeaveItemAwaitingSession, "review_comment": itemComment, "updated_at": now}).Error; err != nil {
					return err
				}
				continue
			}
			outcome, err := applyLeaveToRecord(tx, &request, item, reviewerID, itemComment)
			if err != nil {
				return err
			}
			switch outcome {
			case LeaveItemApplied:
				result.AppliedItems++
			case LeaveItemSuperseded:
				result.Superseded++
			}
		}

		status := LeaveStatusRejected
		if result.ApprovedItems > 0 && result.RejectedItems == 0 {
			status = LeaveStatusApproved
		} else if result.ApprovedItems > 0 {
			status = LeaveStatusPartiallyApproved
		}
		if err := tx.Model(&models.AttendanceLeaveRequest{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":         status,
			"reviewed_by":    reviewerID,
			"reviewed_at":    now,
			"review_comment": strings.TrimSpace(comment),
			"updated_at":     now,
		}).Error; err != nil {
			return err
		}
		request.Status = status
		request.ReviewedBy = &reviewerID
		request.ReviewedAt = &now
		request.ReviewComment = strings.TrimSpace(comment)
		result.Request = request
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// applyLeaveToRecord ลงสถานะ leave ให้ record ของ item (session ต้องมีแล้ว)
// คืน item_status ที่ได้: applied หรือ superseded (นักศึกษาเช็กชื่อมาแล้ว)
func applyLeaveToRecord(tx *gorm.DB, request *models.AttendanceLeaveRequest, item *models.AttendanceLeaveRequestItem, actorID uint, comment string) (string, error) {
	var session models.AttendanceSession
	if err := tx.First(&session, *item.AttendanceSessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// session หายไประหว่างทาง ให้รอจับคู่ใหม่
			return LeaveItemAwaitingSession, tx.Model(item).Updates(map[string]interface{}{"item_status": LeaveItemAwaitingSession, "attendance_session_id": nil, "review_comment": comment, "updated_at": time.Now()}).Error
		}
		return "", err
	}
	record, err := ensureAttendanceRecordInTx(tx, &session, request.StudentID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	// มาเรียนแล้ว (เช็กชื่อเอง) ไม่ทับ ให้ถือว่าคำขอวันนี้ถูกแทนที่
	if (record.Status == "present" || record.Status == "late") && record.StatusSource == AttendanceSourceCheckIn {
		return LeaveItemSuperseded, tx.Model(item).Updates(map[string]interface{}{
			"item_status":       LeaveItemSuperseded,
			"applied_record_id": record.ID,
			"review_comment":    comment,
			"updated_at":        now,
		}).Error
	}
	requestID := request.ID
	note := leaveRecordNote(request.LeaveType)
	if err := tx.Model(&models.AttendanceRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"status":           "leave",
		"status_source":    AttendanceSourceLeaveRequest,
		"leave_request_id": requestID,
		"check_in_time":    nil,
		"note":             note,
		"updated_by":       actorID,
		"updated_at":       now,
	}).Error; err != nil {
		return "", err
	}
	if err := RecordAttendanceStatusHistory(tx, AttendanceStatusChange{
		RecordID:       record.ID,
		SessionID:      session.ID,
		StudentID:      request.StudentID,
		FromStatus:     record.Status,
		ToStatus:       "leave",
		Source:         AttendanceSourceLeaveRequest,
		ActorType:      AttendanceActorUser,
		ActorID:        &actorID,
		LeaveRequestID: &requestID,
		Note:           note,
	}); err != nil {
		return "", err
	}
	if err := tx.Model(item).Updates(map[string]interface{}{
		"item_status":       LeaveItemApplied,
		"previous_status":   record.Status,
		"applied_record_id": record.ID,
		"applied_at":        now,
		"review_comment":    comment,
		"updated_at":        now,
	}).Error; err != nil {
		return "", err
	}
	return LeaveItemApplied, nil
}

func leaveRecordNote(leaveType string) string {
	switch leaveType {
	case LeaveTypeSick:
		return "[ลาผ่านระบบ] ลาป่วย"
	case LeaveTypePersonal:
		return "[ลาผ่านระบบ] ลากิจ"
	case LeaveTypeOfficial:
		return "[ลาผ่านระบบ] ลาราชการ/กิจกรรมมหาวิทยาลัย"
	default:
		return "[ลาผ่านระบบ] ลาอื่น ๆ"
	}
}

// RevokeLeaveRequest ถอนการอนุมัติ คืนสถานะ record กลับเป็นก่อนหน้า (เฉพาะที่ยังเป็น leave จากคำขอนี้)
func RevokeLeaveRequest(id uint, reviewerID uint, comment string) (*models.AttendanceLeaveRequest, int, error) {
	var request models.AttendanceLeaveRequest
	restored := 0
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLeaveRequestNotFound
			}
			return err
		}
		if request.Status != LeaveStatusApproved && request.Status != LeaveStatusPartiallyApproved {
			return ErrLeaveRequestNotRevocable
		}
		var items []models.AttendanceLeaveRequestItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("leave_request_id = ?", id).Find(&items).Error; err != nil {
			return err
		}
		now := time.Now()
		for i := range items {
			item := &items[i]
			switch item.ItemStatus {
			case LeaveItemApplied:
				if item.AppliedRecordID != nil {
					var record models.AttendanceRecord
					if err := tx.First(&record, *item.AppliedRecordID).Error; err == nil && record.Status == "leave" && record.LeaveRequestID != nil && *record.LeaveRequestID == id {
						previous := item.PreviousStatus
						if previous == "" {
							previous = "absent"
						}
						if err := tx.Model(&models.AttendanceRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
							"status":           previous,
							"status_source":    AttendanceSourceManual,
							"leave_request_id": nil,
							"note":             "[ถอนการอนุมัติลา] " + strings.TrimSpace(comment),
							"updated_by":       reviewerID,
							"updated_at":       now,
						}).Error; err != nil {
							return err
						}
						reqID := id
						if err := RecordAttendanceStatusHistory(tx, AttendanceStatusChange{
							RecordID:       record.ID,
							SessionID:      record.AttendanceSessionID,
							StudentID:      record.StudentID,
							FromStatus:     "leave",
							ToStatus:       previous,
							Source:         AttendanceSourceManual,
							ActorType:      AttendanceActorUser,
							ActorID:        &reviewerID,
							LeaveRequestID: &reqID,
							Note:           "ถอนการอนุมัติคำขอลา",
						}); err != nil {
							return err
						}
						restored++
					}
				}
				if err := tx.Model(item).Updates(map[string]interface{}{"item_status": LeaveItemRevoked, "updated_at": now}).Error; err != nil {
					return err
				}
			case LeaveItemAwaitingSession, LeaveItemApproved:
				if err := tx.Model(item).Updates(map[string]interface{}{"item_status": LeaveItemRevoked, "updated_at": now}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&models.AttendanceLeaveRequest{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":         LeaveStatusRevoked,
			"reviewed_by":    reviewerID,
			"reviewed_at":    now,
			"review_comment": strings.TrimSpace(comment),
			"updated_at":     now,
		}).Error; err != nil {
			return err
		}
		request.Status = LeaveStatusRevoked
		request.ReviewComment = strings.TrimSpace(comment)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &request, restored, nil
}

// -----------------------------------------------------------------------------
// hook กับวงจรชีวิตของ session
// -----------------------------------------------------------------------------

// supersedeLeaveRequestItemForRecord ถูกเรียกตอนนักศึกษาเช็กชื่อทับ record ที่เป็น leave จากคำขอ
func supersedeLeaveRequestItemForRecord(tx *gorm.DB, recordID uint) error {
	return tx.Model(&models.AttendanceLeaveRequestItem{}).
		Where("applied_record_id = ? AND item_status = ?", recordID, LeaveItemApplied).
		Updates(map[string]interface{}{"item_status": LeaveItemSuperseded, "updated_at": time.Now()}).Error
}

// BindLeaveItemsToSession จับคู่ item ที่ยังไม่มี session (เลือกวันไว้) กับ session ที่เพิ่งสร้าง
// หรือ item แบบ "เลือกวัน" ที่ผูกกับคาบอื่นในวันเดียวกัน (มีหลายคาบในวันเดียว) แล้วลง leave ถ้าอนุมัติแล้ว
// เรียกหลังสร้าง session และหลังเปลี่ยนเวลา/section ของ session
func BindLeaveItemsToSession(sessionID uint) (int, error) {
	bound := 0
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.First(&session, sessionID).Error; err != nil {
			return err
		}
		day := leaveDateOf(session.StartTime)
		dayEnd := day.AddDate(0, 0, 1)
		sectionIDs, err := attendanceSessionSectionIDsWithDB(tx, &session)
		if err != nil {
			return err
		}

		type candidate struct {
			models.AttendanceLeaveRequestItem
			ReqStudentID uint
			ReqStatus    string
			ReqLeaveType string
		}
		var candidates []candidate
		if err := tx.Raw(`
			SELECT i.*, q.student_id AS req_student_id, q.status AS req_status, q.leave_type AS req_leave_type
			FROM attendance_leave_request_items i
			JOIN attendance_leave_requests q ON q.id = i.leave_request_id
			WHERE q.course_id = ?
			  AND i.leave_date >= ? AND i.leave_date < ?
			  AND i.item_status IN ('pending','awaiting_session','applied')
			  AND (i.attendance_session_id IS NULL OR (i.by_date = true AND i.attendance_session_id <> ?))
			  AND NOT EXISTS (SELECT 1 FROM attendance_leave_request_items x WHERE x.leave_request_id = i.leave_request_id AND x.attendance_session_id = ?)
			ORDER BY i.id ASC
		`, session.CourseID, day, dayEnd, sessionID, sessionID).Scan(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		now := time.Now()
		handledRequests := map[uint]bool{}
		for _, cnd := range candidates {
			if handledRequests[cnd.LeaveRequestID] {
				continue
			}
			eligible, err := attendanceStudentEligibleWithDB(tx, session.CourseID, sectionIDs, cnd.ReqStudentID)
			if err != nil {
				return err
			}
			if !eligible {
				continue
			}
			handledRequests[cnd.LeaveRequestID] = true
			request := models.AttendanceLeaveRequest{ID: cnd.LeaveRequestID, StudentID: cnd.ReqStudentID, Status: cnd.ReqStatus, LeaveType: cnd.ReqLeaveType}
			sid := sessionID

			var target *models.AttendanceLeaveRequestItem
			if cnd.AttendanceSessionID == nil {
				// item ว่างอยู่ ผูกกับ session นี้ตรง ๆ
				item := cnd.AttendanceLeaveRequestItem
				if err := tx.Model(&item).Updates(map[string]interface{}{"attendance_session_id": sid, "updated_at": now}).Error; err != nil {
					return err
				}
				item.AttendanceSessionID = &sid
				target = &item
			} else {
				// วันเดียวกันมีหลายคาบ สร้าง item ใหม่ให้คาบนี้
				status := LeaveItemPending
				if cnd.ItemStatus != LeaveItemPending {
					status = LeaveItemAwaitingSession
				}
				item := models.AttendanceLeaveRequestItem{
					LeaveRequestID:      cnd.LeaveRequestID,
					LeaveDate:           cnd.LeaveDate,
					AttendanceSessionID: &sid,
					ByDate:              true,
					ItemStatus:          status,
					ReviewComment:       cnd.ReviewComment,
					CreatedAt:           now,
					UpdatedAt:           now,
				}
				if err := tx.Create(&item).Error; err != nil {
					return err
				}
				target = &item
			}
			bound++
			if target.ItemStatus == LeaveItemAwaitingSession || target.ItemStatus == LeaveItemApplied {
				if request.Status == LeaveStatusApproved || request.Status == LeaveStatusPartiallyApproved {
					actor := uint(0)
					if _, err := applyLeaveToRecord(tx, &request, target, actor, target.ReviewComment); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	return bound, err
}

// DetachLeaveItemsFromSession ใช้ตอนลบ session: ปลด item ออก เก็บวันไว้รอจับคู่ใหม่
func DetachLeaveItemsFromSession(tx *gorm.DB, sessionID uint) error {
	if tx == nil {
		tx = config.DB
	}
	now := time.Now()
	if err := tx.Model(&models.AttendanceLeaveRequestItem{}).
		Where("attendance_session_id = ? AND item_status IN ('applied','approved')", sessionID).
		Updates(map[string]interface{}{"item_status": LeaveItemAwaitingSession, "attendance_session_id": nil, "applied_record_id": nil, "applied_at": nil, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&models.AttendanceLeaveRequestItem{}).
		Where("attendance_session_id = ? AND item_status IN ('pending','awaiting_session','superseded')", sessionID).
		Updates(map[string]interface{}{"attendance_session_id": nil, "applied_record_id": nil, "updated_at": now}).Error
}

// ReapplyLeaveForSession ลง leave ซ้ำให้ item ที่อนุมัติแล้วของ session นี้ ซึ่ง record ยังไม่ใช่ leave
// ใช้หลัง backfill records (เปลี่ยน section) หรือหลังปรับเวลา
func ReapplyLeaveForSession(sessionID uint) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		type row struct {
			models.AttendanceLeaveRequestItem
			ReqStudentID uint
			ReqStatus    string
			ReqLeaveType string
		}
		var rows []row
		if err := tx.Raw(`
			SELECT i.*, q.student_id AS req_student_id, q.status AS req_status, q.leave_type AS req_leave_type
			FROM attendance_leave_request_items i
			JOIN attendance_leave_requests q ON q.id = i.leave_request_id
			WHERE i.attendance_session_id = ? AND i.item_status IN ('applied','awaiting_session')
			  AND q.status IN ('approved','partially_approved')
		`, sessionID).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			var record models.AttendanceRecord
			err := tx.Where("attendance_session_id = ? AND student_id = ?", sessionID, r.ReqStudentID).First(&record).Error
			if err == nil && record.Status == "leave" && record.LeaveRequestID != nil && *record.LeaveRequestID == r.LeaveRequestID {
				continue
			}
			if err == nil && record.StatusSource == AttendanceSourceCheckIn && record.Status != "absent" {
				continue
			}
			request := models.AttendanceLeaveRequest{ID: r.LeaveRequestID, StudentID: r.ReqStudentID, Status: r.ReqStatus, LeaveType: r.ReqLeaveType}
			item := r.AttendanceLeaveRequestItem
			if _, err := applyLeaveToRecord(tx, &request, &item, 0, item.ReviewComment); err != nil {
				return err
			}
		}
		return nil
	})
}

// -----------------------------------------------------------------------------
// ผู้รับแจ้งเตือน
// -----------------------------------------------------------------------------

// GetLeaveRequestReviewerUserIDs คืน user ที่มีสิทธิ์ตรวจคำขอลาของวิชา
// (ผู้สอนทุกคน + TA ที่เปิด review_leave_requests)
func GetLeaveRequestReviewerUserIDs(courseID string) ([]uint, error) {
	ids := make([]uint, 0)
	var instructors []models.CourseInstructor
	if err := config.DB.Where("course_id = ?", courseID).Find(&instructors).Error; err != nil {
		return nil, err
	}
	for _, ins := range instructors {
		perms := ResolveCourseMemberPermissions("instructor", ins.Permissions, ins.IsPrimary)
		if perms.ReviewLeaveRequests {
			ids = append(ids, ins.UserID)
		}
	}
	var tas []models.CourseTA
	if err := config.DB.Where("course_id = ?", courseID).Find(&tas).Error; err != nil {
		return nil, err
	}
	for _, ta := range tas {
		perms := ResolveCourseMemberPermissions("ta", ta.Permissions, false)
		if perms.ReviewLeaveRequests {
			ids = append(ids, ta.UserID)
		}
	}
	var course models.Course
	if err := config.DB.Select("instructor_id").First(&course, "id = ?", courseID).Error; err == nil && course.InstructorID != nil {
		ids = append(ids, *course.InstructorID)
	}
	return uniqueUints(ids), nil
}

func uniqueUints(in []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(in))
	for _, v := range in {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// PendingLeaveRequestsOlderThan คืนคำขอค้างที่ส่งมาก่อนเวลา before จัดกลุ่มตามวิชา (ใช้ส่งเมลเตือน)
func PendingLeaveRequestsOlderThan(before time.Time) (map[string][]models.AttendanceLeaveRequest, error) {
	var rows []models.AttendanceLeaveRequest
	if err := config.DB.Where("status = ? AND created_at < ?", LeaveStatusPending, before).Order("course_id, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	grouped := map[string][]models.AttendanceLeaveRequest{}
	for _, r := range rows {
		grouped[r.CourseID] = append(grouped[r.CourseID], r)
	}
	return grouped, nil
}

func GetCourseIDByLeaveRequestID(id uint) (string, error) {
	var courseID string
	err := config.DB.Model(&models.AttendanceLeaveRequest{}).Select("course_id").Where("id = ?", id).Take(&courseID).Error
	return courseID, err
}

// leaveRowLockClause ล็อกแถวเฉพาะ Postgres (sqlite ที่ใช้ในเทสต์ไม่รองรับ FOR UPDATE)
func leaveRowLockClause(tx *gorm.DB) string {
	if tx != nil && tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		return " FOR UPDATE OF i"
	}
	return ""
}
