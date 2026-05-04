package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupExamRoutes(app *fiber.App) {
	api := app.Group("/api/courses/:courseId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"))

	api.Get("/exam-settings", handlers.GetExamSettingsHandler)
	api.Put("/exam-settings/:id", handlers.UpdateExamSettingHandler)
	api.Get("/exam-scores", handlers.GetExamScoresHandler)
	api.Get("/exam-scores/stats", handlers.GetExamScoreStatsHandler)
	api.Post("/exam-scores", handlers.UpsertExamScoreHandler)
	api.Post("/exam-scores/bulk", handlers.BulkUpsertExamScoresHandler)
	api.Delete("/exam-scores/:scoreId", handlers.DeleteExamScoreHandler)
}
