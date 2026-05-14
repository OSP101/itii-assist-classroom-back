package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func SetupQueueRoutes(app *fiber.App) {
	public := app.Group("/api/queue")
	public.Post("/verify-pin", handlers.VerifyQueuePINPublicHandler)
	public.Post("/validate", handlers.ValidateQueueBookingInfoPublicHandler)
	public.Post("/check-existing", handlers.CheckExistingQueueBookingPublicHandler)
	public.Post("/bookings", handlers.CreateQueueBookingPublicHandler)
	public.Get("/bookings/:bookingId/status", handlers.GetQueueBookingStatusPublicHandler)
	public.Post("/bookings/:bookingId/cancel", handlers.CancelQueueBookingPublicHandler)
	public.Get("/sessions/:sessionId/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)
	public.Post("/sessions/:sessionId/status", handlers.UpdateQueueSessionStatusPublicHandler)
	public.Post("/sessions/:sessionId/cutoff", handlers.UpdateQueueSessionCutoffPublicHandler)

	legacyProtected := app.Group("/api/queue", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	legacyProtected.Post("/sessions/:sessionId/status", middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionStatusPublicHandler)

	publicCourse := app.Group("/api/courses/:courseId/queue")
	publicCourse.Post("/verify-pin", handlers.VerifyQueuePINPublicHandler)
	publicCourse.Post("/bookings", handlers.CreateQueueBookingPublicHandler)
	publicCourse.Get("/bookings/:bookingId/status", handlers.GetQueueBookingStatusPublicHandler)
	publicCourse.Post("/bookings/:bookingId/cancel", handlers.CancelQueueBookingPublicHandler)
	publicCourse.Get("/sessions/:sessionId/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)

	base := app.Group("/api/courses/:courseId/queue", middlewares.Protected())

	// Session management (instructor/ta level)
	mgmt := base.Group("/sessions", middlewares.RequireRole("admin", "instructor", "ta"))
	mgmt.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionsHandler)
	mgmt.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("courseId"), repositories.PermissionCreateQueueSessions, "instructor", "ta"), handlers.CreateQueueSessionHandler)

	sessionMgmt := base.Group("/sessions/:sessionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"))
	sessionMgmt.Get("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionHandler)
	sessionMgmt.Put("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionHandler)
	sessionMgmt.Post("/status", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.UpdateQueueSessionStatusCompatHandler)
	sessionMgmt.Delete("/", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionDeleteQueueSessions, "instructor", "ta"), handlers.DeleteQueueSessionHandler)
	sessionMgmt.Post("/regenerate-pin", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.RegenerateQueuePINHandler)
	sessionMgmt.Post("/start", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.StartQueueSessionHandler)
	sessionMgmt.Post("/pause", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.PauseQueueSessionHandler)
	sessionMgmt.Post("/resume", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.ResumeQueueSessionHandler)
	sessionMgmt.Post("/close", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.CloseQueueSessionHandler)
	sessionMgmt.Get("/workers", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetWorkersHandler)
	sessionMgmt.Get("/desks", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetDeskStatusesHandler)
	sessionMgmt.Get("/desk-statuses", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueDeskStatusesPublicHandler)
	sessionMgmt.Post("/worker/join", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/join", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/leave", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionUpdateQueueSessions, "instructor", "ta"), handlers.WorkerLeaveHandler)
	sessionMgmt.Get("/workers/current-booking", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetWorkerCurrentBookingHandler)
	sessionMgmt.Get("/bookings", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetBookingsHandler)
	sessionMgmt.Get("/report", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionViewQueue, "instructor", "ta"), handlers.GetQueueSessionReportHandler)
	sessionMgmt.Post("/bookings/:bookingId/complete", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.CompleteQueueBookingCompatHandler)
	sessionMgmt.Post("/bookings/:bookingId/skip", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.SkipQueueBookingCompatHandler)
	sessionMgmt.Put("/bookings/:bookingId/action", middlewares.RequireCoursePermission(middlewares.CourseIDFromQueueSessionParam("sessionId"), repositories.PermissionManageQueueBookings, "instructor", "ta"), handlers.WorkerBookingActionHandler)

	// Student-accessible endpoints (any authenticated user)
	student := base.Group("/sessions/:sessionId")
	student.Post("/verify-pin", handlers.VerifyQueuePINHandler)
	student.Post("/bookings", handlers.CreateBookingHandler)
	student.Get("/bookings/student/:studentId", handlers.GetStudentBookingHandler)
	student.Delete("/bookings/:bookingId", handlers.CancelBookingHandler)
}
