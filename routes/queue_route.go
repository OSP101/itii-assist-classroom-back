package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

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

	legacyProtected := app.Group("/api/queue", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	legacyProtected.Post("/sessions/:sessionId/status", middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"), handlers.UpdateQueueSessionStatusPublicHandler)

	publicCourse := app.Group("/api/courses/:courseId/queue")
	publicCourse.Post("/verify-pin", handlers.VerifyQueuePINPublicHandler)
	publicCourse.Post("/bookings", handlers.CreateQueueBookingPublicHandler)
	publicCourse.Get("/bookings/:bookingId/status", handlers.GetQueueBookingStatusPublicHandler)
	publicCourse.Post("/bookings/:bookingId/cancel", handlers.CancelQueueBookingPublicHandler)
	publicCourse.Get("/sessions/:sessionId/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)

	base := app.Group("/api/courses/:courseId/queue", middlewares.Protected())

	// Session management (instructor/ta level)
	mgmt := base.Group("/sessions", middlewares.RequireRole("admin", "instructor", "ta"))
	mgmt.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetQueueSessionsHandler)
	mgmt.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.CreateQueueSessionHandler)

	sessionMgmt := base.Group("/sessions/:sessionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromQueueSessionParam("sessionId"), "instructor", "ta"))
	sessionMgmt.Get("/", handlers.GetQueueSessionHandler)
	sessionMgmt.Put("/", handlers.UpdateQueueSessionHandler)
	sessionMgmt.Post("/status", handlers.UpdateQueueSessionStatusCompatHandler)
	sessionMgmt.Delete("/", handlers.DeleteQueueSessionHandler)
	sessionMgmt.Post("/regenerate-pin", handlers.RegenerateQueuePINHandler)
	sessionMgmt.Post("/start", handlers.StartQueueSessionHandler)
	sessionMgmt.Post("/pause", handlers.PauseQueueSessionHandler)
	sessionMgmt.Post("/resume", handlers.ResumeQueueSessionHandler)
	sessionMgmt.Post("/close", handlers.CloseQueueSessionHandler)
	sessionMgmt.Get("/workers", handlers.GetWorkersHandler)
	sessionMgmt.Get("/desks", handlers.GetDeskStatusesHandler)
	sessionMgmt.Get("/desk-statuses", handlers.GetQueueDeskStatusesPublicHandler)
	sessionMgmt.Post("/worker/join", handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/join", handlers.WorkerJoinHandler)
	sessionMgmt.Post("/workers/leave", handlers.WorkerLeaveHandler)
	sessionMgmt.Get("/workers/current-booking", handlers.GetWorkerCurrentBookingHandler)
	sessionMgmt.Get("/bookings", handlers.GetBookingsHandler)
	sessionMgmt.Post("/bookings/:bookingId/complete", handlers.CompleteQueueBookingCompatHandler)
	sessionMgmt.Post("/bookings/:bookingId/skip", handlers.SkipQueueBookingCompatHandler)
	sessionMgmt.Put("/bookings/:bookingId/action", handlers.WorkerBookingActionHandler)

	// Student-accessible endpoints (any authenticated user)
	student := base.Group("/sessions/:sessionId")
	student.Post("/verify-pin", handlers.VerifyQueuePINHandler)
	student.Post("/bookings", handlers.CreateBookingHandler)
	student.Get("/bookings/student/:studentId", handlers.GetStudentBookingHandler)
	student.Delete("/bookings/:bookingId", handlers.CancelBookingHandler)
}
