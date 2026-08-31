package services

import (
	"context"
	"encoding/json"
	"itii-assist/models"
	"itii-assist/repositories"
	"log/slog"
	"strconv"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// =============================================================================
// Mirroring security events into the course activity log
//
// Attendance check-ins are recorded in system_logs, which nobody but an
// operator ever reads. That is the right home for the full forensic record, but
// it means an instructor looking at their own course never learns that six
// students were turned away by the campus network guard during last Tuesday's
// class.
//
// Only the exceptional outcomes are mirrored. A successful check-in is already
// on the attendance screen, and copying every one of them would add a row per
// student per session and bury the rest of the course's history.
// =============================================================================

// Mirrored check-in actions. Categorised as "attendance" so they sit with the
// rest of the attendance history rather than in a separate silo.
const (
	ActionCheckInFailed      = "attendance_checkin_failed"
	ActionCheckInBlocked     = "attendance_checkin_blocked"
	ActionCheckInRateLimited = "attendance_checkin_rate_limited"
)

// mirroredCheckInActions maps a check-in result to the action recorded in the
// course log. Results absent from this map are not mirrored: success and
// duplicate are routine, and guard_unavailable is a fault in this system rather
// than anything the student or instructor did.
var mirroredCheckInActions = map[string]string{
	AttendanceResultFailed:         ActionCheckInFailed,
	AttendanceResultNetworkBlocked: ActionCheckInBlocked,
	AttendanceResultRateLimited:    ActionCheckInRateLimited,
}

// courseMirrorPool is a third lane, so mirroring can never crowd out either the
// forensic check-in record or the read audit.
var courseMirrorPool = auditPool{sem: make(chan struct{}, 8), name: "course_mirror"}

// MirrorAttendanceCheckIn copies a refused check-in into the course activity
// log, in the background, so an instructor sees it alongside everything else
// that happened in the course. Successful check-ins are deliberately skipped.
func MirrorAttendanceCheckIn(db *gorm.DB, ev AttendanceCheckInEvent) {
	if db == nil || ev.SessionID == 0 {
		return
	}

	action, mirrored := mirroredCheckInActions[ev.Result]
	if !mirrored {
		return
	}

	go func() {
		select {
		case courseMirrorPool.sem <- struct{}{}:
			defer func() { <-courseMirrorPool.sem }()
		default:
			slog.Warn("audit: dropping course log mirror, lane full",
				"session_id", ev.SessionID, "result", ev.Result)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		courseID, err := repositories.GetCourseIDByAttendanceSessionID(ev.SessionID)
		if err != nil || courseID == "" {
			// A check-in for a session that no longer exists has nowhere to be
			// mirrored to. The forensic record in system_logs still stands.
			return
		}

		detail := map[string]any{
			"result":                ev.Result,
			"fail_code":             ev.FailCode,
			"attendance_session_id": ev.SessionID,
		}
		if len(ev.FailedChecks) > 0 {
			detail["failed_checks"] = ev.FailedChecks
		}
		if ev.Email != "" {
			detail["email"] = ev.Email
		}

		record := models.CourseActivityLog{
			CourseID: courseID,
			// No user account performed this: a student is not a user row, and
			// inventing one would put a stranger's name on the event. The
			// student is carried as the target instead, which the log's
			// resolver turns into a name.
			ActorUserID: 0,
			ActorRole:   "student",
			Action:      action,
			Category:    "attendance",
			TargetType:  "student",
			TargetID:    studentTargetID(ev.StudentID),
			IPAddress:   ev.IP,
			UserAgent:   ev.UserAgent,
			RequestID:   ev.RequestID,
		}

		if payload, err := json.Marshal(detail); err == nil {
			record.Detail = datatypes.JSON(payload)
		}

		if err := db.WithContext(ctx).Create(&record).Error; err != nil {
			slog.Error("audit: failed to mirror check-in into course activity log",
				"error", err, "session_id", ev.SessionID, "result", ev.Result)
		}
	}()
}

// studentTargetID renders the student reference, leaving it empty when the
// identity could not be resolved rather than writing a misleading "0".
func studentTargetID(studentID uint) string {
	if studentID == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(studentID), 10)
}
