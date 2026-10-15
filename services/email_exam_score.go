package services

import (
	"fmt"
	"html"
	"strings"
)

// =============================================================================
// อีเมลแจ้งประกาศคะแนนสอบ
// =============================================================================

const examScoreEmailSection = "ประกาศคะแนนสอบ"

func ExamTypeLabelTH(examType string) string {
	switch examType {
	case "midterm":
		return "สอบกลางภาค"
	case "final":
		return "สอบปลายภาค"
	default:
		return "การสอบ"
	}
}

func ExamComponentLabelTH(component string) string {
	switch component {
	case "lab":
		return "ปฏิบัติ"
	case "lecture":
		return "บรรยาย"
	default:
		return component
	}
}

// StudentExamScoreURL คือลิงก์หน้าดูคะแนนของนักศึกษาในวิชานั้น
func StudentExamScoreURL(courseID string) string {
	cfg := loadEmailConfig()
	return strings.TrimRight(cfg.Frontend, "/") + "/student/courses/" + courseID + "?tab=Scores"
}

// SendExamScorePublishedEmail แจ้งนักศึกษาว่าผู้สอนเปิดให้ดูคะแนนสอบแล้ว
func SendExamScorePublishedEmail(toEmail, toName, courseName, examType, component, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("exam score published email requires a recipient")
	}
	cfg := loadEmailConfig()
	examLabel := ExamTypeLabelTH(examType)
	componentLabel := ExamComponentLabelTH(component)
	subject := fmt.Sprintf("[%s] ประกาศคะแนน%s (%s): %s", cfg.AppName, examLabel, componentLabel, courseName)

	content := emailContent{
		Section:  examScoreEmailSection,
		Title:    "ประกาศคะแนน" + examLabel,
		Subtitle: fmt.Sprintf("%s (%s)", courseName, componentLabel),
		BodyHTML: emailGreeting(toName, emailRecipientStudent) +
			emailParagraph(fmt.Sprintf(`ผู้สอนวิชา %s เปิดให้ดูคะแนน<strong>%s (%s)</strong> แล้ว เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ`, html.EscapeString(courseName), html.EscapeString(examLabel), html.EscapeString(componentLabel))) +
			emailButton("ดูคะแนนของฉัน", link),
	}
	plain := fmt.Sprintf("%s\n\nผู้สอนวิชา %s เปิดให้ดูคะแนน%s (%s) แล้ว เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ\n\nดูคะแนนของฉัน: %s",
		emailGreetingPlain(toName, emailRecipientStudent), courseName, examLabel, componentLabel, link)

	return sendNotificationEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}

// SendExamScoreReminderEmail nudges a student who has a published result but
// has not opened it yet.
func SendExamScoreReminderEmail(toEmail, toName, courseName, examType, component, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("exam score reminder email requires a recipient")
	}
	cfg := loadEmailConfig()
	examLabel := ExamTypeLabelTH(examType)
	componentLabel := ExamComponentLabelTH(component)
	subject := fmt.Sprintf("[%s] เตือน: คะแนน%s (%s) ของคุณพร้อมให้ดูแล้ว: %s", cfg.AppName, examLabel, componentLabel, courseName)

	content := emailContent{
		Section:  examScoreEmailSection,
		Title:    "คะแนน" + examLabel + "พร้อมให้ดูแล้ว",
		Subtitle: fmt.Sprintf("%s (%s)", courseName, componentLabel),
		BodyHTML: emailGreeting(toName, emailRecipientStudent) +
			emailParagraph(fmt.Sprintf(`ผู้สอนวิชา %s ประกาศคะแนน<strong>%s (%s)</strong> แล้ว แต่ระบบยังไม่พบว่าคุณเปิดดู เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ`, html.EscapeString(courseName), html.EscapeString(examLabel), html.EscapeString(componentLabel))) +
			emailButton("ดูคะแนนของฉัน", link),
	}
	plain := fmt.Sprintf("%s\n\nผู้สอนวิชา %s ประกาศคะแนน%s (%s) แล้ว แต่ระบบยังไม่พบว่าคุณเปิดดู เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ\n\nดูคะแนนของฉัน: %s",
		emailGreetingPlain(toName, emailRecipientStudent), courseName, examLabel, componentLabel, link)

	return sendNotificationEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}

// SendExamScoreHiddenEmail tells a student who already saw a score that the
// instructor has withdrawn it for now. The reason is never included.
func SendExamScoreHiddenEmail(toEmail, toName, courseName, examType, component string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("exam score hidden email requires a recipient")
	}
	cfg := loadEmailConfig()
	examLabel := ExamTypeLabelTH(examType)
	componentLabel := ExamComponentLabelTH(component)
	subject := fmt.Sprintf("[%s] ปิดการแสดงคะแนน%s (%s) ชั่วคราว: %s", cfg.AppName, examLabel, componentLabel, courseName)

	content := emailContent{
		Section:  examScoreEmailSection,
		Title:    "ปิดการแสดงคะแนน" + examLabel + "ชั่วคราว",
		Subtitle: fmt.Sprintf("%s (%s)", courseName, componentLabel),
		BodyHTML: emailGreeting(toName, emailRecipientStudent) +
			emailParagraph(fmt.Sprintf(`ผู้สอนวิชา %s ปิดการแสดงคะแนน<strong>%s (%s)</strong> ไว้ชั่วคราวเพื่อตรวจสอบเพิ่มเติม คะแนนที่คุณเห็นก่อนหน้านี้อาจมีการเปลี่ยนแปลง ระบบจะแจ้งอีกครั้งเมื่อประกาศใหม่`, html.EscapeString(courseName), html.EscapeString(examLabel), html.EscapeString(componentLabel))),
	}
	plain := fmt.Sprintf("%s\n\nผู้สอนวิชา %s ปิดการแสดงคะแนน%s (%s) ไว้ชั่วคราวเพื่อตรวจสอบเพิ่มเติม คะแนนที่คุณเห็นก่อนหน้านี้อาจมีการเปลี่ยนแปลง ระบบจะแจ้งอีกครั้งเมื่อประกาศใหม่",
		emailGreetingPlain(toName, emailRecipientStudent), courseName, examLabel, componentLabel)

	return sendNotificationEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}
