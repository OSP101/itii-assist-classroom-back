package handlers

import (
	"errors"
	"fmt"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/services"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// =============================================================================
// Exam score publishing (แบบ B): review → publish/schedule → viewers → hide
//
// Visibility is no longer a checkbox saved together with the max score.
// Publishing is its own confirmed action, every change is written to the
// course activity log, and students are emailed only on the moment scores
// become visible (or when the instructor explicitly reminds / hides).
// =============================================================================

func examSettingIDParam(c fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid setting id")
	}
	return uint(id), nil
}

func examSettingNotFoundOrError(c fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่าการสอบ"})
	}
	return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load exam setting"})
}

// GET /api/courses/:courseId/exam-settings/:id/review
func GetExamSettingReviewHandler(c fiber.Ctx) error {
	settingID, err := examSettingIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	review, err := repositories.GetExamSettingReview(c.Params("courseId"), settingID)
	if err != nil {
		return examSettingNotFoundOrError(c, err)
	}
	return c.JSON(fiber.Map{"success": true, "data": review})
}

type publishExamInput struct {
	SendEmail    *bool  `json:"send_email"`
	ShowAverage  *bool  `json:"show_average"`
	ShowComments *bool  `json:"show_comments"`
	ScheduledAt  string `json:"scheduled_at"` // RFC3339; empty = publish now
	Confirmed    bool   `json:"confirmed"`
}

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

// POST /api/courses/:courseId/exam-settings/:id/publish
func PublishExamSettingHandler(c fiber.Ctx) error {
	settingID, err := examSettingIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	courseID := c.Params("courseId")
	var input publishExamInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if !input.Confirmed {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณายืนยันว่าตรวจทานคะแนนแล้วก่อนประกาศ"})
	}

	current, err := repositories.GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return examSettingNotFoundOrError(c, err)
	}
	opts := repositories.ExamPublishOptions{
		ShowAverage:   boolOr(input.ShowAverage, current.ShowAverage),
		ShowComments:  boolOr(input.ShowComments, current.ShowComments),
		NotifyByEmail: boolOr(input.SendEmail, true),
	}
	actorID, _ := c.Locals("user_id").(uint)

	if strings.TrimSpace(input.ScheduledAt) != "" {
		at, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(input.ScheduledAt))
		if parseErr != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "รูปแบบเวลาไม่ถูกต้อง"})
		}
		if at.After(time.Now().Add(time.Minute)) {
			setting, schedErr := repositories.ScheduleExamSettingPublish(courseID, settingID, at, actorID, opts)
			if schedErr != nil {
				return publishErrorResponse(c, schedErr)
			}
			logCourseActivity(c, courseID, actorID, "schedule_exam_scores_publish", "score", "exam_setting", settingID, "", fiber.Map{
				"exam_type":     setting.ExamType,
				"component":     setting.Component,
				"scheduled_at":  at,
				"send_email":    opts.NotifyByEmail,
				"show_average":  opts.ShowAverage,
				"show_comments": opts.ShowComments,
			})
			return c.JSON(fiber.Map{"success": true, "data": setting, "message": "ตั้งเวลาประกาศคะแนนแล้ว"})
		}
		// A time in the past (or within the next minute) means "now".
	}

	previous, setting, err := repositories.PublishExamSetting(courseID, settingID, actorID, opts)
	if err != nil {
		return publishErrorResponse(c, err)
	}
	review, _ := repositories.GetExamSettingReview(courseID, settingID)
	detail := fiber.Map{
		"exam_type":     setting.ExamType,
		"component":     setting.Component,
		"send_email":    opts.NotifyByEmail,
		"show_average":  opts.ShowAverage,
		"show_comments": opts.ShowComments,
	}
	if review != nil {
		detail["scored_count"] = review.ScoredCount
		detail["absent_count"] = review.AbsentCount
		detail["missing_count"] = review.MissingCount
	}
	if previous != nil && previous.HiddenAt != nil {
		detail["republished"] = true
	}
	logCourseActivity(c, courseID, actorID, "publish_exam_scores", "score", "exam_setting", settingID, "", detail)

	if opts.NotifyByEmail {
		go notifyExamScorePublishedToResults(courseID, setting)
	}
	return c.JSON(fiber.Map{"success": true, "data": setting, "message": "ประกาศคะแนนให้นักศึกษาแล้ว"})
}

func publishErrorResponse(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่าการสอบ"})
	case errors.Is(err, repositories.ErrExamSettingNotActive):
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รายการสอบนี้ยังไม่เปิดใช้ กรุณาเปิดใช้และกำหนดคะแนนเต็มก่อน"})
	case errors.Is(err, repositories.ErrExamSettingAlreadyVisible):
		return c.Status(409).JSON(fiber.Map{"success": false, "message": "คะแนนรายการนี้ประกาศไปแล้ว"})
	case errors.Is(err, repositories.ErrExamSettingNotVisible):
		return c.Status(409).JSON(fiber.Map{"success": false, "message": "คะแนนรายการนี้ยังไม่ได้ประกาศ"})
	default:
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update exam setting"})
	}
}

type hideExamInput struct {
	Reason        string `json:"reason"`
	NotifyViewers bool   `json:"notify_viewers"`
}

// POST /api/courses/:courseId/exam-settings/:id/hide
//
// Hides published scores, or cancels a pending scheduled publish when the
// scores were never visible. Either way the row goes back to draft.
func HideExamSettingHandler(c fiber.Ctx) error {
	settingID, err := examSettingIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	courseID := c.Params("courseId")
	var input hideExamInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	actorID, _ := c.Locals("user_id").(uint)

	current, err := repositories.GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return examSettingNotFoundOrError(c, err)
	}

	if !current.IsVisible {
		if current.ScheduledPublishAt == nil {
			return publishErrorResponse(c, repositories.ErrExamSettingNotVisible)
		}
		setting, cancelErr := repositories.CancelExamSettingSchedule(courseID, settingID)
		if cancelErr != nil {
			return publishErrorResponse(c, cancelErr)
		}
		logCourseActivity(c, courseID, actorID, "cancel_exam_scores_schedule", "score", "exam_setting", settingID, "", fiber.Map{
			"exam_type":    setting.ExamType,
			"component":    setting.Component,
			"scheduled_at": current.ScheduledPublishAt,
		})
		return c.JSON(fiber.Map{"success": true, "data": setting, "message": "ยกเลิกการตั้งเวลาประกาศแล้ว"})
	}

	viewers, _ := repositories.GetExamNotificationRecipients(courseID, settingID)
	viewedCount := 0
	for _, v := range viewers {
		if v.HasViewed {
			viewedCount++
		}
	}

	_, setting, err := repositories.HideExamSetting(courseID, settingID, actorID, input.Reason)
	if err != nil {
		return publishErrorResponse(c, err)
	}
	logCourseActivity(c, courseID, actorID, "hide_exam_scores", "score", "exam_setting", settingID, "", fiber.Map{
		"exam_type":      setting.ExamType,
		"component":      setting.Component,
		"reason":         strings.TrimSpace(input.Reason),
		"viewed_count":   viewedCount,
		"notify_viewers": input.NotifyViewers,
	})

	if input.NotifyViewers && viewedCount > 0 {
		go notifyExamScoreHidden(courseID, setting, viewers)
	}
	return c.JSON(fiber.Map{"success": true, "data": setting, "message": "ซ่อนคะแนนจากนักศึกษาแล้ว"})
}

// GET /api/courses/:courseId/exam-settings/:id/viewers
func GetExamSettingViewersHandler(c fiber.Ctx) error {
	settingID, err := examSettingIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	summary, err := repositories.GetExamSettingViewers(c.Params("courseId"), settingID)
	if err != nil {
		return examSettingNotFoundOrError(c, err)
	}
	return c.JSON(fiber.Map{"success": true, "data": summary})
}

// POST /api/courses/:courseId/exam-settings/:id/remind
//
// Emails the students who have a result but have not opened it yet.
func RemindExamSettingViewersHandler(c fiber.Ctx) error {
	settingID, err := examSettingIDParam(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	courseID := c.Params("courseId")
	setting, err := repositories.GetExamSettingByCourse(courseID, settingID)
	if err != nil {
		return examSettingNotFoundOrError(c, err)
	}
	if !setting.IsVisible {
		return publishErrorResponse(c, repositories.ErrExamSettingNotVisible)
	}
	recipients, err := repositories.GetExamNotificationRecipients(courseID, settingID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}
	pending := recipients[:0:0]
	for _, r := range recipients {
		if r.HasResult && !r.HasViewed && strings.TrimSpace(r.Email) != "" {
			pending = append(pending, r)
		}
	}
	actorID, _ := c.Locals("user_id").(uint)
	logCourseActivity(c, courseID, actorID, "remind_exam_scores", "score", "exam_setting", settingID, "", fiber.Map{
		"exam_type": setting.ExamType,
		"component": setting.Component,
		"count":     len(pending),
	})
	if len(pending) > 0 {
		go sendExamEmails(courseID, setting, pending, "reminder")
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"count": len(pending)}, "message": fmt.Sprintf("กำลังส่งอีเมลเตือน %d คน", len(pending))})
}

// GET /api/courses/:courseId/exam-settings/history
func GetExamPublishHistoryHandler(c fiber.Ctx) error {
	history, err := repositories.GetExamPublishHistory(c.Params("courseId"), 100)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load publish history"})
	}
	return c.JSON(fiber.Map{"success": true, "data": history})
}

// -----------------------------------------------------------------------------
// Email fan-out. All run in the background so the instructor's request never
// waits on a course-wide round of SMTP deliveries; every attempt is recorded
// per student so the viewers panel can show "ส่งแล้ว / ตีกลับ".
// -----------------------------------------------------------------------------

func sendExamEmails(courseID string, setting *models.ExamSetting, recipients []repositories.ExamNotificationRecipient, kind string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("⚠️  exam score %s email fan-out panicked for setting %d: %v", kind, setting.ID, recovered)
		}
	}()
	courseName, err := repositories.GetCourseNameByID(courseID)
	if err != nil {
		log.Printf("⚠️  failed to load course name for exam score %s email: %v", kind, err)
		return
	}
	link := services.StudentExamScoreURL(courseID)
	for _, r := range recipients {
		email := strings.TrimSpace(r.Email)
		if email == "" {
			continue
		}
		var sendErr error
		switch kind {
		case "reminder":
			sendErr = services.SendExamScoreReminderEmail(email, r.FullName, courseName, setting.ExamType, setting.Component, link)
		case "hidden":
			sendErr = services.SendExamScoreHiddenEmail(email, r.FullName, courseName, setting.ExamType, setting.Component)
		default:
			sendErr = services.SendExamScorePublishedEmail(email, r.FullName, courseName, setting.ExamType, setting.Component, link)
		}
		if sendErr != nil {
			services.LogEmailDeliveryError("exam_score_"+kind, sendErr)
		}
		repositories.RecordExamScoreNotification(setting.ID, r.StudentID, kind, email, sendErr)
	}
}

// notifyExamScorePublishedToResults emails students who already have a
// result (a score or an absent mark). Students still missing a score get
// theirs when it is entered (see notifyLateExamResult).
func notifyExamScorePublishedToResults(courseID string, setting *models.ExamSetting) {
	recipients, err := repositories.GetExamNotificationRecipients(courseID, setting.ID)
	if err != nil {
		log.Printf("⚠️  failed to load recipients for exam score publish email: %v", err)
		return
	}
	withResult := recipients[:0:0]
	for _, r := range recipients {
		if r.HasResult {
			withResult = append(withResult, r)
		}
	}
	sendExamEmails(courseID, setting, withResult, "published")
}

func notifyExamScoreHidden(courseID string, setting *models.ExamSetting, recipients []repositories.ExamNotificationRecipient) {
	viewed := recipients[:0:0]
	for _, r := range recipients {
		if r.HasViewed {
			viewed = append(viewed, r)
		}
	}
	sendExamEmails(courseID, setting, viewed, "hidden")
}

// notifyLateExamResult covers the "กรอกเพิ่มทีหลัง" case: scores are already
// published and a student who had no result just got one. Only that student
// is emailed, and only if they have never been notified for this setting.
func notifyLateExamResult(courseID string, setting *models.ExamSetting, studentID uint) {
	if setting == nil || !setting.IsVisible || !setting.NotifyByEmail {
		return
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("⚠️  late exam result email panicked: %v", recovered)
			}
		}()
		recipients, err := repositories.GetExamNotificationRecipients(courseID, setting.ID)
		if err != nil {
			return
		}
		for _, r := range recipients {
			if r.StudentID == studentID && r.HasResult {
				sendExamEmails(courseID, setting, []repositories.ExamNotificationRecipient{r}, "published")
				return
			}
		}
	}()
}

// PublishDueExamSettings is the scheduled-publish worker body: every due
// setting is published exactly as if the instructor had pressed the button
// at that moment, including the announcement email when they asked for it.
// Called from main.go on the leader instance once a minute.
func PublishDueExamSettings() {
	due, err := repositories.GetDueScheduledExamSettings(time.Now())
	if err != nil {
		log.Printf("⚠️  scheduled exam publish: query failed: %v", err)
		return
	}
	for _, pending := range due {
		actorID := uint(0)
		if pending.ScheduledBy != nil {
			actorID = *pending.ScheduledBy
		}
		opts := repositories.ExamPublishOptions{ShowAverage: pending.ShowAverage, ShowComments: pending.ShowComments, NotifyByEmail: pending.NotifyByEmail}
		_, setting, publishErr := repositories.PublishExamSetting(pending.CourseID, pending.ID, actorID, opts)
		if publishErr != nil {
			log.Printf("⚠️  scheduled exam publish failed for setting %d: %v", pending.ID, publishErr)
			// Clear the schedule so a broken row is not retried every minute.
			_, _ = repositories.CancelExamSettingSchedule(pending.CourseID, pending.ID)
			continue
		}
		writeCourseActivityLog(pending.CourseID, actorID, "publish_exam_scores", "score", "exam_setting", pending.ID, "", fiber.Map{
			"exam_type":     setting.ExamType,
			"component":     setting.Component,
			"send_email":    opts.NotifyByEmail,
			"show_average":  opts.ShowAverage,
			"show_comments": opts.ShowComments,
			"scheduled":     true,
			"scheduled_at":  pending.ScheduledPublishAt,
		})
		if opts.NotifyByEmail {
			notifyExamScorePublishedToResults(pending.CourseID, setting)
		}
	}
}
