package middlewares

import (
	"net/netip"
	"os"
	"strings"

	"github.com/gofiber/fiber/v3"
)

var defaultInternalMetricCIDRs = []string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
}

func RestrictMetricsToInternalNetworks() fiber.Handler {
	allowedPrefixes := parseAllowedMetricPrefixes(os.Getenv("METRICS_ALLOWED_CIDRS"))

	return func(c fiber.Ctx) error {
		ipText := strings.TrimSpace(c.IP())
		if ipText == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false, "message": "metrics access denied"})
		}

		ip, err := netip.ParseAddr(ipText)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false, "message": "metrics access denied"})
		}

		for _, prefix := range allowedPrefixes {
			if prefix.Contains(ip) {
				return c.Next()
			}
		}

		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false, "message": "metrics access denied"})
	}
}

func parseAllowedMetricPrefixes(raw string) []netip.Prefix {
	source := defaultInternalMetricCIDRs
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		source = strings.Split(trimmed, ",")
	}

	prefixes := make([]netip.Prefix, 0, len(source))
	for _, item := range source {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err == nil {
			prefixes = append(prefixes, prefix)
		}
	}

	return prefixes
}
