package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Leader is a Redis-backed distributed lock with fencing, used to make sure
// exactly one backend instance runs a given periodic worker (PIN rotation,
// queue midnight sweep, daily backup, ...) when running multiple replicas
// (plan.md ระยะ 4.1). Without this, every replica's own ticker would fire
// the same work independently — harmless for read-only checks, but the PIN
// rotation worker writes a new PIN and broadcasts it, so N replicas would
// rotate N times and broadcast N conflicting "new PIN" events per tick.
//
// Deliberately NOT a fallback-to-"instance 0" scheme: with blue/green
// overlap during a deploy there can be two "first" instances at once
// (blue-0 and green-0), so a hardcoded designation is not actually unique.
// When Redis is unavailable, IsLeader() simply never returns true anywhere
// — the gated worker doesn't run on any replica until Redis comes back,
// which loses freshness (a rotating PIN sits stale) but never produces an
// ungoverned duplicate run. See the Redis-degradation table in plan.md.
//
// This lock reduces HOW OFTEN duplicate work happens; it is not by itself a
// hard guarantee against it (see the fencing note on renew() below) — any
// side-effecting worker gated on this must still be safe to run twice
// concurrently (e.g. rotate checks pin_rotates_at <= now() in its own
// UPDATE ... WHERE clause).
type Leader struct {
	name     string
	owner    string
	ttl      time.Duration
	isLeader atomic.Bool
	stop     chan struct{}
	stopped  chan struct{}
}

const leaderKeyPrefix = "leader:"

// releaseScript deletes the lock only if it still belongs to us — without
// this check, an instance releasing on shutdown after its lease already
// expired and was picked up by someone else would delete THEIR lock instead
// of a no-op.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`)

// renewScript is the fencing check: it only extends the TTL if the stored
// owner still matches ours. This is what makes a stale leader safe — an
// instance that paused (GC, CPU starvation) past its TTL and had the lock
// taken over by someone else fails this compare-and-extend on its next tick
// and demotes itself, instead of blindly extending a lease it no longer
// actually holds.
var renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
	return 0
end
`)

// StartLeaderElection begins a background goroutine that continuously tries
// to acquire/renew the named lock, so the returned Leader's IsLeader() is a
// cheap atomic read a ticking worker can call on every tick without itself
// talking to Redis. ttl should be comfortably longer than the renewal
// interval (renewal happens at ttl/3) and longer than one worker tick, so a
// single missed renewal cycle doesn't flap leadership.
//
// The first acquire attempt happens synchronously, before this returns —
// several callers do "start election, then immediately run once if leader"
// (e.g. a startup catch-up sweep), and without this they'd race their own
// election goroutine and always see IsLeader()==false on that first check
// regardless of whether they actually won, since the background goroutine
// wouldn't have run its first tick yet.
func StartLeaderElection(name string, ttl time.Duration) *Leader {
	if ttl < 3*time.Second {
		ttl = 3 * time.Second
	}
	ownerBytes := make([]byte, 16)
	_, _ = rand.Read(ownerBytes) // crypto/rand.Read never errors on a live OS
	l := &Leader{
		name:    name,
		owner:   hex.EncodeToString(ownerBytes),
		ttl:     ttl,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	l.tick()
	go l.run()
	return l
}

// IsLeader reports whether this instance currently holds the lock. Safe to
// call from any goroutine at any frequency — it never touches Redis itself.
func (l *Leader) IsLeader() bool {
	return l.isLeader.Load()
}

// Stop ends the background renewal loop and, if we were leader, releases
// the lock immediately rather than waiting for it to expire — this is what
// lets deploy-vps.sh's "stop the old slot" step hand leadership to the new
// slot within seconds instead of up to a full ttl later.
func (l *Leader) Stop() {
	close(l.stop)
	<-l.stopped
	if l.isLeader.Load() {
		l.release()
	}
}

func (l *Leader) key() string { return leaderKeyPrefix + l.name }

func (l *Leader) run() {
	defer close(l.stopped)
	interval := l.ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// The first tick already ran synchronously in StartLeaderElection.
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.tick()
		}
	}
}

func (l *Leader) tick() {
	if Redis == nil {
		if l.isLeader.CompareAndSwap(true, false) {
			log.Printf("leader: %s stepping down (Redis unavailable)", l.name)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if l.isLeader.Load() {
		n, err := renewScript.Run(ctx, Redis, []string{l.key()}, l.owner, l.ttl.Milliseconds()).Int()
		if err != nil || n == 0 {
			if l.isLeader.CompareAndSwap(true, false) {
				log.Printf("leader: %s lost leadership (renew failed, err=%v)", l.name, err)
			}
		}
		return
	}

	ok, err := Redis.SetNX(ctx, l.key(), l.owner, l.ttl).Result()
	if err != nil {
		return
	}
	if ok {
		l.isLeader.Store(true)
		log.Printf("leader: %s acquired (owner=%s...)", l.name, l.owner[:8])
	}
}

func (l *Leader) release() {
	if Redis == nil {
		l.isLeader.Store(false)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := releaseScript.Run(ctx, Redis, []string{l.key()}, l.owner).Result(); err != nil {
		log.Printf("leader: %s release failed (will expire on its own via TTL): %v", l.name, err)
	}
	l.isLeader.Store(false)
}
