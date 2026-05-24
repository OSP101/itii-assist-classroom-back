package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func SetupTeamRoutes(app *fiber.App) {
	protected := app.Group("/api/courses/:id/teams", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"))
	protected.Use(middlewares.RequireAdminFeature("menu.teams"))

	protected.Get("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionViewTeams, "instructor", "ta"), handlers.GetTeamsHandler)
	protected.Post("/bulk", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionCreateTeams, "instructor", "ta"), handlers.BulkCreateTeamsHandler)
	protected.Post("/bulk-delete", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionDeleteTeams, "instructor", "ta"), handlers.BulkDeleteTeamsHandler)
	protected.Post("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionCreateTeams, "instructor", "ta"), handlers.CreateTeamHandler)
	protected.Put("/:teamId", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionUpdateTeams, "instructor", "ta"), handlers.UpdateTeamHandler)
	protected.Delete("/:teamId", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionDeleteTeams, "instructor", "ta"), handlers.DeleteTeamHandler)
	protected.Post("/:teamId/members", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageTeamMembers, "instructor", "ta"), handlers.AddTeamMemberHandler)
	protected.Delete("/:teamId/members/:studentId", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageTeamMembers, "instructor", "ta"), handlers.RemoveTeamMemberHandler)
}
