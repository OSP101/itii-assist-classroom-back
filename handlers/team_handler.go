package handlers

import (
	"itii-assist/repositories"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// GET /api/courses/:id/teams
// =============================================================================

func GetTeamsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	groupType := c.Query("type") // permanent | temporary | (all)
	weekStr := c.Query("week")

	var weekNumber *int
	if weekStr != "" {
		w, err := strconv.Atoi(weekStr)
		if err == nil {
			weekNumber = &w
		}
	}

	teams, err := repositories.GetTeamsByCourse(courseID, groupType, weekNumber)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลกลุ่มไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": teams})
}

// =============================================================================
// POST /api/courses/:id/teams
// =============================================================================

func CreateTeamHandler(c fiber.Ctx) error {
	courseID := c.Params("id")

	var input struct {
		Name       string `json:"name"`
		GroupType  string `json:"group_type"`
		WeekNumber *int   `json:"week_number"`
		MemberIDs  []uint `json:"member_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุชื่อกลุ่ม"})
	}
	if len(input.MemberIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาเลือกสมาชิกอย่างน้อย 1 คน"})
	}

	groupType := input.GroupType
	if groupType == "" {
		groupType = "permanent"
	}

	validationErrors := repositories.ValidateTeamInputs(courseID, groupType, input.WeekNumber, []repositories.TeamCreateInput{
		{
			Name:      input.Name,
			MemberIDs: input.MemberIDs,
		},
	})
	if len(validationErrors) > 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": validationErrors[0],
			"data": fiber.Map{
				"errors": validationErrors,
			},
		})
	}

	team, err := repositories.CreateTeam(courseID, input.Name, groupType, input.WeekNumber, input.MemberIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างกลุ่มไม่สำเร็จ"})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "message": "สร้างกลุ่มสำเร็จ", "data": team})
}

// =============================================================================
// POST /api/courses/:id/teams/bulk
// =============================================================================

func BulkCreateTeamsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")

	var input struct {
		GroupType  string `json:"group_type"`
		WeekNumber *int   `json:"week_number"`
		Teams      []struct {
			Name      string `json:"name"`
			MemberIDs []uint `json:"member_ids"`
		} `json:"teams"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if len(input.Teams) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุข้อมูลกลุ่ม"})
	}

	groupType := input.GroupType
	if groupType == "" {
		groupType = "permanent"
	}

	entries := make([]repositories.TeamCreateInput, len(input.Teams))
	for i, t := range input.Teams {
		entries[i].Name = t.Name
		entries[i].MemberIDs = t.MemberIDs
	}

	validationErrors := repositories.ValidateTeamInputs(courseID, groupType, input.WeekNumber, entries)
	if len(validationErrors) > 0 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": validationErrors[0],
			"data": fiber.Map{
				"errors": validationErrors,
			},
		})
	}

	created, err := repositories.BulkCreateTeams(courseID, groupType, input.WeekNumber, entries)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างกลุ่มไม่สำเร็จ"})
	}
	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "สร้างกลุ่มสำเร็จ",
		"data": fiber.Map{
			"createdCount": len(created),
			"teams":        created,
		},
	})
}

// =============================================================================
// POST /api/courses/:id/teams/bulk-delete
// =============================================================================

func BulkDeleteTeamsHandler(c fiber.Ctx) error {
	courseID := c.Params("id")

	var input struct {
		TeamIDs []uint `json:"team_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if len(input.TeamIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายการกลุ่มที่ต้องการลบ"})
	}

	count, err := repositories.BulkDeleteTeams(input.TeamIDs, courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบกลุ่มไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"message": "ลบกลุ่มสำเร็จ",
		"data":    fiber.Map{"deletedCount": count},
	})
}

// =============================================================================
// PUT /api/courses/:id/teams/:teamId
// =============================================================================

func UpdateTeamHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	teamIDStr := c.Params("teamId")
	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid teamId"})
	}

	var input struct {
		Name      string  `json:"name"`
		MemberIDs *[]uint `json:"member_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	team, err := repositories.UpdateTeam(uint(teamID64), courseID, input.Name, input.MemberIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "อัพเดทกลุ่มไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "อัพเดทกลุ่มสำเร็จ", "data": team})
}

// =============================================================================
// DELETE /api/courses/:id/teams/:teamId
// =============================================================================

func DeleteTeamHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	teamIDStr := c.Params("teamId")
	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid teamId"})
	}

	if err := repositories.DeleteTeam(uint(teamID64), courseID); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบกลุ่ม"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "ลบกลุ่มสำเร็จ"})
}

// =============================================================================
// POST /api/courses/:id/teams/:teamId/members
// =============================================================================

func AddTeamMemberHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	teamIDStr := c.Params("teamId")
	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid teamId"})
	}

	var input struct {
		StudentID uint `json:"student_id"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.StudentID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "student_id required"})
	}

	if err := repositories.AddTeamMember(uint(teamID64), courseID, input.StudentID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เพิ่มสมาชิกไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "เพิ่มสมาชิกสำเร็จ"})
}

// =============================================================================
// DELETE /api/courses/:id/teams/:teamId/members/:studentId
// =============================================================================

func RemoveTeamMemberHandler(c fiber.Ctx) error {
	courseID := c.Params("id")
	teamIDStr := c.Params("teamId")
	studentIDStr := c.Params("studentId")

	teamID64, err := strconv.ParseUint(teamIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid teamId"})
	}
	studentID64, err := strconv.ParseUint(studentIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid studentId"})
	}

	if err := repositories.RemoveTeamMember(uint(teamID64), courseID, uint(studentID64)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบสมาชิกไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "ลบสมาชิกสำเร็จ"})
}
