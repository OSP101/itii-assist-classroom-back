package handlers

import (
	"errors"
	"strconv"
	"time"

	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"

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

// GET /api/assignments/course-summary?course_id=
func GetAssignmentCourseSummaryHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	summary, err := repositories.GetAssignmentCourseSummary(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to build assignment report"})
	}
	return c.JSON(fiber.Map{"success": true, "data": summary})
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
		CourseID                   string     `json:"course_id"`
		Name                       string     `json:"name"`
		Description                string     `json:"description"`
		AssignmentType             string     `json:"assignment_type"`
		WeekNumber                 *int       `json:"week_number"`
		LinkedAttendanceSessionID  *uint      `json:"linked_attendance_session_id"`
		LinkedAttendanceSessionIDs []uint     `json:"linked_attendance_session_ids"`
		AttendanceCondition        string     `json:"attendance_condition"`
		MaxScore                   float64    `json:"max_score"`
		DueDate                    *time.Time `json:"due_date"`
		IsScoreVisible             bool       `json:"is_score_visible"`
		IsDraft                    bool       `json:"is_draft"`
		PublishAt                  *time.Time `json:"publish_at"`
		SubItems                   []struct {
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
		CourseID:            input.CourseID,
		Name:                input.Name,
		Description:         input.Description,
		AssignmentType:      assignType,
		WeekNumber:          input.WeekNumber,
		AttendanceCondition: "or",
		MaxScore:            maxScore,
		DueDate:             input.DueDate,
		IsScoreVisible:      input.IsScoreVisible,
		IsDraft:             input.IsDraft,
		PublishAt:           input.PublishAt,
		IsActive:            true,
		CreatedBy:           userID,
	}

	if input.AttendanceCondition == "and" || input.AttendanceCondition == "or" {
		assignment.AttendanceCondition = input.AttendanceCondition
	}

	var subItems []models.AssignmentSubItem
	for _, si := range input.SubItems {
		subItems = append(subItems, models.AssignmentSubItem{Name: si.Name, MaxScore: si.MaxScore})
	}

	if err := repositories.CreateAssignment(&assignment, subItems); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create assignment"})
	}
	sessionIDs := input.LinkedAttendanceSessionIDs
	if len(sessionIDs) == 0 && input.LinkedAttendanceSessionID != nil {
		sessionIDs = []uint{*input.LinkedAttendanceSessionID}
	}
	if err := repositories.LinkAttendanceSessions(assignment.ID, sessionIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to link attendance sessions"})
	}
	createdAssignment, err := repositories.GetAssignmentWithSubItems(assignment.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load created assignment"})
	}
	logCourseActivity(c, input.CourseID, userID, "create_assignment", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{
		"assignment_type":   assignment.AssignmentType,
		"week_number":       assignment.WeekNumber,
		"max_score":         assignment.MaxScore,
		"sub_item_count":    len(subItems),
		"attendance_links":  sessionIDs,
		"attendance_policy": assignment.AttendanceCondition,
	})
	realtime.EmitDataUpdate("assignment", "create", assignment.ID, fiber.Map{
		"courseId":     assignment.CourseID,
		"assignmentId": assignment.ID,
		"name":         assignment.Name,
		"actor_id":     userID,
	})
	if !assignment.IsDraft {
		go createNotificationsForCourseMembers(
			input.CourseID, userID,
			"assignment_created",
			"งานใหม่: "+assignment.Name,
			"มีการสร้างงานใหม่ในวิชา",
			"/classroom/"+input.CourseID+"/assignments",
			buildNotifData(input.CourseID, strconv.FormatUint(uint64(assignment.ID), 10), "assignment", ""),
		)
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": createdAssignment})
}

type assignmentSubItemInput struct {
	ID       *uint   `json:"id"`
	Name     string  `json:"name"`
	MaxScore float64 `json:"max_score"`
}

type assignmentReplaceSubItemInput struct {
	Name     string  `json:"name"`
	MaxScore float64 `json:"max_score"`
}

// buildUpdateSubItems maps an update payload onto sub-item models.
//
// Carrying si.ID through is what keeps scores attached: scores reference
// assignment_sub_items.id and no foreign key repairs the link, so an ID dropped
// here re-creates the sub-item under a fresh ID and silently orphans every score
// on the assignment. The client sends the IDs for exactly this reason.
//
// replace_sub_items has no IDs by contract — it means "discard and rebuild" — so
// it is mapped without them and relies on the repository's deletion guard.
func buildUpdateSubItems(subItems *[]assignmentSubItemInput, replaceSubItems *[]assignmentReplaceSubItemInput) *[]models.AssignmentSubItem {
	switch {
	case subItems != nil:
		items := make([]models.AssignmentSubItem, 0, len(*subItems))
		for _, si := range *subItems {
			item := models.AssignmentSubItem{Name: si.Name, MaxScore: si.MaxScore}
			if si.ID != nil {
				item.ID = *si.ID
			}
			items = append(items, item)
		}
		return &items
	case replaceSubItems != nil:
		items := make([]models.AssignmentSubItem, 0, len(*replaceSubItems))
		for _, si := range *replaceSubItems {
			items = append(items, models.AssignmentSubItem{Name: si.Name, MaxScore: si.MaxScore})
		}
		return &items
	}
	return nil
}

// PUT /api/assignments/:id
// assignmentChangeFields is the slice of an assignment worth diffing in the
// activity log. Max score, due date and draft status are the edits students
// notice, so they are the edits an instructor may need to account for later.
func assignmentChangeFields(assignment models.Assignment) map[string]interface{} {
	fields := map[string]interface{}{
		"name":              assignment.Name,
		"description":       assignment.Description,
		"assignment_type":   assignment.AssignmentType,
		"max_score":         assignment.MaxScore,
		"week_number":       assignment.WeekNumber,
		"is_score_visible":  assignment.IsScoreVisible,
		"is_draft":          assignment.IsDraft,
		"attendance_policy": assignment.AttendanceCondition,
	}
	if assignment.DueDate != nil {
		fields["due_date"] = assignment.DueDate.Format(time.RFC3339)
	}
	return fields
}

func UpdateAssignmentHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	actorID := c.Locals("user_id").(uint)

	var input struct {
		Name                       string                           `json:"name"`
		Description                string                           `json:"description"`
		AssignmentType             string                           `json:"assignment_type"`
		WeekNumber                 *int                             `json:"week_number"`
		LinkedAttendanceSessionID  *uint                            `json:"linked_attendance_session_id"`
		LinkedAttendanceSessionIDs *[]uint                          `json:"linked_attendance_session_ids"`
		AttendanceCondition        string                           `json:"attendance_condition"`
		MaxScore                   *float64                         `json:"max_score"`
		DueDate                    *time.Time                       `json:"due_date"`
		IsScoreVisible             *bool                            `json:"is_score_visible"`
		IsDraft                    *bool                            `json:"is_draft"`
		PublishAt                  *time.Time                       `json:"publish_at"`
		ClearPublishAt             bool                             `json:"clear_publish_at"`
		ConfirmDeleteScores        bool                             `json:"confirm_delete_scores"`
		SubItems                   *[]assignmentSubItemInput        `json:"sub_items"`
		ReplaceSubItems            *[]assignmentReplaceSubItemInput `json:"replace_sub_items"`
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
	if input.AttendanceCondition == "and" || input.AttendanceCondition == "or" {
		a.AttendanceCondition = input.AttendanceCondition
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

	subItemsPtr := buildUpdateSubItems(input.SubItems, input.ReplaceSubItems)

	if err := repositories.UpdateAssignment(&a, subItemsPtr, input.ConfirmDeleteScores); err != nil {
		var scoresErr *repositories.ErrSubItemsHaveScores
		if errors.As(err, &scoresErr) {
			return c.Status(409).JSON(fiber.Map{
				"success": false,
				"code":    "sub_items_have_scores",
				"message": "Removing these sub-items would delete existing scores",
				"data": fiber.Map{
					"sub_items":    scoresErr.Items,
					"total_scores": scoresErr.TotalScores(),
				},
			})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update assignment"})
	}
	var sessionIDsToSave []uint
	shouldUpdateAttendanceLinks := false
	if input.LinkedAttendanceSessionIDs != nil {
		sessionIDsToSave = append(sessionIDsToSave, *input.LinkedAttendanceSessionIDs...)
		shouldUpdateAttendanceLinks = true
	} else if input.LinkedAttendanceSessionID != nil {
		sessionIDsToSave = []uint{*input.LinkedAttendanceSessionID}
		shouldUpdateAttendanceLinks = true
	}
	if shouldUpdateAttendanceLinks {
		if err := repositories.LinkAttendanceSessions(a.ID, sessionIDsToSave); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update attendance links"})
		}
	}
	updatedAssignment, err := repositories.GetAssignmentWithSubItems(a.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load updated assignment"})
	}
	assignmentDetail := fiber.Map{
		"assignment_type":   a.AssignmentType,
		"week_number":       a.WeekNumber,
		"max_score":         a.MaxScore,
		"replace_sub_items": subItemsPtr != nil,
		"attendance_links":  sessionIDsToSave,
		"attendance_policy": a.AttendanceCondition,
	}
	// existingAssignment.Assignment is the row as loaded, before the field-by-field
	// updates above were applied to the copy in `a`.
	assignmentDetail = withChanges(assignmentDetail,
		assignmentChangeFields(existingAssignment.Assignment),
		assignmentChangeFields(a),
	)
	logCourseActivity(c, a.CourseID, actorID, "update_assignment", "assignment", "assignment", a.ID, a.Name, assignmentDetail)
	wasPublished := existingAssignment.IsDraft && !a.IsDraft
	realtime.EmitDataUpdate("assignment", "update", a.ID, fiber.Map{
		"courseId":     a.CourseID,
		"assignmentId": a.ID,
		"name":         a.Name,
		"actor_id":     actorID,
	})
	if !a.IsDraft {
		notifType := "assignment_updated"
		notifTitle := "แก้ไขงาน: " + a.Name
		notifBody := "มีการแก้ไขงานในวิชา"
		if wasPublished {
			notifType = "assignment_created"
			notifTitle = "งานใหม่: " + a.Name
			notifBody = "มีการเผยแพร่งานใหม่ในวิชา"
		}
		go createNotificationsForCourseMembers(
			a.CourseID, actorID,
			notifType,
			notifTitle,
			notifBody,
			"/classroom/"+a.CourseID+"/assignments",
			buildNotifData(a.CourseID, strconv.FormatUint(uint64(a.ID), 10), "assignment", ""),
		)
	}
	return c.JSON(fiber.Map{"success": true, "data": updatedAssignment})
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
	logCourseActivity(c, assignment.CourseID, actorID, "delete_assignment", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{
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
	logCourseActivity(c, input.CourseID, c.Locals("user_id").(uint), "reorder_assignments", "assignment", "course", input.CourseID, "", fiber.Map{"ordered_ids": input.OrderedIDs})
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
	logCourseActivity(c, assignment.CourseID, c.Locals("user_id").(uint), "link_assignment_attendance", "assignment", "assignment", assignment.ID, assignment.Name, fiber.Map{"session_ids": input.SessionIDs})
	realtime.EmitDataUpdate("assignment", "update", assignment.ID, fiber.Map{
		"courseId":     assignment.CourseID,
		"assignmentId": assignment.ID,
		"name":         assignment.Name,
		"actor_id":     c.Locals("user_id").(uint),
	})
	return c.JSON(fiber.Map{"success": true, "message": "Links updated"})
}
