package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupQueueRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	queueHandler := handlers.NewQueueHandler(auditLogger)
	public := app.Group("/api/queue")
	public.Post("/verify-pin", handlers.VerifyQueuePINPublicHandler)
	public.Post("/validate", handlers.ValidateQueueBookingInfoPublicHandler)
	public.Post("/check-existing", handlers.CheckExistingQueueBookingPublicHandler)
	public.Post("/bookings", handlers.CreateQueueBookingPublicHandler)
	public.Get("/bookings/:bookingId/status", handlers.GetQueueBookingStatusPublicHandler)
	public.Post("/bookings/:bookingId/cancel", handlers.CancelQueueBookingPublicHandler)
	public.Get("/sessions/:sessionId/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)
	public.Post("/sessions/:sessionId/heartbeat", handlers.HeartbeatQueueProjectorSessionHandler)
	public.Post("/sessions/:sessionId/status", handlers.UpdateQueueSessionStatusPublicHandler)
	public.Post("/sessions/:sessionId/cutoff", handlers.UpdateQueueSessionCutoffPublicHandler)
	public.Get("/classroom/:classroomId/active-sessions", handlers.GetClassroomActiveSessionsPublicHandler)
	public.Get("/sessions/:sessionId/concurrent-sessions", handlers.GetConcurrentSessionsPublicHandler)
	public.Get("/groups/:groupId/desk-statuses", handlers.GetGroupDeskStatusesPublicHandler)

	legacyProtected := app.Group("/api/queue", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	legacyProtected.Use(middlewares.RequireAdminFeature("menu.queue"))
	legacyProtected.Post("/sessions/:sessionId/status", middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionStatusPublicHandler)

	publicCourse := app.Group("/api/courses/:courseId/queue")
	publicCourse.Post("/verify-pin", handlers.VerifyQueuePINPublicHandler)
	publicCourse.Post("/bookings", handlers.CreateQueueBookingPublicHandler)
	publicCourse.Get("/bookings/:bookingId/status", handlers.GetQueueBookingStatusPublicHandler)
	publicCourse.Post("/bookings/:bookingId/cancel", handlers.CancelQueueBookingPublicHandler)
	publicCourse.Get("/sessions/:sessionId/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)

	base := app.Group("/api/courses/:courseId/queue", middlewares.Protected())
	base.Use(middlewares.RequireAdminFeature("menu.queue"))

	// Session management (instructor/ta level)
	mgmt := base.Group("/sessions")
	mgmt.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionsHandler)
	mgmt.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionCreateQueueSessions, "instructor", "ta"), handlers.CreateQueueSessionHandler)

	sessionMgmt := base.Group("/sessions/:sessionId", middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"))
	sessionMgmt.Get("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionHandler)
	sessionMgmt.Put("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionHandler)
	sessionMgmt.Post("/status", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionStatusCompatHandler)
	sessionMgmt.Delete("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionDeleteQueueSessions, "instructor", "ta"), handlers.DeleteQueueSessionHandler)
	sessionMgmt.Post("/regenerate-pin", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.RegenerateQueuePINHandler)
	sessionMgmt.Post("/start", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), queueHandler.StartQueueSession)
	sessionMgmt.Post("/pause", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.PauseQueueSessionHandler)
	sessionMgmt.Post("/resume", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.ResumeQueueSessionHandler)
	sessionMgmt.Post("/close", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), queueHandler.CloseQueueSession)
	sessionMgmt.Post("/heartbeat", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.HeartbeatQueueProjectorSessionHandler)
	sessionMgmt.Get("/workers", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetWorkersHandler)
	sessionMgmt.Get("/desks", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetDeskStatusesHandler)
	sessionMgmt.Get("/desk-statuses", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueDeskStatusesPublicHandler)
	sessionMgmt.Post("/worker/join", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/join", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/leave", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerLeaveHandler)
	sessionMgmt.Get("/workers/current-booking", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetWorkerCurrentBookingHandler)
	sessionMgmt.Get("/bookings", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetBookingsHandler)
	sessionMgmt.Get("/report", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionReportHandler)
	// Worker actions (complete / skip / action) intentionally sit OUTSIDE sessionMgmt
	// so they do not inherit the group-level RequireCourseAccess that would 403 a
	// mirrored TA operating on partner-session bookings in a shared-room concurrent
	// group. Authorization is handled by RequireQueueWorkerOrCoursePermission which
	// accepts either a course-role permission match or an existing queue_workers row.
	workerActions := base.Group("/sessions/:sessionId")
	workerActions.Post("/bookings/:bookingId/complete", middlewares.RequireQueueWorkerOrCoursePermission("sessionId", repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.CompleteQueueBookingCompatHandler)
	workerActions.Post("/bookings/:bookingId/skip", middlewares.RequireQueueWorkerOrCoursePermission("sessionId", repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.SkipQueueBookingCompatHandler)
	workerActions.Put("/bookings/:bookingId/action", middlewares.RequireQueueWorkerOrCoursePermission("sessionId", repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.WorkerBookingActionHandler)
	sessionMgmt.Get("/group", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetConcurrentGroupHandler)
	sessionMgmt.Post("/group/link", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.LinkConcurrentSessionsHandler)
	sessionMgmt.Delete("/group/unlink", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UnlinkConcurrentSessionHandler)

	// Student-accessible endpoints (any authenticated user)
	student := base.Group("/sessions/:sessionId")
	student.Post("/verify-pin", handlers.VerifyQueuePINHandler)
	student.Post("/bookings", queueHandler.CreateBooking)
	student.Get("/bookings/student/:studentId", handlers.GetStudentBookingHandler)
	student.Delete("/bookings/:bookingId", handlers.CancelBookingHandler)

	// Admin-only: list all active queue sessions across all courses
	adminQueue := app.Group("/api/admin/queue", middlewares.Protected(), middlewares.RequireRole("admin"))
	adminQueue.Get("/sessions/active", handlers.GetActiveQueueSessionsAdminHandler)
}
