package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupUserNotificationRoutes(app *fiber.App) {
	group := app.Group("/api/notifications", middlewares.Protected())
	group.Get("/", handlers.GetUserNotificationsHandler)
	group.Get("/count", handlers.GetNotificationCountHandler)
	group.Patch("/read-all", handlers.MarkAllNotificationsReadHandler)
	group.Patch("/:id/read", handlers.MarkNotificationReadHandler)
	group.Post("/announcements/:id/ack", handlers.AcknowledgeAnnouncementFromInboxHandler)
	group.Post("/announcements/:id/dismiss", handlers.DismissAnnouncementHandler)
	group.Delete("/clear", handlers.ClearReadNotificationsHandler)
}
