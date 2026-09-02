package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

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
	// OptionalProtected, not Protected: a student device registers pre-auth
	// (same trust model as booking), but RegisterPushHandler requires a real
	// session before it will accept a "worker" registration - this middleware
	// is what populates c.Locals("user_id") for that check when a session
	// cookie/token is present, without blocking the anonymous student path.
	push.Post("/register", middlewares.OptionalProtected(), handlers.RegisterPushHandler)
	push.Post("/unsubscribe", handlers.UnsubscribePushHandler)

	// Diagnostic push — signed-in users can trigger a self-test to verify
	// their device subscription actually receives background notifications.
	push.Post("/test", middlewares.Protected(), handlers.SendTestPushHandler)
}
