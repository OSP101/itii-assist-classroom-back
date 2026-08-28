package utils

import (
	"net"
	"os"
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

// EvaluateCampusCheckIn decides whether a physical attendance check-in
// request may proceed, based on the request's Host header, User-Agent,
// resolved client IP, and the session's type. Online sessions are exempt
// from every check.
func EvaluateCampusCheckIn(host, userAgent, clientIP, sessionType string) CampusGuardResult {
	if strings.EqualFold(strings.TrimSpace(sessionType), "online") {
		return CampusGuardResult{Allowed: true, Exempt: true}
	}

	if !campusNetworkGuardEnabled() {
		return CampusGuardResult{Allowed: true}
	}

	deviceType, _, _ := ParseUserAgent(userAgent)

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
