package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

func SetupCourseRoutes(app *fiber.App) {
	protected := app.Group("/api/courses", middlewares.Protected())

	// Dropdown helpers (admin, instructor, ta)
	protected.Get("/instructors", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetInstructorsListHandler)
	protected.Get("/tas-list", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetTAsListHandler)

	// My courses (instructor, ta, student)
	protected.Get("/my-courses", middlewares.RequireRole("instructor", "ta", "student"), handlers.GetMyCoursesHandler)
	protected.Get("/my-courses/stats", middlewares.RequireRole("instructor", "ta"), handlers.GetMyCoursesStatsHandler)

	// Stats (admin, instructor)
	protected.Get("/stats", middlewares.RequireRole("admin", "instructor"), handlers.GetCourseStatsHandler)
	protected.Get("/conflicts", middlewares.RequireRole("admin"), handlers.GetCourseConflictsHandler)

	// Course CRUD
	protected.Get("/", middlewares.RequireRole("admin", "instructor", "ta"), handlers.GetCoursesHandler)

	// Admin-only bulk / export endpoints (must be before /:id dynamic routes)
	protected.Patch("/bulk-toggle", middlewares.RequireRole("admin"), handlers.BulkToggleCourseStatusHandler)
	protected.Delete("/bulk", middlewares.RequireRole("admin"), handlers.BulkDeleteCoursesHandler)
	protected.Get("/export", middlewares.RequireRole("admin"), handlers.ExportCoursesCSVHandler)

	protected.Get("/:id/overview", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.GetCourseOverviewHandler)
	protected.Get("/:id", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), handlers.GetCourseByIDHandler)
	protected.Post("/", middlewares.RequireRole("admin", "instructor"), handlers.CreateCourseHandler)
	protected.Put("/:id", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.UpdateCourseHandler)
	protected.Delete("/:id", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.DeleteCourseHandler)
	protected.Patch("/:id/toggle-status", middlewares.RequireRole("admin", "instructor"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor"), handlers.ToggleCourseStatusHandler)

	// Section management (admin, instructor, ta — checked inside handler for course membership)
	protected.Post("/:id/sections", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionCreateSections, "instructor", "ta"), handlers.AddSectionHandler)
	protected.Put("/:id/sections/:sectionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionUpdateSections, "instructor", "ta"), handlers.UpdateSectionHandler)
	protected.Delete("/:id/sections/:sectionId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionDeleteSections, "instructor", "ta"), handlers.RemoveSectionHandler)

	// TA management (admin, instructor of course)
	protected.Post("/:id/tas", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionAddPeople, "instructor", "ta"), handlers.AddTAHandler)
	protected.Post("/:id/tas/bulk", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionAddPeople, "instructor", "ta"), handlers.BulkAddTAsHandler)
	protected.Delete("/:id/tas/:userId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionRemovePeople, "instructor", "ta"), handlers.RemoveTAHandler)
	protected.Patch("/:id/tas/:userId/permissions", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionEditMemberPermissions, "instructor", "ta"), handlers.UpdateTAPermissionsHandler)

	// Instructor management (admin, instructor of course)
	protected.Post("/:id/instructors", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionAddPeople, "instructor", "ta"), handlers.AddInstructorHandler)
	protected.Post("/:id/instructors/bulk", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionAddPeople, "instructor", "ta"), handlers.BulkAddInstructorsHandler)
	protected.Delete("/:id/instructors/:userId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionRemovePeople, "instructor", "ta"), handlers.RemoveInstructorHandler)
	protected.Patch("/:id/instructors/:userId/permissions", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionEditMemberPermissions, "instructor", "ta"), handlers.UpdateInstructorPermissionsHandler)

	// Section student management (admin, instructor, ta — checked inside handler for course membership)
	protected.Get("/:id/sections/:sectionId/students", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionViewSections, "instructor", "ta"), handlers.GetSectionStudentsHandler)
	protected.Get("/:id/students/removed", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionViewSections, "instructor", "ta"), handlers.GetRemovedStudentsHandler)
	protected.Post("/:id/sections/:sectionId/students", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageSectionStudents, "instructor", "ta"), handlers.AddStudentToSectionHandler)
	protected.Post("/:id/sections/:sectionId/students/bulk", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageSectionStudents, "instructor", "ta"), handlers.BulkAddStudentsToSectionHandler)
	protected.Delete("/:id/sections/:sectionId/students/:studentId", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageSectionStudents, "instructor", "ta"), handlers.RemoveStudentFromSectionHandler)
	protected.Post("/:id/sections/:sectionId/students/:studentId/restore", middlewares.RequireRole("admin", "instructor", "ta"), middlewares.RequireCourseAccess(middlewares.CourseIDFromParam("id"), "instructor", "ta"), middlewares.RequireCoursePermission(middlewares.CourseIDFromParam("id"), repositories.PermissionManageSectionStudents, "instructor", "ta"), handlers.RestoreStudentToSectionHandler)
}
