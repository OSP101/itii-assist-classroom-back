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
	oauth.Delete("/unlink/:provider", middlewares.Protected(), handlers.UnlinkAccountHandler)
}
