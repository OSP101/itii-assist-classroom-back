package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/models"
	"itii-assist/observability"
	"itii-assist/utils"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AttendanceCheckInLogType is the SystemLog.LogType used for every check-in
// forensic event. Query it directly to review who attempted to check in, from
// where, and whether the campus guard let them through:
//
//	SELECT * FROM system_logs WHERE log_type = 'attendance_checkin' ...
const AttendanceCheckInLogType = "attendance_checkin"

// Check-in result codes recorded in SystemLog.Action (prefixed
// "attendance.checkin."). Kept as constants so log queries and dashboards can
// rely on a stable vocabulary.
const (
	AttendanceResultSuccess        = "success"         // present or late, recorded
	AttendanceResultDuplicate      = "duplicate"       // already checked in
	AttendanceResultNetworkBlocked = "network_blocked" // failed campus device/network/domain guard
	AttendanceResultRateLimited    = "rate_limited"    // too many attempts
	AttendanceResultFailed         = "failed"          // wrong PIN, outside area, closed, not eligible, etc.
	// AttendanceResultGuardUnavailable is a check-in refused because the campus
	// guard could not resolve the session and chose to fail closed. Kept
	// distinct from network_blocked: nobody did anything wrong, the system did,
	// and mixing the two would make "blocked attempts" unreadable.
	AttendanceResultGuardUnavailable = "guard_unavailable"
)

// AttendanceActionPrefix is prepended to a result code to form SystemLog.Action.
const AttendanceActionPrefix = "attendance.checkin."

// attendanceNetworkBlockedAction is the Action value of a campus-guard
// rejection — the row CheckAndLogDeviceGuardFlip correlates against.
const attendanceNetworkBlockedAction = AttendanceActionPrefix + AttendanceResultNetworkBlocked

// AttendanceDeviceFlipAction marks the heuristic flag CheckAndLogDeviceGuardFlip
// writes. Exported so the instructor-facing review endpoint can select it.
const AttendanceDeviceFlipAction = AttendanceActionPrefix + "suspicious_device_flip"

const attendanceDeviceFlipAction = AttendanceDeviceFlipAction

// attendanceDeviceFlipIndexPredicate is the literal WHERE fragment that must
// stay byte-identical to the predicate of idx_system_logs_device_guard_flip in
// config/database.go, or that partial index stops being usable. It is built
// from constants declared in this file and never from request data, so
// inlining the values instead of binding them carries no injection risk; the
// literals cannot contain a quote character.
//
// Both actions are in the list because the probe reads its own previous flag
// to avoid writing the same one twice — see CheckAndLogDeviceGuardFlip.
var attendanceDeviceFlipIndexPredicate = fmt.Sprintf(
	"log_type = '%s' AND action IN ('%s', '%s')",
	AttendanceCheckInLogType, attendanceNetworkBlockedAction, attendanceDeviceFlipAction,
)

// AttendanceSessionLogsPredicate is the literal WHERE fragment matching
// idx_system_logs_attendance_session in config/database.go, for queries that
// scan one session's check-in log. Same literal-not-parameter reasoning as
// attendanceDeviceFlipIndexPredicate above.
var AttendanceSessionLogsPredicate = fmt.Sprintf("log_type = '%s'", AttendanceCheckInLogType)

// attendanceLogDetail is the slice of a check-in log's jsonb detail that the
// probe reads back. Everything else in there is write-only forensic context.
type attendanceLogDetail struct {
	StudentID    uint     `json:"student_id"`
	FailedChecks []string `json:"failed_checks"`
}

func decodeAttendanceLogDetail(raw datatypes.JSON) attendanceLogDetail {
	var decoded attendanceLogDetail
	if len(raw) == 0 {
		return decoded
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// A detail payload we can't read is not worth failing a heuristic over;
		// the caller simply finds nothing to correlate.
		return attendanceLogDetail{}
	}
	return decoded
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// auditPool is one bounded lane of background audit work. A caller never
// blocks on it: when the lane is full the job is dropped and counted.
type auditPool struct {
	sem  chan struct{}
	name string
}

// The two lanes are deliberately separate, never one shared semaphore.
//
// auditWritePool carries the primary forensic records — one per allowed or
// blocked check-in. Without a cap, a burst at the start of class (150+
// students in a few seconds) fans out an unbounded number of goroutines and
// DB connections at exactly the moment the DB is busiest with the check-ins
// themselves.
//
// auditProbePool carries correlation probes (CheckAndLogDeviceGuardFlip),
// which hold their slot for a SELECT *and* a write and are therefore much
// slower per job. Sharing one lane would let a burst of probes fill the queue
// and starve out the records an attendance dispute actually rests on: a
// dropped probe costs a hint, a dropped check-in record costs evidence. The
// probe lane is also the smaller of the two so it can never dominate the
// connection pool.
var (
	auditWritePool = auditPool{sem: make(chan struct{}, 32), name: "write"}
	auditProbePool = auditPool{sem: make(chan struct{}, 8), name: "probe"}
)

// ClientDeviceSignals are best-effort hints the check-in page collects about
// the device it's running on. None of this gates check-in and none of it is
// trustworthy: every field arrives in the request body, so anyone able to
// forge a User-Agent can equally well forge these. Treat it as forensic
// context that raises the cost of a careless spoof, never as a control.
//
// What it does and does not catch, stated plainly so nobody mistakes it for
// protection it doesn't provide:
//
//   - Chrome DevTools device toolbar DOES emulate touch, maxTouchPoints,
//     (pointer: coarse), screen size and devicePixelRatio, and for a preset
//     device it also rewrites the UA client hints. Against that tool, every
//     field here looks like a genuine phone. The original "touch and coarse
//     pointer and motion are all false" rule therefore could never fire for
//     the exact scenario it was written for.
//   - A plain UA-switcher extension, or DevTools with a hand-typed custom UA,
//     rewrites the UA string but NOT navigator.userAgentData. Platform/Mobile
//     below are what catch that case.
//   - Motion is the one signal DevTools leaves alone by default (its sensor
//     emulation is off unless switched on), but a real phone reports false too
//     when iOS motion permission was never granted — so it is a weak hint on
//     its own and is scored, not decisive.
//   - Omitting the whole object is recorded separately as
//     client_signals_missing, not as a mismatch: the web client always sends
//     it, but the same endpoints are reachable by clients that never will.
//     See AttendanceCheckInEvent.ClientSignalsExpected.
type ClientDeviceSignals struct {
	Touch         bool `json:"touch"`
	CoarsePointer bool `json:"coarse_pointer"`
	Motion        bool `json:"motion"`

	// Platform and Mobile come from navigator.userAgentData (UA client hints),
	// which a UA-string spoofer usually forgets. Empty on browsers that don't
	// implement UA-CH at all (Safari, Firefox), which is why an empty value is
	// never treated as a mismatch.
	Platform string `json:"platform"`
	Mobile   *bool  `json:"mobile"`

	// Coarse hardware facts, recorded for an operator to eyeball rather than
	// matched against any rule.
	HardwareCores int     `json:"hardware_cores"`
	ScreenW       int     `json:"screen_w"`
	ScreenH       int     `json:"screen_h"`
	DevicePixels  float64 `json:"dpr"`
}

// desktopClientPlatforms are navigator.userAgentData.platform values that
// contradict a User-Agent claiming to be a phone or tablet.
var desktopClientPlatforms = []string{"windows", "macos", "mac os", "linux", "chrome os", "chromeos"}

// sanitizeClientSignals bounds the one free-form, client-controlled string
// before it is stored in jsonb. Everything else in the struct is a bool or a
// number and cannot grow the payload.
func sanitizeClientSignals(sig *ClientDeviceSignals) *ClientDeviceSignals {
	if sig == nil {
		return nil
	}
	clean := *sig
	clean.Platform = strings.TrimSpace(clean.Platform)
	if len(clean.Platform) > 64 {
		clean.Platform = clean.Platform[:64]
	}
	return &clean
}

// clientSignalMismatchReasons returns the reasons a mobile/tablet User-Agent
// disagrees with what the client reported about itself, strongest first, plus
// whether any of them is strong enough to raise the log severity. An empty
// result means "nothing to say", not "verified genuine".
func clientSignalMismatchReasons(deviceType string, sig *ClientDeviceSignals) (reasons []string, strong bool) {
	if sig == nil || (deviceType != "mobile" && deviceType != "tablet") {
		return nil, false
	}

	// UA client hints contradicting the UA string is the strongest thing we
	// can observe here, because the two come from different places and a
	// UA-string spoofer typically only rewrites one of them.
	if platform := strings.ToLower(sig.Platform); platform != "" {
		for _, desktop := range desktopClientPlatforms {
			if strings.Contains(platform, desktop) {
				reasons = append(reasons, "ua_platform_conflict")
				strong = true
				break
			}
		}
	}
	if sig.Mobile != nil && !*sig.Mobile && deviceType == "mobile" {
		reasons = append(reasons, "ua_ch_not_mobile")
		strong = true
	}

	// The original rule, kept but demoted: it cannot fire under DevTools
	// emulation, and it does fire for an honest mouse-only tablet.
	if !sig.Touch && !sig.CoarsePointer && !sig.Motion {
		reasons = append(reasons, "no_device_traits")
	}
	return reasons, strong
}

// AttendanceCheckInEvent is one forensic record of a check-in attempt. Email
// and Google ID are hashed before storage — no plaintext PII lands in the log
// table. StudentID stays in the clear on purpose: identifying which enrolled
// student a suspicious attempt belongs to is the whole point.
type AttendanceCheckInEvent struct {
	SessionID     uint
	StudentID     uint // 0 when identity could not be resolved
	Email         string
	GoogleID      string
	Result        string // one of the AttendanceResult* constants
	FailCode      string // machine code for the failure, e.g. ATTENDANCE_INVALID_PIN
	StatusCode    int
	FailedChecks  []string // subset of "device","network","domain" from the campus guard
	IP            string   // resolved client IP (post-proxy, what the guard actually judged)
	RealIP        string   // raw X-Real-IP header, for spotting proxy/spoof mismatches
	ForwardedFor  string   // raw X-Forwarded-For header, ditto
	Host          string
	UserAgent     string
	RequestID     string
	Method        string
	URL           string
	ClientSignals *ClientDeviceSignals // optional — absent on older clients and on guard-block events

	// ClientSignalsExpected marks the call sites that are reached only after
	// the request body has been parsed, where the official client always
	// supplies ClientSignals. Only those may treat a missing object as a
	// finding; guard middleware rejects before the body is read, so it leaves
	// this false and a nil ClientSignals there means nothing.
	ClientSignalsExpected bool
}

// writeAttendanceSystemLog runs build in a background goroutine, capped by the
// given lane's semaphore, so no caller ever blocks on the write and a check-in
// burst can't fan out unbounded DB connections. Pass auditWritePool for a
// primary forensic record and auditProbePool for heuristic correlation work —
// see the auditPool declarations for why the two must not share a lane.
// build does whatever query/lookup
// work it needs (using the provided context, which carries a 5s timeout) and
// returns the record to write plus its detail payload; returning ok=false
// skips the write entirely (e.g. "nothing to flag"). errLogMsg is logged via
// slog.Error, together with the failing error and logAttrs, if the write itself
// fails. logAttrs are slog key/value pairs identifying which event was lost —
// without them a dropped or failed audit write is unattributable, which defeats
// the point of noticing it at all.
func writeAttendanceSystemLog(pool auditPool, db *gorm.DB, errLogMsg string, logAttrs []any, build func(ctx context.Context) (record models.SystemLog, detail map[string]any, ok bool)) {
	go func() {
		select {
		case pool.sem <- struct{}{}:
			defer func() { <-pool.sem }()
		default:
			// Dropping a forensic record is evidence loss, so it is counted as
			// a metric and not left to be noticed in a text log.
			observability.RecordAttendanceAuditDropped(pool.name)
			slog.Warn("audit: dropping background job, lane full",
				append([]any{"lane", pool.name, "context", errLogMsg}, logAttrs...)...)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		record, detail, ok := build(ctx)
		if !ok {
			return
		}
		if len(detail) > 0 {
			if b, err := json.Marshal(detail); err == nil {
				record.Detail = datatypes.JSON(b)
			}
		}
		if err := db.WithContext(ctx).Create(&record).Error; err != nil {
			slog.Error(errLogMsg, append([]any{"error", err}, logAttrs...)...)
		}
	}()
}

// LogAttendanceCheckIn writes one check-in forensic record to SystemLog. It is
// safe to call from both handlers and middlewares (pass config.DB). The write
// happens in a background goroutine so it never adds latency to the check-in
// response, mirroring AuditLogger.LogSystem.
func LogAttendanceCheckIn(db *gorm.DB, ev AttendanceCheckInEvent) {
	if db == nil {
		return
	}

	writeAttendanceSystemLog(auditWritePool, db, "audit: failed to write attendance check-in log", []any{"result", ev.Result, "session_id", ev.SessionID}, func(ctx context.Context) (models.SystemLog, map[string]any, bool) {
		severity := "info"
		switch ev.Result {
		case AttendanceResultNetworkBlocked, AttendanceResultRateLimited, AttendanceResultFailed, AttendanceResultGuardUnavailable:
			severity = "warn"
		}

		deviceType, browser, osName := utils.ParseUserAgent(ev.UserAgent)

		detail := map[string]any{}
		if ev.StudentID > 0 {
			detail["student_id"] = ev.StudentID
		}
		if h := hashAttendanceIdentity(ev.Email); h != "" {
			detail["email_hash"] = h
		}
		if h := hashAttendanceIdentity(ev.GoogleID); h != "" {
			detail["google_id_hash"] = h
		}
		if len(ev.FailedChecks) > 0 {
			detail["failed_checks"] = ev.FailedChecks
		}
		if host := strings.TrimSpace(ev.Host); host != "" {
			detail["host"] = host
		}
		// The raw proxy headers are the primary signal for detecting IP spoofing:
		// a client injecting its own X-Forwarded-For to fake a campus IP shows up
		// here as a header that disagrees with the resolved IP.
		if xff := strings.TrimSpace(ev.ForwardedFor); xff != "" {
			detail["x_forwarded_for"] = xff
		}
		if realIP := strings.TrimSpace(ev.RealIP); realIP != "" {
			detail["x_real_ip"] = realIP
		}
		sig := sanitizeClientSignals(ev.ClientSignals)
		if sig != nil {
			detail["client_signals"] = sig
		} else if ev.ClientSignalsExpected {
			// Recorded as its own key, deliberately NOT as a mismatch and never
			// as a warn: the web client always sends the object, but the PIN
			// endpoint is also reachable by clients that never will (the native
			// student app has no such payload). Absence is context for an
			// operator, not an accusation.
			detail["client_signals_missing"] = true
		}
		// Unlike CheckAndLogDeviceGuardFlip below, this runs on every single
		// request and needs no prior "block" to correlate against, so it is the
		// only signal here that can say anything about a spoofer who set up
		// emulation before their very first attempt. Read
		// clientSignalMismatchReasons for exactly how much that is worth: it
		// never blocks, and it never proves anything on its own.
		if reasons, strong := clientSignalMismatchReasons(deviceType, sig); len(reasons) > 0 {
			detail["client_signal_mismatch"] = true
			detail["client_signal_mismatch_reasons"] = reasons
			// Only a UA-client-hints contradiction is worth a warn. The weaker
			// trait heuristic stays at info so it cannot flood the security
			// dashboard with honest mouse-only tablets.
			if strong {
				severity = "warn"
			}
		}

		// ActorUserID is deliberately left nil even when the student is known.
		// It is a users.id everywhere else in SystemLog — GetLogByIDHandler and
		// the log listing both resolve it with repositories.FindUserByID — but
		// a check-in identifies a students.id, and students live in their own
		// table (see the comment at handlers/system_log_handler.go:44). Writing
		// one id into a column read as the other made the admin log viewer
		// attach whatever user happened to share that number to somebody else's
		// check-in. The student id lives in detail["student_id"] only.
		var statusCode *int
		if ev.StatusCode > 0 {
			sc := ev.StatusCode
			statusCode = &sc
		}

		record := models.SystemLog{
			LogType:      AttendanceCheckInLogType,
			Severity:     severity,
			Action:       AttendanceActionPrefix + ev.Result,
			HTTPMethod:   ev.Method,
			URL:          ev.URL,
			StatusCode:   statusCode,
			ErrorCode:    ev.FailCode,
			ResourceType: "attendance_session",
			ResourceID:   attendanceSessionResourceID(ev.SessionID),
			IPAddress:    ev.IP,
			UserAgent:    ev.UserAgent,
			DeviceType:   deviceType,
			Browser:      browser,
			OS:           osName,
			RequestID:    ev.RequestID,
		}
		return record, detail, true
	})
}

// CheckAndLogDeviceGuardFlip looks for a "device" campus-guard rejection from
// the same IP for the same session within the last `within` duration. Finding
// one — right after the guard has just let this same IP through — means the
// client failed the device check and then immediately passed it, the profile
// of someone opening DevTools device emulation (or a UA-switcher) and
// retrying, not of a student who genuinely walked off to fetch their phone.
// Call this only when the guard has just allowed a physical (non-exempt)
// check-in through. It never blocks anything — it only leaves a SystemLog
// entry for a TA/admin to review.
//
// Two known limitations, both accepted rather than solved here:
//
//  1. This only fires when a PRIOR block exists to correlate against. A
//     spoofer who sets up device emulation before their very first attempt on
//     this session/IP never gets blocked in the first place, so no "flip" is
//     ever observed and this function stays silent for that case. The
//     client_signal_mismatch flag in LogAttendanceCheckIn above is the signal
//     that covers that gap — it runs per-request and needs no prior block.
//  2. Campus Wi-Fi commonly NATs many students behind the same public IP, so
//     "same IP, same session, blocked then allowed within the window" can
//     legitimately be two different students, not one student flipping
//     state. studentID (when the caller can resolve it — it's only known
//     when the student is signed in via cookie/JWT, not on the anonymous
//     PIN+Google-Sign-In path) is used below to raise or lower confidence
//     rather than pretending IP alone is proof.
//
// Call it only after the check-in has actually succeeded. Running it on the
// way in flagged attempts the handler went on to reject for a wrong PIN or a
// closed session, which put an accusation in the log against a check-in that
// never happened.
func CheckAndLogDeviceGuardFlip(db *gorm.DB, sessionID uint, ip string, studentID uint, within time.Duration) {
	if db == nil || sessionID == 0 || strings.TrimSpace(ip) == "" {
		return
	}

	writeAttendanceSystemLog(auditProbePool, db, "audit: failed to write device guard flip flag", []any{"session_id", sessionID}, func(ctx context.Context) (models.SystemLog, map[string]any, bool) {
		var prior models.SystemLog
		// log_type and action are inlined as SQL literals here (see
		// attendanceDeviceFlipIndexPredicate) rather than bound as parameters,
		// because idx_system_logs_device_guard_flip in config/database.go is a
		// PARTIAL index keyed on exactly those two literals. Postgres uses a
		// partial index only when it can prove the query's WHERE implies the
		// index predicate, and it cannot prove that about a bound parameter
		// under a generic plan — which is what this query gets, since config.DB
		// runs with prepared statements on (EnablePreparedStatements).
		//
		// Do NOT "fix" that with db.Session(&gorm.Session{PrepareStmt: false}):
		// (*DB).Session in gorm.go puts its entire prepared-statement block
		// behind `if config.PrepareStmt`, so there is no code path that turns
		// them back off. Passing false is a silent no-op that leaves the
		// inherited *PreparedStmtDB connection pool in place.
		//
		// Take (not First) so GORM does not append its primary-key ORDER BY on
		// top of ours: the index already stores created_at DESC, and the extra
		// tiebreaker column would force a sort the index could otherwise skip.
		//
		// The action list covers the flip flag as well as the block, so this
		// single query does double duty: the newest of the two rows tells us
		// both whether there is a block to correlate against AND whether this
		// IP+session was already flagged inside the window. Without that, every
		// allowed check-in for the next `within` minutes wrote another
		// identical flag row — a public, unauthenticated endpoint amplifying
		// itself into the log table.
		err := db.WithContext(ctx).
			Model(&models.SystemLog{}).
			Where(attendanceDeviceFlipIndexPredicate).
			Where("resource_type = ?", "attendance_session").
			Where("resource_id = ?", attendanceSessionResourceID(sessionID)).
			Where("ip_address = ?", ip).
			Where("created_at >= ?", time.Now().Add(-within)).
			Order("created_at DESC").
			Take(&prior).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				slog.Error("audit: failed to query for device guard flip", "error", err)
			}
			return models.SystemLog{}, nil, false
		}

		// Already flagged within the window: say it once, not once per request.
		if prior.Action == attendanceDeviceFlipAction {
			return models.SystemLog{}, nil, false
		}
		// The most recent guard event on this IP and session must be a *device*
		// rejection. A network-only or domain-only block followed by a pass is
		// somebody moving onto campus Wi-Fi, not somebody changing what their
		// browser claims to be.
		priorDetail := decodeAttendanceLogDetail(prior.Detail)
		if !containsString(priorDetail.FailedChecks, "device") {
			return models.SystemLog{}, nil, false
		}

		priorStudent := priorDetail.StudentID

		// Both sides have a known, different identity: this is two different
		// students sharing a NAT'd campus IP, not one student flipping device
		// state. Not suspicious — don't flag it at all.
		if studentID > 0 && priorStudent > 0 && studentID != priorStudent {
			return models.SystemLog{}, nil, false
		}

		severity := "warn"
		confidence := "same_student"
		if studentID == 0 || priorStudent == 0 {
			severity = "info"
			confidence = "ip_only"
		}

		record := models.SystemLog{
			LogType: AttendanceCheckInLogType,
			// ActorUserID stays nil for the same reason as in
			// LogAttendanceCheckIn: it is a users.id everywhere else and this
			// is a students.id. It goes in detail["student_id"] instead.
			Severity:     severity,
			Action:       attendanceDeviceFlipAction,
			ResourceType: "attendance_session",
			ResourceID:   attendanceSessionResourceID(sessionID),
			IPAddress:    ip,
		}
		detail := map[string]any{
			"suspicious_device_flip": true,
			"prior_block_id":         prior.ID,
			"prior_block_at":         prior.CreatedAt,
			// "same_student" when both the blocked and allowed attempts are
			// tied to the same signed-in student — a confident flag. "ip_only"
			// when either side's identity is unknown (the common anonymous
			// PIN+Google-Sign-In path) — a weak signal a TA should treat with
			// skepticism given shared campus NAT, hence the lower severity.
			"correlation_confidence": confidence,
		}
		if studentID > 0 {
			detail["student_id"] = studentID
		} else if priorStudent > 0 {
			// The blocked attempt knew who it was and this one doesn't. Carry
			// the identity across rather than filing the flag as anonymous.
			detail["student_id"] = priorStudent
		}
		return record, detail, true
	})
}

// StudentIDFromContext reads the signed-in student's id (a students.id) out of
// the Fiber locals set by OptionalProtected/Protected, returning 0 for an
// anonymous request. It lives here, in the package both handlers and
// middlewares already import, because it previously existed twice: middlewares
// cannot import handlers, so the middleware kept its own copy and the two could
// drift apart on the type switch without anything failing to build.
func StudentIDFromContext(c fiber.Ctx) uint {
	raw := c.Locals("student_id")
	if raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case uint:
		return v
	case float64:
		return uint(v)
	default:
		return 0
	}
}

func attendanceSessionResourceID(sessionID uint) string {
	if sessionID == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(sessionID), 10)
}

// hashAttendanceIdentity returns a short, stable, non-reversible fingerprint of
// an email or Google ID so repeated attempts by the same account can be
// correlated in the logs without ever storing the plaintext identifier.
func hashAttendanceIdentity(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}
