package middlewares

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Actor 2 is the instructor of course_b only. They joined session B and hold a
// mirror row in session A, the way a concurrent group leaves them.
const (
	queueTestActor   = uint(2)
	queueTestGroupID = "grp_mw"
)

type queuePermissionFixture struct {
	app           *fiber.App
	assignedToMe  uint
	notAssignedMe uint
}

func setupQueuePermissionFixture(t *testing.T, linkMode string) queuePermissionFixture {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Course{}, &models.CourseInstructor{}, &models.CourseTA{}, &models.CourseMember{},
		&models.QueueSession{}, &models.QueueWorker{}, &models.QueueBooking{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// timestamptz gets NUMERIC affinity in SQLite and scans back as a string;
	// recreate those tables with datetime so rows round-trip into the models.
	var tables []struct{ Name, SQL string }
	db.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'table' AND sql LIKE '%timestamptz%'").Scan(&tables)
	for _, table := range tables {
		db.Exec("DROP TABLE `" + table.Name + "`")
		if err := db.Exec(strings.ReplaceAll(table.SQL, "timestamptz", "datetime")).Error; err != nil {
			t.Fatalf("recreate %s: %v", table.Name, err)
		}
	}

	prevDB := config.DB
	config.DB = db
	t.Cleanup(func() {
		config.DB = prevDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	instructorA, instructorB := uint(1), queueTestActor
	mustCreate(t, db, &[]models.Course{
		{ID: "course_a", Code: "A", Name: "A", Year: 2569, Semester: 1, InstructorID: &instructorA, IsActive: true},
		{ID: "course_b", Code: "B", Name: "B", Year: 2569, Semester: 1, InstructorID: &instructorB, IsActive: true},
	})
	group := queueTestGroupID
	mustCreate(t, db, &[]models.QueueSession{
		{ID: "qs_a", CourseID: "course_a", ClassroomID: "room", Title: "A", PinCode: "111111", Status: "active", ConcurrentGroupID: &group, LinkMode: linkMode},
		{ID: "qs_b", CourseID: "course_b", ClassroomID: "room", Title: "B", PinCode: "222222", Status: "active", ConcurrentGroupID: &group, LinkMode: linkMode},
	})
	mustCreate(t, db, &[]models.QueueWorker{
		{QueueSessionID: "qs_b", UserID: queueTestActor, AcceptGrading: true, AcceptHelp: true, Status: "online"},
		{QueueSessionID: "qs_a", UserID: queueTestActor, AcceptGrading: true, AcceptHelp: true, Status: "online", IsMirror: true},
	})
	now := time.Now().UTC()
	actor := queueTestActor
	assigned := models.QueueBooking{QueueSessionID: "qs_a", StudentID: 1, DeskID: "d1", DeskNumber: 1, BookingType: "help", QueueNumber: 1, Status: "in_progress", AssignedWorkerID: &actor, StartedAt: &now}
	other := models.QueueBooking{QueueSessionID: "qs_a", StudentID: 2, DeskID: "d2", DeskNumber: 2, BookingType: "help", QueueNumber: 2, Status: "waiting"}
	mustCreate(t, db, &assigned)
	mustCreate(t, db, &other)

	app := fiber.New()
	app.Post("/sessions/:sessionId/bookings/:bookingId/complete", func(c fiber.Ctx) error {
		c.Locals("user_id", queueTestActor)
		c.Locals("user_role", "instructor")
		return c.Next()
	}, RequireQueueWorkerOrCoursePermission("sessionId", repositories.PermissionManageQueueBookings, "instructor", "ta"), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true})
	})
	return queuePermissionFixture{app: app, assignedToMe: assigned.ID, notAssignedMe: other.ID}
}

func mustCreate(t *testing.T, db *gorm.DB, value interface{}) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatalf("create %T: %v", value, err)
	}
}

func (f queuePermissionFixture) status(t *testing.T, sessionID string, bookingID uint) int {
	t.Helper()
	res, err := f.app.Test(httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/sessions/%s/bookings/%d/complete", sessionID, bookingID), nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	return res.StatusCode
}

// In a separated group, a mirror row alone must not open the partner session's
// bookings through a partner-session URL.
func TestRequireQueueWorkerOrCoursePermission_SeparatedRejectsPartnerBookingViaMirror(t *testing.T) {
	f := setupQueuePermissionFixture(t, repositories.QueueLinkModeSeparated)
	if got := f.status(t, "qs_a", f.notAssignedMe); got != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", got)
	}
}

// ...except a booking already assigned to this worker, which must stay
// completable after a switch to separated.
func TestRequireQueueWorkerOrCoursePermission_SeparatedAllowsBookingAlreadyAssigned(t *testing.T) {
	f := setupQueuePermissionFixture(t, repositories.QueueLinkModeSeparated)
	if got := f.status(t, "qs_a", f.assignedToMe); got != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", got)
	}
}

// Regression: a joint group still lets the mirror row through on the strength
// of the actor's permission in another course of the group.
func TestRequireQueueWorkerOrCoursePermission_JointAllowsPartnerBookingViaMirror(t *testing.T) {
	f := setupQueuePermissionFixture(t, repositories.QueueLinkModeJoint)
	if got := f.status(t, "qs_a", f.notAssignedMe); got != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", got)
	}
}

// The worker page sends the TA's own session, which passes on plain course
// permission before any group logic runs. This pins down why separation has
// to be enforced where bookings are assigned, not in this middleware.
func TestRequireQueueWorkerOrCoursePermission_HomeSessionURLPassesOnCoursePermission(t *testing.T) {
	f := setupQueuePermissionFixture(t, repositories.QueueLinkModeSeparated)
	if got := f.status(t, "qs_b", f.notAssignedMe); got != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", got)
	}
}
