package services

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"itii-assist/config"
	"itii-assist/models"
)

// Notification emails to instructors (and admins, who are teaching staff here)
// are only delivered 08:01-20:00 Thai time;
// anything raised outside that window waits in deferred_emails. Students and TAs
// are not held back, and neither are password-reset or 2FA emails,
// which never go through sendNotificationEmail.

var ErrEmailDeferred = errors.New("instructor email deferred to sending window")

const (
	instructorWindowOpenMinute  = 8*60 + 1 // 08:01
	instructorWindowCloseMinute = 20 * 60  // 20:00, inclusive
	deferredEmailMaxAttempts    = 5
)

// Thailand has no DST, so a fixed offset avoids depending on tzdata in the image.
var thaiTime = time.FixedZone("ICT", 7*60*60)

func InstructorEmailWindowOpen(now time.Time) bool {
	local := now.In(thaiTime)
	minute := local.Hour()*60 + local.Minute()
	return minute >= instructorWindowOpenMinute && minute <= instructorWindowCloseMinute
}

func InThaiTime(t time.Time) time.Time {
	return t.In(thaiTime)
}

func nextInstructorEmailWindow(now time.Time) time.Time {
	local := now.In(thaiTime)
	open := time.Date(local.Year(), local.Month(), local.Day(), 8, 1, 0, 0, thaiTime)
	if !local.Before(open) {
		open = open.AddDate(0, 0, 1)
	}
	return open
}

func isInstructorEmail(address string) bool {
	var count int64
	err := config.DB.Model(&models.User{}).
		Where("LOWER(email) = LOWER(?) AND role IN ? AND is_active = ?", strings.TrimSpace(address), []string{"instructor", "admin"}, true).
		Count(&count).Error
	if err != nil {
		log.Printf("[email] instructor lookup failed for quiet hours, sending now: %v", err)
		return false
	}
	return count > 0
}

func sendNotificationEmail(message emailMessage) error {
	now := time.Now()
	if InstructorEmailWindowOpen(now) || !isInstructorEmail(message.To) {
		return sendEmail(message)
	}

	sendAfter := nextInstructorEmailWindow(now)
	row := models.DeferredEmail{
		ToEmail:   message.To,
		Subject:   message.Subject,
		HTML:      message.HTML,
		Plain:     message.Plain,
		SendAfter: sendAfter,
	}
	if err := config.DB.Create(&row).Error; err != nil {
		return fmt.Errorf("could not queue instructor email for %s: %w", message.To, err)
	}
	return fmt.Errorf("%w (ส่งเวลา %s น.)", ErrEmailDeferred, sendAfter.Format("02/01/2006 15:04"))
}

// FlushDeferredEmails sends queued instructor emails that are due. It does
// nothing outside the window, so a retry pushed past 20:00 waits for morning.
func FlushDeferredEmails(limit int) {
	now := time.Now()
	if !InstructorEmailWindowOpen(now) {
		return
	}

	var rows []models.DeferredEmail
	if err := config.DB.
		Where("sent_at IS NULL AND failed_at IS NULL AND send_after <= ?", now).
		Order("send_after, id").
		Limit(limit).
		Find(&rows).Error; err != nil {
		log.Printf("[email] load deferred emails: %v", err)
		return
	}

	for _, row := range rows {
		err := sendEmail(emailMessage{To: row.ToEmail, Subject: row.Subject, HTML: row.HTML, Plain: row.Plain})
		sentAt := time.Now()
		if err == nil {
			config.DB.Model(&models.DeferredEmail{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"sent_at": sentAt, "attempts": row.Attempts + 1, "last_error": ""})
			continue
		}

		attempts := row.Attempts + 1
		updates := map[string]interface{}{"attempts": attempts, "last_error": err.Error()}
		if attempts >= deferredEmailMaxAttempts {
			updates["failed_at"] = sentAt
		} else {
			updates["send_after"] = sentAt.Add(time.Duration(attempts) * 5 * time.Minute)
		}
		config.DB.Model(&models.DeferredEmail{}).Where("id = ?", row.ID).Updates(updates)
		log.Printf("[email] deferred email %d to %s failed (attempt %d): %v", row.ID, row.ToEmail, attempts, err)
	}
}
