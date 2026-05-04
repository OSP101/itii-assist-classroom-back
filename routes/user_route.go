package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupUserRoutes(app *fiber.App) {
	api := app.Group("/api/users", middlewares.Protected(), middlewares.RequireRole("admin"))

	api.Get("/", handlers.GetUsersHandler)                     // GET    /api/users
	api.Get("/stats", handlers.GetUserStatsHandler)            // GET    /api/users/stats
	api.Get("/:id", handlers.GetUserByIDHandler)               // GET    /api/users/:id
	api.Post("/", handlers.CreateUserHandler)                  // POST   /api/users
	api.Put("/:id", handlers.UpdateUserHandler)                // PUT    /api/users/:id
	api.Patch("/:id/status", handlers.ToggleUserStatusHandler) // PATCH  /api/users/:id/status
	api.Delete("/:id", handlers.DeleteUserHandler)             // DELETE /api/users/:id
}
