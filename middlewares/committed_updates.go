package middlewares

import (
	"itii-assist/realtime"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// Only successful server mutations publish invalidations. Course scopes are
// recorded by RequireCourseAccess before the handler can delete the resource.
func CommittedDataUpdates() fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()
		if err != nil || c.Response().StatusCode() < 200 || c.Response().StatusCode() >= 300 {
			return err
		}
		if c.Method() == "GET" || c.Method() == "HEAD" || c.Method() == "OPTIONS" {
			return nil
		}
		path := strings.TrimSuffix(c.Path(), "/")
		resource := ""
		switch {
		case path == "/api/courses" || strings.HasPrefix(path, "/api/courses/"):
			resource = "course"
			switch {
			case strings.Contains(path, "/queue/") || strings.HasSuffix(path, "/queue"):
				resource = "queue"
			case strings.Contains(path, "/sections"):
				resource = "section"
			case strings.Contains(path, "/teams"):
				resource = "group"
			}
		case strings.HasPrefix(path, "/api/assignments"):
			resource = "assignment"
		case strings.HasPrefix(path, "/api/scores") || strings.HasPrefix(path, "/api/score-edit-requests"):
			resource = "score"
		case strings.HasPrefix(path, "/api/attendance/"):
			resource = "attendance"
		case strings.HasPrefix(path, "/api/queue/"):
			resource = "queue"
		case strings.HasPrefix(path, "/api/users"):
			resource = "user"
		case strings.HasPrefix(path, "/api/students"):
			resource = "student"
		case strings.HasPrefix(path, "/api/classrooms"):
			resource = "classroom"
		case strings.HasPrefix(path, "/api/feedback"):
			resource = "feedback"
		}
		if resource == "" {
			return nil
		}
		courses, _ := c.Locals("validated_course_ids").([]string)
		if len(courses) == 0 {
			courses = []string{""}
		}
		actor, _ := GetUserID(c)
		for _, courseID := range courses {
			realtime.EmitDataUpdate(resource, "update", courseID, fiber.Map{"course_id": courseID, "actor_id": actor})
		}
		return nil
	}
}
