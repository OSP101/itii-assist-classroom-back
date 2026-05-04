package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupNotificationRoutes(api *fiber.App) {
	notifications := api.Group("/api/notifications")

	// Public routes (legacy-compatible: student devices may not be authenticated)
	notifications.Post("/register", handlers.RegisterTokenHandler)
	notifications.Post("/unregister", handlers.UnregisterTokenHandler)
	notifications.Post("/update-booking", handlers.UpdateBookingTokenHandler)

	// Protected routes
	protected := notifications.Group("/", middlewares.Protected())
	protected.Get("/tokens", handlers.GetUserTokensHandler)
	protected.Get("/logs", handlers.GetNotificationLogsHandler)
}
