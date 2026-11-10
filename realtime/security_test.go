package realtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPrivateRoomsRejectBareIDsAndUnboundTickets(t *testing.T) {
	for _, event := range []string{"join-user", "join-user-courses", "join-booking", "join-classroom", "join-global-updates", "join-queue-staff"} {
		h := newTestHub()
		c := newTestClient(h, "attacker", 16)
		h.register(c)
		c.handleMessage(incomingMessage{Event: event, Data: json.RawMessage(`"1"`)})
		if len(c.rooms) != 0 {
			t.Fatalf("%s accepted a bare ID", event)
		}
		ticket, _, err := IssueSocketTicket("user-1", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"ticket": ticket})
		c.handleMessage(incomingMessage{Event: event, Data: raw})
		if len(c.rooms) != 0 {
			t.Fatalf("%s accepted an identity-free ticket", event)
		}
	}
}

func TestClientCannotForgeServerUpdates(t *testing.T) {
	h := newTestHub()
	attacker := newTestClient(h, "attacker", 8)
	victim := newTestClient(h, "victim", 8)
	h.register(attacker)
	h.register(victim)
	for _, room := range []string{"global-updates", "global-courses", "classroom-1"} {
		h.join(victim, room)
	}
	for _, event := range []string{"data-change", "course-change", "classroom-change"} {
		attacker.handleMessage(incomingMessage{Event: event, Data: json.RawMessage(`{"resource":"course","classroomId":"1","id":"1","action":"delete"}`)})
	}
	if len(victim.send) != 0 {
		t.Fatal("client-controlled update was relayed")
	}
}

func TestPublicQueueEventsDoNotDiscloseBookingOrReportData(t *testing.T) {
	input := map[string]interface{}{"booking": map[string]interface{}{"student_id": 123, "score": 99, "worker_note": "private"}, "snapshot": map[string]interface{}{"report": "confidential"}, "status": "paused", "session_id": "session"}
	encoded, err := json.Marshal(publicQueueEvent(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"student_id", "score", "worker_note", "private", "confidential", "snapshot"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public feed leaked %s", secret)
		}
	}
	if !strings.Contains(string(encoded), "paused") {
		t.Fatal("lost the public session status")
	}
}

func TestGlobalUpdatesPreserveCourseRoutingWithoutPrivateRows(t *testing.T) {
	got := publicUpdateData(map[string]interface{}{"course_id": "course-a", "score": 99, "student_id": 123})
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), "course-a") || strings.Contains(string(raw), "score") || strings.Contains(string(raw), "student_id") {
		t.Fatalf("unsafe/unusable invalidation: %s", raw)
	}
}

func TestSocketRoomLimitBoundsAnonymousSubscriptions(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "attacker", 8)
	h.register(c)
	for n := 0; n < 1000; n++ {
		c.handleMessage(incomingMessage{Event: "join-queue", Data: json.RawMessage(fmt.Sprintf(`"%d"`, n))})
	}
	if len(c.rooms) != 64 {
		t.Fatalf("expected 64 bounded rooms, got %d", len(c.rooms))
	}
}

func TestSocketOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://trusted.example")
	for _, tc := range []struct {
		origin string
		want   bool
	}{{"https://trusted.example", true}, {"https://same.example", true}, {"https://evil.example", false}, {"null", false}, {"", true}} {
		if got := socketOriginAllowed(tc.origin, "same.example"); got != tc.want {
			t.Errorf("origin %q: got %v", tc.origin, got)
		}
	}
}

func TestSocketConnectionLimitsAndRelease(t *testing.T) {
	t.Setenv("WEBSOCKET_MAX_CONNECTIONS", "2")
	t.Setenv("WEBSOCKET_MAX_CONNECTIONS_PER_IP", "1")
	releaseA, ok := reserveSocketConnection("test-a")
	if !ok {
		t.Fatal("first connection rejected")
	}
	defer releaseA()
	if _, ok := reserveSocketConnection("test-a"); ok {
		t.Fatal("per-IP cap bypassed")
	}
	releaseB, ok := reserveSocketConnection("test-b")
	if !ok {
		t.Fatal("second IP rejected")
	}
	defer releaseB()
	if _, ok := reserveSocketConnection("test-c"); ok {
		t.Fatal("global cap bypassed")
	}
	releaseA()
	releaseA()
	releaseC, ok := reserveSocketConnection("test-c")
	if !ok {
		t.Fatal("released capacity was not reusable")
	}
	releaseC()
}
