package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupClassroomRoutes(app *fiber.App) {
	// Public desk lookup (used by QR scanner – no auth needed)
	app.Get("/api/desks/:deskId", handlers.GetDeskPublicHandler)

	api := app.Group("/api/classrooms", middlewares.Protected())

	api.Get("/stats", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetClassroomStatsHandler)
	api.Get("/", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetClassroomsHandler)
	api.Post("/", middlewares.RequireRole("admin", "instructor"), handlers.CreateClassroomHandler)
	api.Get("/:id", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetClassroomByIDHandler)
	api.Put("/:id", middlewares.RequireRole("admin", "instructor"), handlers.UpdateClassroomHandler)
	api.Put("/:id/layout", middlewares.RequireRole("admin", "instructor", "ta"), handlers.UpdateClassroomLayoutHandler)
	api.Patch("/:id/toggle-status", middlewares.RequireRole("admin", "instructor"), handlers.ToggleClassroomStatusHandler)
	api.Post("/:id/restore", middlewares.RequireRole("admin"), handlers.RestoreClassroomHandler)
	api.Delete("/:id", middlewares.RequireRole("admin", "instructor"), handlers.DeleteClassroomHandler)
}
