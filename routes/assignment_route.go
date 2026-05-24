package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func SetupAssignmentRoutes(app *fiber.App) {
	api := app.Group("/api/assignments", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	api.Use(middlewares.RequireAdminFeature("menu.assignments"))

	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromQuery("course_id"), []string{repositories.PermissionViewAssignments, repositories.PermissionGradeAssignments, repositories.PermissionEditScores}, "instructor", "ta"), handlers.GetAssignmentsHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromBody("course_id"), repositories.PermissionCreateAssignments, "instructor", "ta"), handlers.CreateAssignmentHandler)
	api.Put("/reorder/batch", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromBody("course_id"), repositories.PermissionUpdateAssignments, "instructor", "ta"), handlers.ReorderAssignmentsHandler)
	api.Get("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentParam("id"), []string{repositories.PermissionViewAssignments, repositories.PermissionGradeAssignments, repositories.PermissionEditScores}, "instructor", "ta"), handlers.GetAssignmentHandler)
	api.Put("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAssignmentParam("id"), repositories.PermissionUpdateAssignments, "instructor", "ta"), handlers.UpdateAssignmentHandler)
	api.Delete("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAssignmentParam("id"), repositories.PermissionDeleteAssignments, "instructor", "ta"), handlers.DeleteAssignmentHandler)
	api.Post("/:id/attendance-links", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAssignmentParam("id"), repositories.PermissionUpdateAssignments, "instructor", "ta"), handlers.LinkAttendanceSessionsHandler)
}
