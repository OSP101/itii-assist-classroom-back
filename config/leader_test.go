package config

import (
	"context"
	"testing"
	"time"
)

// waitFor polls cond every 20ms until it's true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// Exactly one of several competing instances must become leader — this is
// the whole point of the lock (plan.md ระยะ 4.1: N replicas must not all
// run the same PIN-rotation tick).
func TestLeaderElection_OnlyOneWinner(t *testing.T) {
	withRedis(t)

	const name = "test-worker"
	const n = 5
	leaders := make([]*Leader, n)
	for i := range leaders {
		leaders[i] = StartLeaderElection(name, 500*time.Millisecond)
	}
	defer func() {
		for _, l := range leaders {
			l.Stop()
		}
	}()

	if !waitFor(t, 2*time.Second, func() bool {
		count := 0
		for _, l := range leaders {
			if l.IsLeader() {
				count++
			}
		}
		return count == 1
	}) {
		count := 0
		for _, l := range leaders {
			if l.IsLeader() {
				count++
			}
		}
		t.Fatalf("expected exactly 1 leader among %d competing instances, got %d", n, count)
	}
}

// Stop() must release the lock immediately (not wait out the TTL), so a
// deploy's "stop the old slot" step hands leadership to the new slot within
// seconds — this is what deploy-vps.sh relies on to avoid a gap where no
// instance runs the gated workers for a whole TTL after a routine deploy.
func TestLeaderElection_StopReleasesImmediately(t *testing.T) {
	withRedis(t)

	const name = "test-worker-stop"
	first := StartLeaderElection(name, 2*time.Second)
	if !waitFor(t, time.Second, first.IsLeader) {
		t.Fatal("first instance never acquired leadership")
	}

	second := StartLeaderElection(name, 2*time.Second)
	defer second.Stop()
	// second must NOT acquire while first still holds a live lease.
	time.Sleep(100 * time.Millisecond)
	if second.IsLeader() {
		t.Fatal("second instance acquired leadership while the first's lease was still live")
	}

	first.Stop()

	if !waitFor(t, time.Second, second.IsLeader) {
		t.Fatal("second instance did not acquire leadership promptly after Stop() released it")
	}
}

// Fencing: an instance that renews using a stale owner token (because
// another instance's lease expired and someone else took over while it was
// unaware — a GC pause, a network partition) must detect the mismatch and
// step down, not keep believing it is leader. This directly tests
// renewScript's compare-and-extend rather than a blind PEXPIRE.
func TestLeaderElection_FencingStepsDownOnOwnerMismatch(t *testing.T) {
	withRedis(t)

	const name = "test-worker-fencing"
	l := StartLeaderElection(name, 300*time.Millisecond)
	defer l.Stop()

	if !waitFor(t, time.Second, l.IsLeader) {
		t.Fatal("instance never acquired leadership")
	}

	// Simulate a different instance having taken over the key (as if our
	// lease expired while we were paused and someone else's SETNX won) by
	// overwriting the stored owner directly.
	if err := Redis.Set(context.Background(), l.key(), "someone-else-entirely", 300*time.Millisecond).Err(); err != nil {
		t.Fatalf("failed to simulate takeover: %v", err)
	}

	if !waitFor(t, 2*time.Second, func() bool { return !l.IsLeader() }) {
		t.Fatal("instance kept believing it was leader after another owner took the key — fencing did not engage")
	}
}

// IsLeader() must be accurate the instant StartLeaderElection returns, with
// no need to poll/wait — several callers do "start election, then if leader
// run a startup catch-up sweep" as their very next line, and would silently
// skip that sweep on every replica if the first acquire only completed
// asynchronously on the background goroutine's own schedule.
func TestLeaderElection_IsLeaderTrueImmediatelyOnWin(t *testing.T) {
	withRedis(t)

	l := StartLeaderElection("test-worker-immediate", time.Second)
	defer l.Stop()

	if !l.IsLeader() {
		t.Fatal("IsLeader() was false immediately after StartLeaderElection returned, with no contender — the first acquire must be synchronous")
	}
}

// Redis unavailable must never fall back to "instance 0" or any other
// always-true condition — IsLeader() must simply stay false forever, per
// plan.md ระยะ 4.1's explicit rejection of a hardcoded fallback (which is
// unsafe exactly because blue/green overlap can produce two "instance 0"s
// at once during a deploy).
func TestLeaderElection_NoRedisNeverBecomesLeader(t *testing.T) {
	original := Redis
	Redis = nil
	defer func() { Redis = original }()

	l := StartLeaderElection("test-worker-noredis", 200*time.Millisecond)
	defer l.Stop()

	time.Sleep(500 * time.Millisecond)
	if l.IsLeader() {
		t.Fatal("instance became leader with no Redis configured — this must never happen (no instance-0 fallback)")
	}
}
