package repositories

import (
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"
)

// DedupeAllAttendanceRecordsWithDB exists because dedupeAttendanceRecordsWithDB
// (the per-check-in version) was removed from the hot path, leaving legacy
// duplicate attendance_records rows — the kind that could only exist before
// idx_attendance_session_student — uncleaned unless an instructor happened to
// open that specific session's live view (caught in review). This test
// simulates exactly that legacy state: duplicates that predate the unique
// index, across MULTIPLE sessions, and confirms one call cleans all of them.
func TestDedupeAllAttendanceRecordsWithDB_CleansLegacyDuplicatesAcrossSessions(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	// GetAppConfigValue/SetAppConfigValue cache by key in a package-level,
	// process-wide map with its own TTL — not scoped to this test's sqlite
	// DB. Start and end from a known-uncached state so this test can't read
	// a stale marker left by (or leave one for) any other test touching the
	// same key.
	invalidateCachedConfigValue(attendanceDedupeDoneKey)
	t.Cleanup(func() { invalidateCachedConfigValue(attendanceDedupeDoneKey) })

	// The unique index (idx_attendance_session_student, present on the model
	// tag since before this fix) would reject the duplicate inserts below —
	// drop it to reproduce the pre-index legacy data this function exists to
	// clean up. A real deployment would already have rows like this sitting
	// in the table from before the index was ever added.
	if err := config.DB.Migrator().DropIndex(&models.AttendanceRecord{}, "idx_attendance_session_student"); err != nil {
		t.Fatalf("failed to drop unique index for test setup: %v", err)
	}

	now := time.Now()
	older := now.Add(-time.Hour)

	// Session 1 / student 10: 3 duplicate rows — a present one should win
	// over two absent ones, matching ensureAttendanceRecordInTx's own
	// ORDER BY preference (non-absent first, then most recent check-in).
	seedAttendanceRecordDirect(t, 1, 10, "absent", nil, older)
	winnerID := seedAttendanceRecordDirect(t, 1, 10, "present", &now, now)
	seedAttendanceRecordDirect(t, 1, 10, "absent", nil, older)

	// Session 2 / student 20: 2 duplicate rows in a completely different
	// session — proves the sweep isn't scoped to just one session.
	loser2ID := seedAttendanceRecordDirect(t, 2, 20, "absent", nil, older)
	winner2ID := seedAttendanceRecordDirect(t, 2, 20, "present", &now, now)

	// Session 3 / student 30: a single row, no duplicate — must survive
	// untouched, proving the sweep only touches genuine duplicate groups.
	untouchedID := seedAttendanceRecordDirect(t, 3, 30, "present", &now, now)

	cleaned, err := DedupeAllAttendanceRecordsWithDB(config.DB)
	if err != nil {
		t.Fatalf("DedupeAllAttendanceRecordsWithDB returned an error: %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("expected 2 duplicate (session, student) groups cleaned, got %d", cleaned)
	}

	var remaining []models.AttendanceRecord
	if err := config.DB.Find(&remaining).Error; err != nil {
		t.Fatalf("failed to read back attendance_records: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("expected exactly 3 rows to survive (1 winner per session + the untouched row), got %d", len(remaining))
	}

	survivingIDs := make(map[uint]bool, len(remaining))
	for _, r := range remaining {
		survivingIDs[r.ID] = true
	}

	if !survivingIDs[winnerID] {
		t.Error("session 1's present/most-recent row should have survived but did not")
	}
	if !survivingIDs[winner2ID] {
		t.Error("session 2's present row should have survived but did not")
	}
	if survivingIDs[loser2ID] {
		t.Error("session 2's absent duplicate should have been deleted but survived")
	}
	if !survivingIDs[untouchedID] {
		t.Error("the non-duplicate row in session 3 should never have been touched")
	}

	// A successful run must record the completion marker so the next boot
	// skips the scan entirely instead of re-running it forever (caught in
	// review).
	var marker models.AppConfig
	if err := config.DB.Where("key = ?", attendanceDedupeDoneKey).First(&marker).Error; err != nil {
		t.Fatalf("expected a %q marker row after a successful run, got error: %v", attendanceDedupeDoneKey, err)
	}
	if marker.Value != "true" {
		t.Fatalf("expected marker value %q, got %q", "true", marker.Value)
	}

	// Re-running against an already-deduped table must be a no-op.
	cleanedAgain, err := DedupeAllAttendanceRecordsWithDB(config.DB)
	if err != nil {
		t.Fatalf("second run returned an error: %v", err)
	}
	if cleanedAgain != 0 {
		t.Fatalf("expected 0 groups cleaned on a re-run against an already-deduped table, got %d", cleanedAgain)
	}
}

// The marker (not just "nothing left to find") is what must stop the scan on
// later boots — otherwise this still costs a full-table GROUP BY every
// single startup forever, which is the exact problem this fix closes. Prove
// the skip is marker-driven by seeding a FRESH duplicate after the marker is
// set and confirming DedupeAllAttendanceRecordsWithDB ignores it.
func TestDedupeAllAttendanceRecordsWithDB_SkipsScanOnceMarkerIsSet(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	invalidateCachedConfigValue(attendanceDedupeDoneKey)
	t.Cleanup(func() { invalidateCachedConfigValue(attendanceDedupeDoneKey) })

	if err := config.DB.Migrator().DropIndex(&models.AttendanceRecord{}, "idx_attendance_session_student"); err != nil {
		t.Fatalf("failed to drop unique index for test setup: %v", err)
	}

	if err := SetAppConfigValue(attendanceDedupeDoneKey, "true"); err != nil {
		t.Fatalf("failed to seed completion marker: %v", err)
	}

	// Duplicates that exist AFTER the marker was set — a real deployment
	// should never produce these (the unique index prevents new ones), but
	// planting them here is exactly how to prove the skip is unconditional
	// on the marker rather than "coincidentally found nothing."
	loserID := seedAttendanceRecordDirect(t, 5, 50, "absent", nil, time.Now().Add(-time.Hour))
	seedAttendanceRecordDirect(t, 5, 50, "present", nil, time.Now())

	cleaned, err := DedupeAllAttendanceRecordsWithDB(config.DB)
	if err != nil {
		t.Fatalf("DedupeAllAttendanceRecordsWithDB returned an error: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected the marker to short-circuit the scan (0 cleaned), got %d — the skip is not marker-driven", cleaned)
	}

	var stillThere models.AttendanceRecord
	if err := config.DB.First(&stillThere, loserID).Error; err != nil {
		t.Fatalf("expected the untouched duplicate to still be in the table (proving the scan never ran), got error: %v", err)
	}
}

// The completion marker must be read/written through the db parameter this
// function receives, not through the package-global config.DB (caught in
// review: GetAppConfigValue/SetAppConfigValue are hardcoded to config.DB,
// and this package already has a real precedent of calling db-parameterized
// helpers from inside db.Transaction(func(tx *gorm.DB) error {...}) blocks,
// where db and config.DB legitimately differ). Prove it by deliberately
// diverging the two: point config.DB at a completely different database
// than the one actually passed in, and confirm the marker still lands in
// the passed-in db, not wherever config.DB happens to point.
func TestDedupeAllAttendanceRecordsWithDB_MarkerUsesDBParamNotGlobalConfigDB(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	invalidateCachedConfigValue(attendanceDedupeDoneKey)
	t.Cleanup(func() { invalidateCachedConfigValue(attendanceDedupeDoneKey) })

	targetDB := config.DB // the real db this test's setup pointed config.DB at

	// A second, unrelated in-memory database standing in for "whatever
	// config.DB happens to be at call time" — if the fix regresses back to
	// reading/writing through config.DB, the marker would land here instead
	// of in targetDB, and this decoy has no attendance_records/app_configs
	// tables at all, so any accidental read/write against it would error
	// loudly rather than silently succeeding in the wrong place.
	decoyCleanup := setupLeaveRepoTestDB(t)
	defer decoyCleanup()

	if config.DB == targetDB {
		t.Fatal("test setup did not actually diverge config.DB from targetDB")
	}

	cleaned, err := DedupeAllAttendanceRecordsWithDB(targetDB)
	if err != nil {
		t.Fatalf("DedupeAllAttendanceRecordsWithDB(targetDB) returned an error even though config.DB points elsewhere: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected 0 (empty table), got %d", cleaned)
	}

	var marker models.AppConfig
	if err := targetDB.Where("key = ?", attendanceDedupeDoneKey).First(&marker).Error; err != nil {
		t.Fatalf("expected the completion marker in targetDB (the db actually passed in), got error: %v", err)
	}
	if marker.Value != "true" {
		t.Fatalf("expected marker value %q in targetDB, got %q", "true", marker.Value)
	}

	var decoyCount int64
	if err := config.DB.Model(&models.AppConfig{}).Where("key = ?", attendanceDedupeDoneKey).Count(&decoyCount).Error; err != nil {
		t.Fatalf("failed to query decoy DB: %v", err)
	}
	if decoyCount != 0 {
		t.Fatal("marker was written to config.DB (the decoy) instead of the db parameter actually passed in")
	}
}

// ResetAttendanceDedupeMarker exists so a database restore (which can
// reintroduce legacy-shaped duplicates from a pre-index backup) isn't
// permanently ignored by an already-set completion marker. Prove it
// actually re-enables the scan: set the marker, clear it, seed a duplicate,
// and confirm DedupeAllAttendanceRecordsWithDB runs for real again instead
// of short-circuiting.
func TestResetAttendanceDedupeMarker_ReEnablesTheScan(t *testing.T) {
	cleanup := setupLeaveRepoTestDB(t)
	defer cleanup()
	invalidateCachedConfigValue(attendanceDedupeDoneKey)
	t.Cleanup(func() { invalidateCachedConfigValue(attendanceDedupeDoneKey) })

	if err := config.DB.Migrator().DropIndex(&models.AttendanceRecord{}, "idx_attendance_session_student"); err != nil {
		t.Fatalf("failed to drop unique index for test setup: %v", err)
	}

	if _, err := DedupeAllAttendanceRecordsWithDB(config.DB); err != nil {
		t.Fatalf("initial run returned an error: %v", err)
	}
	var marker models.AppConfig
	if err := config.DB.Where("key = ?", attendanceDedupeDoneKey).First(&marker).Error; err != nil {
		t.Fatalf("expected marker to exist after the initial run: %v", err)
	}

	if err := ResetAttendanceDedupeMarker(config.DB); err != nil {
		t.Fatalf("ResetAttendanceDedupeMarker returned an error: %v", err)
	}

	var count int64
	if err := config.DB.Model(&models.AppConfig{}).Where("key = ?", attendanceDedupeDoneKey).Count(&count).Error; err != nil {
		t.Fatalf("failed to check marker row: %v", err)
	}
	if count != 0 {
		t.Fatal("expected the marker row to be gone after ResetAttendanceDedupeMarker")
	}

	// A "restored" legacy duplicate, planted after the reset — proves the
	// scan genuinely runs again rather than the marker check just happening
	// to pass.
	loserID := seedAttendanceRecordDirect(t, 9, 90, "absent", nil, time.Now().Add(-time.Hour))
	seedAttendanceRecordDirect(t, 9, 90, "present", nil, time.Now())

	cleaned, err := DedupeAllAttendanceRecordsWithDB(config.DB)
	if err != nil {
		t.Fatalf("post-reset run returned an error: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("expected the scan to actually run and clean the newly-planted duplicate, got cleaned=%d", cleaned)
	}
	if err := config.DB.First(&models.AttendanceRecord{}, loserID).Error; err == nil {
		t.Fatal("expected the post-reset duplicate to be cleaned up, but it still exists")
	}
}

func seedAttendanceRecordDirect(t *testing.T, sessionID, studentID uint, status string, checkInTime *time.Time, updatedAt time.Time) uint {
	t.Helper()
	record := models.AttendanceRecord{
		AttendanceSessionID: sessionID,
		StudentID:           studentID,
		Status:              status,
		CheckInTime:         checkInTime,
		CreatedAt:           updatedAt,
	}
	if err := config.DB.Create(&record).Error; err != nil {
		t.Fatalf("failed to seed attendance record: %v", err)
	}
	// UpdatedAt is autoUpdateTime, so Create already stamped it with "now" —
	// force it to the caller's intended ordering value directly so the
	// present-vs-absent / most-recent-check-in ORDER BY has real spread to
	// sort on instead of every row sharing the same test-run timestamp.
	if err := config.DB.Model(&models.AttendanceRecord{}).Where("id = ?", record.ID).
		UpdateColumn("updated_at", updatedAt).Error; err != nil {
		t.Fatalf("failed to backdate updated_at: %v", err)
	}
	return record.ID
}
