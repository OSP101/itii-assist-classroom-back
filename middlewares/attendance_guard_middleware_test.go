package middlewares

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// captureAttendanceKeyApp builds a minimal app that runs the given
// pre-middleware (to set up Locals the way OptionalProtected would after
// verifying a JWT, or nothing at all for an anonymous caller), then a
// handler that writes whatever key-building function under test computes
// straight into the response body.
func captureAttendanceKeyApp(setup fiber.Handler, extract func(c fiber.Ctx) string) *fiber.App {
	app := fiber.New()
	final := func(c fiber.Ctx) error {
		return c.SendString(extract(c))
	}
	if setup != nil {
		app.Post("/api/attendance/check-in/:sessionId", setup, final)
		app.Post("/api/attendance/check-in", setup, final)
		app.Post("/api/attendance/verify-pin", setup, final)
	} else {
		app.Post("/api/attendance/check-in/:sessionId", final)
		app.Post("/api/attendance/check-in", final)
		app.Post("/api/attendance/verify-pin", final)
	}
	return app
}

func doJSON(t *testing.T, app *fiber.App, path, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read response body failed: %v", err)
	}
	return buf.String()
}

// attendanceClientKey (and every guard built on attendancePrincipalKey) used
// to trust whatever identity field the request body happened to carry —
// student_id, google_email, google_id, then client_request_id — none of
// which are verified at this point in the middleware chain. A caller who
// wanted more requests than the configured limit just had to send a
// different fake value on each request to land in a fresh bucket every
// time, so the limit never actually engaged (found during plan.md ระยะ 2
// review). This test drives a batch of requests that vary ONLY those body
// fields, same session and same IP throughout, and asserts every one
// resolves to the exact same key — which is what makes the limiter actually
// count them together instead of treating each as a new caller.
func TestAttendanceClientKey_IgnoresUnverifiedBodyIdentityFields(t *testing.T) {
	app := captureAttendanceKeyApp(nil, attendanceClientKey)

	bodies := []string{
		`{"student_id": 1, "pin_code": "111111"}`,
		`{"student_id": 999999, "pin_code": "111111"}`,
		`{"google_email": "attacker1@example.com", "pin_code": "111111"}`,
		`{"google_email": "attacker2@example.com", "pin_code": "111111"}`,
		`{"google_id": "some-random-google-id", "pin_code": "111111"}`,
		`{"client_request_id": "11111111-1111-1111-1111-111111111111", "pin_code": "111111"}`,
		`{"client_request_id": "22222222-2222-2222-2222-222222222222", "pin_code": "111111"}`,
		`{"pin_code": "111111"}`,
	}

	var keys []string
	for _, body := range bodies {
		keys = append(keys, doJSON(t, app, "/api/attendance/check-in/42", body))
	}

	for i, key := range keys {
		if key == "" {
			t.Fatalf("request %d: got an empty key", i)
		}
		if key != keys[0] {
			t.Errorf("request %d: key %q differs from request 0's key %q — an attacker varying only body identity fields can still evade the rate limit by requesting a fresh bucket every time", i, key, keys[0])
		}
	}
}

// Once a caller IS authenticated (the only case attendancePrincipalKey
// trusts an identity claim at all), the key must reflect that authenticated
// identity — not fall through to IP, and not be overridable by the body.
func TestAttendanceClientKey_UsesAuthenticatedStudentIDOverBody(t *testing.T) {
	asStudent7 := func(c fiber.Ctx) error {
		c.Locals("student_id", uint(7))
		return c.Next()
	}
	app := captureAttendanceKeyApp(asStudent7, attendanceClientKey)

	keyClaimingSomeoneElse := doJSON(t, app, "/api/attendance/check-in/42", `{"student_id": 999999, "pin_code": "111111"}`)
	keyClaimingNothing := doJSON(t, app, "/api/attendance/check-in/42", `{"pin_code": "111111"}`)

	if keyClaimingSomeoneElse != keyClaimingNothing {
		t.Errorf("authenticated key changed based on the body's own (unverified) student_id claim: %q vs %q", keyClaimingSomeoneElse, keyClaimingNothing)
	}
	if !bytes.Contains([]byte(keyClaimingSomeoneElse), []byte("student:7")) {
		t.Errorf("expected the authenticated student id (7) in the key, got %q", keyClaimingSomeoneElse)
	}
	if bytes.Contains([]byte(keyClaimingSomeoneElse), []byte("999999")) {
		t.Errorf("key leaked the body's spoofed student_id (999999) instead of using the authenticated one: %q", keyClaimingSomeoneElse)
	}
}

// AttendancePinLookupRateLimit's key must not vary with the PIN being
// guessed — attendanceSessionScope hashes pin_code from the body when no
// :sessionId route param exists (true for /verify-pin), so reusing it here
// would let a brute-forcer get a fresh bucket on every guess, the exact bug
// TestAttendanceClientKey_IgnoresUnverifiedBodyIdentityFields guards against
// for the check-in routes. attendancePrincipalKey alone must stay stable
// across different PIN guesses from the same caller.
func TestAttendancePrincipalKey_StableAcrossDifferentPinGuesses(t *testing.T) {
	app := captureAttendanceKeyApp(nil, attendancePrincipalKey)

	keyA := doJSON(t, app, "/api/attendance/verify-pin", `{"pin_code": "111111"}`)
	keyB := doJSON(t, app, "/api/attendance/verify-pin", `{"pin_code": "222222"}`)
	keyC := doJSON(t, app, "/api/attendance/verify-pin", `{"pin_code": "333333"}`)

	if keyA != keyB || keyB != keyC {
		t.Errorf("attendancePrincipalKey varied across PIN guesses from the same caller: %q, %q, %q — a brute-forcer would get a fresh rate-limit bucket per guess", keyA, keyB, keyC)
	}
}
