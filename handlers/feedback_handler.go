package handlers

import (
	"itii-assist/models"
	"itii-assist/repositories"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

// POST /api/feedback
func CreateFeedbackHandler(c fiber.Ctx) error {
	var input struct {
		Type         string         `json:"type"`
		Title        string         `json:"title"`
		Description  string         `json:"description"`
		Attachments  datatypes.JSON `json:"attachments"`
		ContactEmail string         `json:"contact_email"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.Title == "" || input.Description == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "title and description are required"})
	}

	feedbackType := input.Type
	if feedbackType == "" {
		feedbackType = "other"
	}

	var userIDPtr *uint
	if raw := c.Locals("user_id"); raw != nil {
		uid := raw.(uint)
		userIDPtr = &uid
	}

	feedback := models.Feedback{
		UserID:       userIDPtr,
		Type:         feedbackType,
		Title:        input.Title,
		Description:  input.Description,
		Attachments:  input.Attachments,
		Priority:     "medium",
		Status:       "pending",
		ContactEmail: input.ContactEmail,
	}
	if err := repositories.CreateFeedback(&feedback); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create feedback"})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "ส่ง Feedback สำเร็จ", "data": feedback})
}

// GET /api/feedback/my  (authenticated)
func GetMyFeedbacksHandler(c fiber.Ctx) error {
	userID := c.Locals("user_id").(uint)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	result, err := repositories.GetMyFeedbacks(userID, page, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch feedbacks"})
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// GET /api/feedback  (admin)
func GetAllFeedbacksHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	params := repositories.FeedbackListParams{
		Page:      page,
		Limit:     limit,
		Search:    c.Query("search"),
		Type:      c.Query("type"),
		Status:    c.Query("status"),
		Priority:  c.Query("priority"),
		SortBy:    c.Query("sort_by", "created_at"),
		SortOrder: c.Query("sort_order", "DESC"),
	}

	result, err := repositories.GetFeedbacks(params)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch feedbacks"})
	}
	return c.JSON(fiber.Map{"success": true, "data": result})
}

// GET /api/feedback/stats  (admin)
func GetFeedbackStatsHandler(c fiber.Ctx) error {
	stats := repositories.GetFeedbackStats()
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// GET /api/feedback/:id  (admin)
func GetFeedbackByIDHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	feedback, err := repositories.GetFeedbackByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Feedback not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": feedback})
}

// PUT /api/feedback/:id  (admin)
func UpdateFeedbackHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	var input struct {
		Status     string `json:"status"`
		Priority   string `json:"priority"`
		AdminNotes string `json:"admin_notes"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	resolvedBy := c.Locals("user_id").(uint)
	feedback, err2 := repositories.UpdateFeedback(uint(id), input.Status, input.Priority, input.AdminNotes, resolvedBy)
	if err2 != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update feedback"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "อัปเดต Feedback สำเร็จ", "data": feedback})
}

// DELETE /api/feedback/:id  (admin)
func DeleteFeedbackHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	if err := repositories.DeleteFeedback(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete feedback"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "ลบ Feedback สำเร็จ"})
}
