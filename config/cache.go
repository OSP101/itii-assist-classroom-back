package config

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// Redis-backed cache for read-heavy endpoints.
//
// Two rules govern everything here:
//
//  1. The cache is never allowed to break a request. Redis being down, slow, or
//     returning garbage is always treated as a miss, and the caller falls
//     through to the database exactly as it did before. Every function is
//     therefore best-effort and swallows its errors after logging.
//
//  2. Nothing user-specific may be cached under a shared key. The entries added
//     so far (course overview, classroom lists) are identical for every caller;
//     authorisation is enforced by middleware that still runs on every single
//     request, before any of this is consulted. Caching a response whose *body*
//     varies by role or user would leak data between accounts, so any such
//     endpoint must include the user in its key — or stay uncached.

const (
	// cacheOpTimeout keeps a struggling Redis from becoming the very latency it
	// was added to remove. Redis is a local, in-memory hop; anything slower than
	// this is not worth waiting for when Postgres is right there.
	cacheOpTimeout = 200 * time.Millisecond

	// cacheScanBatch is the COUNT hint for SCAN when deleting by prefix.
	cacheScanBatch = 200
)

// CacheAvailable reports whether a Redis client was constructed. Note that
// ConnectRedis keeps the client even when the initial ping fails, so this being
// true does not guarantee Redis is reachable — individual operations still have
// to tolerate failure.
func CacheAvailable() bool {
	return Redis != nil
}

// CacheGetJSON fills dest from the cached JSON at key, reporting whether a
// usable value was found. A decode failure is treated as a miss and the bad
// entry is dropped, so a stale value from an older payload shape can never wedge
// an endpoint until its TTL expires.
func CacheGetJSON(key string, dest any) bool {
	if !CacheAvailable() {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), cacheOpTimeout)
	defer cancel()

	raw, err := Redis.Get(ctx, key).Result()
	if err != nil || raw == "" {
		return false
	}

	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		log.Printf("⚠️  Cache decode failed for %s, dropping entry: %v", key, err)
		CacheDelete(key)
		return false
	}

	return true
}

// CacheSetJSON stores value as JSON under key. Failures are silent by design:
// a cache write that does not happen costs a future database query, which is
// the behaviour the system had before this existed.
func CacheSetJSON(key string, value any, ttl time.Duration) {
	if !CacheAvailable() || ttl <= 0 {
		return
	}

	payload, err := json.Marshal(value)
	if err != nil {
		log.Printf("⚠️  Cache encode failed for %s: %v", key, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cacheOpTimeout)
	defer cancel()

	if err := Redis.Set(ctx, key, payload, ttl).Err(); err != nil {
		log.Printf("⚠️  Cache write failed for %s: %v", key, err)
	}
}

// CacheDelete removes specific keys.
func CacheDelete(keys ...string) {
	if !CacheAvailable() || len(keys) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cacheOpTimeout)
	defer cancel()

	if err := Redis.Del(ctx, keys...).Err(); err != nil {
		log.Printf("⚠️  Cache delete failed for %v: %v", keys, err)
	}
}

// CacheDeletePrefix removes every key starting with prefix.
//
// Uses SCAN rather than KEYS on purpose: KEYS walks the entire keyspace in one
// blocking call, stalling every other Redis client for the duration — including
// the attendance PIN lookups on the check-in hot path. SCAN does the same work
// in interruptible batches.
//
// Deliberately synchronous: callers invalidate right after a write, and the
// following read must not be able to race ahead of the delete and re-cache the
// value that was just replaced.
func CacheDeletePrefix(prefix string) {
	if !CacheAvailable() || strings.TrimSpace(prefix) == "" {
		return
	}

	// Prefix scans can span many round trips, so this gets a larger budget than
	// a single-key operation.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		cursor  uint64
		batch   []string
		scanned int
	)

	for {
		keys, next, err := Redis.Scan(ctx, cursor, prefix+"*", cacheScanBatch).Result()
		if err != nil {
			log.Printf("⚠️  Cache scan failed for prefix %s: %v", prefix, err)
			return
		}

		batch = append(batch, keys...)
		scanned += len(keys)
		cursor = next

		// Delete as we go so a large keyspace does not accumulate in memory
		// here, and so a timeout partway through still removes most entries.
		if len(batch) >= cacheScanBatch {
			if err := Redis.Del(ctx, batch...).Err(); err != nil {
				log.Printf("⚠️  Cache prefix delete failed for %s: %v", prefix, err)
				return
			}
			batch = batch[:0]
		}

		if cursor == 0 {
			break
		}
	}

	if len(batch) > 0 {
		if err := Redis.Del(ctx, batch...).Err(); err != nil {
			log.Printf("⚠️  Cache prefix delete failed for %s: %v", prefix, err)
		}
	}
}
