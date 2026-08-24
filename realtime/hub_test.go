package realtime

import (
	"sync"
	"testing"
)

// newTestClient builds a client wired to the hub but without a real socket, so
// the registration/broadcast/unregister bookkeeping can be exercised on its own.
func newTestClient(h *hub, id string, buffer int) *client {
	return &client{
		id:    id,
		hub:   h,
		send:  make(chan outgoingMessage, buffer),
		rooms: make(map[string]struct{}),
		done:  make(chan struct{}),
	}
}

func newTestHub() *hub {
	return &hub{
		clients: make(map[*client]struct{}),
		rooms:   make(map[string]map[*client]struct{}),
	}
}

// The regression this guards: broadcast() collects its targets under RLock and
// then hands messages over after releasing it. When unregister() used to
// close(client.send), a disconnect landing in that window turned a broadcast
// into a send-on-closed-channel panic — fatal, because it happens on a
// WebSocket goroutine with no recover above it. Run with -race.
func TestBroadcastDuringConcurrentUnregisterDoesNotPanic(t *testing.T) {
	for round := 0; round < 200; round++ {
		h := newTestHub()

		clients := make([]*client, 0, 16)
		for i := 0; i < 16; i++ {
			c := newTestClient(h, "c", 1)
			h.register(c)
			h.join(c, "room-1")
			clients = append(clients, c)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				// Buffer is 1 and nothing drains it, so this also drives the
				// buffer-full path that spawns unregister goroutines.
				h.broadcast("room-1", "tick", map[string]int{"i": i}, nil)
			}
		}()

		go func() {
			defer wg.Done()
			for _, c := range clients {
				h.unregister(c)
			}
		}()

		wg.Wait()
	}
}

// unregister is reachable from three places at once (readPump's defer,
// writePump's failure path, and broadcast's buffer-full branch), so it has to
// stay idempotent — markDone uses sync.Once for exactly this.
func TestUnregisterIsIdempotent(t *testing.T) {
	h := newTestHub()
	c := newTestClient(h, "c", 1)
	h.register(c)
	h.join(c, "room-1")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.unregister(c)
		}()
	}
	wg.Wait()

	select {
	case <-c.done:
	default:
		t.Fatal("done channel was not closed by unregister")
	}

	if clients, rooms := len(h.clients), len(h.rooms); clients != 0 || rooms != 0 {
		t.Fatalf("hub not drained: clients=%d rooms=%d", clients, rooms)
	}
}

// A client already unregistered must not receive further broadcasts, otherwise
// a ghost entry would keep consuming buffer slots.
func TestBroadcastSkipsDoneClient(t *testing.T) {
	h := newTestHub()
	live := newTestClient(h, "live", 4)
	dead := newTestClient(h, "dead", 4)

	h.register(live)
	h.register(dead)
	h.join(live, "room-1")
	h.join(dead, "room-1")

	// Simulate the window where the client is done but not yet removed.
	dead.markDone()

	h.broadcast("room-1", "tick", nil, nil)

	if got := len(live.send); got != 1 {
		t.Fatalf("live client should have 1 queued message, got %d", got)
	}
	if got := len(dead.send); got != 0 {
		t.Fatalf("done client should receive nothing, got %d", got)
	}
}
