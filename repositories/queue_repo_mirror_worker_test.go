package repositories

import (
	"strings"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// migrateWithSQLiteTimestamps runs AutoMigrate, then rewrites the generated DDL so
// timestamptz columns become datetime.
//
// The models pin `type:timestamptz` for Postgres. SQLite gives that declared type
// NUMERIC affinity and hands timestamps back as strings, so scanning a row into a
// model fails with "unsupported Scan ... into type *time.Time" — which is why the
// attendance repo test skips itself instead of running. Rewriting the type keeps
// the schema derived from the models while letting rows round-trip. Tables are
// empty here, so recreating them loses nothing; tag-defined indexes go with them,
// and any index a test needs is created explicitly.
func migrateWithSQLiteTimestamps(t *testing.T, db *gorm.DB, dst ...interface{}) {
	t.Helper()

	if err := db.AutoMigrate(dst...); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	var tables []struct {
		Name string
		SQL  string
	}
	if err := db.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'table' AND sql IS NOT NULL").Scan(&tables).Error; err != nil {
		t.Fatalf("read sqlite schema: %v", err)
	}
	for _, table := range tables {
		if !strings.Contains(table.SQL, "timestamptz") {
			continue
		}
		if err := db.Exec("DROP TABLE `" + table.Name + "`").Error; err != nil {
			t.Fatalf("drop table %s: %v", table.Name, err)
		}
		if err := db.Exec(strings.ReplaceAll(table.SQL, "timestamptz", "datetime")).Error; err != nil {
			t.Fatalf("recreate table %s: %v", table.Name, err)
		}
	}
}

// setupConcurrentGroupTestDB builds two queue sessions from different courses
// sharing one concurrent group, mirroring the shared-room setup where a TA of
// course A is handed bookings belonging to course B.
func setupConcurrentGroupTestDB(t *testing.T) (func(), models.QueueSession, models.QueueSession) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:mirror_worker_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	migrateWithSQLiteTimestamps(t, db, &models.QueueSession{}, &models.QueueWorker{}, &models.QueueBooking{}, &models.QueueDeskStatus{})
	// AutoMigrate does not create the partial/unique indexes that config.database
	// installs in production; mirrorWorkerRowTx relies on this one for idempotency.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_workers_session_user ON queue_workers (queue_session_id, user_id)`).Error; err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	prevDB := config.DB
	config.DB = db

	now := time.Now().UTC()
	groupID := "grp_mirror_001"
	sessionA := models.QueueSession{
		ID: "qs_mirror_a", CourseID: "course_a", ClassroomID: "room_shared",
		Title: "Session A", PinCode: "111111", Status: "active",
		StartTime: &now, ConcurrentGroupID: &groupID,
	}
	sessionB := models.QueueSession{
		ID: "qs_mirror_b", CourseID: "course_b", ClassroomID: "room_shared",
		Title: "Session B", PinCode: "222222", Status: "active",
		StartTime: &now, ConcurrentGroupID: &groupID,
	}
	if err := config.DB.Create(&[]models.QueueSession{sessionA, sessionB}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}

	cleanup := func() {
		db.Exec("DELETE FROM queue_bookings")
		db.Exec("DELETE FROM queue_workers")
		db.Exec("DELETE FROM queue_sessions")
		db.Exec("DELETE FROM queue_desk_statuses")
		config.DB = prevDB
	}
	return cleanup, sessionA, sessionB
}

func createPartnerBooking(t *testing.T, sessionID string, workerUserID uint) models.QueueBooking {
	t.Helper()
	now := time.Now().UTC()
	assigned := workerUserID
	booking := models.QueueBooking{
		QueueSessionID: sessionID,
		StudentID:      501,
		DeskID:         "desk_1",
		DeskNumber:     1,
		BookingType:    "help",
		QueueNumber:    1,
		Status:         "in_progress",
		// The assignment path hands a partner-session booking to a TA even when it
		// cannot find their mirror row, so this state is reachable in production.
		AssignedWorkerID: &assigned,
		AssignedAt:       &now,
		StartedAt:        &now,
	}
	if err := config.DB.Create(&booking).Error; err != nil {
		t.Fatalf("create booking: %v", err)
	}
	return booking
}

// A TA who joined session A before it was linked to session B holds no worker row
// in B. Completing a B booking must heal that row rather than reject the TA, who
// would otherwise receive queue items they can never finish.
func TestCompleteBookingWithScores_HealsMissingMirrorWorkerRow(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	const taUserID = uint(101)
	originWorker := models.QueueWorker{
		QueueSessionID: sessionA.ID,
		UserID:         taUserID,
		AcceptGrading:  true,
		AcceptHelp:     true,
		Status:         "online",
		LastActiveAt:   &now,
	}
	if err := config.DB.Create(&originWorker).Error; err != nil {
		t.Fatalf("create origin worker: %v", err)
	}

	booking := createPartnerBooking(t, sessionB.ID, taUserID)

	completed, err := CompleteBookingWithScores(booking.ID, taUserID, nil, "", "done", nil)
	if err != nil {
		t.Fatalf("complete partner-session booking: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected booking status completed, got %q", completed.Status)
	}

	var healed models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionB.ID, taUserID).First(&healed).Error; err != nil {
		t.Fatalf("expected a mirrored worker row for the partner session: %v", err)
	}
	if healed.TotalHelpCompleted != 1 {
		t.Fatalf("expected the healed row to record the completion, got total_help_completed=%d", healed.TotalHelpCompleted)
	}
	if !healed.AcceptGrading || !healed.AcceptHelp {
		t.Fatalf("expected mirror to inherit origin preferences, got grading=%v help=%v", healed.AcceptGrading, healed.AcceptHelp)
	}
}

// Healing must not turn into an authorization hole: a user with no worker row
// anywhere in the group is still rejected.
func TestCompleteBookingWithScores_RejectsUserWithNoWorkerRowInGroup(t *testing.T) {
	cleanup, _, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	const strangerUserID = uint(999)
	booking := createPartnerBooking(t, sessionB.ID, strangerUserID)

	if _, err := CompleteBookingWithScores(booking.ID, strangerUserID, nil, "", "nope", nil); err == nil {
		t.Fatal("expected a stranger with no worker row in the group to be rejected")
	} else if err.Error() != errWorkerNotRegistered.Error() {
		t.Fatalf("expected %q, got %q", errWorkerNotRegistered, err)
	}

	var count int64
	if err := config.DB.Model(&models.QueueWorker{}).Where("user_id = ?", strangerUserID).Count(&count).Error; err != nil {
		t.Fatalf("count worker rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no worker row to be created for a rejected user, found %d", count)
	}
}

// A worker who opts out of one booking type must be stored that way. The
// assignment query filters purely on accept_grading / accept_help, so a wrong
// value here is indistinguishable from "accept everything" and the worker keeps
// being offered work they declined.
func TestWorkerJoin_PreservesOptOutOnFirstJoin(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()

	const taUserID = uint(303)
	if _, err := WorkerJoin(sessionA.ID, taUserID, true, false); err != nil {
		t.Fatalf("worker join: %v", err)
	}

	var stored models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionA.ID, taUserID).First(&stored).Error; err != nil {
		t.Fatalf("load worker: %v", err)
	}
	if !stored.AcceptGrading {
		t.Error("expected accept_grading to stay true")
	}
	if stored.AcceptHelp {
		t.Error("grading-only worker was stored with accept_help=true; they will still be offered help bookings")
	}
}

// Rejoining goes through Save rather than Create, so cover it separately.
func TestWorkerJoin_PreservesOptOutOnRejoin(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()

	const taUserID = uint(304)
	if _, err := WorkerJoin(sessionA.ID, taUserID, true, true); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := WorkerJoin(sessionA.ID, taUserID, false, true); err != nil {
		t.Fatalf("rejoin: %v", err)
	}

	var stored models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionA.ID, taUserID).First(&stored).Error; err != nil {
		t.Fatalf("load worker: %v", err)
	}
	if stored.AcceptGrading {
		t.Error("help-only worker was stored with accept_grading=true after rejoin")
	}
	if !stored.AcceptHelp {
		t.Error("expected accept_help to stay true")
	}
}

// Linking two sessions must mirror workers who joined beforehand, since
// WorkerJoinMirrorGroup only ever runs at join time.
func TestLinkConcurrentSessions_MirrorsPreexistingWorkers(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	now := time.Now().UTC()
	const taUserID = uint(202)
	origin := models.QueueWorker{
		QueueSessionID: sessionA.ID,
		UserID:         taUserID,
		AcceptGrading:  true,
		AcceptHelp:     false,
		Status:         "online",
		LastActiveAt:   &now,
	}
	if err := config.DB.Create(&origin).Error; err != nil {
		t.Fatalf("create origin worker: %v", err)
	}
	// accept_help is `default:true`, so the Create above silently dropped the false.
	// Force it, or this test cannot tell an inherited false from a defaulted true.
	if err := config.DB.Model(&origin).Update("accept_help", false).Error; err != nil {
		t.Fatalf("force accept_help false: %v", err)
	}

	if err := config.DB.Transaction(func(tx *gorm.DB) error {
		return mirrorWorkersBetweenSessionsTx(tx, sessionA.ID, sessionB.ID)
	}); err != nil {
		t.Fatalf("mirror workers between sessions: %v", err)
	}

	var mirrored models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionB.ID, taUserID).First(&mirrored).Error; err != nil {
		t.Fatalf("expected worker mirrored into the partner session: %v", err)
	}
	if mirrored.AcceptGrading != true || mirrored.AcceptHelp != false {
		t.Fatalf("expected mirror to inherit preferences, got grading=%v help=%v", mirrored.AcceptGrading, mirrored.AcceptHelp)
	}

	// Re-running must stay idempotent — the unique index absorbs the duplicate.
	if err := config.DB.Transaction(func(tx *gorm.DB) error {
		return mirrorWorkersBetweenSessionsTx(tx, sessionA.ID, sessionB.ID)
	}); err != nil {
		t.Fatalf("second mirror pass should be idempotent: %v", err)
	}
	var count int64
	if err := config.DB.Model(&models.QueueWorker{}).Where("user_id = ?", taUserID).Count(&count).Error; err != nil {
		t.Fatalf("count worker rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 worker rows (origin + mirror), got %d", count)
	}
}
