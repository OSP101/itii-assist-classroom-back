package middlewares

import (
	"errors"
	"io"
	"net/http/httptest"
	"sync"
	"testing"

	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

// captureCourseViews swaps the audit sink for the duration of a test and
// returns a getter for what was recorded.
func captureCourseViews(t *testing.T) func() []services.CourseViewEvent {
	t.Helper()

	var mu sync.Mutex
	var recorded []services.CourseViewEvent

	original := recordCourseView
	recordCourseView = func(ev services.CourseViewEvent) {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, ev)
	}
	t.Cleanup(func() { recordCourseView = original })

	return func() []services.CourseViewEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]services.CourseViewEvent(nil), recorded...)
	}
}

func courseIDResolver(ids ...string) CourseAccessResolver {
	return func(fiber.Ctx) ([]string, error) { return ids, nil }
}

// newAuditApp builds an app whose route carries an authenticated actor, the
// audit middleware, and a handler with the given behaviour.
func newAuditApp(actorID uint, role string, handler fiber.Handler, resolver CourseAccessResolver, options ...CourseViewOption) *fiber.App {
	app := fiber.New()
	app.Get("/api/courses/:courseId/thing", func(c fiber.Ctx) error {
		if actorID != 0 {
			c.Locals("user_id", actorID)
		}
		c.Locals("user_role", role)
		return c.Next()
	}, AuditCourseView(services.ActionViewScores, resolver, options...), handler)
	return app
}

func doGet(t *testing.T, app *fiber.App, target string) {
	t.Helper()
	response, err := app.Test(httptest.NewRequest(fiber.MethodGet, target, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

func TestAuditCourseViewRecordsSuccessfulRead(t *testing.T) {
	recorded := captureCourseViews(t)

	app := newAuditApp(42, "admin", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}, courseIDResolver("CS101"))

	doGet(t, app, "/api/courses/CS101/thing")

	events := recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 recorded view, got %d", len(events))
	}
	if events[0].CourseID != "CS101" || events[0].ActorUserID != 42 || events[0].ActorRole != "admin" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
	if events[0].Action != services.ActionViewScores {
		t.Fatalf("unexpected action: %q", events[0].Action)
	}
}

func TestAuditCourseViewSkipsRefusedRequests(t *testing.T) {
	// A 403 must not leave a row claiming the data was seen.
	recorded := captureCourseViews(t)

	app := newAuditApp(42, "admin", func(c fiber.Ctx) error {
		return c.Status(403).JSON(fiber.Map{"success": false})
	}, courseIDResolver("CS101"))

	doGet(t, app, "/api/courses/CS101/thing")

	if events := recorded(); len(events) != 0 {
		t.Fatalf("a refused request must not be recorded, got %#v", events)
	}
}

func TestAuditCourseViewSkipsHandlerErrors(t *testing.T) {
	recorded := captureCourseViews(t)

	app := newAuditApp(42, "admin", func(fiber.Ctx) error {
		return errors.New("boom")
	}, courseIDResolver("CS101"))

	doGet(t, app, "/api/courses/CS101/thing")

	if events := recorded(); len(events) != 0 {
		t.Fatalf("a failed handler must not be recorded, got %#v", events)
	}
}

func TestAuditCourseViewSkipsUnauthenticatedCaller(t *testing.T) {
	recorded := captureCourseViews(t)

	app := newAuditApp(0, "", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}, courseIDResolver("CS101"))

	doGet(t, app, "/api/courses/CS101/thing")

	if events := recorded(); len(events) != 0 {
		t.Fatalf("no actor means nothing to attribute the view to, got %#v", events)
	}
}

func TestAuditCourseViewSkipsWhenCourseCannotBeResolved(t *testing.T) {
	recorded := captureCourseViews(t)

	failing := func(fiber.Ctx) ([]string, error) { return nil, errors.New("no course") }
	app := newAuditApp(42, "admin", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}, failing)

	doGet(t, app, "/api/courses/CS101/thing")

	if events := recorded(); len(events) != 0 {
		t.Fatalf("an unresolvable course must not be recorded, got %#v", events)
	}
}

func TestAuditCourseViewRecordsTargetFromParam(t *testing.T) {
	recorded := captureCourseViews(t)

	app := fiber.New()
	app.Get("/api/courses/:courseId/sessions/:sessionId", func(c fiber.Ctx) error {
		c.Locals("user_id", uint(9))
		c.Locals("user_role", "instructor")
		return c.Next()
	}, AuditCourseView(services.ActionViewAttendance, CourseIDFromParam("courseId"),
		WithViewTarget("attendance_session", "sessionId")), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	})

	doGet(t, app, "/api/courses/CS101/sessions/77")

	events := recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 recorded view, got %d", len(events))
	}
	if events[0].TargetType != "attendance_session" || events[0].TargetID != "77" {
		t.Fatalf("target was not captured: %#v", events[0])
	}
}

func TestAuditCourseViewDeduplicatesResolvedCourses(t *testing.T) {
	recorded := captureCourseViews(t)

	app := newAuditApp(42, "admin", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	}, courseIDResolver("CS101", "CS101", " CS101 ", "CS102"))

	doGet(t, app, "/api/courses/CS101/thing")

	events := recorded()
	if len(events) != 2 {
		t.Fatalf("expected one event per distinct course, got %d: %#v", len(events), events)
	}
}
