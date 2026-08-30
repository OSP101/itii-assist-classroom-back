package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupStudentRoutes(app *fiber.App) {
	self := app.Group("/api/students/me", middlewares.Protected(), middlewares.RequireRole("student"))
	self.Get("/lookup", handlers.LookupMyStudentHandler)
	self.Get("/courses/:courseId", handlers.GetMyStudentCourseHandler)

	// Protected — Admin, Instructor, TA
	read := app.Group("/api/students", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	read.Use(middlewares.RequireAdminFeature("menu.students"))
	read.Post("/search-by-ids", handlers.SearchStudentsByIDsCompatHandler)
	read.Get("/stats", handlers.GetStudentStatsHandler)
	read.Get("/", handlers.GetStudentsHandler)
	read.Get("/:id", handlers.GetStudentByIDHandler)

	// Admin only
	admin := app.Group("/api/students", middlewares.Protected(), middlewares.RequireRole("admin"))
	admin.Use(middlewares.RequireAdminFeature("menu.students"))
	admin.Post("/", handlers.CreateStudentHandler)
	admin.Post("/import", handlers.ImportStudentsHandler)
	admin.Put("/:id", handlers.UpdateStudentHandler)
	admin.Patch("/:id/status", handlers.ToggleStudentStatusHandler)
	admin.Delete("/:id", handlers.DeleteStudentHandler)
}
