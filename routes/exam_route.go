package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func SetupExamRoutes(app *fiber.App) {
	api := app.Group("/api/courses/:courseId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"))

	api.Get("/exam-settings", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamSettingsHandler)
	api.Put("/exam-settings/:id", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionUpdateExamSettings, "instructor", "ta"), handlers.UpdateExamSettingHandler)
	api.Get("/exam-scores", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoresHandler)
	api.Get("/exam-scores/stats", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"), handlers.GetExamScoreStatsHandler)
	api.Post("/exam-scores", middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), handlers.UpsertExamScoreHandler)
	api.Post("/exam-scores/bulk", middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"), handlers.BulkUpsertExamScoresHandler)
	api.Delete("/exam-scores/:scoreId", middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionDeleteExamScores, "instructor", "ta"), handlers.DeleteExamScoreHandler)
}
