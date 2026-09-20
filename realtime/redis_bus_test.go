package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"itii-assist/config"

	"github.com/redis/go-redis/v9"
)

// registerAndJoinDefault puts a real client into defaultHub (the package
// singleton every EmitToRoom/resyncLocalRooms call targets — unlike the
// ticket tests, these can't substitute a fresh hub) and returns a cleanup
// that removes it again, so this test's state can't leak into another test
// sharing the same test binary.
func registerAndJoinDefault(t *testing.T, id string, room string) *client {
	t.Helper()
	c := newTestClient(defaultHub, id, 8)
	defaultHub.register(c)
	defaultHub.join(c, room)
	t.Cleanup(func() { defaultHub.unregister(c) })
	return c
}

func drainOne(t *testing.T, c *client, timeout time.Duration) outgoingMessage {
	t.Helper()
	select {
	case msg := <-c.send:
		return msg
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a message on the client's send channel")
		return outgoingMessage{}
	}
}

// A message published by ANOTHER replica (a different busOrigin) must be
// delivered to this replica's local clients in that room — this is the
// entire point of the bus (plan.md ระยะ 4.1).
func TestHandleBusMessage_DeliversRemoteOriginToLocalRoom(t *testing.T) {
	c := registerAndJoinDefault(t, "remote-delivery", "attendance-777")

	msg := busMessage{Origin: "some-other-replica", Room: "attendance-777", Event: "attendance-pin-updated", Data: map[string]any{"pin_issued": true}}
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	handleBusMessage(&redis.Message{Channel: realtimeEventsChannel, Payload: string(payload)})

	got := drainOne(t, c, time.Second)
	if got.Event != "attendance-pin-updated" {
		t.Fatalf("expected event %q, got %q", "attendance-pin-updated", got.Event)
	}
}

// A message carrying THIS process's own origin must be dropped, not
// delivered a second time — EmitToRoom already delivered it locally before
// ever publishing to the bus; without this check every event a replica
// emits would echo back to its own clients twice.
func TestHandleBusMessage_SkipsOwnOrigin(t *testing.T) {
	c := registerAndJoinDefault(t, "self-echo", "attendance-778")

	msg := busMessage{Origin: busOrigin, Room: "attendance-778", Event: "attendance-pin-updated", Data: nil}
	payload, _ := json.Marshal(msg)

	handleBusMessage(&redis.Message{Channel: realtimeEventsChannel, Payload: string(payload)})

	select {
	case got := <-c.send:
		t.Fatalf("expected no delivery for this process's own origin, got event %q", got.Event)
	case <-time.After(150 * time.Millisecond):
		// correct: nothing arrived
	}
}

// resyncLocalRooms (fired when this replica's bus subscription reconnects,
// meaning it may have missed events while disconnected) must reach every
// client in every room this replica currently holds — not just one room —
// since any of them could have missed something.
func TestResyncLocalRooms_ReachesEveryActiveRoom(t *testing.T) {
	a := registerAndJoinDefault(t, "resync-a", "attendance-901")
	b := registerAndJoinDefault(t, "resync-b", "instructor-901")

	resyncLocalRooms()

	gotA := drainOne(t, a, time.Second)
	gotB := drainOne(t, b, time.Second)
	if gotA.Event != "resync" || gotB.Event != "resync" {
		t.Fatalf("expected \"resync\" on both rooms' clients, got %q and %q", gotA.Event, gotB.Event)
	}
}

// publishRealtimeEvent must actually reach Redis with the right channel,
// origin and payload — the other half of the round trip
// TestHandleBusMessage_DeliversRemoteOriginToLocalRoom exercises from the
// receiving side.
func TestPublishRealtimeEvent_ReachesRedis(t *testing.T) {
	withTestRedis(t)

	ctx := context.Background()
	sub := config.Redis.Subscribe(ctx, realtimeEventsChannel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	publishRealtimeEvent("queue-55", "booking-updated", map[string]any{"status": "assigned"})

	select {
	case msg := <-sub.Channel():
		var decoded busMessage
		if err := json.Unmarshal([]byte(msg.Payload), &decoded); err != nil {
			t.Fatalf("failed to decode published payload: %v", err)
		}
		if decoded.Room != "queue-55" || decoded.Event != "booking-updated" || decoded.Origin != busOrigin {
			t.Fatalf("unexpected published message: %+v", decoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the published message on Redis")
	}
}
