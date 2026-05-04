package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupBonusScoreRoutes(app *fiber.App) {
	api := app.Group("/api/bonus-scores", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))

	api.Post("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromBody("course_id"), "instructor", "ta"), handlers.GiveBonusScoreHandler)
	api.Get("/course/:courseId/students", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetEnrolledStudentsForBonusHandler)
	api.Get("/course/:courseId/summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetBonusScoreSummaryHandler)
	api.Get("/course/:courseId/student/:studentId", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetStudentBonusHistoryHandler)
	api.Get("/course/:courseId", middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("courseId"), "instructor", "ta"), handlers.GetBonusScoresHandler)
	api.Get("/", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetBonusScoresHandler)
	api.Get("/summary", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetBonusScoreSummaryHandler)
	api.Get("/student", middlewares.RequireCourseAccess(middlewares.CourseIDFromQuery("course_id"), "instructor", "ta"), handlers.GetStudentBonusHistoryHandler)
	api.Delete("/:id", middlewares.RequireCourseAccess(middlewares.CourseIDFromBonusScoreParam("id"), "instructor", "ta"), handlers.DeleteBonusScoreHandler)
}
