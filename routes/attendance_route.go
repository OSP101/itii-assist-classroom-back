package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupAttendanceRoutes(app *fiber.App) {
	// Public check-in endpoints (no auth)
	app.Get("/api/attendance/check-in/:sessionId/info", handlers.GetSessionInfoHandler)
	app.Post("/api/attendance/check-in/:sessionId", handlers.StudentCheckInHandler)
	app.Post("/api/attendance/verify-student", handlers.VerifyStudentHandler)

	// Protected endpoints
	api := app.Group("/api/attendance", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetAttendanceSessionsHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), handlers.CreateAttendanceSessionHandler)
	api.Post("/:id/preview-section-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.PreviewSectionChangeHandler)
	api.Post("/:id/preview-time-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.PreviewTimeChangeHandler)
	api.Post("/:id/apply-time-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.ApplyTimeChangeHandler)
	api.Post("/:id/activate", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.ActivateAttendanceSessionHandler)
	api.Post("/:id/close", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.CloseAttendanceSessionHandler)
	api.Get("/:id/records", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.GetAttendanceRecordsHandler)
	api.Put("/:id/records/:recordId", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.UpdateAttendanceRecordByRecordIDHandler)
	api.Get("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.GetAttendanceSessionHandler)
	api.Put("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.UpdateAttendanceSessionHandler)
	api.Delete("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.DeleteAttendanceSessionHandler)
	api.Patch("/:id/records/:studentId", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.UpdateAttendanceRecordHandler)
	api.Post("/:id/records/bulk", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), handlers.BulkUpdateAttendanceRecordsHandler)
}
