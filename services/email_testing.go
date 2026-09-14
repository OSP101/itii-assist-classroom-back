package services

import (
	"fmt"
	"html"
	"strings"
	"time"

	"itii-assist/models"
)

// =============================================================================
// ทดสอบการส่งอีเมล (หน้าตั้งค่าของ admin)
// =============================================================================

// EmailConfigSummary สรุปค่าตั้งค่าอีเมลแบบไม่เปิดเผยความลับ
type EmailConfigSummary struct {
	Provider       string `json:"provider"`
	From           string `json:"from"`
	AppName        string `json:"app_name"`
	FrontendURL    string `json:"frontend_url"`
	SMTPHost       string `json:"smtp_host"`
	SMTPPort       int    `json:"smtp_port"`
	SMTPSecure     bool   `json:"smtp_secure"`
	SMTPUserSet    bool   `json:"smtp_user_set"`
	SMTPPassSet    bool   `json:"smtp_pass_set"`
	ResendKeySet   bool   `json:"resend_key_set"`
	Ready          bool   `json:"ready"`
	ReadinessNote  string `json:"readiness_note"`
	SupportAlertTo int    `json:"support_alert_recipients"`
}

func GetEmailConfigSummary() EmailConfigSummary {
	cfg := loadEmailConfig()
	summary := EmailConfigSummary{
		Provider:       cfg.Provider,
		From:           cfg.From,
		AppName:        cfg.AppName,
		FrontendURL:    cfg.Frontend,
		SMTPHost:       cfg.SMTPHost,
		SMTPPort:       cfg.SMTPPort,
		SMTPSecure:     cfg.SMTPSecure,
		SMTPUserSet:    cfg.SMTPUser != "",
		SMTPPassSet:    cfg.SMTPPass != "",
		ResendKeySet:   cfg.ResendKey != "",
		SupportAlertTo: len(supportAlertRecipients()),
	}
	switch cfg.Provider {
	case "resend":
		summary.Ready = cfg.ResendKey != ""
		if !summary.Ready {
			summary.ReadinessNote = "RESEND_API_KEY ยังไม่ได้ตั้งค่า"
		}
	case "smtp":
		summary.Ready = cfg.SMTPHost != ""
		if !summary.Ready {
			summary.ReadinessNote = "SMTP_HOST ยังไม่ได้ตั้งค่า"
		}
	default:
		summary.ReadinessNote = "EMAIL_PROVIDER ไม่รู้จัก: " + cfg.Provider
	}
	return summary
}

// EmailTestTemplates รายการแม่แบบที่ทดสอบได้ (key → ชื่อ)
var EmailTestTemplates = []struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	{"plain", "อีเมลทดสอบธรรมดา"},
	{"leave_submitted", "คำขอลาใหม่ (ถึงผู้สอน)"},
	{"leave_reviewed_approved", "ผลคำขอลา: อนุมัติ (ถึงนักศึกษา)"},
	{"leave_reviewed_rejected", "ผลคำขอลา: ไม่อนุมัติ (ถึงนักศึกษา)"},
	{"leave_pending_reminder", "เตือนคำขอลาค้าง (ถึงผู้สอน)"},
	{"score_edit_submitted", "คำขอแก้ไขคะแนนใหม่"},
	{"score_edit_reviewed", "ผลคำขอแก้ไขคะแนน"},
	{"password_reset", "รีเซ็ตรหัสผ่าน"},
	{"two_factor", "รหัสยืนยันสองขั้นตอน"},
}

func IsValidEmailTestTemplate(key string) bool {
	for _, t := range EmailTestTemplates {
		if t.Key == key {
			return true
		}
	}
	return false
}

// SendTestEmail ส่งอีเมลตามแม่แบบที่เลือกด้วยข้อมูลตัวอย่าง คืนเวลาที่ใช้และ error
func SendTestEmail(templateKey string, to string, requestedBy string) (time.Duration, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return 0, fmt.Errorf("recipient required")
	}
	recipient := &models.User{Email: to, FullName: "ผู้ทดสอบระบบ", Username: "tester"}
	started := time.Now()
	var err error
	sampleItems := []LeaveEmailItem{
		{DateText: "จันทร์ 15 ก.ย. 2569", SessionText: "Lecture 09:00 ถึง 12:00", Result: "approved"},
		{DateText: "พุธ 17 ก.ย. 2569", SessionText: "", Result: "awaiting"},
	}
	switch templateKey {
	case "plain":
		err = sendPlainTestEmail(to, requestedBy)
	case "leave_submitted":
		err = SendLeaveRequestSubmittedEmail(to, "ผู้ทดสอบระบบ", "CP421024 การเขียนโปรแกรมเชิงวัตถุ", "นางสาวทดสอบ ระบบ", "650001", "sick", "ป่วยเป็นไข้หวัด มีใบรับรองแพทย์แนบ (ข้อความทดสอบ)", sampleItems, 1, LeaveRequestReviewURL("test-course"))
	case "leave_reviewed_approved":
		err = SendLeaveRequestReviewedEmail(to, "ผู้ทดสอบระบบ", "CP421024 การเขียนโปรแกรมเชิงวัตถุ", "sick", "approved", "หายไว ๆ นะ (ข้อความทดสอบ)", sampleItems, StudentLeaveRequestURL("test-course"))
	case "leave_reviewed_rejected":
		rejected := []LeaveEmailItem{{DateText: "จันทร์ 15 ก.ย. 2569", SessionText: "Lecture 09:00 ถึง 12:00", Result: "rejected"}}
		err = SendLeaveRequestReviewedEmail(to, "ผู้ทดสอบระบบ", "CP421024 การเขียนโปรแกรมเชิงวัตถุ", "personal", "rejected", "หลักฐานไม่ครบ (ข้อความทดสอบ)", rejected, StudentLeaveRequestURL("test-course"))
	case "leave_pending_reminder":
		err = SendLeaveRequestPendingReminderEmail(to, "ผู้ทดสอบระบบ", "CP421024 การเขียนโปรแกรมเชิงวัตถุ", 3, 4, LeaveRequestReviewURL("test-course"))
	case "score_edit_submitted":
		err = SendScoreEditRequestSubmittedEmail(recipient, "CP421024 การเขียนโปรแกรมเชิงวัตถุ", "Lab 3", "ผู้ช่วยสอนทดสอบ", "กรอกคะแนนผิด (ข้อความทดสอบ)", CourseApprovalURL("test-course"))
	case "score_edit_reviewed":
		err = SendScoreEditRequestReviewedEmail(recipient, true, "Lab 3", "ตรวจสอบแล้ว (ข้อความทดสอบ)", 1, CourseApprovalURL("test-course"))
	case "password_reset":
		err = SendPasswordResetEmail(recipient, "test-token-not-valid")
	case "two_factor":
		err = SendTwoFactorCodeEmail(recipient, "123456", "login")
	default:
		return 0, fmt.Errorf("unknown template: %s", templateKey)
	}
	return time.Since(started), err
}

func sendPlainTestEmail(to string, requestedBy string) error {
	cfg := loadEmailConfig()
	now := time.Now().Format("2 Jan 2006 15:04:05 MST")
	subject := fmt.Sprintf("[%s] ทดสอบการส่งอีเมล %s", cfg.AppName, now)
	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #0f766e, #1d4ed8); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">ทดสอบการส่งอีเมลสำเร็จ</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px; color: #334155; line-height: 1.7;">
      <p style="margin: 0 0 12px;">ถ้าคุณได้รับข้อความนี้ แสดงว่าระบบส่งอีเมลผ่าน <b>%s</b> ทำงานได้ปกติ</p>
      <table style="font-size: 13px; color: #475569;">
        <tr><td style="padding: 3px 12px 3px 0;">ผู้ส่ง</td><td>%s</td></tr>
        <tr><td style="padding: 3px 12px 3px 0;">ขอทดสอบโดย</td><td>%s</td></tr>
        <tr><td style="padding: 3px 12px 3px 0;">เวลา</td><td>%s</td></tr>
      </table>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(cfg.Provider), html.EscapeString(cfg.From), html.EscapeString(requestedBy), html.EscapeString(now))
	plain := fmt.Sprintf("%s\n\nทดสอบการส่งอีเมลสำเร็จ ผ่าน %s\nผู้ส่ง: %s\nขอทดสอบโดย: %s\nเวลา: %s\n", cfg.AppName, cfg.Provider, cfg.From, requestedBy, now)
	return sendEmail(emailMessage{To: to, Subject: subject, HTML: htmlBody, Plain: plain})
}
