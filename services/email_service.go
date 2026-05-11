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

func sendWithSMTP(cfg emailConfig, message emailMessage) error {
	if cfg.SMTPHost == "" || cfg.SMTPUser == "" || cfg.SMTPPass == "" {
		return fmt.Errorf("SMTP configuration is incomplete")
	}

	fromAddress := extractEmailAddress(cfg.From)
	recipients := []string{message.To}
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	}

	mime := buildMIMEMessage(fromAddress, recipients, message)

	if cfg.SMTPSecure {
		tlsConfig := &tls.Config{ServerName: cfg.SMTPHost}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return err
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return err
		}
		defer client.Close()

		if auth != nil {
			if ok, _ := client.Extension("AUTH"); ok {
				if err := client.Auth(auth); err != nil {
					return err
				}
			}
		}

		if err := client.Mail(fromAddress); err != nil {
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

	return smtp.SendMail(addr, auth, fromAddress, recipients, mime)
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
