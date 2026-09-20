package repositories

import (
	"encoding/json"
	"strings"
	"testing"

	"itii-assist/models"
)

// The idempotency cache's Redis wire format must round-trip Record (found in
// code review: AttendanceCheckInResult.Record is json:"-" because that type
// also doubles as StudentCheckInByPINHandler's direct public API response,
// but the idempotency cache used to marshal through that same tag and always
// lost Record on a cache hit — silently forcing the extra SELECT this field
// exists to avoid, on exactly the retry-storm traffic that matters most).
func TestAttendanceCheckInCachePayload_RoundTripsRecord(t *testing.T) {
	result := &AttendanceCheckInResult{
		Status:      "present",
		IsDuplicate: true,
		Record: &models.AttendanceRecord{
			ID:          42,
			StudentID:   7,
			Status:      "present",
			PinVerified: true,
		},
	}

	payload := attendanceCheckInCachePayload{AttendanceCheckInResult: *result, Record: result.Record}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// The cache wire format itself must actually carry the record — this is
	// the exact bug: previously nothing under "record" would ever appear.
	if !strings.Contains(string(raw), `"record"`) {
		t.Fatalf("cache payload dropped Record entirely: %s", raw)
	}

	var decoded attendanceCheckInCachePayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Record == nil {
		t.Fatal("Record was lost on the cache round trip — a client retry hitting this cache entry would wrongly fall back to the extra SELECT")
	}
	if decoded.Record.ID != 42 || decoded.Record.StudentID != 7 || !decoded.Record.PinVerified {
		t.Errorf("Record fields corrupted on round trip: got %+v", decoded.Record)
	}

	// The public-API type's own marshaling must still hide Record — this
	// guards the reason the field is json:"-" in the first place.
	publicRaw, err := json.Marshal(*result)
	if err != nil {
		t.Fatalf("marshal AttendanceCheckInResult failed: %v", err)
	}
	if strings.Contains(string(publicRaw), `"record"`) || strings.Contains(string(publicRaw), `"Record"`) {
		t.Errorf("AttendanceCheckInResult's own JSON (the public check-in API response) leaked Record: %s", publicRaw)
	}
}
