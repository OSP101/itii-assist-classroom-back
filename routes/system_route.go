package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupSystemRoutes(app *fiber.App) {
	system := app.Group("/api/system", middlewares.Protected(), middlewares.RequireRole("admin"))

	system.Get("/metrics", handlers.GetSystemMetricsHandler)
	system.Get("/cpu", handlers.GetCpuUsageHandler)
	system.Get("/memory", handlers.GetMemoryUsageHandler)
	system.Get("/info", handlers.GetServerInfoHandler)
}
