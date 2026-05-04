package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupTeamRoutes(app *fiber.App) {
	protected := app.Group("/api/courses/:id/teams", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"))

	protected.Get("/", handlers.GetTeamsHandler)
	protected.Post("/bulk", handlers.BulkCreateTeamsHandler)
	protected.Post("/bulk-delete", handlers.BulkDeleteTeamsHandler)
	protected.Post("/", handlers.CreateTeamHandler)
	protected.Put("/:teamId", handlers.UpdateTeamHandler)
	protected.Delete("/:teamId", handlers.DeleteTeamHandler)
	protected.Post("/:teamId/members", handlers.AddTeamMemberHandler)
	protected.Delete("/:teamId/members/:studentId", handlers.RemoveTeamMemberHandler)
}
