package handlers

import (
	"errors"
	"fmt"
	"itii-assist/repositories"
	"itii-assist/services"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// ExamHandler — struct-based handler with audit logger
type ExamHandler struct {
	auditLogger *services.AuditLogger
}

func NewExamHandler(auditLogger *services.AuditLogger) *ExamHandler {
	return &ExamHandler{auditLogger: auditLogger}
}

// GET /api/courses/:courseId/exam-settings
func GetExamSettingsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	settings, err := repositories.GetOrCreateExamSettings(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam settings"})
	}
	return c.JSON(fiber.Map{"success": true, "data": settings})
}

// PUT /api/courses/:courseId/exam-settings/:id
func UpdateExamSettingHandler(c fiber.Ctx) error {
	settingIDStr := c.Params("id")
	settingID, err := strconv.ParseUint(settingIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid setting ID"})
	}
	courseID := c.Params("courseId")

	var input struct {
		MaxScore  *float64 `json:"max_score"`
		IsVisible *bool    `json:"is_visible"`
		IsActive  *bool    `json:"is_active"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	setting, err2 := repositories.UpdateExamSetting(uint(settingID), courseID, input.MaxScore, input.IsVisible, input.IsActive)
	if err2 != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update exam setting"})
	}
	return c.JSON(fiber.Map{"success": true, "data": setting})
}

// GET /api/courses/:courseId/exam-scores
func GetExamScoresHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	view, err := repositories.GetExamScoresView(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam scores"})
	}
	return c.JSON(fiber.Map{"success": true, "data": view})
}

// GET /api/courses/:courseId/exam-scores/stats
func GetExamScoreStatsHandler(c fiber.Ctx) error {
	stats, err := repositories.GetExamScoreStats(c.Params("courseId"))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam score statistics"})
	}
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// POST /api/courses/:courseId/exam-scores
func (h *ExamHandler) UpsertExamScore(c fiber.Ctx) error {
	var input struct {
		ExamSettingID uint     `json:"exam_setting_id"`
		StudentID     uint     `json:"student_id"`
		Score         *float64 `json:"score"`
		Comment       string   `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.ExamSettingID == 0 || input.StudentID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "exam_setting_id and student_id required"})
	}

	setting, err := repositories.GetExamSettingByCourse(c.Params("courseId"), input.ExamSettingID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่าการสอบ"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam setting"})
	}
	if input.Score != nil {
		if *input.Score < 0 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "คะแนนต้องไม่ติดลบ"})
		}
		if *input.Score > setting.MaxScore {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("คะแนนเกินคะแนนเต็ม (%v)", setting.MaxScore)})
		}
	}

	gradedBy := c.Locals("user_id").(uint)
	saved, err := repositories.SaveExamScore(input.ExamSettingID, input.StudentID, input.Score, input.Comment, gradedBy)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to upsert exam score"})
	}
	courseID := c.Params("courseId")
	logCourseActivity(c, courseID, gradedBy, "submit_exam_score", "score", "student", input.StudentID, "", fiber.Map{"exam_setting_id": input.ExamSettingID, "score": input.Score})
	reqID, _, ip := services.ExtractMeta(c)
	h.auditLogger.LogCourse(c.Context(), services.CourseEvent{
		CourseID:    courseID,
		ActorUserID: gradedBy,
		Action:      services.ActionExamScoreUpdated,
		TargetType:  "exam_score",
		TargetID:    strconv.Itoa(int(saved.ID)),
		RequestID:   reqID,
		IPAddress:   ip,
	})
	return c.JSON(fiber.Map{"success": true, "data": saved, "message": "บันทึกคะแนนสำเร็จ"})
}

// POST /api/courses/:courseId/exam-scores/bulk
func BulkUpsertExamScoresHandler(c fiber.Ctx) error {
	var input struct {
		ExamSettingID uint `json:"exam_setting_id"`
		Scores        []struct {
			StudentCode string   `json:"student_id"`
			Score       *float64 `json:"score"`
			Comment     string   `json:"comment"`
		} `json:"scores"`
		Entries []struct {
			SettingID uint     `json:"setting_id"`
			StudentID uint     `json:"student_id"`
			Score     *float64 `json:"score"`
			Comment   string   `json:"comment"`
		} `json:"entries"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	gradedBy := c.Locals("user_id").(uint)
	savedCount := 0
	errorsList := []fiber.Map{}
	courseID := c.Params("courseId")

	if input.ExamSettingID > 0 && len(input.Scores) > 0 {
		setting, err := repositories.GetExamSettingByCourse(courseID, input.ExamSettingID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบการตั้งค่าการสอบ"})
			}
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam setting"})
		}

		studentMap, err := repositories.GetCourseStudentIDMap(courseID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load course students"})
		}

		for _, entry := range input.Scores {
			studentCode := strings.ToLower(strings.TrimSpace(entry.StudentCode))
			studentID, ok := studentMap[studentCode]
			if !ok {
				errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentCode, "reason": "ไม่พบนักศึกษา"})
				continue
			}

			if entry.Score != nil {
				if *entry.Score < 0 {
					errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentCode, "reason": "คะแนนต้องไม่ติดลบ"})
					continue
				}
				if *entry.Score > setting.MaxScore {
					errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentCode, "reason": fmt.Sprintf("คะแนนเกินคะแนนเต็ม (%v)", setting.MaxScore)})
					continue
				}
			}

			if _, err := repositories.SaveExamScore(input.ExamSettingID, studentID, entry.Score, entry.Comment, gradedBy); err != nil {
				errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentCode, "reason": "บันทึกคะแนนไม่สำเร็จ"})
				continue
			}
			savedCount++
		}
	} else if len(input.Entries) > 0 {
		for _, entry := range input.Entries {
			setting, err := repositories.GetExamSettingByCourse(courseID, entry.SettingID)
			if err != nil {
				errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentID, "reason": "ไม่พบการตั้งค่าการสอบ"})
				continue
			}

			if entry.Score != nil {
				if *entry.Score < 0 {
					errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentID, "reason": "คะแนนต้องไม่ติดลบ"})
					continue
				}
				if *entry.Score > setting.MaxScore {
					errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentID, "reason": fmt.Sprintf("คะแนนเกินคะแนนเต็ม (%v)", setting.MaxScore)})
					continue
				}
			}

			if _, err := repositories.SaveExamScore(entry.SettingID, entry.StudentID, entry.Score, entry.Comment, gradedBy); err != nil {
				errorsList = append(errorsList, fiber.Map{"student_id": entry.StudentID, "reason": "บันทึกคะแนนไม่สำเร็จ"})
				continue
			}
			savedCount++
		}
	} else {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "exam_setting_id and scores or entries required"})
	}

	if savedCount > 0 {
		logCourseActivity(c, courseID, gradedBy, "bulk_submit_exam_scores", "score", "course", courseID, "", fiber.Map{"saved": savedCount, "errors": len(errorsList)})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"saved":  savedCount,
			"errors": errorsList,
		},
		"message": fmt.Sprintf("บันทึกคะแนน %d รายการสำเร็จ", savedCount),
	})
}

// DELETE /api/courses/:courseId/exam-scores/:scoreId
func DeleteExamScoreHandler(c fiber.Ctx) error {
	scoreID, err := strconv.ParseUint(c.Params("scoreId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid score ID"})
	}

	if err := repositories.DeleteExamScoreByCourse(uint(scoreID), c.Params("courseId")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบคะแนน"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete exam score"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "ลบคะแนนสำเร็จ"})
}
