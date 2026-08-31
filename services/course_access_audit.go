package services

import (
	"context"
	"encoding/json"
	"itii-assist/models"
	"itii-assist/repositories"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// =============================================================================
// Course access (read) audit
//
// Every other writer for course_activity_logs records a mutation. This one
// records that somebody *looked*: which course they opened, whose scores they
// read, what they exported. It answers the question the change log cannot,
// namely "an admin who is not on this course opened its score sheet at 02:10".
//
// Reads are orders of magnitude more frequent than writes, so two rules govern
// everything below:
//
//  1. A view is recorded at most once per actor, course and action within
//     courseViewDedupeTTL. Opening a page fires a handful of GETs and users
//     refresh constantly; without this the table would grow by tens of rows per
//     page view and the log would be unreadable for the instructor it is for.
//  2. Nothing happens on the request path beyond an in-memory map lookup. The
//     membership check and the insert run in a bounded background lane, so a
//     slow database never slows down a page that is merely being read.
// =============================================================================

// CourseAccessCategory is the CourseActivityLog.Category for read events. It is
// deliberately distinct from the mutation categories so the instructor UI can
// separate "what changed" from "who looked", and so retention can expire access
// rows earlier than the change history.
const CourseAccessCategory = "access"

// Read actions. These land in CourseActivityLog.Action and are translated by
// the frontend, so keep the strings stable.
const (
	ActionViewCourse        = "view_course"
	ActionViewScores        = "view_scores"
	ActionViewRoster        = "view_roster"
	ActionViewAttendance    = "view_attendance"
	ActionViewExam          = "view_exam"
	ActionViewActivityLog   = "view_activity_log"
	ActionExportExamSeats   = "export_exam_seats"
	ActionExportActivityLog = "export_activity_log"
)

// courseViewDedupeTTL is how long one (actor, course, action) triple stays
// suppressed after being recorded. Ten minutes keeps a working session down to
// a handful of rows while still showing a distinct visit later in the day.
var courseViewDedupeTTL = resolveCourseViewTTL()

func resolveCourseViewTTL() time.Duration {
	const fallback = 10 * time.Minute
	raw := strings.TrimSpace(os.Getenv("COURSE_VIEW_AUDIT_TTL_MINUTES"))
	if raw == "" {
		return fallback
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes < 1 {
		slog.Warn("audit: invalid COURSE_VIEW_AUDIT_TTL_MINUTES, using default",
			"value", raw, "default_minutes", int(fallback.Minutes()))
		return fallback
	}
	return time.Duration(minutes) * time.Minute
}

// courseViewAuditPool is its own lane, separate from the attendance lanes: a
// burst of page views must never be able to fill the queue that carries
// check-in evidence. Dropping a view record costs a line in a browsing history;
// dropping a check-in record costs evidence in a dispute.
var courseViewAuditPool = auditPool{sem: make(chan struct{}, 16), name: "course_view"}

// -----------------------------------------------------------------------------
// Dedupe cache
// -----------------------------------------------------------------------------

// courseViewSweepThreshold is the entry count above which a write also sweeps
// expired keys. The cache is bounded by (active users x courses x actions), so
// it is small in practice; the sweep exists so an idle process does not hold
// yesterday's keys forever.
const courseViewSweepThreshold = 4096

type courseViewDedupe struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

var courseViewCache = courseViewDedupe{seen: map[string]time.Time{}}

// admit reports whether this view should be recorded, and marks it as seen when
// it should. Check and mark happen under one lock so two concurrent requests
// cannot both decide to write.
func (d *courseViewDedupe) admit(key string, now time.Time, ttl time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if last, ok := d.seen[key]; ok && now.Sub(last) < ttl {
		return false
	}

	if len(d.seen) >= courseViewSweepThreshold {
		for existing, at := range d.seen {
			if now.Sub(at) >= ttl {
				delete(d.seen, existing)
			}
		}
	}

	d.seen[key] = now
	return true
}

// ResetCourseViewDedupeForTest clears the suppression cache. Tests only.
func ResetCourseViewDedupeForTest() {
	courseViewCache.mu.Lock()
	defer courseViewCache.mu.Unlock()
	courseViewCache.seen = map[string]time.Time{}
}

// courseViewDedupeKey joins the triple that identifies one suppressed view. The
// course ID is length-prefixed so no course ID containing the separator can
// ever produce the same key as a different triple.
func courseViewDedupeKey(actorUserID uint, courseID, action string) string {
	return strconv.FormatUint(uint64(actorUserID), 10) + "|" +
		strconv.Itoa(len(courseID)) + ":" + courseID + "|" + action
}

// -----------------------------------------------------------------------------
// Event
// -----------------------------------------------------------------------------

// CourseViewEvent describes one recorded read of a course resource.
type CourseViewEvent struct {
	CourseID    string
	ActorUserID uint
	ActorRole   string
	ActorEmail  string
	Action      string

	// TargetType and TargetID name the specific thing that was read, when the
	// route identifies one (an attendance session, an exam session).
	TargetType string
	TargetID   string

	IPAddress string
	UserAgent string
	Path      string
	Method    string
}

// LogCourseView records that an actor read part of a course, subject to the
// dedupe window. Safe to call from a middleware on every request: it returns
// after an in-memory lookup and does the rest in the background.
func LogCourseView(db *gorm.DB, ev CourseViewEvent) {
	if db == nil {
		return
	}
	ev.CourseID = strings.TrimSpace(ev.CourseID)
	ev.Action = strings.TrimSpace(ev.Action)
	if ev.CourseID == "" || ev.ActorUserID == 0 || ev.Action == "" {
		return
	}

	// The dedupe key deliberately excludes the target: opening five sessions of
	// one course in a row is one visit to that course's attendance, and the
	// target of the first one is recorded. Splitting by target would put the
	// per-page-view flood straight back.
	if !courseViewCache.admit(courseViewDedupeKey(ev.ActorUserID, ev.CourseID, ev.Action), time.Now(), courseViewDedupeTTL) {
		return
	}

	writeCourseViewLog(db, ev)
}

func writeCourseViewLog(db *gorm.DB, ev CourseViewEvent) {
	go func() {
		select {
		case courseViewAuditPool.sem <- struct{}{}:
			defer func() { <-courseViewAuditPool.sem }()
		default:
			slog.Warn("audit: dropping course view log, lane full",
				"course_id", ev.CourseID, "action", ev.Action)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		detail := map[string]any{
			"path":   ev.Path,
			"method": ev.Method,
		}

		// Snapshot the actor email at event time, matching the mutation writer,
		// so the log stays readable after an account is deleted.
		if strings.TrimSpace(ev.ActorEmail) == "" {
			var actor models.User
			if err := db.WithContext(ctx).Select("email").First(&actor, ev.ActorUserID).Error; err == nil {
				ev.ActorEmail = actor.Email
			}
		}

		// An admin outside the course is the case this whole feature exists to
		// surface, so it is resolved here rather than left for the reader to
		// infer from the role chip. Course members are marked too, so a missing
		// flag never has to be read as "unknown".
		if strings.EqualFold(ev.ActorRole, "admin") {
			_, isMember, err := repositories.GetCourseAccessState(ev.CourseID, ev.ActorUserID, "instructor", "ta")
			if err != nil {
				slog.Warn("audit: failed to resolve course membership for view log",
					"error", err, "course_id", ev.CourseID)
			} else {
				detail["is_course_member"] = isMember
				detail["is_outsider_admin"] = !isMember
			}
		}

		record := models.CourseActivityLog{
			CourseID:    ev.CourseID,
			ActorUserID: ev.ActorUserID,
			ActorEmail:  ev.ActorEmail,
			ActorRole:   ev.ActorRole,
			Action:      ev.Action,
			Category:    CourseAccessCategory,
			TargetType:  ev.TargetType,
			TargetID:    ev.TargetID,
			IPAddress:   ev.IPAddress,
			UserAgent:   ev.UserAgent,
		}

		if payload, err := json.Marshal(detail); err == nil {
			record.Detail = datatypes.JSON(payload)
		}

		if err := db.WithContext(ctx).Create(&record).Error; err != nil {
			slog.Error("audit: failed to write course view log",
				"error", err, "course_id", ev.CourseID, "action", ev.Action)
		}
	}()
}
