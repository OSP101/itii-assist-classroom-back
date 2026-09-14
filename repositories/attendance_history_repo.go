package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"

	"gorm.io/gorm"
)

// ที่มาของสถานะเช็กชื่อ (attendance_records.status_source และ history.source)
const (
	AttendanceSourceSystem       = "system"        // สร้าง absent ล่วงหน้า หรือระบบปรับให้เอง
	AttendanceSourceCheckIn      = "checkin"       // นักศึกษาเช็กชื่อเอง
	AttendanceSourceManual       = "manual"        // ผู้สอน/TA แก้เอง
	AttendanceSourceLeaveRequest = "leave_request" // อนุมัติคำขอลาผ่านระบบ
)

const (
	AttendanceActorSystem  = "system"
	AttendanceActorUser    = "user"
	AttendanceActorStudent = "student"
)

// AttendanceStatusChange คือคำอธิบายการเปลี่ยนสถานะ 1 ครั้ง ทุกเส้นทางที่เขียน
// attendance_records.status ต้องเขียนประวัติผ่าน RecordAttendanceStatusHistory
// เพื่อให้ตามย้อนหลังได้ครบว่าสถานะมาจากไหน
type AttendanceStatusChange struct {
	RecordID       uint
	SessionID      uint
	StudentID      uint
	FromStatus     string
	ToStatus       string
	Source         string
	ActorType      string
	ActorID        *uint
	LeaveRequestID *uint
	Note           string
}

// RecordAttendanceStatusHistory เขียนประวัติ 1 แถวใน transaction ที่ให้มา
// ถ้าสถานะไม่เปลี่ยนจะไม่เขียน (ยกเว้นมาจากคำขอลา เพื่อให้เห็นว่าอนุมัติเมื่อไร)
func RecordAttendanceStatusHistory(tx *gorm.DB, change AttendanceStatusChange) error {
	if tx == nil {
		tx = config.DB
	}
	if change.FromStatus == change.ToStatus && change.Source != AttendanceSourceLeaveRequest {
		return nil
	}
	if change.Source == "" {
		change.Source = AttendanceSourceSystem
	}
	if change.ActorType == "" {
		change.ActorType = AttendanceActorSystem
	}
	row := models.AttendanceRecordHistory{
		AttendanceRecordID:  change.RecordID,
		AttendanceSessionID: change.SessionID,
		StudentID:           change.StudentID,
		FromStatus:          change.FromStatus,
		ToStatus:            change.ToStatus,
		Source:              change.Source,
		ActorType:           change.ActorType,
		ActorID:             change.ActorID,
		LeaveRequestID:      change.LeaveRequestID,
		Note:                change.Note,
		CreatedAt:           time.Now(),
	}
	return tx.Create(&row).Error
}

// GetAttendanceRecordHistory คืนประวัติของ record เรียงใหม่สุดก่อน
func GetAttendanceRecordHistory(recordID uint, limit int) ([]models.AttendanceRecordHistory, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []models.AttendanceRecordHistory
	err := config.DB.Where("attendance_record_id = ?", recordID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// GetAttendanceStudentHistoryInCourse คืนประวัติสถานะของนักศึกษาคนหนึ่งทุก session ในวิชา
func GetAttendanceStudentHistoryInCourse(courseID string, studentID uint, limit int) ([]models.AttendanceRecordHistory, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []models.AttendanceRecordHistory
	err := config.DB.
		Where("student_id = ? AND attendance_session_id IN (SELECT id FROM attendance_sessions WHERE course_id = ?)", studentID, courseID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
