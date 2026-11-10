package handlers

import (
	"errors"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func queueStaffAllowed(c fiber.Ctx, courseID, permission string) bool {
	uid, ok := middlewares.GetUserID(c)
	role, _ := middlewares.GetUserRole(c)
	if !ok {
		return false
	}
	if role == "admin" {
		return true
	}
	_, allowed, err := repositories.GetCourseAccessState(courseID, uid, "instructor", "ta")
	if err != nil || !allowed {
		return false
	}
	allowed, err = repositories.HasCoursePermission(courseID, uid, role, permission)
	return err == nil && allowed
}

func queueBookingAllowed(c fiber.Ctx, booking *models.QueueBooking, permission string) bool {
	if booking == nil {
		return false
	}
	if sid, ok := middlewares.GetStudentID(c); ok {
		return sid == booking.StudentID
	}
	courseID, err := repositories.GetCourseIDByQueueSessionID(booking.QueueSessionID)
	return err == nil && queueStaffAllowed(c, courseID, permission)
}

func queueRequireBookingAccess(c fiber.Ctx, booking *models.QueueBooking, permission string) error {
	if !queueBookingAllowed(c, booking, permission) {
		return fiber.NewError(fiber.StatusForbidden, "คุณไม่มีสิทธิ์เข้าถึงคิวนี้")
	}
	return nil
}

func queueStudentCode(c fiber.Ctx, supplied string) (string, error) {
	if sid, ok := middlewares.GetStudentID(c); ok {
		var student models.Student
		if err := config.DB.Where("id = ? AND is_active = ?", sid, true).First(&student).Error; err != nil {
			return "", fiber.ErrUnauthorized
		}
		if strings.TrimSpace(supplied) != "" && strings.TrimSpace(supplied) != student.StudentID {
			return "", fiber.ErrForbidden
		}
		return student.StudentID, nil
	}
	// Staff may assist students only after the caller's course permission is
	// checked by the handler against the resolved session, never merely a role.
	if _, ok := middlewares.GetUserID(c); ok {
		return strings.TrimSpace(supplied), nil
	}
	return "", fiber.ErrUnauthorized
}

func queueRequireSessionStudent(c fiber.Ctx, courseID string) error {
	if _, ok := middlewares.GetStudentID(c); ok {
		return nil
	}
	if queueStaffAllowed(c, courseID, repositories.PermissionManageQueueBookings) {
		return nil
	}
	return fiber.ErrForbidden
}

func queueRequireStudentParam(c fiber.Ctx) error {
	sid, err := strconv.ParseUint(c.Params("studentId"), 10, 64)
	if err != nil {
		return fiber.ErrBadRequest
	}
	if own, ok := middlewares.GetStudentID(c); ok && own == uint(sid) {
		return nil
	}
	courseID, err := repositories.GetCourseIDByQueueSessionID(c.Params("sessionId"))
	if err == nil && queueStaffAllowed(c, courseID, repositories.PermissionViewQueue) {
		return nil
	}
	return errors.New("student booking access denied")
}

func queueProjectorName(c fiber.Ctx, courseID, value string) string {
	key := "queue_projector_identity:" + courseID
	allowed, ok := c.Locals(key).(bool)
	if !ok {
		allowed = queueStaffAllowed(c, courseID, repositories.PermissionViewQueue)
		c.Locals(key, allowed)
	}
	if allowed {
		return value
	}
	return ""
}
