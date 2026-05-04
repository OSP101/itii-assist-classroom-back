package handlers

import (
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/utils"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// GET /api/classrooms/stats
func GetClassroomStatsHandler(c fiber.Ctx) error {
	stats := repositories.GetClassroomStats()
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// GET /api/classrooms
func GetClassroomsHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	params := repositories.ClassroomListParams{
		Page:        page,
		Limit:       limit,
		Search:      c.Query("search"),
		Building:    c.Query("building"),
		ShowDeleted: c.Query("show_deleted", "false"),
	}

	result, err := repositories.GetClassrooms(params)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch classrooms"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"classrooms": result.Classrooms,
			"pagination": fiber.Map{
				"total":      result.Total,
				"page":       result.CurrentPage,
				"limit":      result.PerPage,
				"totalPages": result.TotalPages,
			},
		},
	})
}

// GET /api/classrooms/:id
func GetClassroomByIDHandler(c fiber.Ctx) error {
	id := c.Params("id")
	classroom, err := repositories.GetClassroomByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Classroom not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": classroom})
}

// POST /api/classrooms
func CreateClassroomHandler(c fiber.Ctx) error {
	var input struct {
		Name        string `json:"name"`
		Building    string `json:"building"`
		Floor       string `json:"floor"`
		Description string `json:"description"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.Name == "" || input.Building == "" || input.Floor == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "name, building, floor are required"})
	}

	userID := c.Locals("user_id").(uint)
	id, err := utils.GenerateNanoID(21)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to generate ID"})
	}

	classroom := models.Classroom{
		ID:          id,
		Name:        input.Name,
		Building:    input.Building,
		Floor:       input.Floor,
		Description: input.Description,
		IsActive:    true,
		CreatedBy:   &userID,
	}
	if err := repositories.CreateClassroom(&classroom); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create classroom"})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": classroom})
}

// PUT /api/classrooms/:id
func UpdateClassroomHandler(c fiber.Ctx) error {
	id := c.Params("id")
	var input struct {
		Name        string `json:"name"`
		Building    string `json:"building"`
		Floor       string `json:"floor"`
		Description string `json:"description"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	classroom, err := repositories.GetClassroomByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Classroom not found"})
	}

	if input.Name != "" {
		classroom.Classroom.Name = input.Name
	}
	if input.Building != "" {
		classroom.Classroom.Building = input.Building
	}
	if input.Floor != "" {
		classroom.Classroom.Floor = input.Floor
	}
	if input.Description != "" {
		classroom.Classroom.Description = input.Description
	}

	if err := repositories.UpdateClassroom(&classroom.Classroom); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update classroom"})
	}
	return c.JSON(fiber.Map{"success": true, "data": classroom.Classroom})
}

// PUT /api/classrooms/:id/layout
func UpdateClassroomLayoutHandler(c fiber.Ctx) error {
	id := c.Params("id")
	var input struct {
		Desks []repositories.DeskInput `json:"desks"`
		Zones []repositories.ZoneInput `json:"zones"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	if err := repositories.UpdateLayout(id, input.Desks, input.Zones); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update layout"})
	}

	classroom, _ := repositories.GetClassroomByID(id)
	return c.JSON(fiber.Map{"success": true, "data": classroom})
}

// PATCH /api/classrooms/:id/toggle-status
func ToggleClassroomStatusHandler(c fiber.Ctx) error {
	id := c.Params("id")
	classroom, err := repositories.ToggleClassroomStatus(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Classroom not found"})
	}
	return c.JSON(fiber.Map{"success": true, "data": classroom})
}

// POST /api/classrooms/:id/restore
func RestoreClassroomHandler(c fiber.Ctx) error {
	id := c.Params("id")
	if err := repositories.RestoreClassroom(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to restore classroom"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Classroom restored"})
}

// DELETE /api/classrooms/:id
func DeleteClassroomHandler(c fiber.Ctx) error {
	id := c.Params("id")
	hard, _ := strconv.ParseBool(c.Query("hard", "false"))

	if hard {
		if err := repositories.HardDeleteClassroom(id); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete classroom"})
		}
	} else {
		if err := repositories.SoftDeleteClassroom(id); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete classroom"})
		}
	}
	return c.JSON(fiber.Map{"success": true, "message": "Classroom deleted"})
}
