package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupNotificationRoutes(api *fiber.App) {
	notifications := api.Group("/api/notifications")

	// Protected routes
	protected := notifications.Group("/", middlewares.Protected())
	protected.Get("/tokens", handlers.GetUserTokensHandler)
	protected.Get("/logs", handlers.GetNotificationLogsHandler)
}
