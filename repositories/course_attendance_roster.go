package repositories

import (
	"time"

	"itii-assist/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Sync inside the enrollment transaction so an added student is immediately
// visible in existing attendance rosters, without overwriting any check-in.
func syncSectionAttendanceRoster(tx *gorm.DB, sectionID uint, studentIDs []uint) error {
	if len(studentIDs) == 0 {
		return nil
	}
	var section models.CourseSection
	if err := tx.Select("id", "course_id").First(&section, sectionID).Error; err != nil {
		return err
	}
	var sessions []models.AttendanceSession
	if err := tx.Select("id").Where("course_id = ?", section.CourseID).
		Where(`EXISTS (
			SELECT 1 FROM attendance_session_sections ass
			WHERE ass.attendance_session_id = attendance_sessions.id AND ass.course_section_id = ?
		) OR (NOT EXISTS (
			SELECT 1 FROM attendance_session_sections ass
			WHERE ass.attendance_session_id = attendance_sessions.id
		) AND (course_section_id IS NULL OR course_section_id = 0 OR course_section_id = ?))`, sectionID, sectionID).
		Find(&sessions).Error; err != nil {
		return err
	}
	now := time.Now()
	for _, session := range sessions {
		records := make([]models.AttendanceRecord, 0, len(studentIDs))
		for _, studentID := range studentIDs {
			records = append(records, models.AttendanceRecord{
				AttendanceSessionID: session.ID, StudentID: studentID,
				Status: "absent", CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "attendance_session_id"}, {Name: "student_id"}},
			DoNothing: true,
		}).CreateInBatches(&records, 100).Error; err != nil {
			return err
		}
	}
	return nil
}
