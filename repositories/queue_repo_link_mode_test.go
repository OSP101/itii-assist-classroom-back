package repositories

import (
	"errors"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"
)

// Users in these tests: 999 is the shared instructor of course_a and course_b
// (linking requires one instructor for both). Worker ids below 999 are TAs.

func setLinkMode(t *testing.T, groupID, mode string) {
	t.Helper()
	if err := config.DB.Model(&models.QueueSession{}).
		Where("concurrent_group_id = ?", groupID).
		Update("link_mode", mode).Error; err != nil {
		t.Fatalf("set link_mode: %v", err)
	}
}

// createOnlineWorker creates the row for a session the user joined directly.
func createOnlineWorker(t *testing.T, sessionID string, userID uint, acceptGrading, acceptHelp bool) models.QueueWorker {
	t.Helper()
	now := time.Now().UTC()
	w := models.QueueWorker{
		QueueSessionID: sessionID,
		UserID:         userID,
		AcceptGrading:  acceptGrading,
		AcceptHelp:     acceptHelp,
		Status:         "online",
		LastActiveAt:   &now,
	}
	if err := config.DB.Create(&w).Error; err != nil {
		t.Fatalf("create worker: %v", err)
	}
	return w
}

// createMirrorWorker creates a row copied into a partner session, the same way
// WorkerJoinMirrorGroup / mirrorWorkersBetweenSessionsTx do.
func createMirrorWorker(t *testing.T, sessionID string, userID uint) {
	t.Helper()
	if err := insertWorkerRowTx(config.DB, sessionID, models.QueueWorker{
		UserID: userID, AcceptGrading: true, AcceptHelp: true, PushNotificationsEnabled: true, Status: "online",
	}, true); err != nil {
		t.Fatalf("create mirror worker: %v", err)
	}
}

func setWorkerLoad(t *testing.T, sessionID string, userID uint, completed int) {
	t.Helper()
	if err := config.DB.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND user_id = ?", sessionID, userID).
		Update("total_help_completed", completed).Error; err != nil {
		t.Fatalf("set worker load: %v", err)
	}
}

func loadWorker(t *testing.T, sessionID string, userID uint) models.QueueWorker {
	t.Helper()
	var w models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionID, userID).First(&w).Error; err != nil {
		t.Fatalf("load worker %d in %s: %v", userID, sessionID, err)
	}
	return w
}

func loadBooking(t *testing.T, id uint) models.QueueBooking {
	t.Helper()
	var b models.QueueBooking
	if err := config.DB.First(&b, id).Error; err != nil {
		t.Fatalf("reload booking: %v", err)
	}
	return b
}

func createWaitingBooking(t *testing.T, sessionID, deskID string, queueNumber int) models.QueueBooking {
	t.Helper()
	now := time.Now().UTC()
	b := models.QueueBooking{
		QueueSessionID: sessionID,
		StudentID:      501,
		DeskID:         deskID,
		DeskNumber:     queueNumber,
		BookingType:    "help",
		QueueNumber:    queueNumber,
		Status:         "waiting",
		CreatedAt:      now,
	}
	if err := config.DB.Create(&b).Error; err != nil {
		t.Fatalf("create waiting booking: %v", err)
	}
	return b
}

// createOfferedBooking creates a booking in sessionID that is currently offered
// to workerID (status waiting, assigned, with an offer deadline), with the
// worker's row in that session marked busy holding it.
func createOfferedBooking(t *testing.T, sessionID string, workerID uint, expiresAt time.Time) models.QueueBooking {
	t.Helper()
	b := createWaitingBooking(t, sessionID, "desk_1", 1)
	now := time.Now().UTC()
	if err := config.DB.Model(&models.QueueBooking{}).Where("id = ?", b.ID).Updates(map[string]interface{}{
		"assigned_worker_id": workerID, "assigned_at": now, "offer_expires_at": expiresAt,
	}).Error; err != nil {
		t.Fatalf("offer booking: %v", err)
	}
	if err := config.DB.Model(&models.QueueWorker{}).
		Where("queue_session_id = ? AND user_id = ?", sessionID, workerID).
		Updates(map[string]interface{}{"status": "busy", "current_booking_id": b.ID}).Error; err != nil {
		t.Fatalf("mark worker busy: %v", err)
	}
	return loadBooking(t, b.ID)
}

func assertNoAssignment(t *testing.T, result *models.QueueBooking, assignedNow bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if assignedNow || result != nil {
		t.Fatalf("expected no assignment, got assignedNow=%v result=%+v", assignedNow, result)
	}
}

func assertUntouched(t *testing.T, bookingID uint) {
	t.Helper()
	b := loadBooking(t, bookingID)
	if b.Status != "waiting" || b.AssignedWorkerID != nil {
		t.Fatalf("expected booking %d to stay unassigned, got status=%q assigned_worker_id=%v", bookingID, b.Status, b.AssignedWorkerID)
	}
}

// ---------- dispatch: AssignNextWaitingBookingToWorker ----------

// Regression: a "joint" group keeps dispatching across the course boundary
// exactly as before link_mode existed.
func TestAssignNextWaitingBookingToWorker_JointModeCrossesCourses(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taUserID = uint(401)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	createMirrorWorker(t, sessionB.ID, taUserID)
	booking := createWaitingBooking(t, sessionB.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !assignedNow || result == nil || result.ID != booking.ID {
		t.Fatalf("expected booking %d to be assigned, got assignedNow=%v result=%+v", booking.ID, assignedNow, result)
	}
	if result.QueueSessionID != sessionB.ID || result.Status != "in_progress" {
		t.Fatalf("expected a direct assignment kept under session B, got session=%q status=%q", result.QueueSessionID, result.Status)
	}
	if w := loadWorker(t, sessionB.ID, taUserID); w.CurrentBookingID == nil || *w.CurrentBookingID != booking.ID {
		t.Fatalf("expected session B worker row to record the current booking, got %v", w.CurrentBookingID)
	}
}

// Pull path: a TA polling from the session they joined never receives the
// partner course's booking.
func TestAssignNextWaitingBookingToWorker_SeparatedModeNeverCrossesCourses(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(402)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	createMirrorWorker(t, sessionB.ID, taUserID)
	booking := createWaitingBooking(t, sessionB.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	assertNoAssignment(t, result, assignedNow, err)
	assertUntouched(t, booking.ID)
}

// Push path: dispatchWaitingBookingsToAvailableWorkers calls the dispatcher for
// every row sitting in a session, mirrors included. A mirror row must get
// nothing from the session it was copied into.
func TestAssignNextWaitingBookingToWorker_SeparatedModeMirrorRowGetsNothing(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(405)
	createOnlineWorker(t, sessionB.ID, taUserID, true, true)
	createMirrorWorker(t, sessionA.ID, taUserID)
	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	assertNoAssignment(t, result, assignedNow, err)
	assertUntouched(t, booking.ID)
}

// The instructor always belongs to both linked courses (linking requires it),
// and a shared TA pool is common. Separation keys on the session the worker
// joined, so being staff of both courses must not reopen the leak.
func TestAssignNextWaitingBookingToWorker_SeparatedModeSeparatesStaffOfBothCourses(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const instructorID = uint(999) // owner of course_a and course_b
	const sharedTA = uint(406)
	addCourseMember(t, "course_a", sharedTA, "ta")
	addCourseMember(t, "course_b", sharedTA, "ta")
	for _, userID := range []uint{instructorID, sharedTA} {
		createOnlineWorker(t, sessionB.ID, userID, true, true) // joined B only
		createMirrorWorker(t, sessionA.ID, userID)
	}
	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	for _, userID := range []uint{instructorID, sharedTA} {
		result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, userID) // push path
		assertNoAssignment(t, result, assignedNow, err)
		result, assignedNow, err = AssignNextWaitingBookingToWorker(sessionB.ID, userID) // pull path
		assertNoAssignment(t, result, assignedNow, err)
	}
	assertUntouched(t, booking.ID)
}

// A TA who belongs to both courses and joins both sessions directly works both.
// Joining the second session promotes the mirror row created by the first join.
func TestAssignNextWaitingBookingToWorker_SeparatedModeTAWhoJoinedBothSessionsGetsBoth(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(407)
	if _, err := WorkerJoin(sessionB.ID, taUserID, true, true); err != nil {
		t.Fatalf("join B: %v", err)
	}
	if err := WorkerJoinMirrorGroup(sessionB.ID, taUserID, true, true); err != nil {
		t.Fatalf("mirror from B: %v", err)
	}
	if !loadWorker(t, sessionA.ID, taUserID).IsMirror {
		t.Fatal("expected the row copied into A to start as a mirror")
	}
	if _, err := WorkerJoin(sessionA.ID, taUserID, true, true); err != nil {
		t.Fatalf("join A: %v", err)
	}
	if loadWorker(t, sessionA.ID, taUserID).IsMirror {
		t.Fatal("joining A directly must promote the mirror row")
	}

	bookingA := createWaitingBooking(t, sessionA.ID, "desk_1", 1)
	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil || !assignedNow || result == nil || result.ID != bookingA.ID {
		t.Fatalf("expected A's booking after joining A, got assignedNow=%v result=%+v err=%v", assignedNow, result, err)
	}
}

// Switching to "separated" must not strand work handed out while "joint".
func TestAssignNextWaitingBookingToWorker_SeparatedModeStillFindsPreexistingCrossCourseAssignment(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taUserID = uint(403)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	createMirrorWorker(t, sessionB.ID, taUserID)
	booking := createPartnerBooking(t, sessionB.ID, taUserID) // in_progress, already theirs

	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	for _, sessionID := range []string{sessionA.ID, sessionB.ID} {
		result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionID, taUserID)
		if err != nil {
			t.Fatalf("assign via %s: %v", sessionID, err)
		}
		if assignedNow || result == nil || result.ID != booking.ID {
			t.Fatalf("expected the existing booking %d via %s, got assignedNow=%v result=%+v", booking.ID, sessionID, assignedNow, result)
		}
	}
}

func TestAssignNextWaitingBookingToWorker_SeparatedModeStillDispatchesOwnCourseBookings(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(404)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil || !assignedNow || result == nil || result.ID != booking.ID {
		t.Fatalf("expected the own-course booking, got assignedNow=%v result=%+v err=%v", assignedNow, result, err)
	}
}

// A mirror row that can never take the booking must not count as an eligible
// worker: it would force a 90s offer instead of an immediate start (feeding the
// timeout re-offer path) and, with a lower load, stall the real TA in the
// fairness gate.
func TestAssignNextWaitingBookingToWorker_SeparatedModeMirrorDoesNotForceOfferOrStallFairness(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taA, taB = uint(410), uint(411)
	createOnlineWorker(t, sessionA.ID, taA, true, true)
	setWorkerLoad(t, sessionA.ID, taA, 5)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB) // load 0
	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taA)
	if err != nil || !assignedNow || result == nil || result.ID != booking.ID {
		t.Fatalf("expected the real TA to get the booking despite the idle mirror, got assignedNow=%v result=%+v err=%v", assignedNow, result, err)
	}
	if result.Status != "in_progress" || result.OfferExpiresAt != nil {
		t.Fatalf("expected an immediate start (only one eligible worker), got status=%q offer_expires_at=%v", result.Status, result.OfferExpiresAt)
	}
}

// Regression: in "joint" mode the mirror is a real candidate, so it still
// counts - here the fairness gate holds the booking back for it.
func TestAssignNextWaitingBookingToWorker_JointModeMirrorStillCountsAsEligible(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taA, taB = uint(412), uint(413)
	createOnlineWorker(t, sessionA.ID, taA, true, true)
	setWorkerLoad(t, sessionA.ID, taA, 5)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taA)
	assertNoAssignment(t, result, assignedNow, err)
	assertUntouched(t, booking.ID)
}

// ---------- offer timeouts: ProcessQueueOfferTimeouts ----------

// The leak seen in production: an expired course-A offer was re-offered to the
// course-B TA's mirror row. It must go to another of session A's own workers.
func TestProcessQueueOfferTimeouts_SeparatedModeReoffersOnlyToOwnWorkers(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taA1, taA2, taB = uint(501), uint(502), uint(503)
	createOnlineWorker(t, sessionA.ID, taA1, true, true)
	createOnlineWorker(t, sessionA.ID, taA2, true, true)
	setWorkerLoad(t, sessionA.ID, taA2, 3)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB) // least loaded, must be skipped
	booking := createOfferedBooking(t, sessionA.ID, taA1, time.Now().Add(-time.Minute))

	reassignments, err := ProcessQueueOfferTimeouts(sessionA.ID)
	if err != nil {
		t.Fatalf("process timeouts: %v", err)
	}
	if len(reassignments) != 1 || reassignments[0].NewWorkerID != taA2 {
		t.Fatalf("expected the offer to move to %d, got %+v", taA2, reassignments)
	}
	if b := loadBooking(t, booking.ID); b.AssignedWorkerID == nil || *b.AssignedWorkerID != taA2 {
		t.Fatalf("expected booking assigned to %d, got %v", taA2, b.AssignedWorkerID)
	}
	if w := loadWorker(t, sessionA.ID, taB); w.CurrentBookingID != nil {
		t.Fatalf("mirror row must not be handed the offer, got current_booking_id=%v", *w.CurrentBookingID)
	}
}

func TestProcessQueueOfferTimeouts_SeparatedModeOnlyMirrorFreeReturnsToPool(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taA, taB = uint(504), uint(505)
	createOnlineWorker(t, sessionA.ID, taA, true, true)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	booking := createOfferedBooking(t, sessionA.ID, taA, time.Now().Add(-time.Minute))

	reassignments, err := ProcessQueueOfferTimeouts(sessionA.ID)
	if err != nil {
		t.Fatalf("process timeouts: %v", err)
	}
	if len(reassignments) != 1 || reassignments[0].NewWorkerID != 0 {
		t.Fatalf("expected the booking back in the pool, got %+v", reassignments)
	}
	assertUntouched(t, booking.ID)
}

// Regression: in "joint" mode a mirror row is a legitimate re-offer target.
func TestProcessQueueOfferTimeouts_JointModeMayReofferToMirror(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taA, taB = uint(506), uint(507)
	createOnlineWorker(t, sessionA.ID, taA, true, true)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	createOfferedBooking(t, sessionA.ID, taA, time.Now().Add(-time.Minute))

	reassignments, err := ProcessQueueOfferTimeouts(sessionA.ID)
	if err != nil {
		t.Fatalf("process timeouts: %v", err)
	}
	if len(reassignments) != 1 || reassignments[0].NewWorkerID != taB {
		t.Fatalf("expected the joint-mode re-offer to reach %d, got %+v", taB, reassignments)
	}
}

// ---------- accepting / finishing: WorkerUpdateBooking, CompleteBookingWithScores ----------

// An offer that reached a mirror row before the group became "separated" (or
// through any path missed in future) cannot be accepted: it lapses and is
// re-offered to the session's own workers instead.
func TestWorkerUpdateBooking_SeparatedModeRefusesAcceptingViaMirror(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taB = uint(601)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	booking := createOfferedBooking(t, sessionA.ID, taB, time.Now().Add(time.Minute))

	if _, err := WorkerUpdateBooking(booking.ID, taB, "start", nil, ""); !errors.Is(err, ErrQueueBookingOtherCourse) {
		t.Fatalf("expected ErrQueueBookingOtherCourse, got %v", err)
	}
	if b := loadBooking(t, booking.ID); b.Status != "waiting" {
		t.Fatalf("expected the refused offer to stay waiting, got %q", b.Status)
	}
}

func TestWorkerUpdateBooking_JointModeAcceptsViaMirror(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taB = uint(602)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	booking := createOfferedBooking(t, sessionA.ID, taB, time.Now().Add(time.Minute))

	updated, err := WorkerUpdateBooking(booking.ID, taB, "start", nil, "")
	if err != nil || updated.Status != "in_progress" {
		t.Fatalf("expected joint-mode accept to succeed, got err=%v booking=%+v", err, updated)
	}
}

func TestWorkerUpdateBooking_SeparatedModeAcceptsOwnOffer(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taA = uint(603)
	createOnlineWorker(t, sessionA.ID, taA, true, true)
	booking := createOfferedBooking(t, sessionA.ID, taA, time.Now().Add(time.Minute))

	updated, err := WorkerUpdateBooking(booking.ID, taA, "start", nil, "")
	if err != nil || updated.Status != "in_progress" {
		t.Fatalf("expected accepting an own-session offer to succeed, got err=%v booking=%+v", err, updated)
	}
}

// Work already started through a mirror row (before the switch) must still be
// completable - refusing it would strand the booking in_progress forever.
func TestCompleteBookingWithScores_SeparatedModeStillCompletesStartedWorkViaMirror(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taB = uint(604)
	createOnlineWorker(t, sessionB.ID, taB, true, true)
	createMirrorWorker(t, sessionA.ID, taB)
	booking := createPartnerBooking(t, sessionA.ID, taB) // in_progress

	completed, err := CompleteBookingWithScores(booking.ID, taB, nil, "", "done", nil)
	if err != nil || completed.Status != "completed" {
		t.Fatalf("expected started work to stay completable, got err=%v booking=%+v", err, completed)
	}
}

// ---------- worker rows: is_mirror labelling ----------

func TestWorkerJoinAndMirrorGroup_LabelRows(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	const taUserID = uint(701)
	if _, err := WorkerJoin(sessionB.ID, taUserID, true, true); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := WorkerJoinMirrorGroup(sessionB.ID, taUserID, true, true); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if loadWorker(t, sessionB.ID, taUserID).IsMirror {
		t.Fatal("the joined session's row must not be a mirror")
	}
	if !loadWorker(t, sessionA.ID, taUserID).IsMirror {
		t.Fatal("the row copied into the partner session must be a mirror")
	}
}

// Going online again in one session re-syncs the partner rows; that must not
// demote a row the user joined directly.
func TestWorkerJoinMirrorGroup_DoesNotDemoteDirectlyJoinedRow(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	const taUserID = uint(702)
	for _, sessionID := range []string{sessionA.ID, sessionB.ID} {
		if _, err := WorkerJoin(sessionID, taUserID, true, true); err != nil {
			t.Fatalf("join %s: %v", sessionID, err)
		}
	}
	if err := WorkerJoinMirrorGroup(sessionB.ID, taUserID, true, true); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if loadWorker(t, sessionA.ID, taUserID).IsMirror {
		t.Fatal("re-syncing from B must not relabel A's directly joined row")
	}
}

// ---------- one-time backfill of rows that predate is_mirror ----------

func TestBackfillQueueWorkerMirrorFlag_LabelsLegacyRowsOnce(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()

	ungrouped := models.QueueSession{ID: "qs_backfill_c", CourseID: "course_c", ClassroomID: "room_other", Title: "C", PinCode: "333333", Status: "active", LinkMode: QueueLinkModeJoint}
	if err := config.DB.Create(&ungrouped).Error; err != nil {
		t.Fatalf("create ungrouped session: %v", err)
	}

	const (
		taOfB   = uint(801) // joined B; A row came from mirroring (not staff of A)
		taOfAll = uint(802) // staff of both; joined A first, B row copied later
		admin   = uint(803) // admin joins without membership
		stray   = uint(804) // non-staff row in an ungrouped session
	)
	addCourseMember(t, "course_b", taOfB, "ta")
	addCourseMember(t, "course_a", taOfAll, "ta")
	addCourseMember(t, "course_b", taOfAll, "ta")
	now := time.Now().UTC()
	if err := config.DB.Exec(`INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (?, 'admin803', 'x', 'admin', ?, ?)`, admin, now, now).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	legacyRow := func(sessionID string, userID uint, createdAt time.Time) {
		createOnlineWorker(t, sessionID, userID, true, true)
		if err := config.DB.Model(&models.QueueWorker{}).
			Where("queue_session_id = ? AND user_id = ?", sessionID, userID).
			Update("created_at", createdAt).Error; err != nil {
			t.Fatalf("set created_at: %v", err)
		}
	}
	earlier, later := now.Add(-time.Hour), now.Add(-time.Hour+time.Second)
	legacyRow(sessionB.ID, taOfB, earlier)
	legacyRow(sessionA.ID, taOfB, later)
	legacyRow(sessionA.ID, taOfAll, earlier)
	legacyRow(sessionB.ID, taOfAll, later)
	legacyRow(sessionA.ID, admin, earlier)
	legacyRow(ungrouped.ID, stray, earlier)

	labelled, err := BackfillQueueWorkerMirrorFlagWithDB(config.DB)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if labelled != 2 {
		t.Fatalf("expected 2 rows labelled, got %d", labelled)
	}

	want := []struct {
		session string
		user    uint
		mirror  bool
	}{
		{sessionB.ID, taOfB, false},
		{sessionA.ID, taOfB, true},
		{sessionA.ID, taOfAll, false},
		{sessionB.ID, taOfAll, true},
		{sessionA.ID, admin, false},
		{ungrouped.ID, stray, false},
	}
	for _, w := range want {
		if got := loadWorker(t, w.session, w.user).IsMirror; got != w.mirror {
			t.Errorf("worker %d in %s: is_mirror=%v, want %v", w.user, w.session, got, w.mirror)
		}
	}

	// The TA of both courses really did join B too: going online there promotes
	// the row, and a later boot must not demote it again.
	if _, err := WorkerJoin(sessionB.ID, taOfAll, true, true); err != nil {
		t.Fatalf("join B: %v", err)
	}
	if relabelled, err := BackfillQueueWorkerMirrorFlagWithDB(config.DB); err != nil || relabelled != 0 {
		t.Fatalf("expected the second run to be a no-op, got relabelled=%d err=%v", relabelled, err)
	}
	if loadWorker(t, sessionB.ID, taOfAll).IsMirror {
		t.Fatal("a row promoted by joining must survive later boots")
	}
}

// ---------- link mode storage ----------

// Re-linking an already-linked pair must apply the requested mode rather than
// silently keeping the old one.
func TestLinkConcurrentSessions_AlreadyLinkedStillAppliesNewMode(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	if err := LinkConcurrentSessions(sessionA.ID, sessionB.ID, QueueLinkModeSeparated); err != nil {
		t.Fatalf("re-link with new mode: %v", err)
	}
	for _, id := range []string{sessionA.ID, sessionB.ID} {
		if mode, err := GetConcurrentGroupMode(id); err != nil || mode != QueueLinkModeSeparated {
			t.Fatalf("expected %s to be separated, got %q err=%v", id, mode, err)
		}
	}
}

func TestGetConcurrentGroupMode_DefaultsToJointWhenUngroupedOrInvalid(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()

	ungrouped := models.QueueSession{
		ID: "qs_mirror_ungrouped", CourseID: "course_c", ClassroomID: "room_shared",
		Title: "Ungrouped", PinCode: "333333", Status: "active", LinkMode: QueueLinkModeJoint,
	}
	if err := config.DB.Create(&ungrouped).Error; err != nil {
		t.Fatalf("create ungrouped session: %v", err)
	}
	if mode, err := GetConcurrentGroupMode(ungrouped.ID); err != nil || mode != QueueLinkModeJoint {
		t.Fatalf("expected joint for ungrouped session, got %q err=%v", mode, err)
	}

	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", sessionA.ID).Update("link_mode", "").Error; err != nil {
		t.Fatalf("force empty link_mode: %v", err)
	}
	if mode, err := GetConcurrentGroupMode(sessionA.ID); err != nil || mode != QueueLinkModeJoint {
		t.Fatalf("expected joint fallback for empty link_mode, got %q err=%v", mode, err)
	}
}

func TestSetConcurrentGroupMode_UpdatesBothSessionsAndValidates(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	if err := SetConcurrentGroupMode(sessionA.ID, QueueLinkModeSeparated); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	for _, id := range []string{sessionA.ID, sessionB.ID} {
		if mode, err := GetConcurrentGroupMode(id); err != nil || mode != QueueLinkModeSeparated {
			t.Fatalf("expected both sessions separated, %s has %q err=%v", id, mode, err)
		}
	}

	if err := SetConcurrentGroupMode(sessionA.ID, "bogus"); err == nil {
		t.Fatal("expected an invalid mode to be rejected")
	}
	if mode, _ := GetConcurrentGroupMode(sessionA.ID); mode != QueueLinkModeSeparated {
		t.Fatalf("expected a rejected update to leave the mode alone, got %q", mode)
	}

	ungrouped := models.QueueSession{
		ID: "qs_mirror_ungrouped_2", CourseID: "course_d", ClassroomID: "room_shared",
		Title: "Ungrouped", PinCode: "444444", Status: "active", LinkMode: QueueLinkModeJoint,
	}
	if err := config.DB.Create(&ungrouped).Error; err != nil {
		t.Fatalf("create ungrouped session: %v", err)
	}
	if err := SetConcurrentGroupMode(ungrouped.ID, QueueLinkModeSeparated); err == nil {
		t.Fatal("expected setting mode on an ungrouped session to fail")
	}
}
