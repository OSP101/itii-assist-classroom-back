package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupFeedbackRoutes(app *fiber.App) {
	// Public support ticket intake
	app.Post("/api/feedback/support", middlewares.SupportTicketGuard(), handlers.CreateSupportTicketHandler)

	// Authenticated: submit feedback & view own
	auth := app.Group("/api/feedback", middlewares.Protected())
	auth.Post("/", handlers.CreateFeedbackHandler)
	auth.Get("/my", handlers.GetMyFeedbacksHandler)

	// Admin only: manage all feedbacks
	admin := app.Group("/api/feedback", middlewares.Protected(), middlewares.RequireRole("admin"))
	admin.Get("/stats", handlers.GetFeedbackStatsHandler)
	admin.Get("/", handlers.GetAllFeedbacksHandler)
	admin.Get("/:id", handlers.GetFeedbackByIDHandler)
	admin.Put("/:id", handlers.UpdateFeedbackHandler)
	admin.Delete("/:id", handlers.DeleteFeedbackHandler)
}
