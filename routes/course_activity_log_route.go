package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupCourseActivityLogRoutes(app *fiber.App) {
	group := app.Group("/api/courses/:courseId/activity-logs", middlewares.Protected(), middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor"))
	group.Get("/", handlers.GetCourseActivityLogsHandler)
	group.Get("/export", handlers.ExportCourseActivityLogsHandler)
	group.Get("/stats", handlers.GetCourseActivityStatsHandler)
	group.Get("/filters", handlers.GetCourseActivityFiltersHandler)
	group.Get("/ta-stats", handlers.GetCourseActivityTAStatsHandler)
	group.Get("/ta-stats/:userId", handlers.GetCourseActivityTADetailHandler)
}
