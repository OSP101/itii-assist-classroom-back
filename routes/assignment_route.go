package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupAssignmentRoutes(app *fiber.App) {
	api := app.Group("/api/assignments", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))

	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetAssignmentsHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), handlers.CreateAssignmentHandler)
	api.Put("/reorder/batch", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), handlers.ReorderAssignmentsHandler)
	api.Get("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), handlers.GetAssignmentHandler)
	api.Put("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), handlers.UpdateAssignmentHandler)
	api.Delete("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), handlers.DeleteAssignmentHandler)
	api.Post("/:id/attendance-links", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), handlers.LinkAttendanceSessionsHandler)
}
