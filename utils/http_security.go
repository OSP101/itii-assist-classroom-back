package utils

import (
	"net"
	"net/url"
	"strings"
)

func AllowedWebOrigins(raw, frontend string) []string {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "*" {
		raw = frontend
	}
	if strings.TrimSpace(raw) == "" {
		raw = "http://localhost:3000,http://127.0.0.1:3000"
	}
	origins := []string{}
	seen := map[string]bool{}
	for _, candidate := range strings.Split(raw, ",") {
		candidate = strings.TrimRight(strings.TrimSpace(candidate), "/")
		u, err := url.Parse(candidate)
		if err != nil || u.User != nil || u.Host == "" || strings.Contains(u.Host, "*") || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		if !seen[candidate] {
			origins = append(origins, candidate)
			seen[candidate] = true
		}
	}
	return origins
}

func TrustedProxyCIDRs(raw string) []string {
	proxies := []string{}
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if net.ParseIP(value) != nil {
			proxies = append(proxies, value)
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil && network != nil {
			if ones, _ := network.Mask.Size(); ones > 0 {
				proxies = append(proxies, value)
			}
		}
	}
	return proxies
}
