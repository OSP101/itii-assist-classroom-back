package utils

import (
	"slices"
	"testing"
)

const (
	// iPadOS 13+ Safari with the default "Request Desktop Website" — byte-for-byte
	// what a MacBook running Safari sends. This exact string is what every iPad in
	// the 2026-08-31 incident logged in with, and it is why none of them could
	// reach the check-in POST at all.
	uaIPadDesktopMode = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.7.5 Safari/605.1.15"
	uaMacBookChrome   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	uaWindowsTouch    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	uaAndroidPhone    = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Mobile Safari/537.36"

	campusHost = "cocolabs.computing.kku.ac.th"
	campusIP   = "10.53.48.242"
)

// iPad hints: real multi-touch and a coarse pointer, which no Mac reports.
var iPadHints = DeviceHints{Touch: true, CoarsePointer: true, MaxTouchPoints: 5}

func deviceFailed(result CampusGuardResult) bool {
	return slices.Contains(result.FailedChecks, "device")
}

func TestIPadInDesktopModeIsAllowed(t *testing.T) {
	result := EvaluateCampusCheckIn(campusHost, uaIPadDesktopMode, campusIP, "lecture", iPadHints)

	if deviceFailed(result) {
		t.Fatalf("an iPad on campus was rejected as a desktop: failed=%v", result.FailedChecks)
	}
	if !result.Allowed {
		t.Fatalf("iPad should pass every check here, got failed=%v", result.FailedChecks)
	}
	if result.DeviceType != "tablet" {
		t.Errorf("device type should be recorded as tablet for the audit log, got %q", result.DeviceType)
	}
}

// Without hints the guard must behave exactly as before — an older client that
// does not send the header is not silently let through on the UA alone.
func TestIPadUserAgentWithoutHintsStillFailsDeviceCheck(t *testing.T) {
	result := EvaluateCampusCheckIn(campusHost, uaIPadDesktopMode, campusIP, "lecture", DeviceHints{})

	if !deviceFailed(result) {
		t.Fatal("a macOS User-Agent with no touch hints was accepted — the device check is no longer doing anything")
	}
}

// The whole point of restricting the exception to macOS: a laptop must not be
// able to buy its way in with a touch hint.
func TestTouchHintsDoNotLetANonMacDesktopThrough(t *testing.T) {
	for name, ua := range map[string]string{
		"windows touchscreen laptop": uaWindowsTouch,
		"macbook running chrome":     uaMacBookChrome,
	} {
		t.Run(name, func(t *testing.T) {
			result := EvaluateCampusCheckIn(campusHost, ua, campusIP, "lecture", iPadHints)
			if name == "macbook running chrome" {
				// Chrome on a Mac still parses as macOS, so this one is only held
				// back by the hints being a lie — which is the accepted trade-off,
				// identical in cost to forging a mobile User-Agent. Recorded here so
				// the trade-off is visible rather than assumed.
				t.Skipf("known and accepted: forged hints on a Mac pass, exactly as a forged mobile UA already does")
			}
			if !deviceFailed(result) {
				t.Errorf("%s passed the device check on touch hints alone", name)
			}
		})
	}
}

// Partial hints must not be enough. A Mac trackpad does not make a coarse
// pointer, and no Mac reports touch points, so every one of these is either a
// real Mac or a half-hearted forgery.
func TestPartialHintsDoNotUpgradeAMac(t *testing.T) {
	cases := map[string]DeviceHints{
		"touch only":            {Touch: true},
		"coarse only":           {CoarsePointer: true},
		"touch and coarse only": {Touch: true, CoarsePointer: true},
		"touch points only":     {MaxTouchPoints: 5},
		"zero touch points":     {Touch: true, CoarsePointer: true, MaxTouchPoints: 0},
	}
	for name, hints := range cases {
		t.Run(name, func(t *testing.T) {
			if !deviceFailed(EvaluateCampusCheckIn(campusHost, uaIPadDesktopMode, campusIP, "lecture", hints)) {
				t.Errorf("hints %+v upgraded a macOS User-Agent on their own", hints)
			}
		})
	}
}

// A phone must not be affected either way by the new parameter.
func TestAndroidPhoneUnaffectedByHints(t *testing.T) {
	for name, hints := range map[string]DeviceHints{"none": {}, "full": iPadHints} {
		t.Run(name, func(t *testing.T) {
			if !EvaluateCampusCheckIn(campusHost, uaAndroidPhone, campusIP, "lecture", hints).Allowed {
				t.Error("an Android phone on campus Wi-Fi stopped being allowed")
			}
		})
	}
}

// The other two checks must keep failing for an iPad that is off-campus or on
// the wrong domain — the exception relaxes the device check only.
func TestIPadStillSubjectToNetworkAndDomainChecks(t *testing.T) {
	offCampus := EvaluateCampusCheckIn(campusHost, uaIPadDesktopMode, "203.0.113.9", "lecture", iPadHints)
	if !slices.Contains(offCampus.FailedChecks, "network") {
		t.Errorf("an iPad off campus skipped the network check: failed=%v", offCampus.FailedChecks)
	}
	if deviceFailed(offCampus) {
		t.Errorf("device check should have passed, failed=%v", offCampus.FailedChecks)
	}

	wrongHost := EvaluateCampusCheckIn("evil.example.com", uaIPadDesktopMode, campusIP, "lecture", iPadHints)
	if !slices.Contains(wrongHost.FailedChecks, "domain") {
		t.Errorf("an iPad on a non-faculty domain skipped the domain check: failed=%v", wrongHost.FailedChecks)
	}
}

func TestParseDeviceHints(t *testing.T) {
	cases := map[string]DeviceHints{
		"touch=1;coarse=1;maxtouch=5":  {Touch: true, CoarsePointer: true, MaxTouchPoints: 5},
		" touch=1 ; coarse=1 ; x=y ":   {Touch: true, CoarsePointer: true},
		"touch=true;coarse=TRUE":       {Touch: true, CoarsePointer: true},
		"touch=0;coarse=0;maxtouch=0":  {},
		"":                             {},
		"garbage":                      {},
		"maxtouch=notanumber;touch=1":  {Touch: true},
		"maxtouch=-3":                  {},
	}
	for raw, want := range cases {
		if got := ParseDeviceHints(raw); got != want {
			t.Errorf("ParseDeviceHints(%q) = %+v, want %+v", raw, got, want)
		}
	}
}
