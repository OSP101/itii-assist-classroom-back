package handlers

import (
	"net/mail"
	"strings"
	"sync"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// ทดสอบการส่งอีเมล (admin)
// =============================================================================

// กันยิงทดสอบรัว ๆ: ต่อ admin 1 คน ส่งได้ไม่เกิน emailTestLimit ครั้งต่อ emailTestWindow
const (
	emailTestLimit  = 10
	emailTestWindow = 10 * time.Minute
)

var emailTestLimiter = struct {
	sync.Mutex
	sent map[uint][]time.Time
}{sent: map[uint][]time.Time{}}

func emailTestAllowed(userID uint) (bool, int) {
	emailTestLimiter.Lock()
	defer emailTestLimiter.Unlock()
	now := time.Now()
	kept := make([]time.Time, 0, emailTestLimit)
	for _, t := range emailTestLimiter.sent[userID] {
		if now.Sub(t) < emailTestWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= emailTestLimit {
		emailTestLimiter.sent[userID] = kept
		return false, 0
	}
	kept = append(kept, now)
	emailTestLimiter.sent[userID] = kept
	return true, emailTestLimit - len(kept)
}

// GET /api/system-settings/email
func GetEmailConfigHandler(c fiber.Ctx) error {
	summary := services.GetEmailConfigSummary()
	defaultTo := ""
	if userID, ok := c.Locals("user_id").(uint); ok {
		var user models.User
		if err := config.DB.Select("email").First(&user, userID).Error; err == nil {
			defaultTo = user.Email
		}
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
		"config":     summary,
		"templates":  services.EmailTestTemplates,
		"default_to": defaultTo,
		"limit":      fiber.Map{"max": emailTestLimit, "window_minutes": int(emailTestWindow.Minutes())},
	}})
}

// POST /api/system-settings/email/test  body: {to, template}
func SendTestEmailHandler(c fiber.Ctx) error {
	var input struct {
		To       string `json:"to"`
		Template string `json:"template"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	to := strings.TrimSpace(input.To)
	if _, err := mail.ParseAddress(to); err != nil || to == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รูปแบบอีเมลผู้รับไม่ถูกต้อง"})
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = "plain"
	}
	if !services.IsValidEmailTestTemplate(template) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่รู้จักแม่แบบอีเมลนี้"})
	}

	userID, _ := c.Locals("user_id").(uint)
	allowed, remaining := emailTestAllowed(userID)
	if !allowed {
		return c.Status(429).JSON(fiber.Map{"success": false, "message": "ส่งทดสอบถี่เกินไป กรุณารอสักครู่แล้วลองใหม่"})
	}

	requestedBy := "admin"
	var actor models.User
	if err := config.DB.Select("username, full_name, email").First(&actor, userID).Error; err == nil {
		requestedBy = actor.Username
		if strings.TrimSpace(actor.FullName) != "" {
			requestedBy = actor.FullName + " (" + actor.Username + ")"
		}
	}

	elapsed, err := services.SendTestEmail(template, to, requestedBy)
	summary := services.GetEmailConfigSummary()
	detail := fiber.Map{
		"to":         to,
		"template":   template,
		"provider":   summary.Provider,
		"elapsed_ms": elapsed.Milliseconds(),
	}
	if err != nil {
		detail["error"] = err.Error()
		logPrivilegedAdminAction(c, userID, "email_test_failed", "warn", "system_settings", "email", detail)
		services.LogEmailDeliveryError("admin_email_test", err)
		return c.Status(502).JSON(fiber.Map{
			"success": false,
			"message": "ส่งอีเมลไม่สำเร็จ",
			"data": fiber.Map{
				"error":      err.Error(),
				"provider":   summary.Provider,
				"elapsed_ms": elapsed.Milliseconds(),
				"remaining":  remaining,
			},
		})
	}
	logPrivilegedAdminAction(c, userID, "email_test_sent", "info", "system_settings", "email", detail)
	return c.JSON(fiber.Map{
		"success": true,
		"message": "ส่งอีเมลทดสอบแล้ว กรุณาตรวจสอบกล่องจดหมาย (รวมถึงโฟลเดอร์สแปม)",
		"data": fiber.Map{
			"to":         to,
			"template":   template,
			"provider":   summary.Provider,
			"from":       summary.From,
			"elapsed_ms": elapsed.Milliseconds(),
			"remaining":  remaining,
			"sent_at":    time.Now(),
		},
	})
}
