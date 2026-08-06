package repositories

import (
	"context"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupAttendanceRuntimeTest(t *testing.T) (*miniredis.Miniredis, func()) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Course{}, &models.AttendanceSession{}, &models.AttendanceRecord{}, &models.AttendancePinHistory{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	autoRotateProbe := false
	probe := models.AttendanceSession{
		CourseID:             "probe",
		Title:                "probe",
		AutoRotatePin:        &autoRotateProbe,
		SessionType:          "lecture",
		StartTime:            time.Now(),
		EndTime:              time.Now().Add(time.Minute),
		LateThresholdMinutes: 15,
		Status:               "draft",
	}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatalf("create probe session: %v", err)
	}
	var reloadedProbe models.AttendanceSession
	if err := db.First(&reloadedProbe, probe.ID).Error; err != nil {
		t.Skipf("skipping attendance runtime repo tests because local sqlite driver cannot round-trip attendance timestamps in this environment: %v", err)
	}

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}

	prevDB := config.DB
	prevRedis := config.Redis
	config.DB = db
	config.Redis = redis.NewClient(&redis.Options{Addr: redisServer.Addr()})

	return redisServer, func() {
		if config.Redis != nil {
			_ = config.Redis.Close()
		}
		redisServer.Close()
		config.DB = prevDB
		config.Redis = prevRedis
	}
}

func createAttendanceSessionFixture(t *testing.T, autoRotate bool) models.AttendanceSession {
	t.Helper()

	now := time.Now()
	session := models.AttendanceSession{
		CourseID:             "CP101",
		Title:                "Quiz attendance",
		AutoRotatePin:        &autoRotate,
		SessionType:          "lecture",
		StartTime:            now.Add(5 * time.Minute),
		EndTime:              now.Add(65 * time.Minute),
		LateThresholdMinutes: 15,
		Status:               "draft",
	}
	if err := config.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session
}

func createCourseFixture(t *testing.T, id string, isActive bool) {
	t.Helper()

	course := models.Course{ID: id, Code: id, Name: "Course " + id, Year: 2026, Semester: 1}
	if err := config.DB.Create(&course).Error; err != nil {
		t.Fatalf("create course %s: %v", id, err)
	}
	// Set is_active via Update: creating from struct would let the
	// default:true tag silently overwrite an explicit false.
	if err := config.DB.Model(&models.Course{}).Where("id = ?", id).Update("is_active", isActive).Error; err != nil {
		t.Fatalf("set course %s is_active: %v", id, err)
	}
}

func TestAutoOpenDueAttendanceSessions(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	createCourseFixture(t, "CP-OPEN", true)
	createCourseFixture(t, "CP-CLOSED", false)

	now := time.Now()
	autoRotate := false
	newSession := func(courseID string, start time.Time) models.AttendanceSession {
		session := models.AttendanceSession{
			CourseID:             courseID,
			Title:                "Scheduled attendance",
			AutoRotatePin:        &autoRotate,
			SessionType:          "lecture",
			StartTime:            start,
			EndTime:              start.Add(time.Hour),
			LateThresholdMinutes: 15,
			Status:               "draft",
		}
		if err := config.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session for %s: %v", courseID, err)
		}
		return session
	}

	due := newSession("CP-OPEN", now.Add(-time.Minute))
	closedCourse := newSession("CP-CLOSED", now.Add(-time.Minute))
	future := newSession("CP-OPEN", now.Add(10*time.Minute))

	opened, err := AutoOpenDueAttendanceSessions(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("auto open due sessions: %v", err)
	}
	if len(opened) != 1 {
		t.Fatalf("expected exactly 1 auto-opened session, got %d", len(opened))
	}
	if opened[0].SessionID != due.ID {
		t.Fatalf("expected session %d to open, got %d", due.ID, opened[0].SessionID)
	}
	if !opened[0].StatusChanged || opened[0].Status != "active" {
		t.Fatalf("expected active status change, got %+v", opened[0].AttendancePinStateChange)
	}
	if opened[0].PinCode == "" {
		t.Fatal("expected auto-opened session to carry a pin")
	}
	if opened[0].Title != due.Title {
		t.Fatalf("expected title %q, got %q", due.Title, opened[0].Title)
	}

	assertStatus := func(id uint, want string) {
		var reloaded models.AttendanceSession
		if err := config.DB.First(&reloaded, id).Error; err != nil {
			t.Fatalf("reload session %d: %v", id, err)
		}
		if reloaded.Status != want {
			t.Fatalf("session %d: expected status %q, got %q", id, want, reloaded.Status)
		}
	}
	assertStatus(due.ID, "active")
	assertStatus(closedCourse.ID, "draft")
	assertStatus(future.ID, "draft")

	// Second tick must be a no-op: the opened session is now active.
	openedAgain, err := AutoOpenDueAttendanceSessions(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("auto open second tick: %v", err)
	}
	if len(openedAgain) != 0 {
		t.Fatalf("expected no sessions on second tick, got %d", len(openedAgain))
	}
}

func TestStartAttendanceSessionStatic(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	session := createAttendanceSessionFixture(t, false)

	result, err := StartAttendanceSession(context.Background(), session.ID, "static-start")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if result.State.Mode != "static" {
		t.Fatalf("expected static mode, got %q", result.State.Mode)
	}
	if result.CurrentPIN == "" {
		t.Fatal("expected current pin")
	}
	if result.NextRotationAt != nil {
		t.Fatal("static session should not expose next rotation")
	}

	var refreshed models.AttendanceSession
	if err := config.DB.First(&refreshed, session.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if refreshed.Status != "active" {
		t.Fatalf("expected active status, got %q", refreshed.Status)
	}
	if refreshed.CurrentPinHash == "" {
		t.Fatal("expected persisted pin hash")
	}
}

func TestRefreshAttendanceSessionPinStateKeepsStaticDraftPIN(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	session := createAttendanceSessionFixture(t, false)

	refreshed, change, err := RefreshAttendanceSessionPinState(session.ID)
	if err != nil {
		t.Fatalf("refresh session pin state: %v", err)
	}
	if refreshed.Status != "draft" {
		t.Fatalf("expected draft status, got %q", refreshed.Status)
	}
	if refreshed.PinCode == "" {
		t.Fatal("expected static draft session to expose a preview pin")
	}
	if refreshed.PinRotatesAt != nil {
		t.Fatal("expected static draft session to have no rotation timestamp")
	}
	if change.Released {
		t.Fatal("did not expect static draft preview pin to be released")
	}

	firstPin := refreshed.PinCode
	refreshedAgain, _, err := RefreshAttendanceSessionPinState(session.ID)
	if err != nil {
		t.Fatalf("refresh session pin state again: %v", err)
	}
	if refreshedAgain.PinCode != firstPin {
		t.Fatalf("expected static draft preview pin %q to persist, got %q", firstPin, refreshedAgain.PinCode)
	}
}

func TestStartAttendanceSessionRotatingAndRotate(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	session := createAttendanceSessionFixture(t, true)

	result, err := StartAttendanceSession(context.Background(), session.ID, "rotating-start")
	if err != nil {
		t.Fatalf("start rotating session: %v", err)
	}
	if result.State.Mode != "rotating" {
		t.Fatalf("expected rotating mode, got %q", result.State.Mode)
	}
	if result.State.NextPIN == "" {
		t.Fatal("expected next pin to be pre-generated")
	}

	rotated, err := RotateAttendanceSessionPIN(context.Background(), session.ID, "manual_regenerate")
	if err != nil {
		t.Fatalf("rotate pin: %v", err)
	}
	if rotated.PreviousPIN != result.CurrentPIN {
		t.Fatalf("expected previous pin %q, got %q", result.CurrentPIN, rotated.PreviousPIN)
	}
	if rotated.CurrentPIN == result.CurrentPIN {
		t.Fatal("expected current pin to change after rotation")
	}
	if rotated.NextPIN == "" || rotated.NextPIN == rotated.CurrentPIN {
		t.Fatal("expected a distinct next pin after rotation")
	}
}

func TestStartAttendanceSessionRequiresRedis(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	config.Redis = nil
	session := createAttendanceSessionFixture(t, true)

	if _, err := StartAttendanceSession(context.Background(), session.ID, "missing-redis"); err != ErrAttendanceRedisUnavailable {
		t.Fatalf("expected redis unavailable error, got %v", err)
	}
}

func TestLookupAttendancePinAndCloseCleanup(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	session := createAttendanceSessionFixture(t, true)
	started, err := StartAttendanceSession(context.Background(), session.ID, "lookup-close")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	foundSessionID, err := LookupAttendanceSessionIDByPIN(context.Background(), started.CurrentPIN)
	if err != nil {
		t.Fatalf("lookup pin: %v", err)
	}
	if foundSessionID != session.ID {
		t.Fatalf("expected session id %d, got %d", session.ID, foundSessionID)
	}

	if _, err := CloseAttendanceRuntimeSession(context.Background(), session.ID); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if _, err := LookupAttendanceSessionIDByPIN(context.Background(), started.CurrentPIN); err != ErrAttendanceInvalidPIN {
		t.Fatalf("expected invalid pin after close, got %v", err)
	}
}

// TestStartAttendanceSessionStaticPreservesDraftPIN verifies that activating a
// static session that already has a pre-assigned PIN (announced to students
// before the session opens) does not replace it with a new PIN.
func TestStartAttendanceSessionStaticPreservesDraftPIN(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	autoRotate := false
	now := time.Now()
	preassignedPIN := "555777"
	session := models.AttendanceSession{
		CourseID:             "CP101",
		Title:                "Pre-announced PIN session",
		AutoRotatePin:        &autoRotate,
		PinMode:              "static",
		PinCode:              preassignedPIN,
		SessionType:          "lecture",
		StartTime:            now.Add(5 * time.Minute),
		EndTime:              now.Add(65 * time.Minute),
		LateThresholdMinutes: 15,
		Status:               "draft",
	}
	if err := config.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := StartAttendanceSession(context.Background(), session.ID, "preassigned-start")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if result.CurrentPIN != preassignedPIN {
		t.Fatalf("expected pre-announced PIN %q to be preserved on activation, got %q", preassignedPIN, result.CurrentPIN)
	}
	if result.State.Mode != "static" {
		t.Fatalf("expected static mode, got %q", result.State.Mode)
	}
}
