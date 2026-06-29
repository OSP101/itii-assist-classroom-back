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
	if err := db.AutoMigrate(&models.AttendanceSession{}, &models.AttendanceRecord{}, &models.AttendancePinHistory{}); err != nil {
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
