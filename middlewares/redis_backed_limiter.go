package middlewares

import (
	"context"
	"errors"
	"itii-assist/config"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisIncrLimiter is the Redis-backed fixed-window counter desk_guard and
// support_guard share — the same INCR+EXPIRE+TTL shape already proven by
// attendance_guard_middleware.go's check-in limiter, pulled out here so
// those two guards don't each grow their own copy now that they need to
// stop being memory-only once there is more than one backend replica
// (plan.md ระยะ 4.1: a per-process map resets independently on every
// replica, silently multiplying the effective limit by the replica count).
//
// attendance_guard_middleware.go deliberately keeps its own already-tested
// Redis implementation rather than being migrated onto this shared helper
// — that file sits on the single hottest path in the app, and a pure
// dedup-motivated change there is not worth the regression risk.
//
// The third return value reports whether Redis was actually usable at all
// (available and reachable within the deadline); callers fall back to
// their in-memory limiter when it's false, exactly like
// allowAttendanceCheckIn already does.
//
// INCR and EXPIRE run as one Lua script rather than two separate round
// trips: a plain INCR-then-EXPIRE has a window where INCR succeeds, the
// connection drops before EXPIRE runs, and the key is left with no TTL —
// it then increments forever on every future call for that key and, once
// the count crosses limit, permanently rate-limits that principal until
// someone manually deletes it (caught in review, not yet in production).
// The script closes that window: both commands happen atomically inside
// Redis, so a network failure can only ever drop the whole call, never
// leave a half-applied key behind.
var incrLimiterScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return count
`)

func redisIncrLimiter(redisKeyPrefix string, key string, limit int, window time.Duration) (retryAfter int, allowed bool, ok bool) {
	if config.Redis == nil {
		return 0, false, false
	}
	if key == "" {
		key = "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	redisKey := redisKeyPrefix + key
	count, err := incrLimiterScript.Run(ctx, config.Redis, []string{redisKey}, int(window.Seconds())).Int64()
	if err != nil {
		return 0, false, false
	}

	if count <= int64(limit) {
		return 0, true, true
	}

	ttl, ttlErr := config.Redis.TTL(ctx, redisKey).Result()
	if ttlErr != nil || errors.Is(ttlErr, redis.Nil) {
		return 1, false, true
	}

	retryAfter = int(math.Ceil(ttl.Seconds()))
	if retryAfter < 1 {
		retryAfter = 1
	}
	return retryAfter, false, true
}
