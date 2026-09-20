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

// เหตุผล/หลักฐานมาตรฐานสำหรับเทสต์ทั่วไปที่ไม่ได้ตั้งใจทดสอบกฎเรื่องเหตุผล/หลักฐานเอง
// (ต้องผ่าน ValidateLeaveReason และมีหลักฐานอย่างน้อย 1 ไฟล์ เพราะบังคับทุกกรณีแล้ว)
const testLeaveReason = "automated test leave reason"

var testLeaveEvidence = []string{"evidence-test.jpg"}

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
		&models.CourseSectionStudentRemoval{}, &models.AppConfig{},
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
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOfficial, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
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

	// หลักฐานบังคับแนบทุกกรณี (ไม่ว่านโยบายวิชาจะเป็นอย่างไร)
	_, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeSick, Reason: testLeaveReason, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestEvidence) {
		t.Fatalf("expected evidence required, got %v", err)
	}
	// แม้วิชาตั้งนโยบายเป็น "ไม่บังคับ" (none) และประเภทลาไม่ใช่ป่วย/กิจ ก็ยังต้องบังคับแนบหลักฐาน
	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("leave_evidence_policy", LeaveEvidencePolicyNone)
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOfficial, Reason: testLeaveReason, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestEvidence) {
		t.Fatalf("expected evidence required even under 'none' policy, got %v", err)
	}
	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("leave_evidence_policy", LeaveEvidencePolicySickPersonal)
	// เหตุผลสั้นเกินไป (ต่ำกว่า 10 ตัวอักษร)
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: "สั้นไป", Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestReasonTooShort) {
		t.Fatalf("expected reason too short, got %v", err)
	}
	// เหตุผลยาวเกินไป (เกิน 100 ตัวอักษร)
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: strings.Repeat("ก", 101), Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestReasonTooLong) {
		t.Fatalf("expected reason too long, got %v", err)
	}
	// นักศึกษาที่ไม่อยู่ในวิชา
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.other.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestNotStudent) {
		t.Fatalf("expected not student, got %v", err)
	}
	// นอกกรอบเวลา (ย้อนหลังเกิน 7 วัน)
	old := seedLeaveSession(t, f, time.Now().Add(-10*24*time.Hour))
	oid := old.ID
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &oid}}})
	if !errors.Is(err, ErrLeaveRequestOutOfWindow) {
		t.Fatalf("expected out of window, got %v", err)
	}
	// เช็กชื่อแล้วขอลาไม่ได้
	config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", sid, f.student.ID).Updates(map[string]interface{}{"status": "present", "status_source": AttendanceSourceCheckIn})
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if !errors.Is(err, ErrLeaveRequestAlreadyPresent) {
		t.Fatalf("expected already present, got %v", err)
	}
	config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", sid, f.student.ID).Updates(map[string]interface{}{"status": "absent", "status_source": AttendanceSourceSystem})

	// ซ้ำ: มี pending อยู่แล้ว
	if _, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}}); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
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
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{Date: dateKey}}})
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
	req, _ := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
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

// ผู้สอนแก้เป็น present ด้วยมือ "ก่อน" คำขอลาถูกอนุมัติ ต้องไม่ถูกทับตอนอนุมัติ (แก้ finding: present overrides leave)
func TestLeaveRequest_ManualPresentBeforeApprovalNotOverwritten(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// TA เจอนักศึกษามาเรียนจริง แก้ด้วยมือเป็น present ก่อนที่ใครจะอนุมัติคำขอลา
	if _, err := UpdateAttendanceRecordReturningPrevious(session.ID, f.student.ID, "present", "มาเรียนจริง", 9); err != nil {
		t.Fatalf("manual update: %v", err)
	}
	result, err := ReviewLeaveRequest(req.ID, 42, true, nil, "")
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if result.Superseded != 1 || result.AppliedItems != 0 {
		t.Fatalf("expected superseded (present kept), got %+v", result)
	}
	record := recordFor(t, session.ID, f.student.ID)
	if record.Status != "present" || record.StatusSource != AttendanceSourceManual {
		t.Fatalf("manual present must survive approval: %+v", record)
	}
}

// ผู้สอนแก้เป็น present ด้วยมือ "หลัง" คำขอลาถูกอนุมัติแล้ว item ต้อง superseded กัน
// ReapplyLeaveForSession (เรียกตอนแก้ session) ทับกลับเป็น leave อีก
func TestLeaveRequest_ManualEditAfterApprovalSupersedesItemAgainstReapply(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID
	req, _ := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if _, err := ReviewLeaveRequest(req.ID, 1, true, nil, ""); err != nil {
		t.Fatalf("review: %v", err)
	}
	// ผู้สอนแก้เองทับเป็น present หลังอนุมัติแล้ว
	if _, err := UpdateAttendanceRecordReturningPrevious(session.ID, f.student.ID, "present", "มาสาย", 9); err != nil {
		t.Fatalf("manual update: %v", err)
	}
	var item models.AttendanceLeaveRequestItem
	if err := config.DB.Where("leave_request_id = ?", req.ID).First(&item).Error; err != nil {
		t.Fatalf("load item: %v", err)
	}
	if item.ItemStatus != LeaveItemSuperseded {
		t.Fatalf("expected item superseded after manual edit, got %s", item.ItemStatus)
	}
	// จำลองผู้สอนแก้เวลา session แล้วระบบเรียก ReapplyLeaveForSession ตามปกติ
	if err := ReapplyLeaveForSession(session.ID); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	record := recordFor(t, session.ID, f.student.ID)
	if record.Status != "present" || record.StatusSource != AttendanceSourceManual {
		t.Fatalf("reapply must not clobber the manual correction: %+v", record)
	}
}

// auto-bind (actorID=0) ต้องบันทึกใน history เป็น actor ระบบ ไม่ใช่ user #0
func TestBindLeaveItemsToSession_SystemActorAttribution(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)

	day := time.Now().In(leaveLocation).AddDate(0, 0, 3)
	dateKey := day.Format("2006-01-02")
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{Date: dateKey}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ReviewLeaveRequest(req.ID, 7, true, nil, ""); err != nil {
		t.Fatalf("review: %v", err)
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, leaveLocation)
	session := seedLeaveSession(t, f, start)
	if _, err := BindLeaveItemsToSession(session.ID); err != nil {
		t.Fatalf("bind: %v", err)
	}
	record := recordFor(t, session.ID, f.student.ID)
	var history models.AttendanceRecordHistory
	if err := config.DB.Where("attendance_record_id = ? AND to_status = ?", record.ID, "leave").First(&history).Error; err != nil {
		t.Fatalf("load history: %v", err)
	}
	if history.ActorType != AttendanceActorSystem || history.ActorID != nil {
		t.Fatalf("auto-bind must be attributed to system, got actor_type=%s actor_id=%v", history.ActorType, history.ActorID)
	}
}

// วิชาปิดแล้ว ต้องพิจารณา/ถอนอนุมัติคำขอลาไม่ได้ (แก้ finding: batch-review ไม่เช็ก closed-course)
func TestReviewAndRevokeLeaveRequest_BlockedWhenCourseInactive(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID
	req, _ := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})

	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("is_active", false)
	if _, err := ReviewLeaveRequest(req.ID, 42, true, nil, ""); !errors.Is(err, ErrLeaveRequestCourseInactive) {
		t.Fatalf("expected course inactive on review, got %v", err)
	}

	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("is_active", true)
	if _, err := ReviewLeaveRequest(req.ID, 42, true, nil, ""); err != nil {
		t.Fatalf("review should succeed while course active: %v", err)
	}

	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("is_active", false)
	if _, _, err := RevokeLeaveRequest(req.ID, 42, "ปิดวิชาแล้ว"); !errors.Is(err, ErrLeaveRequestCourseInactive) {
		t.Fatalf("expected course inactive on revoke, got %v", err)
	}
}

// นักศึกษาถูกถอดออกจาก section สุดท้ายในวิชา คำขอลาที่ค้างถูกยกเลิกอัตโนมัติ
// ถ้า restore กลับภายในเวลาที่กำหนด คำขอลาต้องกลับมา pending ด้วย (แก้ finding: คำขอลาไม่คืนสถานะ)
func TestRestoreStudentToSection_RestoresCancelledLeaveRequests(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	session := seedLeaveSession(t, f, time.Now().Add(24*time.Hour))
	sid := session.ID
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &sid}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	found, _, err := ArchiveAndRemoveStudentFromSection(f.section.ID, f.student.ID, 1)
	if err != nil || !found {
		t.Fatalf("remove: found=%v err=%v", found, err)
	}
	var cancelled models.AttendanceLeaveRequest
	if err := config.DB.First(&cancelled, req.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cancelled.Status != LeaveStatusCancelled || cancelled.CancelledByRemovalID == nil {
		t.Fatalf("expected leave request auto-cancelled with removal link, got %+v", cancelled)
	}

	restored, err := RestoreStudentToSection(f.section.ID, f.student.ID)
	if err != nil || !restored {
		t.Fatalf("restore: restored=%v err=%v", restored, err)
	}
	var afterRestore models.AttendanceLeaveRequest
	if err := config.DB.First(&afterRestore, req.ID).Error; err != nil {
		t.Fatalf("reload after restore: %v", err)
	}
	if afterRestore.Status != LeaveStatusPending || afterRestore.CancelledByRemovalID != nil {
		t.Fatalf("expected leave request restored to pending, got %+v", afterRestore)
	}
	var item models.AttendanceLeaveRequestItem
	if err := config.DB.Where("leave_request_id = ?", req.ID).First(&item).Error; err != nil {
		t.Fatalf("load item: %v", err)
	}
	if item.ItemStatus != LeaveItemPending {
		t.Fatalf("expected item restored to pending, got %s", item.ItemStatus)
	}
}

// ยื่นหลาย session ตรง ๆ ในคำขอเดียว ต้องตรวจซ้ำ/มาเรียนแล้วให้ถูกต้องทุก item
// แม้ query จะถูก batch มาล่วงหน้าแล้ว (ไม่ใช่ query ทีละ session)
func TestLeaveRequest_MultiSessionBatchedLookup(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	base := time.Now().Add(24 * time.Hour)
	s1 := seedLeaveSession(t, f, base)
	s2 := seedLeaveSession(t, f, base.Add(48*time.Hour))
	s3 := seedLeaveSession(t, f, base.Add(72*time.Hour))

	// s2 นักศึกษาเช็กชื่อมาเรียนแล้ว ต้องขอลาไม่ได้เฉพาะ item นั้น
	config.DB.Model(&models.AttendanceRecord{}).Where("attendance_session_id = ? AND student_id = ?", s2.ID, f.student.ID).
		Updates(map[string]interface{}{"status": "present", "status_source": AttendanceSourceCheckIn})

	id1, id2, id3 := s1.ID, s2.ID, s3.ID
	_, err := CreateLeaveRequest(CreateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id1}, {SessionID: &id2}, {SessionID: &id3}},
	})
	if !errors.Is(err, ErrLeaveRequestAlreadyPresent) {
		t.Fatalf("expected already present for s2, got %v", err)
	}

	// ตัด s2 ออก ที่เหลือต้องผ่านและได้ 2 items จาก batched lookup
	req, err := CreateLeaveRequest(CreateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id1}, {SessionID: &id3}},
	})
	if err != nil {
		t.Fatalf("create after removing present session: %v", err)
	}
	var items []models.AttendanceLeaveRequestItem
	config.DB.Where("leave_request_id = ?", req.ID).Find(&items)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// ส่ง session เดิมซ้ำในคำขอเดียวกัน ต้องโดน duplicate ตั้งแต่ในคำขอเดียวกันเอง
	db := config.DB
	other2 := models.Student{StudentID: "650003", FullName: "Student Three", Email: "s3@kkumail.com", IsActive: true}
	db.Create(&other2)
	db.Create(&models.CourseSectionStudent{CourseSectionID: f.section.ID, StudentID: other2.ID, EnrolledAt: time.Now()})
	otherSession := seedLeaveSession(t, f, base.Add(96*time.Hour))
	oid := otherSession.ID
	_, err = CreateLeaveRequest(CreateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: other2.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &oid}, {SessionID: &oid}},
	})
	if !errors.Is(err, ErrLeaveRequestDuplicate) {
		t.Fatalf("expected duplicate within same request, got %v", err)
	}
}

// แก้ไขคำขอที่ยัง pending ได้: เปลี่ยนวันลา/เหตุผลได้ ไม่ชนกับ item เดิมของตัวเอง
func TestUpdateLeaveRequest_ReplacesItemsWhilePending(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	base := time.Now().Add(24 * time.Hour)
	s1 := seedLeaveSession(t, f, base)
	s2 := seedLeaveSession(t, f, base.Add(48*time.Hour))
	id1, id2 := s1.ID, s2.ID

	req, err := CreateLeaveRequest(CreateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id1}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// แก้ไข: เปลี่ยนเป็นลา s2 แทน (s1 เดิมต้องหลุดออก ไม่ชนกันเอง)
	updated, err := UpdateLeaveRequest(req.ID, UpdateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypePersonal, Reason: "เหตุผลใหม่",
		Evidence: []string{"a.jpg"},
		Items:    []LeaveRequestItemInput{{SessionID: &id2}},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.LeaveType != LeaveTypePersonal || updated.Reason != "เหตุผลใหม่" || updated.Status != LeaveStatusPending {
		t.Fatalf("unexpected updated request: %+v", updated)
	}
	var items []models.AttendanceLeaveRequestItem
	config.DB.Where("leave_request_id = ?", req.ID).Find(&items)
	if len(items) != 1 || items[0].AttendanceSessionID == nil || *items[0].AttendanceSessionID != id2 {
		t.Fatalf("expected items replaced with s2 only, got %+v", items)
	}

	// ยื่นแก้ไขทับ session เดิม s1 อีกครั้งต้องทำได้ปกติ (ไม่ถูกนับว่าซ้ำกับตัวเอง)
	updated, err = UpdateLeaveRequest(req.ID, UpdateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypePersonal, Reason: "กลับไปวันเดิม",
		Evidence: []string{"a.jpg"},
		Items:    []LeaveRequestItemInput{{SessionID: &id1}},
	})
	if err != nil {
		t.Fatalf("update back to s1: %v", err)
	}
	config.DB.Where("leave_request_id = ?", req.ID).Find(&items)
	if len(items) != 1 || items[0].AttendanceSessionID == nil || *items[0].AttendanceSessionID != id1 {
		t.Fatalf("expected items replaced with s1 only, got %+v", items)
	}

	// คำขออื่นของนักศึกษาคนเดิมที่ชนกับ s1 จริง ๆ (ไม่ใช่ตัวเอง) ต้องยัง error duplicate ปกติ
	s3 := seedLeaveSession(t, f, base.Add(72*time.Hour))
	id3 := s3.ID
	other, err := CreateLeaveRequest(CreateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id3}},
	})
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	_, err = UpdateLeaveRequest(other.ID, UpdateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id1}},
	})
	if !errors.Is(err, ErrLeaveRequestDuplicate) {
		t.Fatalf("expected duplicate against the OTHER request's item, got %v", err)
	}

	// แก้ไขคำขอที่ไม่ pending แล้วต้องทำไม่ได้
	if _, err := ReviewLeaveRequest(req.ID, 1, true, nil, ""); err != nil {
		t.Fatalf("review: %v", err)
	}
	_, err = UpdateLeaveRequest(req.ID, UpdateLeaveRequestInput{
		CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence,
		Items: []LeaveRequestItemInput{{SessionID: &id2}},
	})
	if !errors.Is(err, ErrLeaveRequestNotPending) {
		t.Fatalf("expected not pending, got %v", err)
	}
}

// คำขอที่ค้าง "รอพิจารณา" นานเกินนโยบายวิชา ต้องถูกปิดอัตโนมัติ ส่วนที่ยังไม่ครบกำหนดต้องไม่โดน
// และถ้าวิชาปิดฟีเจอร์นี้ไว้ (0 วัน) ต้องไม่มีอะไรถูกปิดเลย
func TestExpireStalePendingLeaveRequests(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	f := seedLeaveFixture(t)
	// วิชานี้ตั้งไว้ 14 วัน
	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("leave_auto_expire_days", 14)

	base := time.Now().Add(24 * time.Hour)
	s1 := seedLeaveSession(t, f, base)
	s2 := seedLeaveSession(t, f, base.Add(48*time.Hour))
	id1, id2 := s1.ID, s2.ID

	stale, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &id1}}})
	if err != nil {
		t.Fatalf("create stale: %v", err)
	}
	config.DB.Model(&models.AttendanceLeaveRequest{}).Where("id = ?", stale.ID).Update("created_at", time.Now().Add(-20*24*time.Hour))

	fresh, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &id2}}})
	if err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	grouped, err := ExpireStalePendingLeaveRequests()
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	reqs := grouped[f.course.ID]
	if len(reqs) != 1 || reqs[0].ID != stale.ID {
		t.Fatalf("expected exactly the stale request to expire, got %+v", grouped)
	}

	var reloadedStale models.AttendanceLeaveRequest
	config.DB.First(&reloadedStale, stale.ID)
	if reloadedStale.Status != LeaveStatusExpired {
		t.Fatalf("expected expired status, got %s", reloadedStale.Status)
	}
	var item models.AttendanceLeaveRequestItem
	config.DB.Where("leave_request_id = ?", stale.ID).First(&item)
	if item.ItemStatus != LeaveItemCancelled {
		t.Fatalf("expected item cancelled, got %s", item.ItemStatus)
	}

	var reloadedFresh models.AttendanceLeaveRequest
	config.DB.First(&reloadedFresh, fresh.ID)
	if reloadedFresh.Status != LeaveStatusPending {
		t.Fatalf("fresh request must remain pending, got %s", reloadedFresh.Status)
	}

	// รันซ้ำต้องไม่เจอคำขอเดิมอีก (ไม่ใช่ pending แล้ว)
	grouped2, err := ExpireStalePendingLeaveRequests()
	if err != nil {
		t.Fatalf("expire again: %v", err)
	}
	if len(grouped2) != 0 {
		t.Fatalf("expected nothing left to expire, got %+v", grouped2)
	}

	// วิชาที่ปิดฟีเจอร์นี้ (0 วัน) ต้องไม่แตะคำขอที่ค้างนานแค่ไหนก็ตาม
	config.DB.Model(&models.Course{}).Where("id = ?", f.course.ID).Update("leave_auto_expire_days", 0)
	s3 := seedLeaveSession(t, f, base.Add(72*time.Hour))
	id3 := s3.ID
	other, err := CreateLeaveRequest(CreateLeaveRequestInput{CourseID: f.course.ID, StudentID: f.student.ID, LeaveType: LeaveTypeOther, Reason: testLeaveReason, Evidence: testLeaveEvidence, Items: []LeaveRequestItemInput{{SessionID: &id3}}})
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	config.DB.Model(&models.AttendanceLeaveRequest{}).Where("id = ?", other.ID).Update("created_at", time.Now().Add(-100*24*time.Hour))
	grouped3, err := ExpireStalePendingLeaveRequests()
	if err != nil {
		t.Fatalf("expire disabled: %v", err)
	}
	if len(grouped3) != 0 {
		t.Fatalf("expected nothing to expire when feature disabled, got %+v", grouped3)
	}
}
