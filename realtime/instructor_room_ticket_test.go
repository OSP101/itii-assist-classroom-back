package realtime

import (
	"encoding/json"
	"testing"
	"time"
)

// joinInstructor drives handleMessage the way a real websocket frame would.
func joinInstructor(c *client, data string) {
	c.handleMessage(incomingMessage{Event: "join-instructor", Data: json.RawMessage(data)})
}

func inRoom(h *hub, room string, c *client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.rooms[room][c]
	return ok
}

// The instructor room streams the live attendance PIN and every check-in record
// as it lands (student name, email, Google id). It used to be joinable by
// anyone who could guess a session id, which made both readable from outside
// the classroom. Joining must now cost a ticket that is only minted behind the
// authenticated /api/attendance/sessions/:id/socket-ticket route.
func TestJoinInstructorRejectsClientWithoutTicket(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "anon", 4)
	h.register(c)

	// The old client shape: a bare session id, no ticket anywhere.
	joinInstructor(c, `5`)
	if inRoom(h, "instructor-5", c) {
		t.Fatal("a client with no ticket joined the instructor room")
	}

	joinInstructor(c, `{"ticket":""}`)
	if inRoom(h, "instructor-5", c) {
		t.Fatal("an empty ticket joined the instructor room")
	}

	joinInstructor(c, `{"ticket":"deadbeef-not-a-real-ticket"}`)
	if inRoom(h, "instructor-5", c) {
		t.Fatal("a forged ticket joined the instructor room")
	}
}

func TestJoinInstructorAcceptsIssuedTicket(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "instructor", 4)
	h.register(c)

	ticket, _, err := IssueSocketTicket("instructor-5", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{"ticket": ticket})
	joinInstructor(c, string(payload))

	if !inRoom(h, "instructor-5", c) {
		t.Fatal("a validly ticketed instructor was locked out of their own live view")
	}
}

// A ticket names exactly one room, so one minted for a classroom display must
// not open the instructor room even though both now share a ticket store.
func TestInstructorRoomRejectsDisplayTicket(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "display", 4)
	h.register(c)

	ticket, _, err := IssueSocketTicket("display-attendance-5", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{"ticket": ticket})
	joinInstructor(c, string(payload))

	if inRoom(h, "instructor-5", c) {
		t.Fatal("a display ticket was replayed into the instructor room")
	}
	if inRoom(h, "display-attendance-5", c) {
		t.Fatal("join-instructor put the client in the display room named by the ticket")
	}
}

// An expired ticket is as good as no ticket.
func TestExpiredTicketIsRejected(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "stale", 4)
	h.register(c)

	ticket, _, err := IssueSocketTicket("instructor-5", -time.Second)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{"ticket": ticket})
	joinInstructor(c, string(payload))

	if inRoom(h, "instructor-5", c) {
		t.Fatal("an expired ticket still opened the instructor room")
	}
}

// A ticket is spent against one room only; reusing it must not widen access,
// but it must stay usable across a reconnect within its TTL.
func TestTicketIsReusableWithinTTL(t *testing.T) {
	h := newTestHub()
	ticket, _, err := IssueSocketTicket("instructor-9", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"ticket": ticket})

	first := newTestClient(h, "first", 4)
	h.register(first)
	joinInstructor(first, string(payload))
	h.unregister(first)

	reconnected := newTestClient(h, "reconnected", 4)
	h.register(reconnected)
	joinInstructor(reconnected, string(payload))

	if !inRoom(h, "instructor-9", reconnected) {
		t.Fatal("a reconnect inside the ticket TTL was locked out")
	}
}
