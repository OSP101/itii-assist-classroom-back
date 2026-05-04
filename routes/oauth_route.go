package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupOAuthRoutes(app *fiber.App) {
	oauth := app.Group("/api/oauth")

	// Protected — current user
	oauth.Get("/linked", middlewares.Protected(), handlers.GetLinkedAccountsHandler)
	oauth.Post("/link", middlewares.Protected(), handlers.LinkAccountHandler)
	oauth.Delete("/unlink/:provider", middlewares.Protected(), handlers.UnlinkAccountHandler)

	// Admin only
	oauth.Get("/admin/user/:userId", middlewares.Protected(), middlewares.RequireRole("admin"), handlers.AdminGetLinkedAccountsHandler)
	oauth.Delete("/admin/user/:userId/:provider", middlewares.Protected(), middlewares.RequireRole("admin"), handlers.AdminUnlinkAccountHandler)
}
