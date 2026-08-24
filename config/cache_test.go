package config

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func withRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	server := miniredis.RunT(t)
	original := Redis
	Redis = redis.NewClient(&redis.Options{Addr: server.Addr()})

	t.Cleanup(func() {
		_ = Redis.Close()
		Redis = original
	})

	return server
}

func TestCacheRoundTrip(t *testing.T) {
	withRedis(t)

	CacheSetJSON("k1", sample{Name: "overview", Count: 3}, time.Minute)

	var got sample
	if !CacheGetJSON("k1", &got) {
		t.Fatal("expected a cache hit")
	}
	if got.Name != "overview" || got.Count != 3 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestCacheMissOnUnknownKey(t *testing.T) {
	withRedis(t)

	var got sample
	if CacheGetJSON("missing", &got) {
		t.Fatal("expected a miss for a key that was never written")
	}
}

// The whole design rests on this: with no Redis client the cache must behave as
// a permanent miss and never panic, so the request falls through to the
// database exactly as it did before caching existed.
func TestCacheDegradesWhenRedisIsAbsent(t *testing.T) {
	original := Redis
	Redis = nil
	defer func() { Redis = original }()

	if CacheAvailable() {
		t.Fatal("CacheAvailable must be false with no client")
	}

	// None of these may panic.
	CacheSetJSON("k", sample{Name: "x"}, time.Minute)
	CacheDelete("k")
	CacheDeletePrefix("prefix:")

	var got sample
	if CacheGetJSON("k", &got) {
		t.Fatal("expected a miss with no client")
	}
}

// A payload written by an older build whose struct shape has since changed must
// not wedge the endpoint until its TTL expires — it is dropped and reported as
// a miss so the caller recomputes.
func TestCacheDropsUndecodableEntry(t *testing.T) {
	server := withRedis(t)

	if err := server.Set("broken", "{not json"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var got sample
	if CacheGetJSON("broken", &got) {
		t.Fatal("a corrupt entry must read as a miss")
	}

	if server.Exists("broken") {
		t.Fatal("a corrupt entry must be deleted, not left to expire")
	}
}

func TestCacheSetJSONIgnoresNonPositiveTTL(t *testing.T) {
	server := withRedis(t)

	CacheSetJSON("zero-ttl", sample{Name: "x"}, 0)

	if server.Exists("zero-ttl") {
		t.Fatal("a zero TTL means caching is disabled, nothing should be written")
	}
}

func TestCacheDeletePrefixRemovesOnlyMatchingKeys(t *testing.T) {
	server := withRedis(t)

	// More than one SCAN batch, to exercise the cursor loop and the mid-loop
	// delete rather than just the trailing flush.
	for i := 0; i < 450; i++ {
		CacheSetJSON("cache:course:overview:"+string(rune('a'+i%26))+string(rune('a'+i/26)), sample{Count: i}, time.Minute)
	}
	CacheSetJSON("cache:classroom:list:keep", sample{Name: "keep"}, time.Minute)

	CacheDeletePrefix("cache:course:overview:")

	for _, key := range server.Keys() {
		if len(key) >= 22 && key[:22] == "cache:course:overview:" {
			t.Fatalf("prefix delete left %s behind", key)
		}
	}

	if !server.Exists("cache:classroom:list:keep") {
		t.Fatal("prefix delete removed a key outside the prefix")
	}
}

func TestCacheDeleteRemovesNamedKeys(t *testing.T) {
	server := withRedis(t)

	CacheSetJSON("a", sample{Count: 1}, time.Minute)
	CacheSetJSON("b", sample{Count: 2}, time.Minute)

	CacheDelete("a")

	if server.Exists("a") {
		t.Fatal("key a should be gone")
	}
	if !server.Exists("b") {
		t.Fatal("key b must be untouched")
	}
}
