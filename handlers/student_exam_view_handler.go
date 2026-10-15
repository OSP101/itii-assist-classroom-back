package handlers

import (
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

// POST /api/students/me/courses/:courseId/exam-scores/viewed
//
// The student's course page calls this when its scores tab shows published
// exam results. It records one view row per visible exam component, which is
// what the instructor's "ใครเปิดดูแล้ว" panel counts. Opening the email does
// not count; only seeing the score in the app does.
func RecordMyExamScoreViewsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณาระบุรายวิชา"})
	}
	studentID, ok := middlewares.GetStudentID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูล session นักศึกษา"})
	}
	student, err := repositories.FindStudentByID(studentID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลนักศึกษา"})
	}
	if !repositories.IsStudentInCourse(courseID, student.ID) {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชานี้ในบัญชีนักศึกษา"})
	}
	if err := repositories.RecordExamScoreViews(student.ID, courseID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "บันทึกการเปิดดูไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true})
}
