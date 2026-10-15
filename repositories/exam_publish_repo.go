package repositories

import (
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ============================================================
// Exam score publishing (แบบ B: ร่าง → ตรวจทาน → ประกาศแล้ว)
// ============================================================

// ExamPublishActions are the course-activity actions this lifecycle writes.
// They are what the per-course publish history endpoint filters on.
var ExamPublishActions = []string{
	"publish_exam_scores",
	"schedule_exam_scores_publish",
	"cancel_exam_scores_schedule",
	"hide_exam_scores",
	"remind_exam_scores",
}

var ErrExamSettingNotActive = errors.New("exam setting is not active")
var ErrExamSettingAlreadyVisible = errors.New("exam scores are already visible")
var ErrExamSettingNotVisible = errors.New("exam scores are not visible")

func userDisplayName(userID *uint) *string {
	if userID == nil || *userID == 0 {
		return nil
	}
	var user models.User
	if err := config.DB.Select("id, full_name").First(&user, *userID).Error; err != nil {
		return nil
	}
	name := user.FullName
	return &name
}

func buildExamPublishMeta(setting *models.ExamSetting) ExamPublishMeta {
	return ExamPublishMeta{
		PublishedAt:        setting.PublishedAt,
		PublishedByName:    userDisplayName(setting.PublishedBy),
		HiddenAt:           setting.HiddenAt,
		HiddenByName:       userDisplayName(setting.HiddenBy),
		HideReason:         setting.HideReason,
		ScheduledPublishAt: setting.ScheduledPublishAt,
		ScheduledByName:    userDisplayName(setting.ScheduledBy),
		ShowAverage:        setting.ShowAverage,
		ShowComments:       setting.ShowComments,
		NotifyByEmail:      setting.NotifyByEmail,
	}
}

func examSettingViewCounts(courseID string) (map[uint]int64, error) {
	type row struct {
		ExamSettingID uint  `gorm:"column:exam_setting_id"`
		Count         int64 `gorm:"column:count"`
	}
	var rows []row
	err := config.DB.Raw(`
		SELECT v.exam_setting_id, COUNT(*) AS count
		FROM exam_score_views v
		JOIN exam_settings es ON es.id = v.exam_setting_id
		WHERE es.course_id = ?
		GROUP BY v.exam_setting_id
	`, courseID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[uint]int64, len(rows))
	for _, r := range rows {
		counts[r.ExamSettingID] = r.Count
	}
	return counts, nil
}

// ------------------------------------------------------------
// Review (หน้าตรวจทานก่อนประกาศ)
// ------------------------------------------------------------

type ExamReviewStudent struct {
	ID          uint     `json:"id"`
	StudentCode string   `json:"student_id"`
	FullName    string   `json:"full_name"`
	Score       *float64 `json:"score"`
	IsAbsent    bool     `json:"is_absent"`
	Comment     string   `json:"comment"`
}

type ExamReviewBucket struct {
	Label string  `json:"label"`
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Count int     `json:"count"`
}

// ExamSettingReview is everything the review page needs to let an instructor
// check the numbers before students see them. Averages and the attended
// count only include students with a numeric score: absent students are
// counted separately and never enter the statistics.
type ExamSettingReview struct {
	Setting        models.ExamSetting  `json:"setting"`
	Meta           ExamPublishMeta     `json:"meta"`
	TotalStudents  int                 `json:"total_students"`
	ScoredCount    int                 `json:"scored_count"`
	AbsentCount    int                 `json:"absent_count"`
	MissingCount   int                 `json:"missing_count"`
	CommentCount   int                 `json:"comment_count"`
	InvalidCount   int                 `json:"invalid_count"`
	EmailableCount int                 `json:"emailable_count"`
	Average        *float64            `json:"average"`
	Max            *float64            `json:"max"`
	Min            *float64            `json:"min"`
	BelowHalfCount int                 `json:"below_half_count"`
	Distribution   []ExamReviewBucket  `json:"distribution"`
	Missing        []ExamReviewStudent `json:"missing"`
	Absent         []ExamReviewStudent `json:"absent"`
	Invalid        []ExamReviewStudent `json:"invalid"`
	Preview        *ExamReviewStudent  `json:"preview"`
}

type examReviewRow struct {
	ID          uint     `gorm:"column:id"`
	StudentCode string   `gorm:"column:student_code"`
	FullName    string   `gorm:"column:full_name"`
	Email       string   `gorm:"column:email"`
	Score       *float64 `gorm:"column:score"`
	IsAbsent    *bool    `gorm:"column:is_absent"`
	Comment     *string  `gorm:"column:comment"`
}

func loadExamReviewRows(courseID string, settingID uint) ([]examReviewRow, error) {
	var rows []examReviewRow
	err := config.DB.Raw(`
		SELECT
			s.id,
			s.student_id AS student_code,
			s.full_name,
			s.email,
			esc.score,
			esc.is_absent,
			esc.comment
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		LEFT JOIN exam_scores esc ON esc.student_id = s.id AND esc.exam_setting_id = ?
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name, s.email, esc.score, esc.is_absent, esc.comment
		ORDER BY s.student_id ASC
	`, settingID, courseID).Scan(&rows).Error
	return rows, err
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func GetExamSettingReview(courseID string, settingID uint) (*ExamSettingReview, error) {
	setting, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, err
	}
	rows, err := loadExamReviewRows(courseID, settingID)
	if err != nil {
		return nil, err
	}

	review := &ExamSettingReview{
		Setting:      *setting,
		Meta:         buildExamPublishMeta(setting),
		Distribution: []ExamReviewBucket{},
		Missing:      []ExamReviewStudent{},
		Absent:       []ExamReviewStudent{},
		Invalid:      []ExamReviewStudent{},
	}
	viewCounts, err := examSettingViewCounts(courseID)
	if err == nil {
		review.Meta.ViewedCount = viewCounts[setting.ID]
	}

	var scores []float64
	for _, row := range rows {
		review.TotalStudents++
		student := ExamReviewStudent{ID: row.ID, StudentCode: row.StudentCode, FullName: row.FullName, Score: row.Score}
		if row.Comment != nil {
			student.Comment = *row.Comment
		}
		if row.IsAbsent != nil && *row.IsAbsent {
			student.IsAbsent = true
			review.AbsentCount++
			review.Absent = append(review.Absent, student)
			if strings.TrimSpace(row.Email) != "" {
				review.EmailableCount++
			}
			continue
		}
		if row.Score == nil {
			review.MissingCount++
			review.Missing = append(review.Missing, student)
			continue
		}
		review.ScoredCount++
		if strings.TrimSpace(row.Email) != "" {
			review.EmailableCount++
		}
		if strings.TrimSpace(student.Comment) != "" {
			review.CommentCount++
		}
		if *row.Score < 0 || *row.Score > setting.MaxScore {
			review.InvalidCount++
			review.Invalid = append(review.Invalid, student)
		}
		if setting.MaxScore > 0 && *row.Score < setting.MaxScore/2 {
			review.BelowHalfCount++
		}
		scores = append(scores, *row.Score)
		if review.Preview == nil {
			preview := student
			review.Preview = &preview
		}
	}

	if len(scores) > 0 {
		sum, max, min := 0.0, scores[0], scores[0]
		for _, v := range scores {
			sum += v
			if v > max {
				max = v
			}
			if v < min {
				min = v
			}
		}
		avg := round2(sum / float64(len(scores)))
		review.Average, review.Max, review.Min = &avg, &max, &min
	}

	if setting.MaxScore > 0 {
		const buckets = 6
		width := setting.MaxScore / buckets
		for i := 0; i < buckets; i++ {
			from := round2(float64(i) * width)
			to := round2(float64(i+1) * width)
			review.Distribution = append(review.Distribution, ExamReviewBucket{From: from, To: to})
		}
		for _, v := range scores {
			idx := int(math.Floor(v / width))
			if idx >= buckets {
				idx = buckets - 1
			}
			if idx < 0 {
				idx = 0
			}
			review.Distribution[idx].Count++
		}
	}
	return review, nil
}

// ------------------------------------------------------------
// Publish / schedule / hide
// ------------------------------------------------------------

type ExamPublishOptions struct {
	ShowAverage   bool
	ShowComments  bool
	NotifyByEmail bool
}

// PublishExamSetting flips a setting to visible and stamps who did it. The
// view counter is reset so "who has opened it" starts fresh for this
// announcement. Returns the row as it was before the change.
func PublishExamSetting(courseID string, settingID uint, actorID uint, opts ExamPublishOptions) (*models.ExamSetting, *models.ExamSetting, error) {
	var setting models.ExamSetting
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND course_id = ?", settingID, courseID).First(&setting).Error; err != nil {
			return err
		}
		if !setting.IsActive {
			return ErrExamSettingNotActive
		}
		if setting.IsVisible {
			return ErrExamSettingAlreadyVisible
		}
		now := time.Now()
		updates := map[string]interface{}{
			"is_visible":           true,
			"published_at":         now,
			"published_by":         actorID,
			"hidden_at":            nil,
			"hidden_by":            nil,
			"hide_reason":          "",
			"scheduled_publish_at": nil,
			"scheduled_by":         nil,
			"show_average":         opts.ShowAverage,
			"show_comments":        opts.ShowComments,
			"notify_by_email":      opts.NotifyByEmail,
			"updated_at":           now,
		}
		if err := tx.Model(&models.ExamSetting{}).Where("id = ?", setting.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("exam_setting_id = ?", setting.ID).Delete(&models.ExamScoreView{}).Error
	})
	if err != nil {
		return nil, nil, err
	}
	previous := setting
	updated, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, nil, err
	}
	return &previous, updated, nil
}

func ScheduleExamSettingPublish(courseID string, settingID uint, at time.Time, actorID uint, opts ExamPublishOptions) (*models.ExamSetting, error) {
	setting, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, err
	}
	if !setting.IsActive {
		return nil, ErrExamSettingNotActive
	}
	if setting.IsVisible {
		return nil, ErrExamSettingAlreadyVisible
	}
	now := time.Now()
	err = config.DB.Model(&models.ExamSetting{}).Where("id = ?", setting.ID).Updates(map[string]interface{}{
		"scheduled_publish_at": at,
		"scheduled_by":         actorID,
		"show_average":         opts.ShowAverage,
		"show_comments":        opts.ShowComments,
		"notify_by_email":      opts.NotifyByEmail,
		"updated_at":           now,
	}).Error
	if err != nil {
		return nil, err
	}
	return GetExamSettingByCourse(courseID, settingID)
}

func CancelExamSettingSchedule(courseID string, settingID uint) (*models.ExamSetting, error) {
	setting, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, err
	}
	err = config.DB.Model(&models.ExamSetting{}).Where("id = ?", setting.ID).Updates(map[string]interface{}{
		"scheduled_publish_at": nil,
		"scheduled_by":         nil,
		"updated_at":           time.Now(),
	}).Error
	if err != nil {
		return nil, err
	}
	return GetExamSettingByCourse(courseID, settingID)
}

// HideExamSetting takes a published setting back to draft. Scores and the
// view rows are kept (the hide dialog shows how many students already saw
// it); PublishExamSetting clears the views when it is announced again.
func HideExamSetting(courseID string, settingID uint, actorID uint, reason string) (*models.ExamSetting, *models.ExamSetting, error) {
	setting, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, nil, err
	}
	if !setting.IsVisible {
		return nil, nil, ErrExamSettingNotVisible
	}
	previous := *setting
	now := time.Now()
	err = config.DB.Model(&models.ExamSetting{}).Where("id = ?", setting.ID).Updates(map[string]interface{}{
		"is_visible":           false,
		"hidden_at":            now,
		"hidden_by":            actorID,
		"hide_reason":          strings.TrimSpace(reason),
		"scheduled_publish_at": nil,
		"scheduled_by":         nil,
		"updated_at":           now,
	}).Error
	if err != nil {
		return nil, nil, err
	}
	updated, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, nil, err
	}
	return &previous, updated, nil
}

// GetDueScheduledExamSettings returns settings whose scheduled publish time
// has passed and that are still hidden; the background worker publishes them.
func GetDueScheduledExamSettings(now time.Time) ([]models.ExamSetting, error) {
	var settings []models.ExamSetting
	err := config.DB.
		Where("scheduled_publish_at IS NOT NULL AND scheduled_publish_at <= ? AND is_visible = false AND is_active = true", now).
		Order("scheduled_publish_at ASC").
		Limit(50).
		Find(&settings).Error
	return settings, err
}

// ------------------------------------------------------------
// Views (ใครเปิดดูแล้ว)
// ------------------------------------------------------------

// RecordExamScoreViews marks every visible exam component of the course as
// seen by the student. Called when the student's scores tab shows exam
// results, so the count means "opened the page", not "received the email".
func RecordExamScoreViews(studentID uint, courseID string) error {
	return config.DB.Exec(`
		INSERT INTO exam_score_views (exam_setting_id, student_id, first_viewed_at, last_viewed_at, view_count)
		SELECT es.id, ?, NOW(), NOW(), 1
		FROM exam_settings es
		WHERE es.course_id = ? AND es.is_active = true AND es.is_visible = true
		ON CONFLICT (exam_setting_id, student_id)
		DO UPDATE SET last_viewed_at = EXCLUDED.last_viewed_at, view_count = exam_score_views.view_count + 1
	`, studentID, courseID).Error
}

type ExamViewerRow struct {
	StudentID          uint       `json:"student_id" gorm:"column:student_id"`
	StudentCode        string     `json:"student_code" gorm:"column:student_code"`
	FullName           string     `json:"full_name" gorm:"column:full_name"`
	Email              string     `json:"email" gorm:"column:email"`
	Score              *float64   `json:"score" gorm:"column:score"`
	IsAbsent           bool       `json:"is_absent" gorm:"column:is_absent"`
	HasResult          bool       `json:"has_result" gorm:"column:has_result"`
	FirstViewedAt      *time.Time `json:"first_viewed_at" gorm:"column:first_viewed_at"`
	LastViewedAt       *time.Time `json:"last_viewed_at" gorm:"column:last_viewed_at"`
	ViewCount          int        `json:"view_count" gorm:"column:view_count"`
	NotificationKind   string     `json:"notification_kind" gorm:"column:notification_kind"`
	NotificationSentAt *time.Time `json:"notification_sent_at" gorm:"column:notification_sent_at"`
	NotificationError  string     `json:"notification_error" gorm:"column:notification_error"`
}

type ExamViewersSummary struct {
	Setting       models.ExamSetting `json:"setting"`
	Meta          ExamPublishMeta    `json:"meta"`
	TotalStudents int                `json:"total_students"`
	ViewedCount   int                `json:"viewed_count"`
	UnviewedCount int                `json:"unviewed_count"`
	ViewedIn24h   int                `json:"viewed_within_24h"`
	EmailSent     int                `json:"email_sent"`
	EmailFailed   int                `json:"email_failed"`
	Rows          []ExamViewerRow    `json:"rows"`
}

func GetExamSettingViewers(courseID string, settingID uint) (*ExamViewersSummary, error) {
	setting, err := GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return nil, err
	}
	var rows []ExamViewerRow
	err = config.DB.Raw(`
		SELECT
			s.id AS student_id,
			s.student_id AS student_code,
			s.full_name,
			COALESCE(s.email, '') AS email,
			esc.score,
			COALESCE(esc.is_absent, false) AS is_absent,
			(esc.score IS NOT NULL OR COALESCE(esc.is_absent, false)) AS has_result,
			v.first_viewed_at,
			v.last_viewed_at,
			COALESCE(v.view_count, 0) AS view_count,
			COALESCE(n.kind, '') AS notification_kind,
			n.sent_at AS notification_sent_at,
			COALESCE(n.error, '') AS notification_error
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		LEFT JOIN exam_scores esc ON esc.student_id = s.id AND esc.exam_setting_id = ?
		LEFT JOIN exam_score_views v ON v.student_id = s.id AND v.exam_setting_id = ?
		LEFT JOIN LATERAL (
			SELECT kind, sent_at, error
			FROM exam_score_notifications
			WHERE exam_setting_id = ? AND student_id = s.id
			ORDER BY created_at DESC
			LIMIT 1
		) n ON true
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name, s.email, esc.score, esc.is_absent,
			v.first_viewed_at, v.last_viewed_at, v.view_count, n.kind, n.sent_at, n.error
		ORDER BY s.student_id ASC
	`, settingID, settingID, settingID, courseID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ExamViewerRow{}
	}

	summary := &ExamViewersSummary{Setting: *setting, Meta: buildExamPublishMeta(setting), Rows: rows}
	for _, row := range rows {
		summary.TotalStudents++
		if row.FirstViewedAt != nil {
			summary.ViewedCount++
			if setting.PublishedAt != nil && row.FirstViewedAt.Sub(*setting.PublishedAt) <= 24*time.Hour {
				summary.ViewedIn24h++
			}
		} else {
			summary.UnviewedCount++
		}
		if row.NotificationKind != "" {
			if row.NotificationSentAt != nil {
				summary.EmailSent++
			} else {
				summary.EmailFailed++
			}
		}
	}
	summary.Meta.ViewedCount = int64(summary.ViewedCount)
	return summary, nil
}

// ------------------------------------------------------------
// Notifications (อีเมลถึงนักศึกษา)
// ------------------------------------------------------------

type ExamNotificationRecipient struct {
	StudentID   uint
	StudentCode string
	FullName    string
	Email       string
	HasResult   bool
	HasViewed   bool
}

// GetExamNotificationRecipients lists enrolled students with the facts the
// publish/remind flows need: whether they have a result (score or absent)
// and whether they already opened it.
func GetExamNotificationRecipients(courseID string, settingID uint) ([]ExamNotificationRecipient, error) {
	type row struct {
		StudentID   uint   `gorm:"column:student_id"`
		StudentCode string `gorm:"column:student_code"`
		FullName    string `gorm:"column:full_name"`
		Email       string `gorm:"column:email"`
		HasResult   bool   `gorm:"column:has_result"`
		HasViewed   bool   `gorm:"column:has_viewed"`
	}
	var rows []row
	err := config.DB.Raw(`
		SELECT
			s.id AS student_id,
			s.student_id AS student_code,
			s.full_name,
			COALESCE(s.email, '') AS email,
			(esc.score IS NOT NULL OR COALESCE(esc.is_absent, false)) AS has_result,
			(v.id IS NOT NULL) AS has_viewed
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		LEFT JOIN exam_scores esc ON esc.student_id = s.id AND esc.exam_setting_id = ?
		LEFT JOIN exam_score_views v ON v.student_id = s.id AND v.exam_setting_id = ?
		WHERE cs.course_id = ?
		GROUP BY s.id, s.student_id, s.full_name, s.email, esc.score, esc.is_absent, v.id
		ORDER BY s.student_id ASC
	`, settingID, settingID, courseID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ExamNotificationRecipient, 0, len(rows))
	for _, r := range rows {
		out = append(out, ExamNotificationRecipient{
			StudentID: r.StudentID, StudentCode: r.StudentCode, FullName: r.FullName,
			Email: r.Email, HasResult: r.HasResult, HasViewed: r.HasViewed,
		})
	}
	return out, nil
}

func RecordExamScoreNotification(settingID, studentID uint, kind, toEmail string, sendErr error) {
	now := time.Now()
	row := models.ExamScoreNotification{
		ExamSettingID: settingID,
		StudentID:     studentID,
		Kind:          kind,
		ToEmail:       toEmail,
		CreatedAt:     now,
	}
	if sendErr == nil {
		row.SentAt = &now
	} else {
		row.Error = sendErr.Error()
	}
	config.DB.Create(&row)
}

// ------------------------------------------------------------
// History (ประวัติการประกาศ)
// ------------------------------------------------------------

type ExamPublishHistoryEntry struct {
	ID            uint           `json:"id" gorm:"column:id"`
	Action        string         `json:"action" gorm:"column:action"`
	ActorUserID   uint           `json:"actor_user_id" gorm:"column:actor_user_id"`
	ActorName     string         `json:"actor_name" gorm:"column:actor_name"`
	ExamSettingID uint           `json:"exam_setting_id" gorm:"column:exam_setting_id"`
	ExamType      string         `json:"exam_type" gorm:"column:exam_type"`
	Component     string         `json:"component" gorm:"column:component"`
	Detail        datatypes.JSON `json:"detail" gorm:"column:detail"`
	CreatedAt     time.Time      `json:"created_at" gorm:"column:created_at"`
}

func GetExamPublishHistory(courseID string, limit int) ([]ExamPublishHistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []ExamPublishHistoryEntry
	err := config.DB.Raw(`
		SELECT
			l.id,
			l.action,
			l.actor_user_id,
			COALESCE(u.full_name, l.actor_email) AS actor_name,
			es.id AS exam_setting_id,
			COALESCE(es.exam_type, '') AS exam_type,
			COALESCE(es.component, '') AS component,
			l.detail,
			l.created_at
		FROM course_activity_logs l
		LEFT JOIN users u ON u.id = l.actor_user_id
		LEFT JOIN exam_settings es ON es.id::text = l.target_id AND es.course_id = l.course_id
		WHERE l.course_id = ? AND l.target_type = 'exam_setting' AND l.action IN ?
		ORDER BY l.created_at DESC
		LIMIT ?
	`, courseID, ExamPublishActions, limit).Scan(&rows).Error
	if rows == nil {
		rows = []ExamPublishHistoryEntry{}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows, err
}
