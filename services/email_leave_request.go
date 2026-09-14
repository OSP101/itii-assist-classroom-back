package services

import (
	"fmt"
	"html"
	"strings"
)

// =============================================================================
// อีเมลคำขอลา
// =============================================================================

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

func leaveItemResultLabel(result string) (string, string) {
	switch result {
	case "approved", "applied":
		return "อนุมัติ", "#0f766e"
	case "awaiting", "awaiting_session":
		return "อนุมัติ (รอคาบเรียน)", "#0f766e"
	case "rejected":
		return "ไม่อนุมัติ", "#b91c1c"
	case "superseded":
		return "มาเรียนแล้ว", "#475569"
	default:
		return "รอพิจารณา", "#b45309"
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
	return strings.TrimRight(cfg.Frontend, "/") + "/classroom/" + courseID + "?tab=attendance&view=leave"
}

func StudentLeaveRequestURL(courseID string) string {
	cfg := loadEmailConfig()
	return strings.TrimRight(cfg.Frontend, "/") + "/student/courses/" + courseID + "?tab=leave"
}

// SendLeaveRequestSubmittedEmail แจ้งผู้สอน/TA ว่ามีคำขอลาใหม่
func SendLeaveRequestSubmittedEmail(toEmail, toName, courseName, studentName, studentCode, leaveType, reason string, items []LeaveEmailItem, evidenceCount int, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave request email requires a recipient")
	}
	cfg := loadEmailConfig()
	typeLabel := LeaveTypeLabelTH(leaveType)
	subject := fmt.Sprintf("[%s] คำขอลาใหม่ (%s): %s %s", cfg.AppName, typeLabel, studentCode, studentName)

	reasonBlock := ""
	if strings.TrimSpace(reason) != "" {
		reasonBlock = fmt.Sprintf(`
      <div style="margin: 0 0 20px; padding: 18px; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; white-space: pre-wrap; line-height: 1.7; color: #334155;">
        <div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px; margin-bottom: 8px;">เหตุผล</div>%s</div>`, html.EscapeString(strings.TrimSpace(reason)))
	}
	evidenceText := "ไม่มีหลักฐานแนบ"
	if evidenceCount > 0 {
		evidenceText = fmt.Sprintf("แนบหลักฐาน %d ไฟล์ (ดูได้ในระบบ)", evidenceCount)
	}

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #1d4ed8, #0f766e); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">คำขอลาใหม่</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">
        %s (%s) ส่งคำขอ<strong>%s</strong>ในวิชา %s กรุณาเข้าไปพิจารณา
      </p>
      %s%s
      <p style="margin: 0 0 20px; font-size: 13px; color: #64748b;">%s</p>
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          เปิดหน้าคำขอลา
        </a>
      </p>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(toName), html.EscapeString(studentName), html.EscapeString(studentCode), html.EscapeString(typeLabel), html.EscapeString(courseName), leaveItemsTableHTML(items, false), reasonBlock, html.EscapeString(evidenceText), html.EscapeString(link))

	plainBody := fmt.Sprintf("%s\n\nสวัสดีคุณ%s,\n\n%s (%s) ส่งคำขอ%sในวิชา %s กรุณาเข้าไปพิจารณา\n\nวันที่ขอลา:\n%s\nเหตุผล: %s\n%s\n\n%s\n",
		cfg.AppName, toName, studentName, studentCode, typeLabel, courseName, leaveItemsPlain(items, false), strings.TrimSpace(reason), evidenceText, link)

	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: htmlBody, Plain: plainBody})
}

// SendLeaveRequestReviewedEmail แจ้งผลให้นักศึกษา (อนุมัติ/บางส่วน/ไม่อนุมัติ/ถอนอนุมัติ)
func SendLeaveRequestReviewedEmail(toEmail, toName, courseName, leaveType, status, comment string, items []LeaveEmailItem, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave request result email requires a recipient")
	}
	cfg := loadEmailConfig()
	typeLabel := LeaveTypeLabelTH(leaveType)

	resultText := "ได้รับการอนุมัติ"
	headerColor := "linear-gradient(135deg, #0f766e, #1d4ed8)"
	switch status {
	case "partially_approved":
		resultText = "ได้รับการอนุมัติบางส่วน"
		headerColor = "linear-gradient(135deg, #b45309, #0f766e)"
	case "rejected":
		resultText = "ไม่ได้รับการอนุมัติ"
		headerColor = "linear-gradient(135deg, #b91c1c, #ea580c)"
	case "revoked":
		resultText = "ถูกถอนการอนุมัติ"
		headerColor = "linear-gradient(135deg, #b91c1c, #7c2d12)"
	}
	subject := fmt.Sprintf("[%s] คำขอ%s%s: %s", cfg.AppName, typeLabel, resultText, courseName)

	commentBlock := ""
	if strings.TrimSpace(comment) != "" {
		commentBlock = fmt.Sprintf(`
      <div style="margin: 0 0 20px; padding: 18px; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; white-space: pre-wrap; line-height: 1.7; color: #334155;">
        <div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px; margin-bottom: 8px;">ความเห็นของผู้สอน</div>%s</div>`, html.EscapeString(strings.TrimSpace(comment)))
	}
	note := ""
	if status == "approved" || status == "partially_approved" {
		note = `<p style="margin: 0 0 20px; font-size: 13px; color: #64748b;">วันที่อนุมัติแล้ว ระบบบันทึกสถานะเช็กชื่อเป็น "ลา" ให้อัตโนมัติ ถ้าคาบเรียนของวันนั้นยังไม่ถูกสร้าง ระบบจะบันทึกให้เมื่อผู้สอนสร้างคาบ</p>`
	}

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: %s; color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">คำขอ%s%s</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">คำขอ%sของคุณในวิชา %s %sแล้ว รายละเอียดรายวัน:</p>
      %s%s%s
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          ดูคำขอลาของฉัน
        </a>
      </p>
    </div>
  </div>
</div>`, headerColor, html.EscapeString(typeLabel), html.EscapeString(resultText), html.EscapeString(cfg.AppName), html.EscapeString(toName), html.EscapeString(typeLabel), html.EscapeString(courseName), html.EscapeString(resultText), leaveItemsTableHTML(items, true), commentBlock, note, html.EscapeString(link))

	plainBody := fmt.Sprintf("%s\n\nสวัสดีคุณ%s,\n\nคำขอ%sของคุณในวิชา %s %sแล้ว\n\n%s\nความเห็น: %s\n\n%s\n",
		cfg.AppName, toName, typeLabel, courseName, resultText, leaveItemsPlain(items, true), strings.TrimSpace(comment), link)

	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: htmlBody, Plain: plainBody})
}

// SendLeaveRequestPendingReminderEmail เตือนผู้สอนว่ามีคำขอค้างนาน
func SendLeaveRequestPendingReminderEmail(toEmail, toName, courseName string, pendingCount int, oldestDays int, link string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("leave reminder email requires a recipient")
	}
	cfg := loadEmailConfig()
	subject := fmt.Sprintf("[%s] มีคำขอลาค้างพิจารณา %d รายการ: %s", cfg.AppName, pendingCount, courseName)
	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #b45309, #1d4ed8); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">คำขอลาค้างพิจารณา</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">วิชา %s มีคำขอลาที่ยังไม่ได้พิจารณา %d รายการ รายการที่เก่าที่สุดค้างมา %d วันแล้ว</p>
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">เปิดหน้าคำขอลา</a>
      </p>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(toName), html.EscapeString(courseName), pendingCount, oldestDays, html.EscapeString(link))
	plainBody := fmt.Sprintf("%s\n\nสวัสดีคุณ%s,\n\nวิชา %s มีคำขอลาที่ยังไม่ได้พิจารณา %d รายการ รายการที่เก่าที่สุดค้างมา %d วันแล้ว\n%s\n", cfg.AppName, toName, courseName, pendingCount, oldestDays, link)
	return sendEmail(emailMessage{To: strings.TrimSpace(toEmail), Subject: subject, HTML: htmlBody, Plain: plainBody})
}
