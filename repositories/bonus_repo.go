package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"
)

// ============================================================
// Bonus Score
// ============================================================

func GiveBonusScore(courseID string, studentID uint, score float64, reason string, givenBy uint) (*models.BonusScore, error) {
	now := time.Now()
	b := models.BonusScore{
		CourseID:  courseID,
		StudentID: studentID,
		Score:     score,
		Reason:    reason,
		GivenBy:   givenBy,
		GivenAt:   now,
		CreatedAt: now,
	}
	if err := config.DB.Create(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

type BonusScoreWithNames struct {
	models.BonusScore
	StudentName   string `json:"student_name"`
	StudentNumber string `json:"student_number"`
	GiverName     string `json:"giver_name"`
}

func GetBonusScoresByCourse(courseID string) ([]BonusScoreWithNames, error) {
	var rows []BonusScoreWithNames
	err := config.DB.Raw(`
		SELECT bs.*,
			s.full_name as student_name, s.student_id as student_number,
			u.full_name as giver_name
		FROM bonus_scores bs
		JOIN students s ON s.id = bs.student_id
		JOIN users u ON u.id = bs.given_by
		WHERE bs.course_id = ?
		ORDER BY bs.given_at DESC
	`, courseID).Scan(&rows).Error
	return rows, err
}

type BonusScoreSummaryItem struct {
	StudentID     uint    `json:"student_id"`
	StudentNumber string  `json:"student_number"`
	StudentName   string  `json:"student_name"`
	TotalScore    float64 `json:"total_score"`
	Count         int     `json:"count"`
}

func GetBonusScoreSummary(courseID string) ([]BonusScoreSummaryItem, error) {
	var rows []BonusScoreSummaryItem
	err := config.DB.Raw(`
		SELECT s.id as student_id, s.student_id as student_number, s.full_name as student_name,
			SUM(bs.score) as total_score, COUNT(*) as count
		FROM bonus_scores bs
		JOIN students s ON s.id = bs.student_id
		WHERE bs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name
		ORDER BY total_score DESC
	`, courseID).Scan(&rows).Error
	return rows, err
}

func GetStudentBonusHistory(courseID string, studentID uint) ([]BonusScoreWithNames, error) {
	var rows []BonusScoreWithNames
	err := config.DB.Raw(`
		SELECT bs.*,
			s.full_name as student_name, s.student_id as student_number,
			u.full_name as giver_name
		FROM bonus_scores bs
		JOIN students s ON s.id = bs.student_id
		JOIN users u ON u.id = bs.given_by
		WHERE bs.course_id = ? AND bs.student_id = ?
		ORDER BY bs.given_at DESC
	`, courseID, studentID).Scan(&rows).Error
	return rows, err
}

func DeleteBonusScore(id uint) error {
	return config.DB.Where("id = ?", id).Delete(&models.BonusScore{}).Error
}

type BonusEnrolledStudent struct {
	ID            uint    `json:"id" gorm:"column:id"`
	StudentNumber string  `json:"student_id" gorm:"column:student_id"`
	FullName      string  `json:"full_name" gorm:"column:full_name"`
	SectionNo     string  `json:"section_no" gorm:"column:section_no"`
	TotalBonus    float64 `json:"totalBonus" gorm:"column:total_bonus"`
}

func GetEnrolledStudentsForBonus(courseID string) ([]BonusEnrolledStudent, error) {
	var rows []BonusEnrolledStudent
	err := config.DB.Raw(`
		SELECT
			s.id,
			s.student_id,
			s.full_name,
			cs.section_no,
			COALESCE(SUM(bs.score), 0) AS total_bonus
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		LEFT JOIN bonus_scores bs ON bs.course_id = cs.course_id AND bs.student_id = s.id
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name, cs.section_no
		ORDER BY s.student_id ASC
	`, courseID).Scan(&rows).Error
	return rows, err
}

func GetBonusTotalForStudent(courseID string, studentID uint) (float64, error) {
	var total float64
	err := config.DB.Raw(`
		SELECT COALESCE(SUM(score), 0)
		FROM bonus_scores
		WHERE course_id = ? AND student_id = ?
	`, courseID, studentID).Scan(&total).Error
	return total, err
}
