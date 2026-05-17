package services

import (
	"context"
	"encoding/json"
	"itii-assist/models"
	"itii-assist/utils"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Action constants
// ---------------------------------------------------------------------------

const (
	// Auth
	ActionAuthLoginSuccess = "auth.login.success"
	ActionAuthLoginFailed  = "auth.login.failed"
	ActionAuthLogout       = "auth.logout"
	ActionAuth2FAVerified  = "auth.2fa.verified"
	ActionAuthTokenRevoked = "auth.token.revoked"
	ActionAuthOAuthLinked  = "auth.oauth.linked"

	// Score
	ActionScoreUpdated             = "score.updated"
	ActionScoreEditRequestCreated  = "score.edit_request.created"
	ActionScoreEditRequestApproved = "score.edit_request.approved"
	ActionScoreEditRequestRejected = "score.edit_request.rejected"

	// Exam
	ActionExamScoreUpdated = "exam_score.updated"

	// Bonus
	ActionBonusScoreGiven   = "bonus_score.given"
	ActionBonusScoreDeleted = "bonus_score.deleted"

	// Course management
	ActionCourseTAAdded        = "course.ta.added"
	ActionCourseTARemoved      = "course.ta.removed"
	ActionCourseStudentRemoved = "course.student.removed"
	ActionCourseSectionCreated = "course.section.created"

	// Attendance
	ActionAttendanceSessionCreated = "attendance.session.created"
	ActionAttendanceRecordUpdated  = "attendance.record.updated"

	// Queue
	ActionQueueSessionOpened  = "queue.session.opened"
	ActionQueueSessionClosed  = "queue.session.closed"
	ActionQueueBookingCreated = "queue.booking.created"
	ActionQueueBookingCalled  = "queue.booking.called"

	// Admin
	ActionAdminUserDeactivated = "admin.user.deactivated"
	ActionAdminUserActivated   = "admin.user.activated"
	ActionAdminConfigChanged   = "admin.config.changed"
)

// ---------------------------------------------------------------------------
// AuditLogger
// ---------------------------------------------------------------------------

type AuditLogger struct {
	db *gorm.DB
}

func NewAuditLogger(db *gorm.DB) *AuditLogger {
	return &AuditLogger{db: db}
}

// ---------------------------------------------------------------------------
// SystemEvent / LogSystem
// ---------------------------------------------------------------------------

type SystemEvent struct {
	ActorUserID  uint
	Action       string
	LogType      string
	Severity     string
	ResourceType string
	ResourceID   string
	Detail       map[string]any
	IPAddress    string
	UserAgent    string
	RequestID    string
	TraceID      string
}

func (l *AuditLogger) LogSystem(ctx context.Context, e SystemEvent) {
	go func() {
		masked := maskSensitive(e.Detail)
		var detailJSON datatypes.JSON
		if len(masked) > 0 {
			b, err := json.Marshal(masked)
			if err != nil {
				slog.Error("audit: failed to marshal system log detail", "error", err, "action", e.Action)
				return
			}
			detailJSON = datatypes.JSON(b)
		}

		deviceType, browser, osName := utils.ParseUserAgent(e.UserAgent)
		actorID := e.ActorUserID
		record := models.SystemLog{
			LogType:      e.LogType,
			Severity:     e.Severity,
			ActorUserID:  &actorID,
			Action:       e.Action,
			ResourceType: e.ResourceType,
			ResourceID:   e.ResourceID,
			Detail:       detailJSON,
			IPAddress:    e.IPAddress,
			UserAgent:    e.UserAgent,
			DeviceType:   deviceType,
			Browser:      browser,
			OS:           osName,
			RequestID:    e.RequestID,
			TraceID:      e.TraceID,
		}

		if err := l.db.WithContext(context.Background()).Create(&record).Error; err != nil {
			slog.Error("audit: failed to write system log", "error", err, "action", e.Action)
		}
	}()
}

// ---------------------------------------------------------------------------
// CourseEvent / LogCourse
// ---------------------------------------------------------------------------

type CourseEvent struct {
	CourseID    string
	ActorUserID uint
	Action      string
	TargetType  string
	TargetID    string
	Description string
	RequestID   string
	IPAddress   string
}

func (l *AuditLogger) LogCourse(ctx context.Context, e CourseEvent) {
	go func() {
		record := models.CourseActivityLog{
			CourseID:    e.CourseID,
			ActorUserID: e.ActorUserID,
			Action:      e.Action,
			TargetType:  e.TargetType,
			TargetID:    e.TargetID,
			Description: e.Description,
			RequestID:   e.RequestID,
			IPAddress:   e.IPAddress,
		}

		if err := l.db.WithContext(context.Background()).Create(&record).Error; err != nil {
			slog.Error("audit: failed to write course log", "error", err, "action", e.Action)
		}
	}()
}

// ---------------------------------------------------------------------------
// maskSensitive
// ---------------------------------------------------------------------------

var sensitiveKeys = []string{"password", "token", "secret", "authorization", "api_key"}

func maskSensitive(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		lower := strings.ToLower(k)
		redact := false
		for _, s := range sensitiveKeys {
			if strings.Contains(lower, s) {
				redact = true
				break
			}
		}
		if redact {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// ExtractMeta
// ---------------------------------------------------------------------------

func ExtractMeta(c fiber.Ctx) (requestID, traceID, ip string) {
	if v, ok := c.Locals("requestID").(string); ok {
		requestID = v
	}
	if v, ok := c.Locals("traceID").(string); ok {
		traceID = v
	}
	ip = c.IP()
	return
}
