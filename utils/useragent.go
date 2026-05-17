package utils

import ua "github.com/mileusna/useragent"

// ParseUserAgent parses a raw User-Agent string and returns
// (deviceType, browser, osName). deviceType is one of "mobile", "tablet",
// "bot", or "desktop". Returns empty strings when ua is blank.
func ParseUserAgent(uaStr string) (deviceType, browser, osName string) {
	if uaStr == "" {
		return "", "", ""
	}
	parsed := ua.Parse(uaStr)
	browser = parsed.Name
	osName = parsed.OS
	switch {
	case parsed.Bot:
		deviceType = "bot"
	case parsed.Mobile:
		deviceType = "mobile"
	case parsed.Tablet:
		deviceType = "tablet"
	default:
		deviceType = "desktop"
	}
	return
}
