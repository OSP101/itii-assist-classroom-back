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
	api := app.Group("/api/courses/:courseId", middlewares.Protected())

	api.Get("/exam-settings", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamSettingsHandler)
	api.Put("/exam-settings/:id", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionUpdateExamSettings, "instructor", "ta"), handlers.UpdateExamSettingHandler)
	api.Get("/exam-scores", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoresHandler)
	api.Get("/exam-scores/stats", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoreStatsHandler)
	api.Post("/exam-scores", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), examHandler.UpsertExamScore)
	api.Post("/exam-scores/bulk", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), handlers.BulkUpsertExamScoresHandler)
	api.Delete("/exam-scores/:scoreId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionDeleteExamScores, "instructor", "ta"), handlers.DeleteExamScoreHandler)

	// ─── Exam Seat routes ─────────────────────────────────────────────────────

	// Exam sessions CRUD
	api.Get("/exam-sessions", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSessionsHandler)
	api.Post("/exam-sessions", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.CreateExamSessionHandler)
	api.Post("/exam-sessions/import/preview", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ImportExamSeatsPreviewHandler)
	api.Post("/exam-sessions/import/commit", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ImportExamSeatsCommitHandler)
	api.Put("/exam-sessions/:sessionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UpdateExamSessionHandler)
	api.Put("/exam-sessions/:sessionId/classrooms", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UpdateExamSessionClassroomsHandler)
	api.Delete("/exam-sessions/:sessionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.DeleteExamSessionHandler)

	// Seat management within a session
	api.Get("/exam-sessions/:sessionId/seats", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSeatsHandler)
	api.Post("/exam-sessions/:sessionId/seats", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.AssignExamSeatHandler)
	api.Post("/exam-sessions/:sessionId/seats/auto-assign", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.AutoAssignExamSeatsHandler)
	api.Put("/exam-sessions/:sessionId/seats", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ReplaceExamSeatsHandler)
	api.Delete("/exam-sessions/:sessionId/seats", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ClearExamSeatsHandler)
	api.Delete("/exam-sessions/:sessionId/seats/:seatId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UnassignExamSeatHandler)

	// Export
	api.Get("/exam-sessions/:sessionId/export", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSeatingExportHandler)

	// ─── Student-facing (separate group with student role) ────────────────────
	api.Get("/my-exam-seats", middlewares.RequireRole("student"), handlers.GetMyExamSeatsHandler)
	api.Get("/my-exam-seats/layouts", middlewares.RequireRole("student"), handlers.GetMyExamSeatLayoutsHandler)
}
