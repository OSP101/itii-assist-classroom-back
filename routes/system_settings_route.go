package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupSystemSettingsRoutes(app *fiber.App) {
	// Public endpoint — no auth required
	app.Get("/api/maintenance-status", handlers.GetPublicMaintenanceStatusHandler)

	settingsPublic := app.Group("/api/system-settings", middlewares.Protected())
	settingsPublic.Get("/announcements/active", handlers.ListActiveAnnouncementsForCurrentUserHandler)
	settingsPublic.Get("/feature-flags", handlers.ListFeatureFlagsHandler)

	settings := app.Group("/api/system-settings", middlewares.Protected(), middlewares.RequireRole("admin"))

	settings.Get("/backups", handlers.GetBackupRecordsHandler)
	settings.Get("/backups/status", handlers.GetBackupStatusHandler)
	settings.Post("/backups/run-now", middlewares.RequirePrivilegedStepUp("system_settings.backups.run_now"), handlers.RunBackupNowHandler)
	settings.Post("/backups/restore", middlewares.RequirePrivilegedStepUp("system_settings.backups.restore"), handlers.RestoreBackupHandler)
	settings.Get("/backups/:id/download-url", middlewares.RequirePrivilegedStepUp("system_settings.backups.download"), handlers.GetBackupDownloadURLHandler)

	settings.Get("/announcements", handlers.ListAnnouncementsHandler)
	settings.Post("/announcements/upload-image", handlers.UploadAnnouncementImageHandler)
	settings.Post("/announcements", handlers.CreateAnnouncementHandler)
	settings.Put("/announcements/:id", handlers.UpdateAnnouncementHandler)
	settings.Post("/announcements/:id/ack", handlers.AcknowledgeAnnouncementHandler)

	settings.Put("/feature-flags/:key", middlewares.RequirePrivilegedStepUp("system_settings.feature_flags.update"), handlers.UpdateFeatureFlagHandler)

	settings.Get("/maintenance", handlers.GetMaintenanceModeHandler)
	settings.Put("/maintenance", middlewares.RequirePrivilegedStepUp("system_settings.maintenance.update"), handlers.UpdateMaintenanceModeHandler)

	settings.Get("/health", handlers.GetServiceHealthHandler)
}
