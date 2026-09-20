package realtime

import (
	"testing"
	"time"

	"itii-assist/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func withTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	original := config.Redis
	config.Redis = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = config.Redis.Close()
		config.Redis = original
	})
	return server
}

// IssueSocketTicket/validateSocketTicket are package-level functions with no
// per-replica state of their own (plan.md ระยะ 4.1) — every existing test in
// this package runs with config.Redis == nil and only ever exercises the
// in-memory fallback map, which would keep passing even if the Redis-backed
// path were completely broken. This test is the one that actually proves
// the ticket round-trips through Redis: the whole reason for this change is
// that nginx's load balancing gives no guarantee the REST call that mints a
// ticket and the /ws upgrade that redeems it land on the same backend
// replica, so an in-memory-only ticket would silently never validate once
// there is more than one replica.
func TestSocketTicket_RoundTripsThroughRedis(t *testing.T) {
	server := withTestRedis(t)

	ticket, _, err := IssueSocketTicket("instructor-42", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	// Prove it actually landed in Redis, not just the in-memory fallback —
	// deleting the in-memory map entry (there shouldn't be one, but this
	// makes the assertion airtight regardless) before validating.
	if !server.Exists(socketTicketRedisKey(ticket)) {
		t.Fatal("ticket was not written to Redis at all — the in-memory fallback engaged even though Redis was available")
	}
	socketTickets.mu.Lock()
	delete(socketTickets.items, ticket)
	socketTickets.mu.Unlock()

	room, ok := validateSocketTicket(ticket)
	if !ok || room != "instructor-42" {
		t.Fatalf("expected ticket to validate to room \"instructor-42\", got room=%q ok=%v", room, ok)
	}
}

// A ticket must stay reusable within its TTL through the Redis-backed path
// too, matching TestTicketIsReusableWithinTTL's contract for the in-memory
// path — a client that reconnects inside the TTL re-sends the same ticket
// rather than fetching a new one.
func TestSocketTicket_RedisTicketReusableWithinTTL(t *testing.T) {
	withTestRedis(t)

	ticket, _, err := IssueSocketTicket("instructor-7", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	room1, ok1 := validateSocketTicket(ticket)
	room2, ok2 := validateSocketTicket(ticket)
	if !ok1 || !ok2 || room1 != "instructor-7" || room2 != "instructor-7" {
		t.Fatalf("ticket should validate repeatedly within its TTL via Redis: first=(%q,%v) second=(%q,%v)", room1, ok1, room2, ok2)
	}
}

// A ticket minted while Redis was down (landing in the in-memory fallback)
// must still validate on the SAME replica once Redis becomes available
// again — the read path must not assume "not in Redis" means "does not
// exist" and refuse it outright.
func TestSocketTicket_InMemoryFallbackStillValidatesAfterRedisReturns(t *testing.T) {
	// Issue while Redis is nil (forces the in-memory fallback).
	ticket, _, err := IssueSocketTicket("instructor-99", time.Minute)
	if err != nil {
		t.Fatalf("issuing a ticket failed: %v", err)
	}

	withTestRedis(t) // Redis "comes back" after the ticket was already minted

	room, ok := validateSocketTicket(ticket)
	if !ok || room != "instructor-99" {
		t.Fatalf("expected the in-memory-fallback ticket to still validate once Redis was available again, got room=%q ok=%v", room, ok)
	}
}
