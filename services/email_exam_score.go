package services

import (
	"fmt"
	"html"
	"strings"
)

// =============================================================================
// อีเมลแจ้งประกาศคะแนนสอบ
// =============================================================================

const examScoreEmailSection = "ระบบสอบ · ประกาศคะแนน"

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
		Subtitle: fmt.Sprintf("%s · %s", courseName, componentLabel),
		BodyHTML: emailGreeting(toName) +
			emailParagraph(fmt.Sprintf(`ผู้สอนวิชา %s เปิดให้ดูคะแนน<strong>%s (%s)</strong> แล้ว เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ`, html.EscapeString(courseName), html.EscapeString(examLabel), html.EscapeString(componentLabel))) +
			emailButton("ดูคะแนนของฉัน", link),
	}
	plain := fmt.Sprintf("สวัสดีคุณ%s,\n\nผู้สอนวิชา %s เปิดให้ดูคะแนน%s (%s) แล้ว เข้าไปตรวจสอบคะแนนของคุณได้ในระบบ\n\nดูคะแนนของฉัน: %s",
		toName, courseName, examLabel, componentLabel, link)

	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}
