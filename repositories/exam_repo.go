package repositories

import (
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"math"
	"time"

	"gorm.io/gorm"
)

// ============================================================
// Exam Settings
// ============================================================

var defaultExamSettings = []struct {
	ExamType  string
	Component string
}{
	{"midterm", "lecture"},
	{"midterm", "lab"},
	{"final", "lecture"},
	{"final", "lab"},
}

func GetOrCreateExamSettings(courseID string) ([]models.ExamSetting, error) {
	settings := make([]models.ExamSetting, 0, len(defaultExamSettings))

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "exam_settings:"+courseID).Error; err != nil {
			return err
		}

		for _, d := range defaultExamSettings {
			var setting models.ExamSetting
			err := tx.Where("course_id = ? AND exam_type = ? AND component = ?", courseID, d.ExamType, d.Component).
				Order("id ASC").
				First(&setting).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				setting = models.ExamSetting{
					CourseID:  courseID,
					ExamType:  d.ExamType,
					Component: d.Component,
					MaxScore:  0,
					IsVisible: false,
					IsActive:  false,
					CreatedAt: time.Now(),
				}
				if err := tx.Select("CourseID", "ExamType", "Component", "MaxScore", "IsVisible", "IsActive", "CreatedAt").Create(&setting).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			settings = append(settings, setting)
		}

		return nil
	})
	return settings, err
}

func UpdateExamSetting(settingID uint, courseID string, maxScore *float64, isVisible *bool, isActive *bool) (*models.ExamSetting, error) {
	_, updated, err := UpdateExamSettingReturningPrevious(settingID, courseID, maxScore, isVisible, isActive)
	return updated, err
}

// UpdateExamSettingReturningPrevious updates a setting and returns both the row
// as it was and the row as it now is, so callers can log what actually changed.
func UpdateExamSettingReturningPrevious(settingID uint, courseID string, maxScore *float64, isVisible *bool, isActive *bool) (*models.ExamSetting, *models.ExamSetting, error) {
	var setting models.ExamSetting
	if err := config.DB.Where("id = ? AND course_id = ?", settingID, courseID).First(&setting).Error; err != nil {
		return nil, nil, err
	}
	previous := setting

	if maxScore != nil {
		setting.MaxScore = *maxScore
	}
	if isVisible != nil {
		setting.IsVisible = *isVisible
	}
	if isActive != nil {
		setting.IsActive = *isActive
	}
	config.DB.Save(&setting)
	return &previous, &setting, nil
}

// ============================================================
// Exam Scores
// ============================================================

type ExamScoreRow struct {
	StudentID  uint     `json:"student_id"`
	StudNumber string   `json:"student_id_number"`
	FullName   string   `json:"full_name"`
	SettingID  uint     `json:"exam_setting_id"`
	ExamType   string   `json:"exam_type"`
	Component  string   `json:"component"`
	Score      *float64 `json:"score"`
}

type ExamStudentView struct {
	ID          uint   `json:"id" gorm:"column:id"`
	StudentCode string `json:"student_id" gorm:"column:student_code"`
	FullName    string `json:"full_name" gorm:"column:full_name"`
	SectionNo   string `json:"section_no" gorm:"column:section_no"`
}

type ExamScoreViewEntry struct {
	ID            uint       `json:"id" gorm:"column:id"`
	ExamSettingID uint       `json:"exam_setting_id,omitempty" gorm:"column:exam_setting_id"`
	StudentID     uint       `json:"student_id" gorm:"column:student_id"`
	Score         *float64   `json:"score" gorm:"column:score"`
	GraderID      *uint      `json:"grader_id,omitempty" gorm:"column:grader_id"`
	GraderName    *string    `json:"grader_name" gorm:"column:grader_name"`
	GradedAt      *time.Time `json:"graded_at,omitempty" gorm:"column:graded_at"`
}

type ExamSettingWithScores struct {
	ID        uint                 `json:"id"`
	ExamType  string               `json:"exam_type"`
	Component string               `json:"component"`
	MaxScore  float64              `json:"max_score"`
	IsVisible bool                 `json:"is_visible"`
	IsActive  bool                 `json:"is_active"`
	Scores    []ExamScoreViewEntry `json:"scores"`
}

type ExamScoresView struct {
	Students []ExamStudentView       `json:"students"`
	Settings []ExamSettingWithScores `json:"settings"`
}

type ExamScoreStatsSummary struct {
	Count   int64   `json:"count"`
	Average float64 `json:"average"`
	Max     float64 `json:"max"`
	Min     float64 `json:"min"`
}

type ExamScoreStatsItem struct {
	ID        uint                  `json:"id"`
	ExamType  string                `json:"exam_type"`
	Component string                `json:"component"`
	MaxScore  float64               `json:"max_score"`
	Stats     ExamScoreStatsSummary `json:"stats"`
}

func GetExamScores(courseID string) ([]ExamScoreRow, error) {
	var rows []ExamScoreRow
	err := config.DB.Raw(`
		SELECT
			s.id as student_id,
			s.student_id as stud_number,
			s.full_name,
			es.id as setting_id,
			es.exam_type,
			es.component,
			esc.score
		FROM students s
		JOIN course_section_students css ON css.student_id = s.id
		JOIN course_sections cs ON cs.id = css.course_section_id
		JOIN exam_settings es ON es.course_id = cs.course_id
		LEFT JOIN exam_scores esc ON esc.student_id = s.id AND esc.exam_setting_id = es.id
		WHERE cs.course_id = ?
		ORDER BY s.student_id, es.exam_type, es.component
	`, courseID).Scan(&rows).Error
	return rows, err
}

func GetExamScoresView(courseID string) (*ExamScoresView, error) {
	settings, err := GetOrCreateExamSettings(courseID)
	if err != nil {
		return nil, err
	}

	var students []ExamStudentView
	if err := config.DB.Raw(`
		SELECT
			s.id,
			s.student_id AS student_code,
			s.full_name,
			MIN(cs.section_no) AS section_no
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name
		ORDER BY s.student_id ASC
	`, courseID).Scan(&students).Error; err != nil {
		return nil, err
	}

	var scoreRows []ExamScoreViewEntry
	if err := config.DB.Raw(`
		SELECT
			esc.id,
			esc.exam_setting_id,
			esc.student_id,
			esc.score,
			esc.graded_by AS grader_id,
			u.full_name AS grader_name,
			esc.graded_at
		FROM exam_scores esc
		JOIN exam_settings es ON es.id = esc.exam_setting_id
		LEFT JOIN users u ON u.id = esc.graded_by
		WHERE es.course_id = ?
		ORDER BY esc.id ASC
	`, courseID).Scan(&scoreRows).Error; err != nil {
		return nil, err
	}

	scoreMap := make(map[uint][]ExamScoreViewEntry)
	for _, row := range scoreRows {
		scoreMap[row.ExamSettingID] = append(scoreMap[row.ExamSettingID], row)
	}

	settingsView := make([]ExamSettingWithScores, len(settings))
	for index, setting := range settings {
		scores := scoreMap[setting.ID]
		if scores == nil {
			scores = []ExamScoreViewEntry{}
		}
		settingsView[index] = ExamSettingWithScores{
			ID:        setting.ID,
			ExamType:  setting.ExamType,
			Component: setting.Component,
			MaxScore:  setting.MaxScore,
			IsVisible: setting.IsVisible,
			IsActive:  setting.IsActive,
			Scores:    scores,
		}
	}

	if students == nil {
		students = []ExamStudentView{}
	}

	return &ExamScoresView{Students: students, Settings: settingsView}, nil
}

func GetExamSettingByCourse(courseID string, settingID uint) (*models.ExamSetting, error) {
	var setting models.ExamSetting
	err := config.DB.Where("id = ? AND course_id = ?", settingID, courseID).First(&setting).Error
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func SaveExamScore(settingID uint, studentID uint, score *float64, comment string, gradedBy uint) (*ExamScoreViewEntry, error) {
	entry, _, err := SaveExamScoreReturningPrevious(settingID, studentID, score, comment, gradedBy)
	return entry, err
}

// SaveExamScoreReturningPrevious upserts an exam score and hands back the row as
// it was before the write, or nil when this is the first entry for the pair.
func SaveExamScoreReturningPrevious(settingID uint, studentID uint, score *float64, comment string, gradedBy uint) (*ExamScoreViewEntry, *models.ExamScore, error) {
	var examScore models.ExamScore
	var previous *models.ExamScore
	now := time.Now()

	err := config.DB.Where("exam_setting_id = ? AND student_id = ?", settingID, studentID).First(&examScore).Error
	if err == nil {
		snapshot := examScore
		previous = &snapshot
		examScore.Score = score
		examScore.Comment = comment
		examScore.GradedBy = &gradedBy
		examScore.GradedAt = &now
		if err := config.DB.Save(&examScore).Error; err != nil {
			return nil, nil, err
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		examScore = models.ExamScore{
			ExamSettingID: settingID,
			StudentID:     studentID,
			Score:         score,
			Comment:       comment,
			GradedBy:      &gradedBy,
			GradedAt:      &now,
			CreatedAt:     now,
		}
		if err := config.DB.Create(&examScore).Error; err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}

	var graderName *string
	if examScore.GradedBy != nil {
		var grader models.User
		if err := config.DB.Select("id, full_name").First(&grader, *examScore.GradedBy).Error; err == nil {
			name := grader.FullName
			graderName = &name
		}
	}

	return &ExamScoreViewEntry{
		ID:            examScore.ID,
		ExamSettingID: examScore.ExamSettingID,
		StudentID:     examScore.StudentID,
		Score:         examScore.Score,
		GraderID:      examScore.GradedBy,
		GraderName:    graderName,
		GradedAt:      examScore.GradedAt,
	}, previous, nil
}

func GetCourseStudentIDMap(courseID string) (map[string]uint, error) {
	type studentLookupRow struct {
		DBID        uint   `gorm:"column:db_id"`
		StudentCode string `gorm:"column:student_code"`
	}

	var rows []studentLookupRow
	if err := config.DB.Raw(`
		SELECT
			s.id AS db_id,
			LOWER(s.student_id) AS student_code
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id
	`, courseID).Scan(&rows).Error; err != nil {
		return nil, err
	}

	lookup := make(map[string]uint, len(rows))
	for _, row := range rows {
		lookup[row.StudentCode] = row.DBID
	}
	return lookup, nil
}

func GetExamScoreStats(courseID string) ([]ExamScoreStatsItem, error) {
	type statsRow struct {
		ID        uint    `gorm:"column:id"`
		ExamType  string  `gorm:"column:exam_type"`
		Component string  `gorm:"column:component"`
		MaxScore  float64 `gorm:"column:max_score"`
		Count     int64   `gorm:"column:count"`
		Average   float64 `gorm:"column:average"`
		Max       float64 `gorm:"column:max"`
		Min       float64 `gorm:"column:min"`
	}

	var rows []statsRow
	err := config.DB.Raw(`
		SELECT
			es.id,
			es.exam_type,
			es.component,
			es.max_score,
			COUNT(esc.id) AS count,
			COALESCE(AVG(esc.score), 0) AS average,
			COALESCE(MAX(esc.score), 0) AS max,
			COALESCE(MIN(esc.score), 0) AS min
		FROM exam_settings es
		LEFT JOIN exam_scores esc ON esc.exam_setting_id = es.id AND esc.score IS NOT NULL
		WHERE es.course_id = ? AND es.is_active = true
		GROUP BY es.id, es.exam_type, es.component, es.max_score
		ORDER BY es.exam_type ASC, es.component ASC
	`, courseID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	stats := make([]ExamScoreStatsItem, len(rows))
	for index, row := range rows {
		stats[index] = ExamScoreStatsItem{
			ID:        row.ID,
			ExamType:  row.ExamType,
			Component: row.Component,
			MaxScore:  row.MaxScore,
			Stats: ExamScoreStatsSummary{
				Count:   row.Count,
				Average: math.Round(row.Average*100) / 100,
				Max:     row.Max,
				Min:     row.Min,
			},
		}
	}
	return stats, nil
}

func DeleteExamScoreByCourse(scoreID uint, courseID string) error {
	var examScore models.ExamScore
	err := config.DB.Table("exam_scores AS esc").
		Select("esc.*").
		Joins("JOIN exam_settings es ON es.id = esc.exam_setting_id").
		Where("esc.id = ? AND es.course_id = ?", scoreID, courseID).
		Take(&examScore).Error
	if err != nil {
		return err
	}

	return config.DB.Delete(&models.ExamScore{}, scoreID).Error
}

func UpsertExamScore(settingID uint, studentID uint, score float64, comment string, gradedBy uint) error {
	db := config.DB
	var existing models.ExamScore
	now := time.Now()
	if err := db.Where("exam_setting_id = ? AND student_id = ?", settingID, studentID).First(&existing).Error; err == nil {
		existing.Score = &score
		existing.Comment = comment
		existing.GradedBy = &gradedBy
		existing.GradedAt = &now
		return db.Save(&existing).Error
	}
	return db.Create(&models.ExamScore{
		ExamSettingID: settingID,
		StudentID:     studentID,
		Score:         &score,
		Comment:       comment,
		GradedBy:      &gradedBy,
		GradedAt:      &now,
		CreatedAt:     now,
	}).Error
}

func BulkUpsertExamScores(entries []struct {
	SettingID uint
	StudentID uint
	Score     float64
	Comment   string
	GradedBy  uint
}) error {
	for _, e := range entries {
		UpsertExamScore(e.SettingID, e.StudentID, e.Score, e.Comment, e.GradedBy)
	}
	return nil
}
