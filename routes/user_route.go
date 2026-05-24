package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupUserRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	userHandler := handlers.NewUserHandler(auditLogger)
	api := app.Group("/api/users", middlewares.Protected(), middlewares.RequireRole("admin"))
	api.Use(middlewares.RequireAdminFeature("menu.users"))

	api.Get("/", handlers.GetUsersHandler)                 // GET    /api/users
	api.Get("/stats", handlers.GetUserStatsHandler)        // GET    /api/users/stats
	api.Get("/:id", handlers.GetUserByIDHandler)           // GET    /api/users/:id
	api.Post("/", handlers.CreateUserHandler)              // POST   /api/users
	api.Put("/:id", handlers.UpdateUserHandler)            // PUT    /api/users/:id
	api.Patch("/:id/status", userHandler.ToggleUserStatus) // PATCH  /api/users/:id/status
	api.Delete("/:id", handlers.DeleteUserHandler)         // DELETE /api/users/:id
}
