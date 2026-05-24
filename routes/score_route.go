package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

func SetupScoreRoutes(app *fiber.App, auditLogger *services.AuditLogger) {
	scoreHandler := handlers.NewScoreHandler(auditLogger)
	serHandler := handlers.NewScoreEditRequestHandler(auditLogger)

	api := app.Group("/api/scores", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	api.Use(middlewares.RequireAdminFeature("menu.scores"))

	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentQuery("assignment_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentQuery("assignment_id"), []string{repositories.PermissionGradeAssignments, repositories.PermissionEditScores, repositories.PermissionViewScoreSummary}, "instructor", "ta"), handlers.GetScoresHandler)
	api.Get("/summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQuery("course_id"), repositories.PermissionViewScoreSummary, "instructor", "ta"), handlers.GetStudentScoresSummaryHandler)
	api.Get("/matrix", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQuery("course_id"), repositories.PermissionViewScoreSummary, "instructor", "ta"), handlers.GetScoreMatrixHandler)
	api.Get("/students/search", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQuery("course_id"), repositories.PermissionViewScoreSummary, "instructor", "ta"), handlers.SearchScoreStudentsHandler)
	api.Get("/groups", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentQuery("assignment_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentQuery("assignment_id"), []string{repositories.PermissionGradeAssignments, repositories.PermissionEditScores, repositories.PermissionViewScoreSummary}, "instructor", "ta"), handlers.GetGroupsForAssignmentHandler)
	api.Get("/ungraded-summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromQuery("course_id"), repositories.PermissionViewScoreSummary, "instructor", "ta"), handlers.GetUngradedSummaryHandler)
	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentBody("assignment_id"), []string{repositories.PermissionGradeAssignments, repositories.PermissionEditScores}, "instructor", "ta"), scoreHandler.SubmitScore)
	api.Post("/bulk", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentBody("assignment_id"), []string{repositories.PermissionGradeAssignments, repositories.PermissionEditScores}, "instructor", "ta"), handlers.BulkSubmitScoresHandler)
	api.Post("/group", middlewares.RequireCourseAccess(middlewares.CourseIDFromAssignmentBody("assignment_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromAssignmentBody("assignment_id"), []string{repositories.PermissionGradeAssignments, repositories.PermissionEditScores}, "instructor", "ta"), handlers.SubmitGroupScoreHandler)
	api.Post("/edit-request", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreBody("score_id"), "instructor", "ta"), handlers.CreateScoreEditRequestHandler)
	api.Get("/edit-requests", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromQuery("course_id"), []string{repositories.PermissionReviewOwnScoreRequests, repositories.PermissionReviewAllScoreRequests}, "instructor", "ta"), handlers.GetPendingEditRequestsHandler)
	api.Put("/edit-requests/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromScoreEditRequestParam("id"), repositories.PermissionReviewAllScoreRequests, "instructor", "ta"), handlers.ReviewEditRequestHandler)

	// /api/score-edit-requests — separate prefix used by frontend
	ser := app.Group("/api/score-edit-requests", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	ser.Use(middlewares.RequireAdminFeature("menu.scores"))
	ser.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromQuery("course_id"), []string{repositories.PermissionReviewOwnScoreRequests, repositories.PermissionReviewAllScoreRequests}, "instructor", "ta"), handlers.GetScoreEditRequestsCompatHandler)
	ser.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreBody("score_id"), "instructor", "ta"), handlers.CreateScoreEditRequestCompatHandler)
	ser.Post("/batch", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreBody("score_ids"), "instructor", "ta"), handlers.CreateBatchScoreEditRequestCompatHandler)
	ser.Post("/batch-detailed", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreEditBatchDetailedBody("edits"), "instructor", "ta"), handlers.CreateBatchDetailedScoreEditRequestCompatHandler)
	ser.Get("/pending-count", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), middlewares.RequireAnyCoursePermission(middlewares.CourseIDFromQuery("course_id"), []string{repositories.PermissionReviewOwnScoreRequests, repositories.PermissionReviewAllScoreRequests}, "instructor", "ta"), handlers.GetPendingCountHandler)
	ser.Delete("/:id/cancel", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), handlers.CancelScoreEditRequestCompatHandler)
	ser.Post("/:id/approve", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromScoreEditRequestParam("id"), repositories.PermissionReviewAllScoreRequests, "instructor", "ta"), serHandler.ApproveRequest)
	ser.Post("/:id/reject", middlewares.RequireCourseAccess(middlewares.CourseIDFromScoreEditRequestParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromScoreEditRequestParam("id"), repositories.PermissionReviewAllScoreRequests, "instructor", "ta"), serHandler.RejectRequest)
	ser.Post("/batch-approve", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), repositories.PermissionReviewAllScoreRequests, "instructor", "ta"), handlers.BatchApproveScoreEditRequestsCompatHandler)
	ser.Post("/batch-reject", middlewares.RequireCourseAccess(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDsFromScoreEditRequestBody("request_ids"), repositories.PermissionReviewAllScoreRequests, "instructor", "ta"), handlers.BatchRejectScoreEditRequestsCompatHandler)
}
