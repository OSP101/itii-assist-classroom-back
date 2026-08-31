package middlewares

import (
	"itii-assist/config"
	"itii-assist/services"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// AuditCourseView records that the caller read part of a course.
//
// Place it last in a route's middleware chain, after the access checks: it only
// records a view the server actually served, so a rejected request never leaves
// a row claiming somebody saw data they were refused.
//
// The course is resolved with the same CourseAccessResolver the route already
// uses for its access check, so the two can never disagree about which course a
// request refers to.
//
// Options let a route name the specific record that was read; without them the
// event is just "this course, this kind of view".
type CourseViewOption func(c fiber.Ctx, ev *services.CourseViewEvent)

// WithViewTarget records which record was read, taking the ID from a route
// param. Purely descriptive: the dedupe window is per course and action, so the
// target names the first view in a window, not every one.
func WithViewTarget(targetType, param string) CourseViewOption {
	return func(c fiber.Ctx, ev *services.CourseViewEvent) {
		value := strings.TrimSpace(c.Params(param))
		if value == "" {
			return
		}
		ev.TargetType = targetType
		ev.TargetID = value
	}
}

// WithViewTargetQuery is WithViewTarget for an ID that arrives as a query
// parameter instead of a path segment.
func WithViewTargetQuery(targetType, param string) CourseViewOption {
	return func(c fiber.Ctx, ev *services.CourseViewEvent) {
		value := strings.TrimSpace(c.Query(param))
		if value == "" {
			return
		}
		ev.TargetType = targetType
		ev.TargetID = value
	}
}

// recordCourseView is the sink the middleware writes to. It is a variable so
// tests can assert on which requests are recorded without a database.
var recordCourseView = func(ev services.CourseViewEvent) {
	services.LogCourseView(config.DB, ev)
}

func AuditCourseView(action string, resolver CourseAccessResolver, options ...CourseViewOption) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()

		// Only successful reads are recorded. A 403 is already covered by the
		// security log, and recording it here would read as access that was
		// granted.
		if err != nil || c.Response().StatusCode() >= 400 {
			return err
		}

		userID, ok := GetUserID(c)
		if !ok || userID == 0 {
			return err
		}

		courseIDs, resolveErr := resolver(c)
		if resolveErr != nil || len(courseIDs) == 0 {
			return err
		}

		role, _ := GetUserRole(c)

		for _, courseID := range uniqueCourseIDs(courseIDs) {
			event := services.CourseViewEvent{
				CourseID:    courseID,
				ActorUserID: userID,
				ActorRole:   role,
				Action:      action,
				IPAddress:   c.IP(),
				UserAgent:   string(c.Request().Header.UserAgent()),
				Path:        c.Path(),
				Method:      c.Method(),
			}
			for _, option := range options {
				option(c, &event)
			}
			recordCourseView(event)
		}

		return err
	}
}
