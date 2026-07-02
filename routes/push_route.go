package routes

import (
	"itii-assist/handlers"

	"github.com/gofiber/fiber/v3"
)

// SetupPushRoutes registers self-hosted Web Push (webpush-go, VAPID) endpoints.
// No external push provider account is required — the server owns its own
// VAPID key pair (VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY env vars). All routes
// are public (legacy-compatible: student devices book queue tickets without a
// JWT session, same trust model as the old /api/notifications/* endpoints).
func SetupPushRoutes(api *fiber.App) {
	push := api.Group("/api/push")

	push.Get("/vapid-public-key", handlers.GetVapidPublicKeyHandler)
	push.Post("/register", handlers.RegisterPushHandler)
	push.Post("/unsubscribe", handlers.UnsubscribePushHandler)
}
