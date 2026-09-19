package repositories

import (
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"
)

func setLinkMode(t *testing.T, groupID, mode string) {
	t.Helper()
	if err := config.DB.Model(&models.QueueSession{}).
		Where("concurrent_group_id = ?", groupID).
		Update("link_mode", mode).Error; err != nil {
		t.Fatalf("set link_mode: %v", err)
	}
}

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

// Baseline regression: an ungrouped-by-mode-name but "joint" linked group must
// keep dispatching bookings across the course boundary exactly as it did
// before link_mode existed. A worker who only ever joined session A can still
// be handed a waiting booking that actually lives in session B.
func TestAssignNextWaitingBookingToWorker_JointModeCrossesCourses(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taUserID = uint(401)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	createOnlineWorker(t, sessionB.ID, taUserID, true, true) // mirrored at link time

	booking := createWaitingBooking(t, sessionB.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !assignedNow {
		t.Fatal("expected a fresh assignment")
	}
	if result == nil || result.ID != booking.ID {
		t.Fatalf("expected booking %d to be assigned, got %+v", booking.ID, result)
	}
	if result.QueueSessionID != sessionB.ID {
		t.Fatalf("expected the cross-course booking to stay recorded under session B, got %q", result.QueueSessionID)
	}
	if result.Status != "in_progress" {
		t.Fatalf("expected direct assignment (single eligible worker), got status %q", result.Status)
	}

	var sessionBWorker models.QueueWorker
	if err := config.DB.Where("queue_session_id = ? AND user_id = ?", sessionB.ID, taUserID).First(&sessionBWorker).Error; err != nil {
		t.Fatalf("load session B worker row: %v", err)
	}
	if sessionBWorker.CurrentBookingID == nil || *sessionBWorker.CurrentBookingID != booking.ID {
		t.Fatalf("expected session B worker row to record the current booking, got %v", sessionBWorker.CurrentBookingID)
	}
}

// The actual feature: a "separated" group must never hand a worker a booking
// that belongs to the partner course, even though the worker still holds a
// mirrored row there for visibility.
func TestAssignNextWaitingBookingToWorker_SeparatedModeNeverCrossesCourses(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(402)
	addCourseMember(t, "course_a", taUserID, "ta") // genuine home course, not just a worker row
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	createOnlineWorker(t, sessionB.ID, taUserID, true, true) // still mirrored/visible

	booking := createWaitingBooking(t, sessionB.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if assignedNow || result != nil {
		t.Fatalf("expected no assignment across the course boundary, got assignedNow=%v result=%+v", assignedNow, result)
	}

	var untouched models.QueueBooking
	if err := config.DB.First(&untouched, booking.ID).Error; err != nil {
		t.Fatalf("reload booking: %v", err)
	}
	if untouched.Status != "waiting" || untouched.AssignedWorkerID != nil {
		t.Fatalf("expected the partner-course booking to remain untouched, got status=%q assigned_worker_id=%v", untouched.Status, untouched.AssignedWorkerID)
	}
}

// Switching a group to "separated" must not strand work that was already
// handed out while it was "joint" - the in-flight booking still has to be
// found and returned so the TA can finish it.
func TestAssignNextWaitingBookingToWorker_SeparatedModeStillFindsPreexistingCrossCourseAssignment(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	const taUserID = uint(403)
	addCourseMember(t, "course_a", taUserID, "ta") // genuine home course
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)
	inProgress := createOnlineWorker(t, sessionB.ID, taUserID, true, true)
	_ = inProgress

	booking := createPartnerBooking(t, sessionB.ID, taUserID) // status in_progress, assigned already

	// Now switch the group to separated mid-flight.
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if assignedNow {
		t.Fatal("expected the existing assignment to be returned, not a fresh one")
	}
	if result == nil || result.ID != booking.ID {
		t.Fatalf("expected the pre-existing cross-course booking %d to still be found, got %+v", booking.ID, result)
	}
}

// Positive control for the membership gate: a worker with genuine access to
// their own session's course must keep receiving that course's own bookings
// in a separated group - the gate must not accidentally block same-course
// dispatch, only cross-course dispatch.
func TestAssignNextWaitingBookingToWorker_SeparatedModeStillDispatchesOwnCourseBookings(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(404)
	addCourseMember(t, "course_a", taUserID, "ta")
	createOnlineWorker(t, sessionA.ID, taUserID, true, true)

	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1)

	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !assignedNow || result == nil || result.ID != booking.ID {
		t.Fatalf("expected the worker's own-course booking to still be dispatched, got assignedNow=%v result=%+v", assignedNow, result)
	}
}

// Reproduces the background push-sweep path: dispatchWaitingBookingsToAvailableWorkers
// iterates every QueueWorker row physically sitting in a session - including a
// mirrored row that exists purely for cross-group visibility - and calls
// AssignNextWaitingBookingToWorker(thatSession, thatWorker) for each one. A
// worker who is only mirrored into session A (their real course is B) must
// never receive session A's own bookings this way, even though scoping the
// booking *pool* to session A alone would otherwise let it happen.
func TestAssignNextWaitingBookingToWorker_SeparatedModeMirroredWorkerCannotDispatchInPartnerSession(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeSeparated)

	const taUserID = uint(405)
	addCourseMember(t, "course_b", taUserID, "ta") // real home course is B, not A
	createOnlineWorker(t, sessionB.ID, taUserID, true, true)
	createOnlineWorker(t, sessionA.ID, taUserID, true, true) // mirrored into A for visibility only

	booking := createWaitingBooking(t, sessionA.ID, "desk_1", 1) // course A's own booking

	// Called with sessionA as the target - exactly what the push sweep does
	// for every worker row it finds sitting in session A, mirrored or not.
	result, assignedNow, err := AssignNextWaitingBookingToWorker(sessionA.ID, taUserID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if assignedNow || result != nil {
		t.Fatalf("expected a mirrored-only worker to never receive the partner session's own booking, got assignedNow=%v result=%+v", assignedNow, result)
	}

	var untouched models.QueueBooking
	if err := config.DB.First(&untouched, booking.ID).Error; err != nil {
		t.Fatalf("reload booking: %v", err)
	}
	if untouched.Status != "waiting" || untouched.AssignedWorkerID != nil {
		t.Fatalf("expected session A's booking to remain untouched, got status=%q assigned_worker_id=%v", untouched.Status, untouched.AssignedWorkerID)
	}
}

// Calling LinkConcurrentSessions again on a pair that's already linked must
// still apply the requested mode, not silently keep whatever mode was set the
// first time (a caller switching modes this way, e.g. a retried request,
// must not get a false "success" while nothing actually changed).
func TestLinkConcurrentSessions_AlreadyLinkedStillAppliesNewMode(t *testing.T) {
	cleanup, sessionA, sessionB := setupConcurrentGroupTestDB(t)
	defer cleanup()
	setLinkMode(t, *sessionA.ConcurrentGroupID, QueueLinkModeJoint)

	if err := LinkConcurrentSessions(sessionA.ID, sessionB.ID, QueueLinkModeSeparated); err != nil {
		t.Fatalf("re-link with new mode: %v", err)
	}

	for _, id := range []string{sessionA.ID, sessionB.ID} {
		mode, err := GetConcurrentGroupMode(id)
		if err != nil {
			t.Fatalf("get mode for %s: %v", id, err)
		}
		if mode != QueueLinkModeSeparated {
			t.Fatalf("expected re-linking an already-linked pair to apply the new mode, %s still has %q", id, mode)
		}
	}
}

func TestGetConcurrentGroupMode_DefaultsToJointWhenUngroupedOrInvalid(t *testing.T) {
	cleanup, sessionA, _ := setupConcurrentGroupTestDB(t)
	defer cleanup()

	// Ungrouped session.
	ungrouped := models.QueueSession{
		ID: "qs_mirror_ungrouped", CourseID: "course_c", ClassroomID: "room_shared",
		Title: "Ungrouped", PinCode: "333333", Status: "active", LinkMode: QueueLinkModeJoint,
	}
	if err := config.DB.Create(&ungrouped).Error; err != nil {
		t.Fatalf("create ungrouped session: %v", err)
	}
	mode, err := GetConcurrentGroupMode(ungrouped.ID)
	if err != nil {
		t.Fatalf("get mode: %v", err)
	}
	if mode != QueueLinkModeJoint {
		t.Fatalf("expected joint default for ungrouped session, got %q", mode)
	}

	// Legacy/corrupt row with an empty link_mode column must still default safely.
	if err := config.DB.Model(&models.QueueSession{}).Where("id = ?", sessionA.ID).Update("link_mode", "").Error; err != nil {
		t.Fatalf("force empty link_mode: %v", err)
	}
	mode, err = GetConcurrentGroupMode(sessionA.ID)
	if err != nil {
		t.Fatalf("get mode: %v", err)
	}
	if mode != QueueLinkModeJoint {
		t.Fatalf("expected joint fallback for empty link_mode, got %q", mode)
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
		mode, err := GetConcurrentGroupMode(id)
		if err != nil {
			t.Fatalf("get mode for %s: %v", id, err)
		}
		if mode != QueueLinkModeSeparated {
			t.Fatalf("expected both sessions to share the new mode, %s has %q", id, mode)
		}
	}

	if err := SetConcurrentGroupMode(sessionA.ID, "bogus"); err == nil {
		t.Fatal("expected an invalid mode to be rejected")
	}
	// Rejected call must not have clobbered the previously-set mode.
	mode, err := GetConcurrentGroupMode(sessionA.ID)
	if err != nil {
		t.Fatalf("get mode: %v", err)
	}
	if mode != QueueLinkModeSeparated {
		t.Fatalf("expected mode to remain unchanged after a rejected update, got %q", mode)
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
