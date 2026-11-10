package utils

import "testing"

func TestAllowedWebOriginsNeverExpandsWildcard(t *testing.T) {
	for _, raw := range []string{"", "*", "https://trusted.example,*,null,https://*.example"} {
		got := AllowedWebOrigins(raw, "https://trusted.example")
		for _, origin := range got {
			if origin != "https://trusted.example" {
				t.Fatalf("unexpected allowed origin: %q", origin)
			}
		}
	}
}

func TestAllowedWebOriginsRejectsCredentialsPathsAndInvalidSchemes(t *testing.T) {
	got := AllowedWebOrigins("https://user:pass@example.com,https://example.com/path,file:///tmp,a,https://safe.example/", "")
	if len(got) != 1 || got[0] != "https://safe.example" {
		t.Fatalf("bad origins: %v", got)
	}
}

func TestTrustedProxyCIDRsDoNotTrustAllPrivateNetworks(t *testing.T) {
	got := TrustedProxyCIDRs("0.0.0.0/0,::/0,invalid,172.30.248.2/32,127.0.0.1")
	if len(got) != 2 || got[0] != "172.30.248.2/32" {
		t.Fatalf("bad proxy trust: %v", got)
	}
}
