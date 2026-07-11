package middlewares

import (
	"errors"
	"itii-assist/repositories"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// RequireQueueWorkerOrCoursePermission authorizes worker actions on a queue
// booking. It first tries the standard course-role permission check against
// the queue session's own course; if that fails, it falls back to treating an
// existing QueueWorker row for that session as the source of truth. The
// fallback path is what lets a TA who was auto-mirrored into a partner session
// (shared-room / concurrent group) complete/skip/act on that session's
// bookings without being a TA of the partner course.
//
// The URL param named by sessionParam MUST resolve to a real queue_session.id.
// The check ignores the courseId path scope so mirrored TAs whose URL courseId
// differs from the session's course_id are still permitted.
func RequireQueueWorkerOrCoursePermission(sessionParam string, permissionKey string, allowedCourseRoles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		sessionID := strings.TrimSpace(c.Params(sessionParam))
		if sessionID == "" {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Queue session ID is required"})
		}

		sessionCourseID, err := repositories.GetCourseIDByQueueSessionID(sessionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "Queue session not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate queue session"})
		}

		courseExists, isActive, err := repositories.GetCourseActiveState(sessionCourseID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course access"})
		}
		if !courseExists {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "Course not found"})
		}
		if !isActive {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "รายวิชานี้ถูกปิดแล้วและอนุญาตให้ดูข้อมูลได้อย่างเดียว"})
		}

		userRole, ok := GetUserRole(c)
		if !ok {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "ไม่สามารถตรวจสอบสิทธิ์ได้"})
		}
		if userRole == "admin" {
			return c.Next()
		}

		userID, ok := GetUserID(c)
		if !ok {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "ไม่สามารถตรวจสอบสิทธิ์ได้"})
		}

		if _, allowed, err := repositories.GetCourseAccessState(sessionCourseID, userID, allowedCourseRoles...); err == nil && allowed {
			hasPermission, permErr := repositories.HasCoursePermission(sessionCourseID, userID, userRole, permissionKey)
			if permErr == nil && hasPermission {
				return c.Next()
			}
		}

		isWorker, workerErr := repositories.IsUserInQueueWorkersOfSession(sessionID, userID)
		if workerErr != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate queue worker access"})
		}
		if !isWorker {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์ใช้งานฟังก์ชันนี้"})
		}

		groupCourseIDs, err := repositories.GetConcurrentGroupCourseIDs(sessionID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate queue worker access"})
		}
		for _, groupCourseID := range groupCourseIDs {
			if groupCourseID == sessionCourseID {
				continue
			}
			_, allowed, err := repositories.GetCourseAccessState(groupCourseID, userID, allowedCourseRoles...)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course access"})
			}
			if !allowed {
				continue
			}
			hasPermission, permErr := repositories.HasCoursePermission(groupCourseID, userID, userRole, permissionKey)
			if permErr != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course permission"})
			}
			if hasPermission {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์ใช้งานฟังก์ชันนี้"})
	}
}
