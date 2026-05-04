package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupScoreRoutes(app *fiber.App) {
	api := app.Group("/api/scores", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))

	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentQuery("assignment_id"), "instructor", "ta"), handlers.GetScoresHandler)
	api.Get("/summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetStudentScoresSummaryHandler)
	api.Get("/matrix", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetScoreMatrixHandler)
	api.Get("/students/search", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.SearchScoreStudentsHandler)
	api.Get("/groups", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentQuery("assignment_id"), "instructor", "ta"), handlers.GetGroupsForAssignmentHandler)
	api.Get("/ungraded-summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetUngradedSummaryHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), handlers.SubmitScoreHandler)
	api.Post("/bulk", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), handlers.BulkSubmitScoresHandler)
	api.Post("/group", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), handlers.SubmitGroupScoreHandler)
	api.Post("/edit-request", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreBody("score_id"), "instructor", "ta"), handlers.CreateScoreEditRequestHandler)
	api.Get("/edit-requests", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetPendingEditRequestsHandler)
	api.Put("/edit-requests/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), handlers.ReviewEditRequestHandler)

	// /api/score-edit-requests — separate prefix used by frontend
	ser := app.Group("/api/score-edit-requests", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	ser.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetScoreEditRequestsCompatHandler)
	ser.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreBody("score_id"), "instructor", "ta"), handlers.CreateScoreEditRequestCompatHandler)
	ser.Post("/batch", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreBody("score_ids"), "instructor", "ta"), handlers.CreateBatchScoreEditRequestCompatHandler)
	ser.Get("/pending-count", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetPendingCountHandler)
	ser.Delete("/:id/cancel", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), handlers.CancelScoreEditRequestCompatHandler)
	ser.Post("/:id/approve", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), handlers.ApproveScoreEditRequestCompatHandler)
	ser.Post("/:id/reject", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), handlers.RejectScoreEditRequestCompatHandler)
	ser.Post("/batch-approve", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), "instructor", "ta"), handlers.BatchApproveScoreEditRequestsCompatHandler)
	ser.Post("/batch-reject", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), "instructor", "ta"), handlers.BatchRejectScoreEditRequestsCompatHandler)
}
