package handlers

import (
	"context"
	"encoding/json"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/services"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

// attendanceSecurityFlagLimit bounds one page of the review list. The panel is
// a review aid, not an export: an instructor who hits the cap has a session
// with bigger problems than pagination, and the response says so explicitly
// rather than silently truncating.
const attendanceSecurityFlagLimit = 100

type attendanceSecurityFlag struct {
	ID        uint      `json:"id"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"` // "device_flip" or "client_signal_mismatch"
	Severity  string    `json:"severity"`
	StudentID uint      `json:"student_id,omitempty"`
	// StudentCode is the human-facing รหัสนักศึกษา; StudentName the full name.
	// Both empty when the attempt could not be tied to an enrolled student,
	// which is the normal case on the anonymous PIN path.
	StudentCode string `json:"student_code,omitempty"`
	StudentName string `json:"student_name,omitempty"`

	IPAddress  string `json:"ip_address,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	Browser    string `json:"browser,omitempty"`
	OS         string `json:"os,omitempty"`

	// Reasons is the machine vocabulary from clientSignalMismatchReasons;
	// Confidence is "same_student" or "ip_only" for a device flip.
	Reasons    []string `json:"reasons,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
}

// securityFlagDetail is the part of a check-in log's jsonb detail this endpoint
// reads back.
type securityFlagDetail struct {
	StudentID              uint     `json:"student_id"`
	SuspiciousDeviceFlip   bool     `json:"suspicious_device_flip"`
	CorrelationConfidence  string   `json:"correlation_confidence"`
	ClientSignalMismatch   bool     `json:"client_signal_mismatch"`
	ClientSignalMismatchOf []string `json:"client_signal_mismatch_reasons"`
}

// GetAttendanceSessionSecurityFlagsHandler returns the anti-spoofing flags
// raised during one attendance session, scoped to the course the caller
// already has attendance-view rights on.
//
// This exists because the flags were previously written to system_logs and
// nowhere else: /api/logs is admin-only behind menu.logs, so the instructor and
// TA who are actually running the class — the only people who know whether a
// flagged check-in is a cheat or a student with a flaky phone — could not see
// them at all. A detection nobody with the context can read is not a detection.
//
// It reports the two heuristics and nothing more. Both are hints: read
// services.ClientDeviceSignals for exactly how weak they are, and never treat a
// row here as proof on its own.
func GetAttendanceSessionSecurityFlagsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || sessionID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "invalid session id"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var logs []models.SystemLog
	query := config.DB.WithContext(ctx).Model(&models.SystemLog{}).
		Where(services.AttendanceSessionLogsPredicate).
		Where("resource_type = ?", "attendance_session").
		Where("resource_id = ?", strconv.FormatUint(sessionID, 10)).
		Where(
			config.DB.Where("action = ?", services.AttendanceDeviceFlipAction).
				Or("detail ->> 'client_signal_mismatch' = ?", "true"),
		).
		Order("created_at DESC").
		// One extra row so the caller can be told the list was cut, instead of
		// being shown a full page that looks complete.
		Limit(attendanceSecurityFlagLimit + 1)

	if err := query.Find(&logs).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถโหลดรายการที่น่าสงสัยได้"})
	}

	truncated := len(logs) > attendanceSecurityFlagLimit
	if truncated {
		logs = logs[:attendanceSecurityFlagLimit]
	}

	flags := make([]attendanceSecurityFlag, 0, len(logs))
	studentIDs := make([]uint, 0, len(logs))
	for _, entry := range logs {
		detail := decodeSecurityFlagDetail(entry)

		kind := "client_signal_mismatch"
		if entry.Action == services.AttendanceDeviceFlipAction || detail.SuspiciousDeviceFlip {
			kind = "device_flip"
		}

		flag := attendanceSecurityFlag{
			ID:         entry.ID,
			At:         entry.CreatedAt,
			Kind:       kind,
			Severity:   entry.Severity,
			StudentID:  detail.StudentID,
			IPAddress:  entry.IPAddress,
			DeviceType: entry.DeviceType,
			Browser:    entry.Browser,
			OS:         entry.OS,
			Reasons:    detail.ClientSignalMismatchOf,
			Confidence: detail.CorrelationConfidence,
		}
		if detail.StudentID > 0 {
			studentIDs = append(studentIDs, detail.StudentID)
		}
		flags = append(flags, flag)
	}

	// Names come from `students`, never from SystemLog.ActorUserID: that column
	// is a users.id everywhere else in the table, and students live outside the
	// users table entirely.
	if len(studentIDs) > 0 {
		var students []models.Student
		if err := config.DB.WithContext(ctx).
			Select("id", "student_id", "full_name").
			Where("id IN ?", studentIDs).
			Find(&students).Error; err == nil {
			byID := make(map[uint]models.Student, len(students))
			for _, student := range students {
				byID[student.ID] = student
			}
			for i := range flags {
				if student, ok := byID[flags[i].StudentID]; ok {
					flags[i].StudentCode = student.StudentID
					flags[i].StudentName = student.FullName
				}
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session_id": sessionID,
			"flags":      flags,
			"truncated":  truncated,
		},
	})
}

func decodeSecurityFlagDetail(entry models.SystemLog) securityFlagDetail {
	var detail securityFlagDetail
	if len(entry.Detail) == 0 {
		return detail
	}
	if err := json.Unmarshal(entry.Detail, &detail); err != nil {
		return securityFlagDetail{}
	}
	return detail
}
