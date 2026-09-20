package middlewares

import (
	"testing"
	"time"

	"itii-assist/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func withLimiterTestRedis(t *testing.T) *miniredis.Miniredis {
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

// The desk/queue-status/support guards share this limiter specifically so
// their counters live in Redis instead of per-process memory once there is
// more than one backend replica (plan.md ระยะ 4.1) — a memory-only limiter
// would let each replica independently allow up to `limit` requests,
// multiplying the effective limit by the replica count. This proves the
// counter is actually shared: two "requests" issued through two logically
// separate calls (standing in for two replicas, since both just talk to
// the same Redis) must count against the same total.
func TestRedisIncrLimiter_SharedAcrossCallers(t *testing.T) {
	withLimiterTestRedis(t)

	const prefix = "test:limiter:"
	const key = "desk:1|1.2.3.4"
	limit := 3
	window := time.Minute

	var lastAllowed bool
	for i := 0; i < limit; i++ {
		_, allowed, ok := redisIncrLimiter(prefix, key, limit, window)
		if !ok {
			t.Fatalf("attempt %d: redisIncrLimiter reported Redis unusable", i)
		}
		if !allowed {
			t.Fatalf("attempt %d: expected allowed within the configured limit of %d", i, limit)
		}
		lastAllowed = allowed
	}
	if !lastAllowed {
		t.Fatal("sanity check failed")
	}

	retryAfter, allowed, ok := redisIncrLimiter(prefix, key, limit, window)
	if !ok {
		t.Fatal("redisIncrLimiter reported Redis unusable on the over-limit attempt")
	}
	if allowed {
		t.Fatalf("expected the (limit+1)th attempt to be rejected, limit=%d", limit)
	}
	if retryAfter < 1 {
		t.Fatalf("expected a positive Retry-After once over limit, got %d", retryAfter)
	}
}

// A different key (a different desk, a different IP) must get its own
// independent bucket — the whole point of scoping by key.
func TestRedisIncrLimiter_DistinctKeysDoNotShareBuckets(t *testing.T) {
	withLimiterTestRedis(t)

	const prefix = "test:limiter:"
	limit := 1
	window := time.Minute

	_, allowedA1, _ := redisIncrLimiter(prefix, "key-a", limit, window)
	_, allowedA2, _ := redisIncrLimiter(prefix, "key-a", limit, window)
	_, allowedB1, _ := redisIncrLimiter(prefix, "key-b", limit, window)

	if !allowedA1 || allowedA2 {
		t.Fatalf("key-a: expected first=true second=false, got first=%v second=%v", allowedA1, allowedA2)
	}
	if !allowedB1 {
		t.Fatal("key-b's first request should be allowed independently of key-a's bucket")
	}
}

// Without Redis configured, redisIncrLimiter must report itself unusable
// (ok=false) rather than silently allowing or denying — the caller is what
// decides fail-open (attendance_guard_middleware.go, desk_guard,
// support_guard all fall back to their in-memory limiter, never fail
// fail-closed on the whole class).
func TestRedisIncrLimiter_UnusableWithoutRedis(t *testing.T) {
	original := config.Redis
	config.Redis = nil
	defer func() { config.Redis = original }()

	_, _, ok := redisIncrLimiter("test:limiter:", "any-key", 5, time.Minute)
	if ok {
		t.Fatal("expected ok=false with no Redis configured")
	}
}
