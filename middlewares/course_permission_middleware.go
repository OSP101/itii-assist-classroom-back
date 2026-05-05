package middlewares

import (
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func RequireCoursePermission(resolver CourseAccessResolver, permissionKey string, allowedCourseRoles ...string) fiber.Handler {
	return RequireAnyCoursePermission(resolver, []string{permissionKey}, allowedCourseRoles...)
}

func RequireAnyCoursePermission(resolver CourseAccessResolver, permissionKeys []string, allowedCourseRoles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		courseIDs, err := resolver(c)
		if err != nil {
			return writeCourseAccessError(c, err)
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

		for _, courseID := range uniqueCourseIDs(courseIDs) {
			courseExists, allowed, err := repositories.GetCourseAccessState(courseID, userID, allowedCourseRoles...)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course access"})
			}
			if !courseExists {
				return c.Status(404).JSON(fiber.Map{"success": false, "message": "Course not found"})
			}
			if !allowed {
				return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
			}

			hasPermission := false
			for _, permissionKey := range permissionKeys {
				allowedByPermission, err := repositories.HasCoursePermission(courseID, userID, userRole, permissionKey)
				if err != nil {
					return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course permission"})
				}
				if allowedByPermission {
					hasPermission = true
					break
				}
			}

			if !hasPermission {
				return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์ใช้งานฟังก์ชันนี้"})
			}
		}

		return c.Next()
	}
}
