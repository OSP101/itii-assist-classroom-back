package services

import (
	"fmt"
	"html"
	"strings"
)

// =============================================================================
// อีเมลคำขอลา
// =============================================================================

const leaveEmailSection = "ระบบเช็กชื่อ · คำขอลา"

// LeaveEmailItem รายการวันลา 1 แถวในอีเมล
type LeaveEmailItem struct {
	DateText    string // เช่น "จันทร์ 15 ก.ย. 2569"
	SessionText string // ชื่อคาบ ว่างได้ถ้ายังไม่มีคาบ
	Result      string // approved, rejected, awaiting, superseded, pending
}

func LeaveTypeLabelTH(leaveType string) string {
	switch leaveType {
	case "sick":
		return "ลาป่วย"
	case "personal":
		return "ลากิจ"
	case "official":
		return "ลาราชการ/กิจกรรมมหาวิทยาลัย"
	default:
		return "ลาอื่น ๆ"
	}
}

// LeaveRequestReference เลขอ้างอิงคำขอลาที่แสดงในอีเมลและหน้าจอ เช่น LR-123
func LeaveRequestReference(id uint) string {
	return emailReference("LR", id)
}

func leaveItemResultLabel(result string) (string, string) {
	switch result {
	case "approved", "applied":
		return "อนุมัติ", emailThemeSuccess
	case "awaiting", "awaiting_session":
		return "อนุมัติ (รอคาบเรียน)", emailThemeSuccess
	case "rejected":
		return "ไม่อนุมัติ", emailThemeDanger
	case "superseded":
		return "มาเรียนแล้ว", emailThemeText2
	default:
		return "รอพิจารณา", emailThemeWarning
	}
}

func leaveItemsTableHTML(items []LeaveEmailItem, showResult bool) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<table style="width: 100%; border-collapse: collapse; margin: 0 0 20px; font-size: 14px;">`)
	for _, it := range items {
		session := it.SessionText
		if strings.TrimSpace(session) == "" {
			session = "ทุกคาบในวันนั้น"
		}
		b.WriteString(`<tr><td style="padding: 10px 12px; border-bottom: 1px solid #e2e8f0; color: #0f172a;">`)
		b.WriteString(html.EscapeString(it.DateText))
		b.WriteString(`<div style="font-size: 12px; color: #64748b;">`)
		b.WriteString(html.EscapeString(session))
		b.WriteString(`</div></td>`)
		if showResult {
			label, color := leaveItemResultLabel(it.Result)
			b.WriteString(fmt.Sprintf(`<td style="padding: 10px 12px; border-bottom: 1px solid #e2e8f0; text-align: right; color: %s; font-weight: 600; white-space: nowrap;">%s</td>`, color, html.EscapeString(label)))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	return b.String()
}

func leaveItemsPlain(items []LeaveEmailItem, showResult bool) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it.DateText)
		if strings.TrimSpace(it.SessionText) != "" {
			b.WriteString(" (" + it.SessionText + ")")
		}
		if showResult {
			label, _ := leaveItemResultLabel(it.Result)
			b.WriteString(": " + label)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func LeaveRequestReviewURL(courseID string) string {
	cfg := loadEmailConfig()
	return strings.TrimRight(cfg.Frontend, "/") + "/classroom/" + courseID + "?tab=leave-requests"
}

func StudentLeaveRequestURL(courseID string) string {
	cfg := loadEmailConfig()
	return strings.TrimRight(cfg.Frontend, "/") + "/student/courses/" + courseID + "?tab=leave"
}

// SendLeaveRequestSubmittedEmail แจ้งผู้สอน/TA ว่ามีคำขอลาใหม่
func SendLeaveRequestSubmittedEmail(requestID uint, toEmail, toName, courseName, studentName, studentCode, leaveType, reason string, items []LeaveEmailItem, evidenceCount int, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave request email requires a recipient")
	}
	cfg := loadEmailConfig()
	typeLabel := LeaveTypeLabelTH(leaveType)
	ref := LeaveRequestReference(requestID)
	subject := fmt.Sprintf("[%s] คำขอลาใหม่ %s (%s): %s %s", cfg.AppName, ref, typeLabel, studentCode, studentName)

	evidenceText := "ไม่มีหลักฐานแนบ"
	if evidenceCount > 0 {
		evidenceText = fmt.Sprintf("แนบหลักฐาน %d ไฟล์ (เปิดดูได้ในระบบเท่านั้น)", evidenceCount)
	}

	content := emailContent{
		Section:   leaveEmailSection,
		Title:     "คำขอลาใหม่รอพิจารณา",
		Subtitle:  fmt.Sprintf("%s · %s", courseName, typeLabel),
		Reference: ref,
		BodyHTML: emailGreeting(toName) +
			emailParagraph(fmt.Sprintf(`%s (%s) ส่งคำขอ<strong>%s</strong>ในวิชา %s จำนวน %d วัน กรุณาเข้าไปพิจารณา`, html.EscapeString(studentName), html.EscapeString(studentCode), html.EscapeString(typeLabel), html.EscapeString(courseName), len(items))) +
			leaveItemsTableHTML(items, false) +
			emailQuoteBlock("เหตุผล", reason) +
			emailMuted(html.EscapeString(evidenceText)) +
			emailButton("เปิดหน้าคำขอลา", link),
	}
	plain := fmt.Sprintf("สวัสดีคุณ%s,\n\n%s (%s) ส่งคำขอ%sในวิชา %s จำนวน %d วัน กรุณาเข้าไปพิจารณา\n\nวันที่ขอลา:\n%s\nเหตุผล: %s\n%s\n\nเปิดหน้าคำขอลา: %s",
		toName, studentName, studentCode, typeLabel, courseName, len(items), leaveItemsPlain(items, false), strings.TrimSpace(reason), evidenceText, link)

	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}

// SendLeaveRequestReviewedEmail แจ้งผลให้นักศึกษา (อนุมัติ/บางส่วน/ไม่อนุมัติ/ถอนอนุมัติ)
func SendLeaveRequestReviewedEmail(requestID uint, toEmail, toName, courseName, leaveType, status, comment string, items []LeaveEmailItem, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave request result email requires a recipient")
	}
	cfg := loadEmailConfig()
	typeLabel := LeaveTypeLabelTH(leaveType)
	ref := LeaveRequestReference(requestID)

	resultText := "ได้รับการอนุมัติ"
	gradient := emailGradientSuccess
	switch status {
	case "partially_approved":
		resultText = "ได้รับการอนุมัติบางส่วน"
		gradient = emailGradientWarning
	case "rejected":
		resultText = "ไม่ได้รับการอนุมัติ"
		gradient = emailGradientDanger
	case "revoked":
		resultText = "ถูกถอนการอนุมัติ"
		gradient = emailGradientDanger
	}
	subject := fmt.Sprintf("[%s] คำขอ%s %s %s: %s", cfg.AppName, typeLabel, ref, resultText, courseName)

	note := ""
	if status == "approved" || status == "partially_approved" {
		note = emailMuted(`วันที่อนุมัติแล้ว ระบบบันทึกสถานะเช็กชื่อเป็น "ลา" ให้อัตโนมัติ ถ้าคาบเรียนของวันนั้นยังไม่ถูกสร้าง ระบบจะบันทึกให้เมื่อผู้สอนสร้างคาบ`)
	}

	content := emailContent{
		Section:   leaveEmailSection,
		Title:     "คำขอ" + typeLabel + resultText,
		Subtitle:  courseName,
		Gradient:  gradient,
		Reference: ref,
		BodyHTML: emailGreeting(toName) +
			emailParagraph(fmt.Sprintf(`คำขอ%sของคุณในวิชา %s %sแล้ว รายละเอียดรายวัน:`, html.EscapeString(typeLabel), html.EscapeString(courseName), html.EscapeString(resultText))) +
			leaveItemsTableHTML(items, true) +
			emailQuoteBlock("ความเห็นของผู้สอน", comment) +
			note +
			emailButton("ดูคำขอลาของฉัน", link),
	}
	plain := fmt.Sprintf("สวัสดีคุณ%s,\n\nคำขอ%sของคุณในวิชา %s %sแล้ว\n\n%s\nความเห็นของผู้สอน: %s\n\nดูคำขอลาของฉัน: %s",
		toName, typeLabel, courseName, resultText, leaveItemsPlain(items, true), strings.TrimSpace(comment), link)

	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}

// SendLeaveRequestPendingReminderEmail เตือนผู้สอนว่ามีคำขอค้างนาน
func SendLeaveRequestPendingReminderEmail(toEmail, toName, courseName string, pendingCount int, oldestDays int, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave reminder email requires a recipient")
	}
	cfg := loadEmailConfig()
	subject := fmt.Sprintf("[%s] มีคำขอลาค้างพิจารณา %d รายการ: %s", cfg.AppName, pendingCount, courseName)
	content := emailContent{
		Section:  leaveEmailSection,
		Title:    "คำขอลาค้างพิจารณา",
		Subtitle: courseName,
		Gradient: emailGradientWarning,
		BodyHTML: emailGreeting(toName) +
			emailParagraph(fmt.Sprintf(`วิชา %s มีคำขอลาที่ยังไม่ได้พิจารณา <strong>%d รายการ</strong> รายการที่เก่าที่สุดค้างมา %d วันแล้ว`, html.EscapeString(courseName), pendingCount, oldestDays)) +
			emailMuted("ระบบส่งเตือนวันละครั้งจนกว่าคำขอจะถูกพิจารณา") +
			emailButton("เปิดหน้าคำขอลา", link),
	}
	plain := fmt.Sprintf("สวัสดีคุณ%s,\n\nวิชา %s มีคำขอลาที่ยังไม่ได้พิจารณา %d รายการ รายการที่เก่าที่สุดค้างมา %d วันแล้ว\n\nเปิดหน้าคำขอลา: %s", toName, courseName, pendingCount, oldestDays, link)
	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: renderEmailHTML(content), Plain: renderEmailPlain(content, plain)})
}
