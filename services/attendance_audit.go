package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"itii-assist/models"
	"itii-assist/utils"
	"log/slog"
	"strconv"
	"strings"

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
)

// AttendanceCheckInEvent is one forensic record of a check-in attempt. Email
// and Google ID are hashed before storage — no plaintext PII lands in the log
// table. StudentID stays in the clear on purpose: identifying which enrolled
// student a suspicious attempt belongs to is the whole point.
type AttendanceCheckInEvent struct {
	SessionID    uint
	StudentID    uint // 0 when identity could not be resolved
	Email        string
	GoogleID     string
	Result       string // one of the AttendanceResult* constants
	FailCode     string // machine code for the failure, e.g. ATTENDANCE_INVALID_PIN
	StatusCode   int
	FailedChecks []string // subset of "device","network","domain" from the campus guard
	IP           string   // resolved client IP (post-proxy, what the guard actually judged)
	RealIP       string   // raw X-Real-IP header, for spotting proxy/spoof mismatches
	ForwardedFor string   // raw X-Forwarded-For header, ditto
	Host         string
	UserAgent    string
	RequestID    string
	Method       string
	URL          string
}

// LogAttendanceCheckIn writes one check-in forensic record to SystemLog. It is
// safe to call from both handlers and middlewares (pass config.DB). The write
// happens in a background goroutine so it never adds latency to the check-in
// response, mirroring AuditLogger.LogSystem.
func LogAttendanceCheckIn(db *gorm.DB, ev AttendanceCheckInEvent) {
	if db == nil {
		return
	}

	go func() {
		severity := "info"
		switch ev.Result {
		case AttendanceResultNetworkBlocked, AttendanceResultRateLimited, AttendanceResultFailed:
			severity = "warn"
		}

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

		var detailJSON datatypes.JSON
		if len(detail) > 0 {
			if b, err := json.Marshal(detail); err == nil {
				detailJSON = datatypes.JSON(b)
			}
		}

		var actorID *uint
		if ev.StudentID > 0 {
			id := ev.StudentID
			actorID = &id
		}
		var statusCode *int
		if ev.StatusCode > 0 {
			sc := ev.StatusCode
			statusCode = &sc
		}

		deviceType, browser, osName := utils.ParseUserAgent(ev.UserAgent)

		record := models.SystemLog{
			LogType:      AttendanceCheckInLogType,
			Severity:     severity,
			ActorUserID:  actorID,
			Action:       "attendance.checkin." + ev.Result,
			HTTPMethod:   ev.Method,
			URL:          ev.URL,
			StatusCode:   statusCode,
			Detail:       detailJSON,
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

		if err := db.WithContext(context.Background()).Create(&record).Error; err != nil {
			slog.Error("audit: failed to write attendance check-in log", "error", err, "result", ev.Result)
		}
	}()
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
