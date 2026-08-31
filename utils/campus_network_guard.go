package utils

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// CampusGuardResult is the outcome of evaluating whether a physical
// attendance check-in request is allowed to proceed: on campus Wi-Fi,
// through the canonical domain, from a mobile/tablet device.
type CampusGuardResult struct {
	Allowed      bool
	Exempt       bool     // true when the session is session_type == "online"
	FailedChecks []string // subset of "device", "network", "domain"
	DeviceType   string
}

var (
	_, campusAllowNet, _ = net.ParseCIDR("10.0.0.0/8")
	_, campusVPNNet, _   = net.ParseCIDR("10.48.0.0/16")
	_, campusLabNet, _   = net.ParseCIDR("10.199.0.0/16")
)

// DeviceHintsHeader carries the touch capabilities a User-Agent cannot express.
// Format: semicolon-separated key=value pairs, e.g. "touch=1;coarse=1;maxtouch=5".
const DeviceHintsHeader = "X-Client-Device-Hints"

// DeviceHints is the parsed DeviceHintsHeader. Absent or unparseable hints
// leave every field zero, which never widens the guard.
type DeviceHints struct {
	Touch          bool
	CoarsePointer  bool
	MaxTouchPoints int
}

// ParseDeviceHints reads DeviceHintsHeader. Unknown keys are ignored and a
// malformed header degrades to "no hints" rather than an error, because the
// only thing hints can do is relax the device check for one specific case.
func ParseDeviceHints(raw string) DeviceHints {
	hints := DeviceHints{}
	for _, part := range strings.Split(raw, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "touch":
			hints.Touch = value == "1" || strings.EqualFold(value, "true")
		case "coarse":
			hints.CoarsePointer = value == "1" || strings.EqualFold(value, "true")
		case "maxtouch":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				hints.MaxTouchPoints = n
			}
		}
	}
	return hints
}

// isTouchMac reports whether a request that parsed as a macOS desktop is really
// an iPad.
//
// Since iPadOS 13, Safari on iPad ships with "Request Desktop Website" on by
// default and sends a User-Agent byte-identical to Safari on a MacBook, so
// ParseUserAgent classifies every stock iPad as a desktop and the device check
// rejects it. Students on iPads could not check in at all, and because the
// verdict is reached while the page loads, nothing was even written to the
// audit log.
//
// The separator is that no Mac has ever shipped with a touchscreen: macOS
// Safari reports maxTouchPoints 0, no ontouchstart, and (pointer: coarse)
// false. A macOS User-Agent that also reports real multi-touch and a coarse
// pointer is therefore an iPad, not a laptop.
//
// The hints travel in a request header, so they are forgeable — but so is the
// User-Agent this whole check already rests on, and forging a mobile UA is
// strictly easier than forging both. The exception is deliberately narrow: it
// applies only to macOS, only upgrades to "tablet" (which the guard already
// allows), and every check-in still records client_signals for review.
func isTouchMac(deviceType, osName string, hints DeviceHints) bool {
	return deviceType == "desktop" &&
		strings.EqualFold(osName, "macOS") &&
		hints.Touch &&
		hints.CoarsePointer &&
		hints.MaxTouchPoints > 0
}

// EvaluateCampusCheckIn decides whether a physical attendance check-in
// request may proceed, based on the request's Host header, User-Agent,
// resolved client IP, the session's type, and the client's device hints.
// Online sessions are exempt from every check.
func EvaluateCampusCheckIn(host, userAgent, clientIP, sessionType string, hints DeviceHints) CampusGuardResult {
	if strings.EqualFold(strings.TrimSpace(sessionType), "online") {
		return CampusGuardResult{Allowed: true, Exempt: true}
	}

	if !campusNetworkGuardEnabled() {
		return CampusGuardResult{Allowed: true}
	}

	deviceType, _, osName := ParseUserAgent(userAgent)
	if isTouchMac(deviceType, osName, hints) {
		deviceType = "tablet"
	}

	failed := []string{}
	if deviceType != "mobile" && deviceType != "tablet" {
		failed = append(failed, "device")
	}
	if !isAllowedCampusHost(host) {
		failed = append(failed, "domain")
	}
	if !isCampusWifiIP(clientIP) {
		failed = append(failed, "network")
	}

	return CampusGuardResult{
		Allowed:      len(failed) == 0,
		FailedChecks: failed,
		DeviceType:   deviceType,
	}
}

// CampusNetworkGuardEnabled reports whether the campus guard is actually
// enforcing. Callers outside this file need it to decide how to behave when
// the guard cannot reach a verdict: failing a request closed is only correct
// while the guard is switched on, and skipping guard-only side work (the
// device-flip probe) is pointless when it can never fire.
func CampusNetworkGuardEnabled() bool {
	return campusNetworkGuardEnabled()
}

func campusNetworkGuardEnabled() bool {
	rawValue := strings.TrimSpace(os.Getenv("ATTENDANCE_NETWORK_GUARD_ENABLED"))
	if rawValue == "" {
		return true
	}
	switch strings.ToLower(rawValue) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func isAllowedCampusHost(host string) bool {
	allowedHost := strings.TrimSpace(os.Getenv("ATTENDANCE_NETWORK_GUARD_HOST"))
	if allowedHost == "" {
		allowedHost = "cocolabs.computing.kku.ac.th"
	}

	normalized := strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(normalized); err == nil {
		normalized = h
	}

	return normalized == strings.ToLower(allowedHost)
}

func isCampusWifiIP(clientIP string) bool {
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}

	if !campusAllowNet.Contains(ip) {
		return false
	}
	if campusVPNNet.Contains(ip) || campusLabNet.Contains(ip) {
		return false
	}
	return true
}
