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
	coursePrefix := "/api/courses/:courseId"

	app.Get(coursePrefix+"/exam-settings",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"),
		handlers.GetExamSettingsHandler,
	)
	app.Put(coursePrefix+"/exam-settings/:id",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionUpdateExamSettings, "instructor", "ta"),
		handlers.UpdateExamSettingHandler,
	)
	app.Get(coursePrefix+"/exam-scores",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"),
		handlers.GetExamScoresHandler,
	)
	app.Get(coursePrefix+"/exam-scores/stats",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewExamScores, "instructor", "ta"),
		handlers.GetExamScoreStatsHandler,
	)
	app.Post(coursePrefix+"/exam-scores",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"),
		examHandler.UpsertExamScore,
	)
	app.Post(coursePrefix+"/exam-scores/bulk",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromParam("courseId"), []string{repositories.PermissionCreateExamScores, repositories.PermissionUpdateExamScores}, "instructor", "ta"),
		handlers.BulkUpsertExamScoresHandler,
	)
	app.Delete(coursePrefix+"/exam-scores/:scoreId",
		middlewares.Protected(),
		middlewares.RequireRole("admin", "instructor", "ta"),
		middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"),
		middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionDeleteExamScores, "instructor", "ta"),
		handlers.DeleteExamScoreHandler,
	)

	app.Get(coursePrefix+"/exam-sessions", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSessionsHandler)
	app.Post(coursePrefix+"/exam-sessions", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.CreateExamSessionHandler)
	app.Post(coursePrefix+"/exam-sessions/import/preview", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ImportExamSeatsPreviewHandler)
	app.Post(coursePrefix+"/exam-sessions/import/commit", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ImportExamSeatsCommitHandler)
	app.Put(coursePrefix+"/exam-sessions/:sessionId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UpdateExamSessionHandler)
	app.Put(coursePrefix+"/exam-sessions/:sessionId/classrooms", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UpdateExamSessionClassroomsHandler)
	app.Delete(coursePrefix+"/exam-sessions/:sessionId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.DeleteExamSessionHandler)

	app.Get(coursePrefix+"/exam-sessions/:sessionId/seats", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSeatsHandler)
	app.Post(coursePrefix+"/exam-sessions/:sessionId/seats", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.AssignExamSeatHandler)
	app.Post(coursePrefix+"/exam-sessions/:sessionId/seats/auto-assign", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.AutoAssignExamSeatsHandler)
	app.Put(coursePrefix+"/exam-sessions/:sessionId/seats", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ReplaceExamSeatsHandler)
	app.Delete(coursePrefix+"/exam-sessions/:sessionId/seats", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.ClearExamSeatsHandler)
	app.Delete(coursePrefix+"/exam-sessions/:sessionId/seats/:seatId", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.UnassignExamSeatHandler)

	app.Get(coursePrefix+"/exam-sessions/:sessionId/export", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetExamSeatingExportHandler)

	app.Get(coursePrefix+"/my-exam-seats", middlewares.Protected(), middlewares.RequireRole("student"), handlers.GetMyExamSeatsHandler)
}
