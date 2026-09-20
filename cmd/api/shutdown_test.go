package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"itii-assist/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func withShutdownTestRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	original := config.Redis
	config.Redis = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = config.Redis.Close()
		config.Redis = original
	})
}

func waitForShutdown(t *testing.T, timeout time.Duration, wg *sync.WaitGroup) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// This is the exact regression caught in review: every leader-gated worker's
// `defer leader.Stop()` used to be reachable only by the goroutine returning
// on its own, which a `for range ticker.C` loop never does — so SIGTERM
// (docker compose stop during a routine blue/green cutover) killed the
// process without ever releasing the Redis leader key, leaving it to expire
// on its own after the full leaderTTL. This proves the fix: canceling the
// shared shutdown context makes the worker return AND actually release its
// lock (not just exit) well within 2s, versus this worker's own 10s sweep
// interval — so a tick firing first isn't what's making this pass.
func TestStartQueueOfferTimeoutWorker_ShutdownReleasesLockPromptly(t *testing.T) {
	withShutdownTestRedis(t)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	startQueueOfferTimeoutWorker(ctx, &wg)

	// Let the worker actually win leadership before we ask it to stop —
	// otherwise this would trivially pass by racing StartLeaderElection's
	// own first acquire rather than testing shutdown at all.
	leaderKey := "leader:queue-offer-timeout"
	if !waitFor(t, time.Second, func() bool {
		exists, err := config.Redis.Exists(context.Background(), leaderKey).Result()
		return err == nil && exists == 1
	}) {
		t.Fatal("worker never acquired the leader lock")
	}

	cancel()

	if !waitForShutdown(t, 2*time.Second, &wg) {
		t.Fatal("worker did not return within 2s of shutdown context cancellation — select is not preferring ctx.Done()")
	}

	// The real assertion: the lock itself must be gone, not just the
	// goroutine — a plain `return` without the deferred leader.Stop()
	// actually running would leave this key alive until leaderTTL.
	exists, err := config.Redis.Exists(context.Background(), leaderKey).Result()
	if err != nil {
		t.Fatalf("unexpected redis error: %v", err)
	}
	if exists != 0 {
		t.Fatal("leader lock still exists after shutdown — leader.Stop() did not run, so a routine deploy would leave this worker unled until the full leaderTTL")
	}
}

// waitFor is defined in config/leader_test.go for package config; this is
// the same small polling helper, duplicated here because it's one function
// and not worth exporting across a package boundary for a single test file.
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
