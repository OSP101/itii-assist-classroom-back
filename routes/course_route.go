package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupCourseRoutes(app *fiber.App) {
	protected := app.Group("/api/courses", middlewares.Protected())

	// Dropdown helpers (admin, instructor, ta)
	protected.Get("/instructors", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetInstructorsListHandler)
	protected.Get("/tas-list", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetTAsListHandler)

	// My courses (instructor, ta)
	protected.Get("/my-courses", middlewares.RequireRole("instructor", "ta"), handlers.GetMyCoursesHandler)
	protected.Get("/my-courses/stats", middlewares.RequireRole("instructor", "ta"), handlers.GetMyCoursesStatsHandler)

	// Stats (admin, instructor)
	protected.Get("/stats", middlewares.RequireRole("admin", "instructor"), handlers.GetCourseStatsHandler)

	// Course CRUD
	protected.Get("/", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetCoursesHandler)
	protected.Get("/:id/overview", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.GetCourseOverviewHandler)
	protected.Get("/:id", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.GetCourseByIDHandler)
	protected.Post("/", middlewares.RequireRole("admin", "instructor"), handlers.CreateCourseHandler)
	protected.Put("/:id", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.UpdateCourseHandler)
	protected.Delete("/:id", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.DeleteCourseHandler)
	protected.Patch("/:id/toggle-status", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.ToggleCourseStatusHandler)

	// Section management (admin, instructor, ta — checked inside handler for course membership)
	protected.Post("/:id/sections", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.AddSectionHandler)
	protected.Put("/:id/sections/:sectionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.UpdateSectionHandler)
	protected.Delete("/:id/sections/:sectionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.RemoveSectionHandler)

	// TA management (admin, instructor of course)
	protected.Post("/:id/tas", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.AddTAHandler)
	protected.Post("/:id/tas/bulk", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.BulkAddTAsHandler)
	protected.Delete("/:id/tas/:userId", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.RemoveTAHandler)

	// Instructor management (admin, instructor of course)
	protected.Post("/:id/instructors", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.AddInstructorHandler)
	protected.Post("/:id/instructors/bulk", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.BulkAddInstructorsHandler)
	protected.Delete("/:id/instructors/:userId", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.RemoveInstructorHandler)

	// Section student management (admin, instructor, ta — checked inside handler for course membership)
	protected.Get("/:id/sections/:sectionId/students", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.GetSectionStudentsHandler)
	protected.Post("/:id/sections/:sectionId/students", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.AddStudentToSectionHandler)
	protected.Post("/:id/sections/:sectionId/students/bulk", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.BulkAddStudentsToSectionHandler)
	protected.Delete("/:id/sections/:sectionId/students/:studentId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.RemoveStudentFromSectionHandler)
}
