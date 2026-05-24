package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupSystemRoutes(app *fiber.App) {
	system := app.Group("/api/system", middlewares.Protected(), middlewares.RequireRole("admin"))
	system.Use(middlewares.RequireAdminFeature("menu.monitoring"))

	system.Get("/metrics", handlers.GetSystemMetricsHandler)
	system.Get("/cpu", handlers.GetCpuUsageHandler)
	system.Get("/memory", handlers.GetMemoryUsageHandler)
	system.Get("/info", handlers.GetServerInfoHandler)
	system.Get("/cloud/overview", handlers.GetCloudOverviewHandler)
	system.Get("/cloud/cost", handlers.GetCloudCostHandler)
	system.Get("/trends", handlers.GetMonitoringTrendsHandler)
	system.Get("/containers", handlers.GetContainerMetricsHandler)
	system.Get("/website", handlers.GetWebsiteMetricsHandler)
	system.Get("/actions/history", handlers.GetSystemOperationHistoryHandler)
	system.Post("/actions/restart-service", middlewares.RequirePrivilegedStepUp("system.monitoring.operations.restart_service"), handlers.RestartSystemServiceHandler)
	system.Post("/actions/reboot-host", middlewares.RequirePrivilegedStepUp("system.monitoring.operations.reboot_host"), handlers.RebootSystemHostHandler)
	system.Post("/actions/cancel", middlewares.RequirePrivilegedStepUp("system.monitoring.operations.cancel"), handlers.CancelSystemOperationHandler)
}
