package routes

import (
	"itii-assist/handlers"
	"itii-assist/middlewares"

	"github.com/gofiber/fiber/v3"
)

func SetupStudentRoutes(app *fiber.App) {
	// Student lookup — public for compatibility with the legacy student self-check flow
	app.Get("/api/students/lookup/:student_id", handlers.LookupStudentHandler)

	// Protected — Admin, Instructor, TA
	read := app.Group("/api/students", middlewares.Protected(), middlewares.RequireRole("admin", "instructor", "ta"))
	read.Post("/search-by-ids", handlers.SearchStudentsByIDsCompatHandler)
	read.Get("/stats", handlers.GetStudentStatsHandler)
	read.Get("/", handlers.GetStudentsHandler)
	read.Get("/:id", handlers.GetStudentByIDHandler)

	// Admin only
	admin := app.Group("/api/students", middlewares.Protected(), middlewares.RequireRole("admin"))
	admin.Post("/", handlers.CreateStudentHandler)
	admin.Post("/import", handlers.ImportStudentsHandler)
	admin.Put("/:id", handlers.UpdateStudentHandler)
	admin.Patch("/:id/status", handlers.ToggleStudentStatusHandler)
	admin.Delete("/:id", handlers.DeleteStudentHandler)
}
