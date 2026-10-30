package utils

import "testing"

const (
	policyCampusHost = "cocolabs.computing.kku.ac.th"
	policyCampusIP   = "10.20.30.40"
	policyDesktopUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36"
	policyPhoneUA    = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Mobile Safari/537.36"
)

func TestCampusGuardPolicy(t *testing.T) {
	t.Setenv("ATTENDANCE_NETWORK_GUARD_ENABLED", "true")
	cases := []struct {
		name    string
		host    string
		ua      string
		ip      string
		policy  CampusGuardPolicy
		allowed bool
		failed  []string
	}{
		{"strict policy blocks a desktop", policyCampusHost, policyDesktopUA, policyCampusIP, CampusGuardPolicy{}, false, []string{"device"}},
		{"strict policy blocks mobile data", "cocolab.osp101.com", policyPhoneUA, "203.0.113.9", CampusGuardPolicy{}, false, []string{"domain", "network"}},
		{"desktop allowed but still on campus", policyCampusHost, policyDesktopUA, policyCampusIP, CampusGuardPolicy{AllowDesktop: true}, true, nil},
		{"desktop allowed off campus still blocked", "cocolab.osp101.com", policyDesktopUA, "203.0.113.9", CampusGuardPolicy{AllowDesktop: true}, false, []string{"domain", "network"}},
		{"any network allows a phone on mobile data", "cocolab.osp101.com", policyPhoneUA, "203.0.113.9", CampusGuardPolicy{AllowAnyNetwork: true}, true, nil},
		{"any network still requires a phone", "cocolab.osp101.com", policyDesktopUA, "203.0.113.9", CampusGuardPolicy{AllowAnyNetwork: true}, false, []string{"device"}},
		{"everything relaxed", "cocolab.osp101.com", policyDesktopUA, "203.0.113.9", CampusGuardPolicy{AllowDesktop: true, AllowAnyNetwork: true}, true, nil},
	}
	for _, tc := range cases {
		r := EvaluateCampusCheckIn(tc.host, tc.ua, tc.ip, "lecture", DeviceHints{}, tc.policy)
		if r.Allowed != tc.allowed {
			t.Errorf("%s: allowed=%v, want %v (failed %v)", tc.name, r.Allowed, tc.allowed, r.FailedChecks)
		}
		if len(r.FailedChecks) != len(tc.failed) {
			t.Errorf("%s: failed=%v, want %v", tc.name, r.FailedChecks, tc.failed)
			continue
		}
		for i := range tc.failed {
			if r.FailedChecks[i] != tc.failed[i] {
				t.Errorf("%s: failed=%v, want %v", tc.name, r.FailedChecks, tc.failed)
			}
		}
	}
}

func TestInsideKKUCampus(t *testing.T) {
	if !InsideKKUCampus(16.474912, 102.823561) {
		t.Error("College of Computing should be on campus")
	}
	if !InsideKKUCampus(17.8049854, 102.7475011) {
		t.Error("Nong Khai campus should be on campus")
	}
	if InsideKKUCampus(16.4322, 102.8250) {
		t.Error("a point in town should be off campus")
	}
	if InsideKKUCampus(13.7563, 100.5018) {
		t.Error("Bangkok should be off campus")
	}
}
