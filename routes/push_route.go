package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

// SetupPushRoutes exposes the public VAPID key; registration requires a verified owner.
func SetupPushRoutes(api *fiber.App) {
	push := api.Group("/api/push")

	push.Get("/vapid-public-key", handlers.GetVapidPublicKeyHandler)
	push.Post("/register", middlewares.Protected(), handlers.RegisterPushHandler)
	push.Post("/unsubscribe", handlers.UnsubscribePushHandler)

	// Diagnostic push — signed-in users can trigger a self-test to verify
	// their device subscription actually receives background notifications.
	push.Post("/test", middlewares.Protected(), handlers.SendTestPushHandler)
}
