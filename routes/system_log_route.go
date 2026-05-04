package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupSystemLogRoutes(api *fiber.App) {
	logs := api.Group("/api/logs", middlewares.Protected(), middlewares.RequireRole("admin"))
	logs.Get("/filters", handlers.GetLogFiltersHandler)
	logs.Get("/stats", handlers.GetLogStatsHandler)
	logs.Get("/timeline", handlers.GetLogsTimelineHandler)
	logs.Get("/export", handlers.ExportLogsHandler)
	logs.Get("/errors/recent", handlers.GetRecentErrorsHandler)
	logs.Get("/security/recent", handlers.GetRecentSecurityEventsHandler)
	logs.Get("/", handlers.GetLogsHandler)
	logs.Get("/:id", handlers.GetLogByIDHandler)
	logs.Post("/cleanup", handlers.CleanupLogsHandler)
}
