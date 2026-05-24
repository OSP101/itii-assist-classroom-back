package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupExamRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	examHandler := handlers.NewExamHandler(auditLogger)
	api := app.Group("/api/courses/:courseId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"))

	api.Get("/exam-settings", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamSettingsHandler)
	api.Put("/exam-settings/:id", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionUpdateExamSettings, "instructor", "ta"), handlers.UpdateExamSettingHandler)
	api.Get("/exam-scores", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoresHandler)
	api.Get("/exam-scores/stats", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoreStatsHandler)
	api.Post("/exam-scores", middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), examHandler.UpsertExamScore)
	api.Post("/exam-scores/bulk", middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), handlers.BulkUpsertExamScoresHandler)
	api.Delete("/exam-scores/:scoreId", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionDeleteExamScores, "instructor", "ta"), handlers.DeleteExamScoreHandler)

	// ─── Exam Seat routes ─────────────────────────────────────────────────────

	// Exam sessions CRUD
	api.Get("/exam-sessions", handlers.GetExamSessionsHandler)
	api.Post("/exam-sessions", handlers.CreateExamSessionHandler)
	api.Post("/exam-sessions/import/preview", handlers.ImportExamSeatsPreviewHandler)
	api.Post("/exam-sessions/import/commit", handlers.ImportExamSeatsCommitHandler)
	api.Put("/exam-sessions/:sessionId", handlers.UpdateExamSessionHandler)
	api.Put("/exam-sessions/:sessionId/classrooms", handlers.UpdateExamSessionClassroomsHandler)
	api.Delete("/exam-sessions/:sessionId", handlers.DeleteExamSessionHandler)

	// Seat management within a session
	api.Get("/exam-sessions/:sessionId/seats", handlers.GetExamSeatsHandler)
	api.Post("/exam-sessions/:sessionId/seats", handlers.AssignExamSeatHandler)
	api.Post("/exam-sessions/:sessionId/seats/auto-assign", handlers.AutoAssignExamSeatsHandler)
	api.Put("/exam-sessions/:sessionId/seats", handlers.ReplaceExamSeatsHandler)
	api.Delete("/exam-sessions/:sessionId/seats", handlers.ClearExamSeatsHandler)
	api.Delete("/exam-sessions/:sessionId/seats/:seatId", handlers.UnassignExamSeatHandler)

	// Export
	api.Get("/exam-sessions/:sessionId/export", handlers.GetExamSeatingExportHandler)

	// ─── Student-facing (separate group with student role) ────────────────────
	studentApi := app.Group("/api/courses/:courseId", middlewares.Protected(), middlewares.RequireRole("student"))
	studentApi.Get("/my-exam-seats", handlers.GetMyExamSeatsHandler)
}

