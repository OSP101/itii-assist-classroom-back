package handlers

import (
	"strings"

	"itii-assist/models"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

type SearchStudentsByIDsCompatInput struct {
	StudentIDsCamel []string `json:"studentIds"`
	StudentIDsSnake []string `json:"student_ids"`
	CourseID        string   `json:"course_id"`
	Section         string   `json:"section"`
}

func SearchStudentsByIDsCompatHandler(c fiber.Ctx) error {
	var input SearchStudentsByIDsCompatInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาส่ง studentIds"})
	}

	studentIDs := input.StudentIDsCamel
	if len(studentIDs) == 0 {
		studentIDs = input.StudentIDsSnake
	}
	if len(studentIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาส่ง studentIds"})
	}

	var students []models.Student
	var err error
	courseID := strings.TrimSpace(input.CourseID)
	if courseID != "" {
		students, err = repositories.FindStudentsByStudentIDsInCourse(studentIDs, courseID)
	} else {
		students, err = repositories.FindStudentsByStudentIDs(studentIDs)
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ค้นหาไม่สำเร็จ"})
	}

	foundMap := make(map[string]models.Student, len(students))
	for _, student := range students {
		foundMap[student.StudentID] = student
	}

	found := make([]fiber.Map, 0, len(studentIDs))
	notFound := make([]string, 0)
	for _, query := range studentIDs {
		if student, ok := foundMap[query]; ok {
			found = append(found, fiber.Map{
				"query": query,
				"student": fiber.Map{
					"id":         student.ID,
					"student_id": student.StudentID,
					"full_name":  student.FullName,
					"email":      student.Email,
				},
			})
			continue
		}

		notFound = append(notFound, query)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"found":     found,
			"not_found": notFound,
		},
	})
}
