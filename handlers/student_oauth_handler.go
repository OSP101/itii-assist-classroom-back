package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/utils"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	studentSelectionCookie = "student_login_selection"
	studentSelectionPath   = "/api/auth/student-selection"
	studentSelectionTTL    = 5 * time.Minute
)

func writeStudentSelectionCookie(c fiber.Ctx, token string, maxAge int) {
	c.Cookie(&fiber.Cookie{
		Name: studentSelectionCookie, Value: token, MaxAge: maxAge,
		Path: studentSelectionPath, HTTPOnly: true, SameSite: "Lax",
		Secure: strings.HasPrefix(getFrontendURL(c), "https://"),
	})
}

func studentOAuthCallback(c fiber.Ctx, email, provider string, detail map[string]any) error {
	frontendURL := getFrontendURL(c)
	redirectError := func(message string) error {
		return c.Redirect().To(frontendURL + "/student/login?error=" + url.QueryEscape(message))
	}
	students, err := repositories.FindActiveStudentsByLoginEmail(email)
	if err != nil {
		return redirectError("ตรวจสอบบัญชีนักศึกษาไม่สำเร็จ กรุณาลองใหม่")
	}
	switch len(students) {
	case 0:
		return redirectError("ไม่พบบัญชีนักศึกษาที่ใช้งานได้สำหรับอีเมลนี้")
	case 1:
		student := &students[0]
		access, refresh, err := issueStudentOAuthSession(c, config.DB, student, provider)
		if err != nil {
			return redirectError("สร้างเซสชันนักศึกษาไม่สำเร็จ")
		}
		finishStudentOAuth(c, student, provider, email, detail, access, refresh)
		return c.Redirect().To(frontendURL + "/auth/callback?login=success")
	default:
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return redirectError("เริ่มขั้นตอนเลือกรหัสนักศึกษาไม่สำเร็จ กรุณาลองใหม่")
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		metadata, _ := json.Marshal(detail)
		selection := models.StudentLoginSelection{
			ID: loginChallengeHash(token), Email: email, Provider: provider,
			Metadata: datatypes.JSON(metadata), ExpiresAt: time.Now().Add(studentSelectionTTL),
		}
		if err := config.DB.Create(&selection).Error; err != nil {
			return redirectError("เริ่มขั้นตอนเลือกรหัสนักศึกษาไม่สำเร็จ กรุณาลองใหม่")
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		writeStudentSelectionCookie(c, token, int(studentSelectionTTL.Seconds()))
		return c.Redirect().To(frontendURL + "/auth/callback?selectStudent=1")
	}
}

func readStudentLoginSelection(c fiber.Ctx) (*models.StudentLoginSelection, error) {
	token := c.Cookies(studentSelectionCookie)
	if len(token) != 43 {
		return nil, fiber.ErrUnauthorized
	}
	var selection models.StudentLoginSelection
	err := config.DB.Where("id = ? AND expires_at > ?", loginChallengeHash(token), time.Now()).First(&selection).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrUnauthorized
	}
	return &selection, err
}

func GetStudentLoginSelection(c fiber.Ctx) error {
	if origin := c.Get(fiber.HeaderOrigin); origin != "" && origin != getFrontendURL(c) {
		return fiber.ErrForbidden
	}
	selection, err := readStudentLoginSelection(c)
	if err != nil {
		return err
	}
	students, err := repositories.FindActiveStudentsByLoginEmail(selection.Email)
	if err != nil {
		return err
	}
	type choice struct {
		StudentID string `json:"student_id"`
		FullName  string `json:"full_name"`
	}
	choices := make([]choice, 0, len(students))
	for _, student := range students {
		choices = append(choices, choice{StudentID: student.StudentID, FullName: student.FullName})
	}
	// The hash identifies the current attempt for the form. Only the HttpOnly
	// cookie can authenticate it, and the form must echo the hash on submission.
	return c.JSON(fiber.Map{"choices": choices, "selection_id": selection.ID})
}

func CompleteStudentLoginSelection(c fiber.Ctx) error {
	if c.Get(fiber.HeaderOrigin) != getFrontendURL(c) || getFrontendURL(c) == "" {
		return fiber.ErrForbidden
	}
	selection, err := readStudentLoginSelection(c)
	if err != nil {
		return err
	}
	var input struct {
		StudentID   string `json:"student_id"`
		SelectionID string `json:"selection_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.StudentID == "" || len(input.StudentID) > 44 {
		return fiber.ErrBadRequest
	}
	if subtle.ConstantTimeCompare([]byte(input.SelectionID), []byte(selection.ID)) != 1 {
		return fiber.ErrUnauthorized
	}

	var student *models.Student
	var access, refresh string
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		// Deletion claims the pending login exactly once. A failed choice or
		// session write rolls it back so the browser can retry.
		result := tx.Where("id = ? AND expires_at > ?", selection.ID, time.Now()).Delete(&models.StudentLoginSelection{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fiber.ErrUnauthorized
		}
		var err error
		student, err = repositories.LockActiveStudentLoginChoice(tx, selection.Email, input.StudentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrConflict
		}
		if err != nil {
			return err
		}
		access, refresh, err = issueStudentOAuthSession(c, tx, student, selection.Provider)
		return err
	})
	if err != nil {
		return err
	}
	var detail map[string]any
	_ = json.Unmarshal(selection.Metadata, &detail)
	finishStudentOAuth(c, student, selection.Provider, selection.Email, detail, access, refresh)
	return c.SendStatus(fiber.StatusNoContent)
}

func issueStudentOAuthSession(c fiber.Ctx, db *gorm.DB, student *models.Student, provider string) (access, refresh string, err error) {
	access, refresh, jti, err := utils.GenerateStudentTokenPair(student.ID)
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	meta, _ := json.Marshal(sessionMeta{
		IP: c.IP(), UserAgent: string(c.Request().Header.UserAgent()), Provider: provider, SessionStartedAt: now,
	})
	err = db.Create(&models.RefreshToken{
		JTI: jti, UserID: student.ID, Kind: "s", ExpiresAt: now.Add(7 * 24 * time.Hour), Meta: datatypes.JSON(meta),
	}).Error
	return access, refresh, err
}

func finishStudentOAuth(c fiber.Ctx, student *models.Student, provider, email string, detail map[string]any, access, refresh string) {
	if detail == nil {
		detail = make(map[string]any)
	}
	detail["student_id"] = student.ID
	detail["student_no"] = student.StudentID
	detail["full_name"] = student.FullName
	detail["email"] = email
	detail["provider"] = provider
	recordAuthLoginSystemLog(c, nil, provider+"_student", detail)
	writeStudentSelectionCookie(c, "", -1)
	utils.SetAuthCookies(c, access, refresh)
}
