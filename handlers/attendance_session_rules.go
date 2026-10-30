package handlers

import (
	"fmt"
	"strings"
	"time"

	"itii-assist/models"
	"itii-assist/utils"
)

// Limits for a new attendance session. The create modal enforces the same
// numbers before it ever calls the API (lib/attendance/session-rules.ts); the
// server repeats the blocking ones so an old client, a script or a stale tab
// cannot save a session nobody could check in to.
const (
	attendanceMinOpenMinutes = 5
	attendanceMaxAheadDays   = 180
	attendanceMinRadius      = 10
	attendanceMaxRadius      = 1000
)

// attendanceSessionSettings is the slice of a session the rules look at.
type attendanceSessionSettings struct {
	SessionType          string
	Start, End, Late     time.Time
	CheckLocation        bool
	LocationLat          *float64
	LocationLng          *float64
	RadiusMeters         int
	RequireCampusNetwork bool
	RequireMobile        bool
}

// attendanceSecurityOf picks the fields validateAttendanceSecurity judges.
func attendanceSecurityOf(s models.AttendanceSession) attendanceSessionSettings {
	return attendanceSessionSettings{
		SessionType:          s.SessionType,
		CheckLocation:        s.CheckLocation,
		LocationLat:          s.LocationLat,
		LocationLng:          s.LocationLng,
		RadiusMeters:         s.RadiusMeters,
		RequireCampusNetwork: s.CampusNetworkRequired(),
		RequireMobile:        s.MobileRequired(),
	}
}

func sameFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// sameAttendanceSecurity reports whether an update left the security settings as they were.
func sameAttendanceSecurity(a, b attendanceSessionSettings) bool {
	return strings.EqualFold(a.SessionType, b.SessionType) && a.CheckLocation == b.CheckLocation &&
		sameFloatPtr(a.LocationLat, b.LocationLat) && sameFloatPtr(a.LocationLng, b.LocationLng) &&
		(!a.CheckLocation || a.RadiusMeters == b.RadiusMeters) && a.RequireCampusNetwork == b.RequireCampusNetwork &&
		a.RequireMobile == b.RequireMobile
}

// attendanceSettingsProblem is one reason a session cannot be saved.
type attendanceSettingsProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// validateNewAttendanceTimes holds the time rules that only make sense when a
// session is created: an edited session may already be running or closed.
func validateNewAttendanceTimes(s attendanceSessionSettings, now time.Time) []attendanceSettingsProblem {
	var out []attendanceSettingsProblem
	add := func(code, msg string) { out = append(out, attendanceSettingsProblem{code, msg}) }

	if !s.End.After(s.Start) {
		add("end_before_start", "เวลาปิดรับต้องอยู่หลังเวลาเปิดรับ")
	} else if s.End.Sub(s.Start) < attendanceMinOpenMinutes*time.Minute {
		add("too_short", fmt.Sprintf("ต้องเปิดรับอย่างน้อย %d นาที", attendanceMinOpenMinutes))
	}
	if s.Late.Before(s.Start) || s.Late.After(s.End) {
		add("late_out_of_range", "เวลานับสายต้องอยู่ระหว่างเวลาเปิดรับกับเวลาปิดรับ")
	}
	if s.End.After(s.Start) && !s.End.After(now) {
		add("already_ended", "ช่วงเวลานี้ปิดรับไปแล้ว สร้างรอบย้อนหลังไม่ได้")
	}
	if s.Start.After(now.AddDate(0, 0, attendanceMaxAheadDays)) {
		add("too_far_ahead", fmt.Sprintf("วันที่ห่างจากวันนี้เกิน %d วัน", attendanceMaxAheadDays))
	}
	return out
}

// validateAttendanceSecurity holds the rules that keep a session's checks from
// contradicting each other. They apply on create and on every edit.
func validateAttendanceSecurity(s attendanceSessionSettings) []attendanceSettingsProblem {
	var out []attendanceSettingsProblem
	add := func(code, msg string) { out = append(out, attendanceSettingsProblem{code, msg}) }

	online := strings.EqualFold(strings.TrimSpace(s.SessionType), "online")
	if online && s.RequireCampusNetwork {
		add("online_campus_network", "รอบออนไลน์จะบังคับเครือข่าย มข. ไม่ได้")
	}
	if online && s.CheckLocation {
		add("online_gps", "รอบออนไลน์จะเปิดตรวจ GPS ไม่ได้")
	}
	if !s.CheckLocation {
		return out
	}
	if s.LocationLat == nil || s.LocationLng == nil {
		add("gps_without_pin", "เปิดตรวจ GPS แต่ยังไม่ได้ปักหมุด")
		return out
	}
	if s.RadiusMeters < attendanceMinRadius || s.RadiusMeters > attendanceMaxRadius {
		add("radius_out_of_range", fmt.Sprintf("รัศมีต้องอยู่ระหว่าง %d ถึง %d เมตร", attendanceMinRadius, attendanceMaxRadius))
	}
	if s.RequireCampusNetwork && !utils.InsideKKUCampus(*s.LocationLat, *s.LocationLng) {
		add("campus_network_pin_off_campus", "บังคับใช้เครือข่าย มข. แต่หมุด GPS อยู่นอกมหาวิทยาลัย นักศึกษาจะเช็กชื่อไม่ได้")
	}
	return out
}

func attendanceSettingsResponse(problems []attendanceSettingsProblem) map[string]interface{} {
	messages := make([]string, 0, len(problems))
	for _, p := range problems {
		messages = append(messages, p.Message)
	}
	return map[string]interface{}{
		"success":  false,
		"code":     "ATTENDANCE_SETTINGS_INVALID",
		"message":  strings.Join(messages, " / "),
		"problems": problems,
	}
}

func boolValue(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}
