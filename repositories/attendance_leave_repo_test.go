package repositories

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

var leaveTestDBSeq int

// sqliteTimeDialector แปลง type:timestamptz ของ Postgres เป็น datetime
// เพื่อให้ไดรเวอร์ sqlite parse ค่าเวลากลับเป็น time.Time ได้ในเทสต์
type sqliteTimeDialector struct{ gorm.Dialector }

func (d sqliteTimeDialector) DataTypeOf(field *schema.Field) string {
	if strings.EqualFold(string(field.DataType), "timestamptz") {
		return "datetime"
	}
	return d.Dialector.DataTypeOf(field)
}

func (d sqliteTimeDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return sqlite.Migrator{Migrator: migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   d,
		CreateIndexAfterCreateTable: true,
	}}}
}

func setupLeaveRepoTestDB(t *testing.T) func() {
	t.Helper()
	leaveTestDBSeq++
	db, err := gorm.Open(sqliteTimeDialector{sqlite.Open(fmt.Sprintf("file:leave_test_%d?mode=memory&cache=shared", leaveTestDBSeq))}, &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Course{}, &models.CourseSection{}, &models.CourseSectionStudent{}, &models.CourseMember{},
		&models.CourseInstructor{}, &models.CourseTA{}, &models.Student{}, &models.User{},
		&models.AttendanceSession{}, &models.AttendanceSessionSection{}, &models.AttendanceRecord{},
		&models.AttendanceRecordHistory{}, &models.AttendanceLeaveRequest{}, &models.AttendanceLeaveRequestItem{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	prevDB := config.DB
	config.DB = db
	return func() { config.DB = prevDB }
}

type leaveFixture struct {
	course  models.Course
	section models.CourseSection
	student models.Student
	other   models.Student
}

func seedLeaveFixture(t *testing.T) leaveFixture {
	t.Helper()
	db := config.DB
	enabled := true
	course := models.Course{ID: "course-leave-1", Code: "CP1", Name: "Leave Test", Year: 2569, Semester: 1, IsActive: true, LeaveRequestEnabled: &enabled, LeaveEvidencePolicy: LeaveEvidencePolicySickPersonal, LeaveBackdateDays: 7, LeaveAdvanceDays: 60, LeaveMaxPending: 5}
	if err := db.Create(&course).Error; err != nil {
		t.Fatalf("seed course: %v", err)
	}
	section := models.CourseSection{CourseID: course.ID, SectionNo: "1"}
	db.Create(&section)
	student := models.Student{StudentID: "650001", FullName: "Student One", Email: "s1@kkumail.com", IsActive: true}
	db.Create(&student)
	other := models.Student{StudentID: "650002", FullName: "Student Two", Email: "s2@kkumail.com", IsActive: true}
	db.Create(&other)
	db.Create(&models.CourseSectionStudent{CourseSectionID: section.ID, StudentID: student.ID, EnrolledAt: time.Now()})
	db.Create(&models.CourseMember{CourseID: course.ID, UserID: 0, Role: "student"})
	return leaveFixture{course: course, section: section, student: student, other: other}
}

func seedLeaveSession(t *testing.T, f leaveFixture, start time.Time) models.AttendanceSession {
	t.Helper()
	session := models.AttendanceSession{CourseID: f.course.ID, Title: "Lecture", StartTime: start, EndTime: start.Add(2 * time.Hour), Status: "draft"}
	if err := CreateAttendanceSession(&session, []uint{f.section.ID}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session
}

func recordFor(t *testing.T, sessionID, studentID uint) models.AttendanceRecord {
	t.Helper()
	var r models.AttendanceRecord
	if err := config.DB.Where("attendance_session_id = ? AND student_id = ?", sessionID, studentID).First(&r).Error; err != nil {
		t.Fatalf("record lookup: %v", err)
	}
	return r
}

func TestLeaveRequest_ApproveAppliesLeaveAndHistory(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))

	sid := session.ID
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOfficial, Reason: "งานมหาวิทยาลัย", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Status != LeaveStatusPending {
		t.Fatalf("expected pending, got %s", req.Status)
	}

	result, err := ReviewLeaveRequest(req.ID, 42, true, nil, "ok")
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if result.Request.Status != LeaveStatusApproved || result.AppliedItems != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	record := recordFor(t, session.ID, f.student.ID)
	if record.Status != "leave" || record.StatusSource != AttendanceSourceLeaveRequest || record.LeaveRequestID == nil || *record.LeaveRequestID != req.ID {
		t.Fatalf("record not applied: %+v", record)
	}
	var history []models.AttendanceRecordHistory
	config.DB.Where("attendance_record_id = ?", record.ID).Find(&history)
	if len(history) != 1 || history[0].FromStatus != "absent" || history[0].ToStatus != "leave" || history[0].Source != AttendanceSourceLeaveRequest {
		t.Fatalf("history missing or wrong: %+v", history)
	}

	// พิจารณาซ้ำต้องไม่ได้
	if _, err := ReviewLeaveRequest(req.ID, 42, false, nil, ""); !errors.Is(err, ErrLeaveRequestNotReviewable) {
		t.Fatalf("expected not reviewable, got %v", err)
	}

	// ถอนอนุมัติ คืนสถานะเดิม
	_, restored, err := RevokeLeaveRequest(req.ID, 42, "ตรวจสอบพบว่าไม่ได้ไป")
	if err != nil || restored != 1 {
		t.Fatalf("revoke: err=%v restored=%d", err, restored)
	}
	record = recordFor(t, session.ID, f.student.ID)
	if record.Status != "absent" || record.LeaveRequestID != nil || record.StatusSource != AttendanceSourceManual {
		t.Fatalf("record not restored: %+v", record)
	}
}

func TestLeaveRequest_ValidationRules(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID

	// ลาป่วยต้องมีหลักฐาน (นโยบาย sick_personal)
	_, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeSick, Reason: "ป่วย", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestEvidence) {
		t.Fatalf("expected evidence required, got %v", err)
	}
	// นักศึกษาที่ไม่อยู่ในวิชา
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.other.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestNotStudent) {
		t.Fatalf("expected not student, got %v", err)
	}
	// นอกกรอบเวลา (ย้อนหลังเกิน 7 วัน)
	old := seedLeaveSession(t, f, time.Now().Add(-10*24*time.Hour))
	oid := old.ID
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &oid}}})
	if !errors.Is(err, ErrLeaveRequestOutOfWindow) {
		t.Fatalf("expected out of window, got %v", err)
	}
	// เช็กชื่อแล้วขอลาไม่ได้
	config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", sid, f.student.ID).Updates(map[string]interface{}{"status": "present", "status_source": AttendanceSourceCheckIn})
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestAlreadyPresent) {
		t.Fatalf("expected already present, got %v", err)
	}
	config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", sid, f.student.ID).Updates(map[string]interface{}{"status": "absent", "status_source": AttendanceSourceSystem})

	// ซ้ำ: มี pending อยู่แล้ว
	if _, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &sid}}}); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestDuplicate) {
		t.Fatalf("expected duplicate, got %v", err)
	}
}

func TestLeaveRequest_DateOnlyBindsWhenSessionCreated(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)

	day := time.Now().In(leaveLocation).AddDate(0, 0, 3)
	dateKey := day.Format("2006-01-02")
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "ล่วงหน้า", Items: []LeaveRequestItemInput{{Date: dateKey}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := ReviewLeaveRequest(req.ID, 7, true, nil, "")
	if err != nil || result.AwaitingItems != 1 {
		t.Fatalf("expected awaiting item, err=%v result=%+v", err, result)
	}

	// สร้าง session วันนั้นทีหลัง ต้องจับคู่และลง leave ให้เอง
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, leaveLocation)
	session := seedLeaveSession(t, f, start)
	bound, err := BindLeaveItemsToSession(session.ID)
	if err != nil || bound != 1 {
		t.Fatalf("bind: err=%v bound=%d", err, bound)
	}
	record := recordFor(t, session.ID, f.student.ID)
	if record.Status != "leave" || record.StatusSource != AttendanceSourceLeaveRequest {
		t.Fatalf("record not applied after bind: %+v", record)
	}

	// คาบที่สองในวันเดียวกัน ต้องได้ item ใหม่และลง leave ด้วย
	second := seedLeaveSession(t, f, start.Add(3*time.Hour))
	bound, err = BindLeaveItemsToSession(second.ID)
	if err != nil || bound != 1 {
		t.Fatalf("bind second: err=%v bound=%d", err, bound)
	}
	if r := recordFor(t, second.ID, f.student.ID); r.Status != "leave" {
		t.Fatalf("second session not applied: %+v", r)
	}
	var items []models.AttendanceLeaveRequestItem
	config.DB.Where("leave_request_id = ?", req.ID).Find(&items)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// ลบ session แล้ว item กลับไปรอ
	if err := DeleteAttendanceSession(second.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var awaiting int64
	config.DB.Model(&models.AttendanceLeaveRequestItem{}).Where("leave_request_id = ? AND item_status = ? AND attendance_session_id IS NULL", req.ID, LeaveItemAwaitingSession).Count(&awaiting)
	if awaiting != 1 {
		t.Fatalf("expected 1 awaiting item after delete, got %d", awaiting)
	}
}

func TestLeaveRequest_ManualEditKeepsTrace(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID
	req, _ := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "x", Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if _, err := ReviewLeaveRequest(req.ID, 1, true, nil, ""); err != nil {
		t.Fatalf("review: %v", err)
	}
	// ผู้สอนแก้เองทับ
	prev, err := UpdateAttendanceRecordReturningPrevious(session.ID, f.student.ID, "present", "มาสาย", 9)
	if err != nil || prev.Status != "leave" {
		t.Fatalf("manual update: err=%v prev=%+v", err, prev)
	}
	record := recordFor(t, session.ID, f.student.ID)
	if record.StatusSource != AttendanceSourceManual || record.LeaveRequestID == nil {
		t.Fatalf("manual edit should keep leave_request_id link: %+v", record)
	}
	var history []models.AttendanceRecordHistory
	config.DB.Where("attendance_record_id = ?", record.ID).Order("id").Find(&history)
	if len(history) != 2 || history[1].Source != AttendanceSourceManual || history[1].ActorID == nil || *history[1].ActorID != 9 {
		t.Fatalf("history chain wrong: %+v", history)
	}
}
