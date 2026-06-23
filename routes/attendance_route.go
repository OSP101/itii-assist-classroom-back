package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupAttendanceRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	attendanceHandler := handlers.NewAttendanceHandler(auditLogger)
	// Public check-in endpoints (no auth)
	app.Get("/api/attendance/check-in/:sessionId/info", handlers.GetSessionInfoHandler)
	app.Post("/api/attendance/check-in/:sessionId", middlewares.AttendanceCheckInGuard(), handlers.StudentCheckInHandler)
	app.Post("/api/attendance/check-in", middlewares.AttendanceCheckInGuard(), handlers.StudentCheckInByPINHandler)
	app.Post("/api/attendance/verify-pin", handlers.VerifyAttendancePINHandler)
	app.Post("/api/attendance/verify-student", handlers.VerifyStudentHandler)
	app.Post("/api/attendance/display/bootstrap", handlers.BootstrapAttendanceDisplayHandler)
	app.Post("/api/attendance/display/confirm", handlers.ConfirmAttendanceDisplayHandler)
	app.Get("/api/attendance/display/pairing-status", handlers.GetAttendanceDisplayPairingStatusHandler)
	app.Get("/api/attendance/display/current", handlers.GetAttendanceDisplayCurrentHandler)
	app.Get("/api/attendance/display/records", handlers.GetAttendanceDisplayRecordsHandler)
	app.Get("/api/attendance/display/socket-ticket", handlers.GetAttendanceDisplaySocketTicketHandler)
	app.Post("/api/attendance/display/heartbeat", handlers.HeartbeatAttendanceDisplayHandler)

	display := app.Group("/api/attendance/display", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	display.Get("/pairings/:token", handlers.GetAttendanceDisplayPairingHandler)
	display.Post("/pairings/:token/claim", handlers.ClaimAttendanceDisplayPairingHandler)
	display.Post("/grants/:id/revoke", handlers.RevokeAttendanceDisplayGrantHandler)

	// Protected endpoints
	api := app.Group("/api/attendance", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	api.Use(middlewares.RequireAdminFeature("menu.attendance"))
	api.Post("/sessions/start", handlers.StartAttendanceSessionHandler)
	api.Get("/sessions/:id/pin", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionViewAttendance, "instructor", "ta"), handlers.GetAttendanceSessionPinHandler)
	api.Post("/sessions/:id/rotate", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.RotateAttendanceSessionPinHandler)
	api.Post("/sessions/:id/close", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.CloseAttendanceSessionHandler)
	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQuery("course_id"), repositories.PermissionViewAttendance, "instructor", "ta"), handlers.GetAttendanceSessionsHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromBody("course_id"), repositories.PermissionCreateAttendanceSessions, "instructor", "ta"), attendanceHandler.CreateAttendanceSession)
	api.Post("/:id/preview-section-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.PreviewSectionChangeHandler)
	api.Post("/:id/preview-time-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.PreviewTimeChangeHandler)
	api.Post("/:id/apply-time-change", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.ApplyTimeChangeHandler)
	api.Post("/:id/activate", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.ActivateAttendanceSessionHandler)
	api.Post("/:id/close", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.CloseAttendanceSessionHandler)
	api.Get("/:id/records", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionViewAttendance, "instructor", "ta"), handlers.GetAttendanceRecordsHandler)
	api.Put("/:id/records/:recordId", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceStatus, "instructor", "ta"), attendanceHandler.UpdateAttendanceRecordByRecordID)
	api.Get("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionViewAttendance, "instructor", "ta"), handlers.GetAttendanceSessionHandler)
	api.Put("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceSessions, "instructor", "ta"), handlers.UpdateAttendanceSessionHandler)
	api.Delete("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionDeleteAttendanceSessions, "instructor", "ta"), handlers.DeleteAttendanceSessionHandler)
	api.Patch("/:id/records/:studentId", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceStatus, "instructor", "ta"), attendanceHandler.UpdateAttendanceRecord)
	api.Post("/:id/records/bulk", middlewares.RequireCourseAccess(middlewares.CourseIDFromAttendanceSessionParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromAttendanceSessionParam("id"), repositories.PermissionUpdateAttendanceStatus, "instructor", "ta"), handlers.BulkUpdateAttendanceRecordsHandler)
}
