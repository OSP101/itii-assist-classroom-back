package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"itii-assist/models"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type emailMessage struct {
	To      string
	Subject string
	HTML    string
	Plain   string
}

type emailConfig struct {
	Provider   string
	From       string
	AppName    string
	Frontend   string
	ResendKey  string
	SMTPHost   string
	SMTPPort   int
	SMTPSecure bool
	SMTPUser   string
	SMTPPass   string
	// SMTPTLSSkipVerify ข้ามการตรวจใบรับรอง (relay ภายในที่ใช้ self-signed)
	SMTPTLSSkipVerify bool
	// SMTPTLSMinVersion เช่น tls.VersionTLS10 สำหรับ relay เก่าที่ยังไม่รองรับ TLS 1.2
	SMTPTLSMinVersion uint16
	// SMTPStartTLSOpportunistic = ถ้าเซิร์ฟเวอร์ไม่มี STARTTLS ให้ส่งแบบไม่เข้ารหัสแทนที่จะล้มเหลว
	SMTPStartTLSOpportunistic bool
	// SMTPTLSLegacyCiphers เปิด cipher แบบ RSA key exchange (ไม่มี forward secrecy) ที่ Go 1.22+
	// ตัดออกจากค่าเริ่มต้น relay ของ มข. (smtp.kku.ac.th) รับเฉพาะแบบนี้
	SMTPTLSLegacyCiphers bool
}

func smtpTLSConfig(cfg emailConfig) *tls.Config {
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, InsecureSkipVerify: cfg.SMTPTLSSkipVerify} //nolint:gosec // opt-in ผ่าน env สำหรับ relay ภายใน
	if cfg.SMTPTLSMinVersion != 0 {
		tlsConfig.MinVersion = cfg.SMTPTLSMinVersion
	}
	if cfg.SMTPTLSLegacyCiphers {
		tlsConfig.CipherSuites = legacyCompatibleCipherSuites()
	}
	return tlsConfig
}

// legacyCompatibleCipherSuites คืน cipher ทั้งหมดที่ Go รู้จัก (รวม TLS_RSA_* ที่ถูกปิดเป็นค่าเริ่มต้น)
// เรียงให้ตัวที่ปลอดภัยกว่ามาก่อน เซิร์ฟเวอร์ที่รองรับ ECDHE จะยังได้ forward secrecy
func legacyCompatibleCipherSuites() []uint16 {
	ids := make([]uint16, 0, 32)
	for _, suite := range tls.CipherSuites() {
		ids = append(ids, suite.ID)
	}
	for _, suite := range tls.InsecureCipherSuites() {
		ids = append(ids, suite.ID)
	}
	return ids
}

// isTLSHandshakeFailure = เซิร์ฟเวอร์ตอบ alert handshake_failure (ตกลง cipher/เวอร์ชันกันไม่ได้)
func isTLSHandshakeFailure(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "handshake failure")
}

func parseTLSMinVersion(raw string) uint16 {
	switch strings.TrimSpace(raw) {
	case "1.0", "10", "tls1.0":
		return tls.VersionTLS10
	case "1.1", "11", "tls1.1":
		return tls.VersionTLS11
	case "1.2", "12", "tls1.2":
		return tls.VersionTLS12
	case "1.3", "13", "tls1.3":
		return tls.VersionTLS13
	}
	return 0
}

func loadEmailConfig() emailConfig {
	resendKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	provider := strings.TrimSpace(strings.ToLower(os.Getenv("EMAIL_PROVIDER")))
	if provider == "" {
		if resendKey != "" {
			provider = "resend"
		} else {
			provider = "smtp"
		}
	}

	from := strings.TrimSpace(os.Getenv("EMAIL_FROM"))
	if from == "" {
		from = strings.TrimSpace(os.Getenv("SMTP_FROM"))
	}
	if from == "" {
		from = "ITII Assist Classroom <noreply@localhost>"
	}

	appName := strings.TrimSpace(os.Getenv("EMAIL_APP_NAME"))
	if appName == "" {
		appName = strings.TrimSpace(os.Getenv("TWO_FACTOR_APP_NAME"))
	}
	if appName == "" {
		appName = "ITII Assist Classroom"
	}

	frontendURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_URL")), "/")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	port := 587
	if rawPort := strings.TrimSpace(os.Getenv("SMTP_PORT")); rawPort != "" {
		if parsed, err := strconv.Atoi(rawPort); err == nil && parsed > 0 {
			port = parsed
		}
	}

	return emailConfig{
		Provider:   provider,
		From:       from,
		AppName:    appName,
		Frontend:   frontendURL,
		ResendKey:  resendKey,
		SMTPHost:   strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:   port,
		SMTPSecure: strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_SECURE")), "true"),
		SMTPUser:   strings.TrimSpace(os.Getenv("SMTP_USER")),
		SMTPPass:   os.Getenv("SMTP_PASS"),

		SMTPTLSSkipVerify:         strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_TLS_SKIP_VERIFY")), "true"),
		SMTPTLSMinVersion:         parseTLSMinVersion(os.Getenv("SMTP_TLS_MIN_VERSION")),
		SMTPStartTLSOpportunistic: strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_STARTTLS_OPPORTUNISTIC")), "true"),
		SMTPTLSLegacyCiphers:      strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_TLS_LEGACY_CIPHERS")), "true"),
	}
}

func PasswordResetURL(token string) string {
	cfg := loadEmailConfig()
	resetURL, err := url.Parse(cfg.Frontend + "/auth/reset-password")
	if err != nil {
		return cfg.Frontend + "/auth/reset-password?token=" + url.QueryEscape(token)
	}
	query := resetURL.Query()
	query.Set("token", token)
	resetURL.RawQuery = query.Encode()
	return resetURL.String()
}

func CourseApprovalURL(courseID string) string {
	cfg := loadEmailConfig()
	return strings.TrimRight(cfg.Frontend, "/") + "/classroom/" + courseID + "/approval"
}

func SendScoreEditRequestSubmittedEmail(user *models.User, courseName, assignmentName, requesterName, reason, link string) error {
	if user == nil || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("score edit request email requires a recipient")
	}

	cfg := loadEmailConfig()
	displayName := displayNameForEmail(user)
	subject := fmt.Sprintf("[%s] มีคำขอแก้ไขคะแนนใหม่: %s", cfg.AppName, assignmentName)

	reasonBlock := ""
	if strings.TrimSpace(reason) != "" {
		reasonBlock = fmt.Sprintf(`
      <div style="margin: 0 0 20px; padding: 18px; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; white-space: pre-wrap; line-height: 1.7; color: #334155;">%s</div>`, html.EscapeString(strings.TrimSpace(reason)))
	}

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #1d4ed8, #0f766e); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">คำขอแก้ไขคะแนนใหม่</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">
        %s ส่งคำขอแก้ไขคะแนนของงาน "%s" ในวิชา %s กรุณาเข้าไปตรวจสอบและพิจารณาอนุมัติ
      </p>%s
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          เปิดหน้ารายการอนุมัติ
        </a>
      </p>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(displayName), html.EscapeString(requesterName), html.EscapeString(assignmentName), html.EscapeString(courseName), reasonBlock, html.EscapeString(link))

	plainBody := fmt.Sprintf(
		"%s\n\nสวัสดีคุณ%s,\n\n%s ส่งคำขอแก้ไขคะแนนของงาน \"%s\" ในวิชา %s กรุณาเข้าไปตรวจสอบและพิจารณาอนุมัติ:\n%s\n",
		cfg.AppName,
		displayName,
		requesterName,
		assignmentName,
		courseName,
		link,
	)

	return sendEmail(emailMessage{
		To:      strings.TrimSpace(user.Email),
		Subject: subject,
		HTML:    htmlBody,
		Plain:   plainBody,
	})
}

func SendScoreEditRequestReviewedEmail(user *models.User, approved bool, assignmentName, comment string, count int, link string) error {
	if user == nil || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("score edit request result email requires a recipient")
	}

	cfg := loadEmailConfig()
	displayName := displayNameForEmail(user)

	resultText := "ได้รับการอนุมัติ"
	headerColor := "linear-gradient(135deg, #0f766e, #1d4ed8)"
	if !approved {
		resultText = "ถูกปฏิเสธ"
		headerColor = "linear-gradient(135deg, #b91c1c, #ea580c)"
	}

	countText := ""
	if count > 1 {
		countText = fmt.Sprintf(" (%d รายการ)", count)
	}

	subject := fmt.Sprintf("[%s] คำขอแก้ไขคะแนน%s: %s", cfg.AppName, resultText, assignmentName)

	commentBlock := ""
	if strings.TrimSpace(comment) != "" {
		commentBlock = fmt.Sprintf(`
      <div style="margin: 0 0 20px; padding: 18px; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; white-space: pre-wrap; line-height: 1.7; color: #334155;">
        <div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px; margin-bottom: 8px;">ความเห็นของผู้ตรวจสอบ</div>
        %s
      </div>`, html.EscapeString(strings.TrimSpace(comment)))
	}

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: %s; color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">คำขอแก้ไขคะแนน%s</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">
        คำขอแก้ไขคะแนนของงาน "%s" ที่คุณส่งมา%s%s แล้ว
      </p>%s
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          เปิดหน้ารายการอนุมัติ
        </a>
      </p>
    </div>
  </div>
</div>`, headerColor, html.EscapeString(resultText), html.EscapeString(cfg.AppName), html.EscapeString(displayName), html.EscapeString(assignmentName), html.EscapeString(countText), html.EscapeString(resultText), commentBlock, html.EscapeString(link))

	plainBody := fmt.Sprintf(
		"%s\n\nสวัสดีคุณ%s,\n\nคำขอแก้ไขคะแนนของงาน \"%s\" ที่คุณส่งมา%s%s แล้ว\n%s\n",
		cfg.AppName,
		displayName,
		assignmentName,
		countText,
		resultText,
		link,
	)

	return sendEmail(emailMessage{
		To:      strings.TrimSpace(user.Email),
		Subject: subject,
		HTML:    htmlBody,
		Plain:   plainBody,
	})
}

func SendSystemAnnouncementEmail(user *models.User, announcement *models.SystemAnnouncement) error {
	if user == nil || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("announcement email requires a recipient")
	}
	if announcement == nil {
		return fmt.Errorf("announcement email requires an announcement")
	}

	cfg := loadEmailConfig()
	displayName := displayNameForEmail(user)

	title := strings.TrimSpace(announcement.TitleTH)
	if title == "" {
		title = strings.TrimSpace(announcement.Title)
	}
	message := strings.TrimSpace(announcement.MessageTH)
	if message == "" {
		message = strings.TrimSpace(announcement.Message)
	}

	subject := fmt.Sprintf("[%s] ประกาศ: %s", cfg.AppName, title)

	actionBlock := ""
	plainAction := ""
	actionURL := strings.TrimSpace(announcement.ActionURL)
	if actionURL != "" {
		actionLabel := strings.TrimSpace(announcement.ActionLabelTH)
		if actionLabel == "" {
			actionLabel = strings.TrimSpace(announcement.ActionLabel)
		}
		if actionLabel == "" {
			actionLabel = "ดูรายละเอียด"
		}
		actionBlock = fmt.Sprintf(`
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          %s
        </a>
      </p>`, html.EscapeString(actionURL), html.EscapeString(actionLabel))
		plainAction = fmt.Sprintf("\n%s: %s\n", actionLabel, actionURL)
	}

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #1d4ed8, #0f766e); color: #ffffff;">
      <p style="margin: 0; font-size: 12px; letter-spacing: 2px; text-transform: uppercase; opacity: 0.85;">ประกาศจากระบบ</p>
      <h1 style="margin: 10px 0 0; font-size: 24px;">%s</h1>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <div style="margin: 0 0 20px; color: #475569; line-height: 1.7; white-space: pre-wrap;">%s</div>%s
    </div>
  </div>
</div>`, html.EscapeString(title), html.EscapeString(displayName), html.EscapeString(message), actionBlock)

	plainBody := fmt.Sprintf(
		"%s\n\nสวัสดีคุณ%s,\n\n%s\n%s",
		cfg.AppName,
		displayName,
		message,
		plainAction,
	)

	return sendEmail(emailMessage{
		To:      strings.TrimSpace(user.Email),
		Subject: subject,
		HTML:    htmlBody,
		Plain:   plainBody,
	})
}

func SendPasswordResetEmail(user *models.User, token string) error {
	if user == nil || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("password reset email requires a recipient")
	}

	cfg := loadEmailConfig()
	displayName := displayNameForEmail(user)
	resetURL := PasswordResetURL(token)
	subject := fmt.Sprintf("[%s] รีเซ็ตรหัสผ่านของคุณ", cfg.AppName)

	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #1d4ed8, #0f766e); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">รีเซ็ตรหัสผ่าน</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">
        มีการร้องขอให้รีเซ็ตรหัสผ่านสำหรับบัญชีของคุณ หากคุณเป็นผู้ดำเนินการเอง กรุณากดปุ่มด้านล่างภายใน 1 ชั่วโมง
      </p>
      <p style="margin: 28px 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          รีเซ็ตรหัสผ่าน
        </a>
      </p>
      <p style="margin: 0 0 12px; color: #64748b; line-height: 1.7;">
        หากปุ่มใช้งานไม่ได้ คุณสามารถเปิดลิงก์นี้ในเบราว์เซอร์:
      </p>
      <p style="margin: 0; word-break: break-all; color: #0f766e;">%s</p>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(displayName), html.EscapeString(resetURL), html.EscapeString(resetURL))

	plainBody := fmt.Sprintf(
		"%s\n\nสวัสดีคุณ%s,\n\nมีการร้องขอให้รีเซ็ตรหัสผ่านสำหรับบัญชีของคุณ หากคุณเป็นผู้ดำเนินการเอง กรุณาเปิดลิงก์นี้ภายใน 1 ชั่วโมง:\n%s\n",
		cfg.AppName,
		displayName,
		resetURL,
	)

	return sendEmail(emailMessage{
		To:      strings.TrimSpace(user.Email),
		Subject: subject,
		HTML:    htmlBody,
		Plain:   plainBody,
	})
}

func SendTwoFactorCodeEmail(user *models.User, code string, purpose string) error {
	if user == nil || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("2fa email requires a recipient")
	}

	cfg := loadEmailConfig()
	displayName := displayNameForEmail(user)
	purposeText := "ยืนยันตัวตน"
	switch strings.TrimSpace(purpose) {
	case "setup":
		purposeText = "เปิดใช้งานการยืนยันตัวตนสองขั้นตอน"
	case "login":
		purposeText = "เข้าสู่ระบบ"
	}

	subject := fmt.Sprintf("[%s] รหัสยืนยัน %s", cfg.AppName, purposeText)
	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 520px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 32px; background: linear-gradient(135deg, #0f766e, #1d4ed8); color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px;">รหัสยืนยัน</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">%s</p>
    </div>
    <div style="padding: 32px;">
      <p style="margin: 0 0 16px; color: #0f172a;">สวัสดีคุณ%s,</p>
      <p style="margin: 0 0 24px; color: #475569; line-height: 1.7;">
        ใช้รหัสนี้เพื่อ%s รหัสมีอายุ 5 นาที และใช้ได้เพียงครั้งเดียว
      </p>
      <div style="margin: 0 0 24px; padding: 20px; border-radius: 16px; background: #ecfeff; border: 2px solid #22d3ee; text-align: center;">
        <div style="font-size: 12px; color: #0f766e; letter-spacing: 2px; text-transform: uppercase; font-weight: 700;">Verification Code</div>
        <div style="margin-top: 10px; font-size: 38px; letter-spacing: 10px; font-weight: 800; color: #0f172a;">%s</div>
      </div>
      <p style="margin: 0; color: #64748b; line-height: 1.7;">
        หากคุณไม่ได้เป็นผู้ร้องขอ กรุณาเปลี่ยนรหัสผ่านและตรวจสอบความปลอดภัยของบัญชีทันที
      </p>
    </div>
  </div>
</div>`, html.EscapeString(cfg.AppName), html.EscapeString(displayName), html.EscapeString(purposeText), html.EscapeString(code))

	plainBody := fmt.Sprintf(
		"%s\n\nสวัสดีคุณ%s,\n\nรหัสยืนยันสำหรับ%sของคุณคือ %s\nรหัสมีอายุ 5 นาที และใช้ได้เพียงครั้งเดียว\n",
		cfg.AppName,
		displayName,
		purposeText,
		code,
	)

	return sendEmail(emailMessage{
		To:      strings.TrimSpace(user.Email),
		Subject: subject,
		HTML:    htmlBody,
		Plain:   plainBody,
	})
}

func SendSupportTicketAlert(feedback *models.Feedback) error {
	if feedback == nil {
		return fmt.Errorf("support alert requires feedback")
	}

	recipients := supportAlertRecipients()
	if len(recipients) == 0 {
		return nil
	}

	cfg := loadEmailConfig()
	adminURL := strings.TrimRight(cfg.Frontend, "/") + "/admin/feedback?type=support"
	createdAt := feedback.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	priorityLabel := strings.ToUpper(strings.TrimSpace(feedback.Priority))
	if priorityLabel == "" {
		priorityLabel = "MEDIUM"
	}

	contactEmail := strings.TrimSpace(feedback.ContactEmail)
	if contactEmail == "" {
		contactEmail = "ไม่ระบุ"
	}

	subject := fmt.Sprintf("[%s] Support ticket #%d (%s)", cfg.AppName, feedback.ID, priorityLabel)
	htmlBody := fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: #f3f6fb; padding: 32px 16px;">
  <div style="max-width: 640px; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 28px 32px; background: linear-gradient(135deg, #0f766e, #1d4ed8); color: #ffffff;">
      <p style="margin: 0; font-size: 12px; letter-spacing: 2px; text-transform: uppercase; opacity: 0.85;">Support Ticket Alert</p>
      <h1 style="margin: 10px 0 0; font-size: 24px;">%s</h1>
      <p style="margin: 12px 0 0; opacity: 0.92;">Ticket #%d • Priority %s</p>
    </div>
    <div style="padding: 32px; color: #0f172a;">
      <div style="display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-bottom: 24px;">
        <div style="padding: 16px; border-radius: 14px; background: #f8fafc; border: 1px solid #e2e8f0;">
          <div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px;">Contact Email</div>
          <div style="margin-top: 6px; font-size: 15px; font-weight: 600;">%s</div>
        </div>
        <div style="padding: 16px; border-radius: 14px; background: #f8fafc; border: 1px solid #e2e8f0;">
          <div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px;">Created At</div>
          <div style="margin-top: 6px; font-size: 15px; font-weight: 600;">%s</div>
        </div>
      </div>
      <div style="margin-bottom: 16px; font-size: 13px; color: #64748b; text-transform: uppercase; letter-spacing: 1px;">รายละเอียดคำขอ</div>
      <div style="padding: 18px; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; white-space: pre-wrap; line-height: 1.7; color: #334155;">%s</div>
      <p style="margin: 28px 0 0;">
        <a href="%s" style="display: inline-block; background: #1d4ed8; color: #ffffff; text-decoration: none; padding: 14px 22px; border-radius: 12px; font-weight: 600;">
          เปิดหน้า Feedback Admin
        </a>
      </p>
    </div>
  </div>
</div>`, html.EscapeString(feedback.Title), feedback.ID, html.EscapeString(priorityLabel), html.EscapeString(contactEmail), html.EscapeString(createdAt.Format("2006-01-02 15:04:05 MST")), html.EscapeString(strings.TrimSpace(feedback.Description)), html.EscapeString(adminURL))

	plainBody := fmt.Sprintf(
		"Support ticket #%d\nหัวข้อ: %s\nPriority: %s\nContact: %s\nCreated At: %s\n\nรายละเอียด:\n%s\n\nเปิดในระบบ: %s\n",
		feedback.ID,
		strings.TrimSpace(feedback.Title),
		priorityLabel,
		contactEmail,
		createdAt.Format("2006-01-02 15:04:05 MST"),
		strings.TrimSpace(feedback.Description),
		adminURL,
	)

	for _, recipient := range recipients {
		if err := sendEmail(emailMessage{
			To:      recipient,
			Subject: subject,
			HTML:    htmlBody,
			Plain:   plainBody,
		}); err != nil {
			return err
		}
	}

	return nil
}

func sendEmail(message emailMessage) error {
	cfg := loadEmailConfig()
	switch cfg.Provider {
	case "resend":
		return sendWithResend(cfg, message)
	case "smtp":
		return sendWithSMTP(cfg, message)
	default:
		return fmt.Errorf("unsupported email provider %q", cfg.Provider)
	}
}

func sendWithResend(cfg emailConfig, message emailMessage) error {
	if cfg.ResendKey == "" {
		return fmt.Errorf("RESEND_API_KEY is not configured")
	}

	payload := map[string]any{
		"from":    cfg.From,
		"to":      []string{message.To},
		"subject": message.Subject,
		"html":    message.HTML,
		"text":    message.Plain,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.ResendKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("resend returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	return nil
}

// sendWithSMTP ส่งผ่าน SMTP ถ้า TLS handshake ถูกปฏิเสธและยังไม่ได้เปิด legacy ciphers
// จะลองใหม่อีกครั้งด้วย cipher ชุดเต็ม (relay ของ มข. ต้องการแบบนี้) แล้ว log ให้รู้ว่าควรตั้ง env ถาวร
func sendWithSMTP(cfg emailConfig, message emailMessage) error {
	err := sendWithSMTPOnce(cfg, message)
	if err == nil || cfg.SMTPTLSLegacyCiphers || !isTLSHandshakeFailure(err) {
		return err
	}
	log.Printf("event=smtp_tls_handshake_failure host=%s retrying with legacy RSA cipher suites; set SMTP_TLS_LEGACY_CIPHERS=true to skip the failed first attempt", cfg.SMTPHost)
	cfg.SMTPTLSLegacyCiphers = true
	if retryErr := sendWithSMTPOnce(cfg, message); retryErr != nil {
		return fmt.Errorf("%w; retry with legacy ciphers also failed: %v", err, retryErr)
	}
	return nil
}

func sendWithSMTPOnce(cfg emailConfig, message emailMessage) error {
	if cfg.SMTPHost == "" {
		return fmt.Errorf("SMTP configuration is incomplete")
	}
	if (cfg.SMTPUser == "") != (cfg.SMTPPass == "") {
		return fmt.Errorf("SMTP_USER and SMTP_PASS must be set together")
	}

	fromAddress := extractEmailAddress(cfg.From)
	recipients := []string{message.To}
	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))

	// Relays that trust the sender's IP (e.g. an allowlisted VM) need no
	// credentials at all, so auth is only attempted when both are set.
	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}

	mime := buildMIMEMessage(fromAddress, recipients, message)

	// Port 465 is implicit TLS (encrypt before talking SMTP); everything
	// else (587, 25, ...) is plaintext-then-STARTTLS.
	if cfg.SMTPSecure && cfg.SMTPPort == 465 {
		dialer := &net.Dialer{Timeout: 15 * time.Second}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, smtpTLSConfig(cfg))
		if err != nil {
			return fmt.Errorf("implicit TLS to %s failed: %w (%s)", addr, err, smtpTLSHint(err))
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("smtp greeting on %s failed: %w", addr, err)
		}
		defer client.Close()

		return deliverSMTP(client, auth, fromAddress, recipients, mime)
	}

	rawConn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("tcp connect to %s failed: %w", addr, err)
	}
	client, err := smtp.NewClient(rawConn, cfg.SMTPHost)
	if err != nil {
		rawConn.Close()
		return fmt.Errorf("smtp greeting on %s failed: %w", addr, err)
	}
	defer client.Close()

	if cfg.SMTPSecure {
		ok, _ := client.Extension("STARTTLS")
		switch {
		case ok:
			if err := client.StartTLS(smtpTLSConfig(cfg)); err != nil {
				return fmt.Errorf("STARTTLS on %s failed: %w (%s)", addr, err, smtpTLSHint(err))
			}
		case cfg.SMTPStartTLSOpportunistic:
			log.Printf("event=smtp_starttls_unavailable addr=%s sending in plaintext (SMTP_STARTTLS_OPPORTUNISTIC=true)", addr)
		default:
			return fmt.Errorf("smtp server %s does not advertise STARTTLS; set SMTP_SECURE=false for a plaintext relay or SMTP_STARTTLS_OPPORTUNISTIC=true", addr)
		}
	}

	return deliverSMTP(client, auth, fromAddress, recipients, mime)
}

// smtpTLSHint แปล error TLS ที่พบบ่อยเป็นคำแนะนำสั้น ๆ
func smtpTLSHint(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "handshake failure"):
		return "server rejected the TLS handshake: usually a relay that only accepts RSA key-exchange ciphers (set SMTP_TLS_LEGACY_CIPHERS=true, this is the case for smtp.kku.ac.th), a port/mode mismatch (465 = implicit TLS, 587/25 = STARTTLS), or a relay that needs a client certificate"
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "certificate could not be verified: the relay may use a self-signed cert or SMTP_HOST does not match the cert name (try SMTP_TLS_SKIP_VERIFY=true for an internal relay)"
	case strings.Contains(msg, "first record does not look like a tls handshake"):
		return "the server answered in plaintext: it does not speak implicit TLS on this port, use port 587/25 with STARTTLS"
	case strings.Contains(msg, "protocol version"):
		return "TLS version mismatch (try SMTP_TLS_MIN_VERSION=1.0)"
	}
	return "check SMTP_HOST/SMTP_PORT/SMTP_SECURE"
}

func deliverSMTP(client *smtp.Client, auth smtp.Auth, from string, recipients []string, mime []byte) error {
	if auth != nil {
		ok, _ := client.Extension("AUTH")
		if !ok {
			return fmt.Errorf("smtp server does not support AUTH but SMTP_USER/SMTP_PASS were provided")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(mime); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}

func buildMIMEMessage(from string, recipients []string, message emailMessage) []byte {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("From: %s\r\n", from))
	builder.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(recipients, ", ")))
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", message.Subject))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(message.HTML)
	return []byte(builder.String())
}

func displayNameForEmail(user *models.User) string {
	for _, candidate := range []string{user.FullName, user.Username, user.Email} {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return "ผู้ใช้งาน"
}

func extractEmailAddress(from string) string {
	start := strings.LastIndex(from, "<")
	end := strings.LastIndex(from, ">")
	if start >= 0 && end > start {
		address := strings.TrimSpace(from[start+1 : end])
		if address != "" {
			return address
		}
	}
	return strings.TrimSpace(from)
}

func supportAlertRecipients() []string {
	rawRecipients := strings.TrimSpace(os.Getenv("SUPPORT_ALERT_EMAILS"))
	if rawRecipients == "" {
		return nil
	}

	parts := strings.FieldsFunc(rawRecipients, func(r rune) bool {
		return r == ',' || r == ';'
	})

	recipients := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		recipients = append(recipients, trimmed)
	}

	return recipients
}

func LogEmailDeliveryError(context string, err error) {
	if err != nil {
		log.Printf("[email] %s: %v", context, err)
	}
}
