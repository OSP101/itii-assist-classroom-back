package services

import (
	"fmt"
	"html"
	"os"
	"strings"
	"time"
)

// =============================================================================
// โครงอีเมลกลาง: แถบชื่อระบบ + หัวเรื่อง + เลขอ้างอิง + เนื้อหา + ท้ายเมล
// ทุกแม่แบบต้อง render ผ่านตัวนี้ เพื่อให้ผู้รับรู้ว่ามาจากระบบไหน
// ตรวจสอบเลขอ้างอิงได้ และเห็นว่าเป็นเมลอัตโนมัติที่ตอบกลับไม่ได้
// =============================================================================

type emailContent struct {
	// Section คือชื่อส่วนของระบบที่ส่ง เช่น "ระบบเช็กชื่อ · คำขอลา"
	Section string
	// Title หัวเรื่องใหญ่ในแถบสี
	Title string
	// Subtitle บรรทัดใต้หัวเรื่อง (ว่างได้)
	Subtitle string
	// Gradient CSS background ของแถบหัว (ว่าง = ค่าเริ่มต้น)
	Gradient string
	// Reference เลขอ้างอิง เช่น "LR-123" แสดงเป็นป้ายในแถบหัวและท้ายเมล (ว่างได้)
	Reference string
	// BodyHTML เนื้อหาส่วนกลาง (escape มาแล้ว)
	BodyHTML string
	// MaxWidth ความกว้างการ์ด (0 = 560)
	MaxWidth int
}

// สีตามธีมของระบบ (ตรงกับ --cg-* ใน app/campus-glass.css และไอคอนแอป)
const (
	emailThemeAccent       = "#2b7fff"
	emailThemeAccentStrong = "#1d4ed8"
	emailThemeViolet       = "#6d5bf0"
	emailThemeSuccess      = "#15803d"
	emailThemeWarning      = "#b45309"
	emailThemeDanger       = "#b91c1c"
	emailThemeText         = "#0f172a"
	emailThemeText2        = "#64748b"
	emailThemeText3        = "#94a3b8"
	emailThemeBg           = "#f4f7fb"
	emailThemeSurface      = "#ffffff"
	emailThemeFill         = "#f1f5f9"
	emailThemeLine         = "#e2e8f0"
	emailThemeDark         = "#0f172a"
)

// gradient ของแถบหัวตามผลลัพธ์ ใช้ชุดเดียวกันทุกแม่แบบ
const (
	emailGradientAccent  = "linear-gradient(135deg, " + emailThemeAccent + ", " + emailThemeViolet + ")"
	emailGradientSuccess = "linear-gradient(135deg, " + emailThemeSuccess + ", " + emailThemeAccent + ")"
	emailGradientWarning = "linear-gradient(135deg, " + emailThemeWarning + ", " + emailThemeAccent + ")"
	emailGradientDanger  = "linear-gradient(135deg, " + emailThemeDanger + ", #ea580c)"
)

const emailDefaultGradient = emailGradientAccent

func emailLogoURL() string {
	return strings.TrimSpace(os.Getenv("EMAIL_LOGO_URL"))
}

func emailContactLine() string {
	return strings.TrimSpace(os.Getenv("EMAIL_CONTACT"))
}

// emailReference สร้างเลขอ้างอิงแบบมีคำนำหน้า เช่น LR-123
func emailReference(prefix string, id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%s-%d", prefix, id)
}

func renderEmailHTML(c emailContent) string {
	cfg := loadEmailConfig()
	maxWidth := c.MaxWidth
	if maxWidth == 0 {
		maxWidth = 560
	}
	gradient := c.Gradient
	if gradient == "" {
		gradient = emailDefaultGradient
	}

	logo := ""
	if url := emailLogoURL(); url != "" {
		logo = fmt.Sprintf(`<img src="%s" alt="" width="36" height="36" style="width: 36px; height: 36px; border-radius: 10px; vertical-align: middle; margin-right: 10px;" />`, html.EscapeString(url))
	}
	section := c.Section
	if strings.TrimSpace(section) == "" {
		section = "แจ้งเตือนจากระบบ"
	}
	subtitle := ""
	if strings.TrimSpace(c.Subtitle) != "" {
		subtitle = fmt.Sprintf(`<p style="margin: 10px 0 0; opacity: 0.92; font-size: 14px;">%s</p>`, html.EscapeString(c.Subtitle))
	}
	referenceChip := ""
	referenceFooter := ""
	if strings.TrimSpace(c.Reference) != "" {
		referenceChip = fmt.Sprintf(`<span style="display: inline-block; margin-top: 14px; padding: 5px 12px; border-radius: 999px; background: rgba(255,255,255,0.18); font-size: 12px; font-family: Consolas, Menlo, monospace; letter-spacing: 0.5px;">เลขอ้างอิง %s</span>`, html.EscapeString(c.Reference))
		referenceFooter = fmt.Sprintf(`<p style="margin: 0 0 6px;">เลขอ้างอิง <span style="font-family: Consolas, Menlo, monospace;">%s</span> ใช้แจ้งผู้สอนหรือผู้ดูแลเมื่อต้องการสอบถามเรื่องนี้</p>`, html.EscapeString(c.Reference))
	}
	contact := ""
	if line := emailContactLine(); line != "" {
		contact = fmt.Sprintf(`<p style="margin: 0 0 6px;">ติดต่อผู้ดูแลระบบ: %s</p>`, html.EscapeString(line))
	}

	return fmt.Sprintf(`
<div style="font-family: 'Segoe UI', Tahoma, sans-serif; background: %s; padding: 32px 16px;">
  <div style="max-width: %dpx; margin: 0 auto; background: #ffffff; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08);">
    <div style="padding: 14px 32px; background: %s; color: #e2e8f0; font-size: 13px;">
      %s<span style="font-weight: 700; color: #ffffff; letter-spacing: 0.3px;">%s</span>
      <span style="opacity: 0.65;"> · %s</span>
    </div>
    <div style="padding: 28px 32px; background: %s; color: #ffffff;">
      <h1 style="margin: 0; font-size: 24px; line-height: 1.3;">%s</h1>
      %s
      %s
    </div>
    <div style="padding: 32px;">
      %s
    </div>
    <div style="padding: 20px 32px 24px; background: %s; border-top: 1px solid %s; font-size: 12px; line-height: 1.7; color: %s; text-align: center;">
      %s
      <p style="margin: 0 0 6px;"><strong style="color: %s;">อีเมลฉบับนี้เป็นการส่งจากระบบอัตโนมัติ กรุณาอย่าตอบกลับอีเมลนี้</strong></p>
      %s
      <p style="margin: 0;">© %d %s - College of Computing | Developed by ITII Development Team.</p>
    </div>
  </div>
</div>`,
		emailThemeBg,
		maxWidth,
		emailThemeDark,
		logo, html.EscapeString(cfg.AppName), html.EscapeString(section),
		gradient,
		html.EscapeString(c.Title),
		subtitle,
		referenceChip,
		c.BodyHTML,
		emailThemeFill, emailThemeLine, emailThemeText2,
		referenceFooter,
		emailThemeText2,
		contact,
		time.Now().In(leaveEmailLocation()).Year(), html.EscapeString(cfg.AppName),
	)
}

// renderEmailPlain ประกอบข้อความธรรมดาให้มีหัว/ท้ายเหมือน HTML
func renderEmailPlain(c emailContent, body string) string {
	cfg := loadEmailConfig()
	var b strings.Builder
	b.WriteString(cfg.AppName)
	if strings.TrimSpace(c.Section) != "" {
		b.WriteString(" · " + c.Section)
	}
	b.WriteString("\n" + c.Title + "\n")
	if strings.TrimSpace(c.Subtitle) != "" {
		b.WriteString(c.Subtitle + "\n")
	}
	if strings.TrimSpace(c.Reference) != "" {
		b.WriteString("เลขอ้างอิง " + c.Reference + "\n")
	}
	b.WriteString("\n" + strings.TrimSpace(body) + "\n\n")
	b.WriteString("--\n")
	if strings.TrimSpace(c.Reference) != "" {
		b.WriteString("เลขอ้างอิง " + c.Reference + " ใช้แจ้งผู้สอนหรือผู้ดูแลเมื่อต้องการสอบถามเรื่องนี้\n")
	}
	b.WriteString("อีเมลฉบับนี้เป็นการส่งจากระบบอัตโนมัติ กรุณาอย่าตอบกลับอีเมลนี้\n")
	if line := emailContactLine(); line != "" {
		b.WriteString("ติดต่อผู้ดูแลระบบ: " + line + "\n")
	}
	b.WriteString(fmt.Sprintf("© %d %s - College of Computing | Developed by ITII Development Team.\n", time.Now().In(leaveEmailLocation()).Year(), cfg.AppName))
	return b.String()
}

func leaveEmailLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}

// ---- ชิ้นส่วนที่ใช้ซ้ำ ----

func emailGreeting(name string) string {
	return fmt.Sprintf(`<p style="margin: 0 0 16px; color: %s;">สวัสดีคุณ%s,</p>`, emailThemeText, html.EscapeString(name))
}

func emailParagraph(text string) string {
	return fmt.Sprintf(`<p style="margin: 0 0 20px; color: #475569; line-height: 1.7;">%s</p>`, text)
}

// emailButton ปุ่มหลัก จัดกึ่งกลาง สี accent ตามธีม
func emailButton(label, url string) string {
	return fmt.Sprintf(`<table role="presentation" style="width: 100%%; border-collapse: collapse; margin: 28px 0 8px;"><tr><td style="text-align: center;">
        <a href="%s" style="display: inline-block; background: %s; color: #ffffff; text-decoration: none; padding: 14px 28px; border-radius: 14px; font-weight: 600; font-size: 15px; box-shadow: 0 8px 22px -10px rgba(43, 127, 255, 0.85);">%s</a>
      </td></tr></table>`, html.EscapeString(url), emailThemeAccent, html.EscapeString(label))
}

func emailQuoteBlock(label, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	labelHTML := ""
	if label != "" {
		labelHTML = fmt.Sprintf(`<div style="font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 1px; margin-bottom: 8px;">%s</div>`, html.EscapeString(label))
	}
	return fmt.Sprintf(`<div style="margin: 0 0 20px; padding: 18px; border-radius: 16px; background: %s; border: 1px solid %s; white-space: pre-wrap; line-height: 1.7; color: #334155;">%s%s</div>`, emailThemeFill, emailThemeLine, labelHTML, html.EscapeString(strings.TrimSpace(text)))
}

func emailMuted(text string) string {
	return fmt.Sprintf(`<p style="margin: 0 0 12px; font-size: 13px; color: #64748b; line-height: 1.7;">%s</p>`, text)
}
