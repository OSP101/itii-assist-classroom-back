package handlers

import (
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

// GET /api/assignments?course_id=
func GetAssignmentsHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	assignments, err := repositories.GetAssignments(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch assignments"})
	}
	return c.JSON(fiber.Map{"success": true, "data": assignments})
}

// GET /api/assignments/:id
func GetAssignmentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	assignment, err := repositories.GetAssignmentWithSubItems(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": assignment})
}

// POST /api/assignments
func CreateAssignmentHandler(c fiber.Ctx) error {
	var input struct {
		CourseID       string     `json:"course_id"`
		Name           string     `json:"name"`
		Description    string     `json:"description"`
		AssignmentType string     `json:"assignment_type"`
		WeekNumber     *int       `json:"week_number"`
		MaxScore       float64    `json:"max_score"`
		DueDate        *time.Time `json:"due_date"`
		IsScoreVisible bool       `json:"is_score_visible"`
		IsDraft        bool       `json:"is_draft"`
		PublishAt      *time.Time `json:"publish_at"`
		SubItems       []struct {
			Name     string  `json:"name"`
			MaxScore float64 `json:"max_score"`
		} `json:"sub_items"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.CourseID == "" || input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id and name are required"})
	}

	userID := c.Locals("user_id").(uint)
	assignType := input.AssignmentType
	if assignType == "" {
		assignType = "individual"
	}
	maxScore := input.MaxScore
	if maxScore == 0 {
		maxScore = 10
	}

	assignment := models.Assignment{
		CourseID:       input.CourseID,
		Name:           input.Name,
		Description:    input.Description,
		AssignmentType: assignType,
		WeekNumber:     input.WeekNumber,
		MaxScore:       maxScore,
		DueDate:        input.DueDate,
		IsScoreVisible: input.IsScoreVisible,
		IsDraft:        input.IsDraft,
		PublishAt:      input.PublishAt,
		IsActive:       true,
		CreatedBy:      userID,
	}

	var subItems []models.AssignmentSubItem
	for _, si := range input.SubItems {
		subItems = append(subItems, models.AssignmentSubItem{Name: si.Name, MaxScore: si.MaxScore})
	}

	if err := repositories.CreateAssignment(&assignment, subItems); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create assignment"})
	}
	writeCourseActivityLog(input.CourseID, userID, "create_assignment", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{
		"assignment_type": assignment.AssignmentType,
		"week_number":     assignment.WeekNumber,
		"max_score":       assignment.MaxScore,
		"sub_item_count":  len(subItems),
	})
	realtime.EmitDataUpdate("assignment", "create", assignment.ID, fiber.Map{
		"courseId":     assignment.CourseID,
		"assignmentId": assignment.ID,
		"name":         assignment.Name,
		"actor_id":     userID,
	})
	go createNotificationsForCourseMembers(
		input.CourseID, userID,
		"assignment_created",
		"งานใหม่: "+assignment.Name,
		"มีการสร้างงานใหม่ในวิชา",
		"/classroom/"+input.CourseID+"/assignments",
		buildNotifData(input.CourseID, strconv.FormatUint(uint64(assignment.ID), 10), "assignment", ""),
	)
	return c.Status(201).JSON(fiber.Map{"success": true, "data": assignment})
}

// PUT /api/assignments/:id
func UpdateAssignmentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)

	var input struct {
		Name            string     `json:"name"`
		Description     string     `json:"description"`
		AssignmentType  string     `json:"assignment_type"`
		WeekNumber      *int       `json:"week_number"`
		MaxScore        *float64   `json:"max_score"`
		DueDate         *time.Time `json:"due_date"`
		IsScoreVisible  *bool      `json:"is_score_visible"`
		IsDraft         *bool      `json:"is_draft"`
		PublishAt       *time.Time `json:"publish_at"`
		ClearPublishAt  bool       `json:"clear_publish_at"`
		ReplaceSubItems *[]struct {
			Name     string  `json:"name"`
			MaxScore float64 `json:"max_score"`
		} `json:"replace_sub_items"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	existingAssignment, err2 := repositories.GetAssignmentWithSubItems(uint(id))
	if err2 != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}

	a := existingAssignment.Assignment
	if input.Name != "" {
		a.Name = input.Name
	}
	if input.Description != "" {
		a.Description = input.Description
	}
	if input.AssignmentType != "" {
		a.AssignmentType = input.AssignmentType
	}
	if input.WeekNumber != nil {
		a.WeekNumber = input.WeekNumber
	}
	if input.MaxScore != nil {
		a.MaxScore = *input.MaxScore
	}
	if input.DueDate != nil {
		a.DueDate = input.DueDate
	}
	if input.IsScoreVisible != nil {
		a.IsScoreVisible = *input.IsScoreVisible
	}
	if input.IsDraft != nil {
		a.IsDraft = *input.IsDraft
	}
	if input.PublishAt != nil {
		a.PublishAt = input.PublishAt
	}
	if input.ClearPublishAt {
		a.PublishAt = nil
	}

	var subItemsPtr *[]models.AssignmentSubItem
	if input.ReplaceSubItems != nil {
		var items []models.AssignmentSubItem
		for _, si := range *input.ReplaceSubItems {
			items = append(items, models.AssignmentSubItem{Name: si.Name, MaxScore: si.MaxScore})
		}
		subItemsPtr = &items
	}

	if err := repositories.UpdateAssignment(&a, subItemsPtr); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update assignment"})
	}
	writeCourseActivityLog(a.CourseID, actorID, "update_assignment", "assignment", "assignment", a.ID, a.Name, fiber.Map{
		"assignment_type":   a.AssignmentType,
		"week_number":       a.WeekNumber,
		"max_score":         a.MaxScore,
		"replace_sub_items": subItemsPtr != nil,
	})
	realtime.EmitDataUpdate("assignment", "update", a.ID, fiber.Map{
		"courseId":     a.CourseID,
		"assignmentId": a.ID,
		"name":         a.Name,
		"actor_id":     actorID,
	})
	go createNotificationsForCourseMembers(
		a.CourseID, actorID,
		"assignment_updated",
		"แก้ไขงาน: "+a.Name,
		"มีการแก้ไขงานในวิชา",
		"/classroom/"+a.CourseID+"/assignments",
		buildNotifData(a.CourseID, strconv.FormatUint(uint64(a.ID), 10), "assignment", ""),
	)
	return c.JSON(fiber.Map{"success": true, "data": a})
}

// DELETE /api/assignments/:id
func DeleteAssignmentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)
	assignment, err := repositories.GetAssignmentWithSubItems(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}
	if err := repositories.SoftDeleteAssignment(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete assignment"})
	}
	writeCourseActivityLog(assignment.CourseID, actorID, "delete_assignment", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{
		"assignment_type": assignment.AssignmentType,
		"week_number":     assignment.WeekNumber,
	})
	realtime.EmitDataUpdate("assignment", "delete", assignment.ID, fiber.Map{
		"courseId":     assignment.CourseID,
		"assignmentId": assignment.ID,
		"name":         assignment.Name,
		"actor_id":     actorID,
	})
	return c.JSON(fiber.Map{"success": true, "message": "Assignment deleted"})
}

// PUT /api/assignments/reorder/batch
func ReorderAssignmentsHandler(c fiber.Ctx) error {
	var input struct {
		CourseID   string `json:"course_id"`
		OrderedIDs []uint `json:"ordered_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if err := repositories.ReorderAssignments(input.CourseID, input.OrderedIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to reorder assignments"})
	}
	writeCourseActivityLog(input.CourseID, c.Locals("user_id").(uint), "reorder_assignments", "assignment", "course", input.CourseID, "", fiber.Map{"ordered_ids": input.OrderedIDs})
	realtime.EmitDataUpdate("assignment", "update", nil, fiber.Map{
		"courseId":    input.CourseID,
		"ordered_ids": input.OrderedIDs,
		"actor_id":    c.Locals("user_id").(uint),
	})
	return c.JSON(fiber.Map{"success": true, "message": "Reordered"})
}

// POST /api/assignments/:id/attendance-links
func LinkAttendanceSessionsHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	assignment, err := repositories.GetAssignmentWithSubItems(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}
	var input struct {
		SessionIDs []uint `json:"session_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if err := repositories.LinkAttendanceSessions(uint(id), input.SessionIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update links"})
	}
	writeCourseActivityLog(assignment.CourseID, c.Locals("user_id").(uint), "link_assignment_attendance", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{"session_ids": input.SessionIDs})
	realtime.EmitDataUpdate("assignment", "update", assignment.ID, fiber.Map{
		"courseId":     assignment.CourseID,
		"assignmentId": assignment.ID,
		"name":         assignment.Name,
		"actor_id":     c.Locals("user_id").(uint),
	})
	return c.JSON(fiber.Map{"success": true, "message": "Links updated"})
}
