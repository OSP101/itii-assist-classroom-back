package repositories

import (
	"itii-assist/config"
	"itii-assist/models"

	"gorm.io/gorm"
)

// ─── ExamSession CRUD ─────────────────────────────────────────────────────────

func GetExamSessionsByCourse(courseID string) ([]models.ExamSession, error) {
	var sessions []models.ExamSession
	err := config.DB.
		Preload("ExamSetting").
		Preload("Rooms", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, id ASC")
		}).
		Preload("Rooms.Classroom").
		Where("course_id = ?", courseID).
		Order("exam_date ASC").
		Find(&sessions).Error
	return sessions, err
}

func GetExamSessionByID(id uint) (*models.ExamSession, error) {
	var session models.ExamSession
	err := config.DB.
		Preload("ExamSetting").
		Preload("Rooms", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, id ASC")
		}).
		Preload("Rooms.Classroom").
		Preload("Seats").
		First(&session, id).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func CreateExamSession(session *models.ExamSession) error {
	return config.DB.Create(session).Error
}

func UpdateExamSession(id uint, updates map[string]interface{}) error {
	return config.DB.Model(&models.ExamSession{}).Where("id = ?", id).Updates(updates).Error
}

func DeleteExamSession(id uint) error {
	return config.DB.Delete(&models.ExamSession{}, id).Error
}

func ClearExamSessionRooms(sessionID uint) error {
	return config.DB.Where("exam_session_id = ?", sessionID).Delete(&models.ExamSessionRoom{}).Error
}

func ReplaceExamSessionRooms(sessionID uint, classroomIDs []string) error {
	uniqueIDs := make([]string, 0, len(classroomIDs))
	seen := make(map[string]struct{}, len(classroomIDs))
	for _, classroomID := range classroomIDs {
		if classroomID == "" {
			continue
		}
		if _, exists := seen[classroomID]; exists {
			continue
		}
		seen[classroomID] = struct{}{}
		uniqueIDs = append(uniqueIDs, classroomID)
	}

	return config.DB.Transaction(func(tx *gorm.DB) error {
		if len(uniqueIDs) == 0 {
			if err := tx.Where("exam_session_id = ?", sessionID).Delete(&models.ExamSeat{}).Error; err != nil {
				return err
			}
			return tx.Where("exam_session_id = ?", sessionID).Delete(&models.ExamSessionRoom{}).Error
		}

		invalidDeskSubquery := tx.
			Table("desks").
			Select("id").
			Where("classroom_id NOT IN ?", uniqueIDs)
		if err := tx.
			Where("exam_session_id = ? AND desk_id IN (?)", sessionID, invalidDeskSubquery).
			Delete(&models.ExamSeat{}).Error; err != nil {
			return err
		}

		if err := tx.
			Where("exam_session_id = ? AND classroom_id NOT IN ?", sessionID, uniqueIDs).
			Delete(&models.ExamSessionRoom{}).Error; err != nil {
			return err
		}

		var existing []models.ExamSessionRoom
		if err := tx.Where("exam_session_id = ?", sessionID).Find(&existing).Error; err != nil {
			return err
		}

		existingByClassroom := make(map[string]models.ExamSessionRoom, len(existing))
		for _, room := range existing {
			existingByClassroom[room.ClassroomID] = room
		}

		for index, classroomID := range uniqueIDs {
			if room, exists := existingByClassroom[classroomID]; exists {
				if err := tx.Model(&models.ExamSessionRoom{}).
					Where("id = ?", room.ID).
					Updates(map[string]interface{}{"sort_order": index + 1}).Error; err != nil {
					return err
				}
				continue
			}

			if err := tx.Create(&models.ExamSessionRoom{
				ExamSessionID: sessionID,
				ClassroomID:   classroomID,
				SortOrder:     index + 1,
			}).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func SyncExamSessionRoomsFromSeats(sessionID uint) error {
	var classroomIDs []string
	err := config.DB.
		Table("exam_seats es").
		Select("DISTINCT d.classroom_id").
		Joins("JOIN desks d ON d.id = es.desk_id").
		Joins("JOIN classrooms c ON c.id = d.classroom_id").
		Where("es.exam_session_id = ?", sessionID).
		Order("c.name ASC").
		Scan(&classroomIDs).Error
	if err != nil {
		return err
	}
	return ReplaceExamSessionRooms(sessionID, classroomIDs)
}

// ─── ExamSeat queries ─────────────────────────────────────────────────────────

type ExamSeatWithDetails struct {
	models.ExamSeat
	ClassroomID   string `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	DeskNumber    int    `json:"desk_number"`
	SeatLabel     string `json:"seat_label"`
}

func GetExamSeatsBySession(sessionID uint) ([]ExamSeatWithDetails, error) {
	type row struct {
		models.ExamSeat
		ClassroomID   string `gorm:"column:classroom_id" json:"classroom_id"`
		ClassroomName string `gorm:"column:classroom_name" json:"classroom_name"`
		DeskNumber    int    `gorm:"column:desk_number" json:"desk_number"`
		StudentCode   string `gorm:"column:student_code"`
		StudentName   string `gorm:"column:student_name"`
	}

	var rows []row
	err := config.DB.
		Table("exam_seats es").
		Select(`es.*, d.classroom_id, c.name AS classroom_name, d.number AS desk_number, s.student_id AS student_code, s.full_name AS student_name`).
		Joins("JOIN desks d ON d.id = es.desk_id").
		Joins("JOIN classrooms c ON c.id = d.classroom_id").
		Joins("JOIN students s ON s.id = es.student_id").
		Where("es.exam_session_id = ?", sessionID).
		Order("CASE WHEN es.seat_number > 0 THEN es.seat_number ELSE d.number END ASC, c.name ASC, d.number ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]ExamSeatWithDetails, len(rows))
	for i, r := range rows {
		out[i] = ExamSeatWithDetails{
			ExamSeat:      r.ExamSeat,
			ClassroomID:   r.ClassroomID,
			ClassroomName: r.ClassroomName,
			DeskNumber:    r.DeskNumber,
			SeatLabel:     buildExamSeatLabel(r.ClassroomName, r.SeatNumber, r.DeskNumber),
		}
		out[i].Student = models.Student{ID: r.StudentRefID, StudentID: r.StudentCode, FullName: r.StudentName}
	}
	return out, nil
}

func UpsertExamSeat(seat *models.ExamSeat) error {
	return config.DB.
		Where("exam_session_id = ? AND student_id = ?", seat.ExamSessionID, seat.StudentRefID).
		Assign(models.ExamSeat{DeskID: seat.DeskID, SeatNumber: seat.SeatNumber}).
		FirstOrCreate(seat).Error
}

func BulkCreateExamSeats(seats []models.ExamSeat) error {
	if len(seats) == 0 {
		return nil
	}
	return config.DB.CreateInBatches(seats, 100).Error
}

func DeleteExamSeat(seatID uint) error {
	return config.DB.Delete(&models.ExamSeat{}, seatID).Error
}

func ClearExamSeats(sessionID uint) error {
	return config.DB.Where("exam_session_id = ?", sessionID).Delete(&models.ExamSeat{}).Error
}

func ReplaceExamSeats(sessionID uint, seats []models.ExamSeat) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("exam_session_id = ?", sessionID).Delete(&models.ExamSeat{}).Error; err != nil {
			return err
		}
		if len(seats) == 0 {
			return nil
		}
		return tx.CreateInBatches(seats, 100).Error
	})
}

// ─── Student-facing: my exam seats ───────────────────────────────────────────

type MyExamSeat struct {
	SessionID     uint   `json:"session_id"`
	ExamType      string `json:"exam_type"`
	Component     string `json:"component"`
	ExamDate      string `json:"exam_date"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time"`
	ClassroomName string `json:"classroom_name"`
	DeskNumber    int    `json:"desk_number"`
	SeatNumber    int    `json:"seat_number"`
	SeatLabel     string `json:"seat_label"` // e.g. "CP9226-28"
}

func GetMyExamSeats(courseID string, studentID uint) ([]MyExamSeat, error) {
	type row struct {
		SessionID     uint   `gorm:"column:session_id"`
		ExamType      string `gorm:"column:exam_type"`
		Component     string `gorm:"column:component"`
		ExamDate      string `gorm:"column:exam_date"`
		StartTime     string `gorm:"column:start_time"`
		EndTime       string `gorm:"column:end_time"`
		ClassroomName string `gorm:"column:classroom_name"`
		DeskNumber    int    `gorm:"column:desk_number"`
		SeatNumber    int    `gorm:"column:seat_number"`
	}

	var rows []row
	err := config.DB.
		Table("exam_seats es").
		Select(`es.exam_session_id AS session_id,
			es2.exam_setting_id,
			et.exam_type,
			et.component,
			to_char(es2.exam_date AT TIME ZONE 'Asia/Bangkok', 'DD Mon YYYY') AS exam_date,
			es2.start_time,
			es2.end_time,
			c.name AS classroom_name,
			d.number AS desk_number,
			es.seat_number AS seat_number`).
		Joins("JOIN exam_sessions es2 ON es2.id = es.exam_session_id").
		Joins("JOIN exam_settings et ON et.id = es2.exam_setting_id").
		Joins("JOIN desks d ON d.id = es.desk_id").
		Joins("JOIN classrooms c ON c.id = d.classroom_id").
		Where("es2.course_id = ? AND es.student_id = ?", courseID, studentID).
		Order("es2.exam_date ASC, CASE WHEN es.seat_number > 0 THEN es.seat_number ELSE d.number END ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]MyExamSeat, len(rows))
	for i, r := range rows {
		out[i] = MyExamSeat{
			SessionID:     r.SessionID,
			ExamType:      r.ExamType,
			Component:     r.Component,
			ExamDate:      r.ExamDate,
			StartTime:     r.StartTime,
			EndTime:       r.EndTime,
			ClassroomName: r.ClassroomName,
			DeskNumber:    r.DeskNumber,
			SeatNumber:    effectiveSeatNumber(r.SeatNumber, r.DeskNumber),
			SeatLabel:     buildExamSeatLabel(r.ClassroomName, r.SeatNumber, r.DeskNumber),
		}
	}
	return out, nil
}

// ─── Seating export: seats grouped by classroom ───────────────────────────────

type ExportSeatRow struct {
	RowNum        int    `json:"row_num"`
	StudentID     string `json:"student_id"`
	FullName      string `json:"full_name"`
	Major         string `json:"major"`
	SeatLabel     string `json:"seat_label"`
	ClassroomName string `json:"classroom_name"`
	DeskNumber    int    `json:"desk_number"`
	SeatNumber    int    `json:"seat_number"`
}

func GetExamSeatingExport(sessionID uint) ([]ExportSeatRow, error) {
	type row struct {
		StudentID     string `gorm:"column:student_id"`
		FullName      string `gorm:"column:full_name"`
		Extra         []byte `gorm:"column:extra"`
		ClassroomName string `gorm:"column:classroom_name"`
		DeskNumber    int    `gorm:"column:desk_number"`
		SeatNumber    int    `gorm:"column:seat_number"`
	}

	var rows []row
	err := config.DB.
		Table("exam_seats es").
		Select(`s.student_id, s.full_name, s.extra, c.name AS classroom_name, d.number AS desk_number, es.seat_number AS seat_number`).
		Joins("JOIN students s ON s.id = es.student_id").
		Joins("JOIN desks d ON d.id = es.desk_id").
		Joins("JOIN classrooms c ON c.id = d.classroom_id").
		Where("es.exam_session_id = ?", sessionID).
		Order("CASE WHEN es.seat_number > 0 THEN es.seat_number ELSE d.number END ASC, c.name ASC, d.number ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]ExportSeatRow, len(rows))
	for i, r := range rows {
		out[i] = ExportSeatRow{
			RowNum:        i + 1,
			StudentID:     r.StudentID,
			FullName:      r.FullName,
			Major:         "",
			SeatLabel:     buildExamSeatLabel(r.ClassroomName, r.SeatNumber, r.DeskNumber),
			ClassroomName: r.ClassroomName,
			DeskNumber:    r.DeskNumber,
			SeatNumber:    effectiveSeatNumber(r.SeatNumber, r.DeskNumber),
		}
	}
	return out, nil
}

func effectiveSeatNumber(seatNumber int, deskNumber int) int {
	if seatNumber > 0 {
		return seatNumber
	}
	return deskNumber
}

func buildExamSeatLabel(classroomName string, seatNumber int, deskNumber int) string {
	return classroomName + "-" + itoa(effectiveSeatNumber(seatNumber, deskNumber))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// ─── Classroom lookup helper ──────────────────────────────────────────────────

func FindClassroomByName(name string) (*models.Classroom, error) {
	var c models.Classroom
	err := config.DB.Where("name = ? AND is_deleted = false", name).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func FindDeskByClassroomAndNumber(classroomID string, number int) (*models.Desk, error) {
	var d models.Desk
	err := config.DB.Where("classroom_id = ? AND number = ? AND is_enabled = true", classroomID, number).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FindDesksByClassroom returns all enabled desks sorted by Y then X (for auto-assign)
func FindEnabledDesksByClassroomSorted(classroomID string) ([]models.Desk, error) {
	var desks []models.Desk
	err := config.DB.
		Where("classroom_id = ? AND is_enabled = true AND type != 'teacher'", classroomID).
		Order("y ASC, x ASC").
		Find(&desks).Error
	return desks, err
}

// ─── Enrollment helpers ───────────────────────────────────────────────────────

// GetEnrolledStudents returns all students enrolled in a course ordered by student_id.
func GetEnrolledStudents(courseID string) ([]models.Student, error) {
	var students []models.Student
	err := config.DB.
		Joins("JOIN course_section_students css ON css.student_id = students.id").
		Joins("JOIN course_sections cs ON cs.id = css.course_section_id").
		Where("cs.course_id = ?", courseID).
		Order("students.student_id ASC").
		Find(&students).Error
	return students, err
}
