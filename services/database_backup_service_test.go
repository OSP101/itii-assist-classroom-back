package services

import (
	"context"
	"testing"

	"itii-assist/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func withBackupTestRedis(t *testing.T) *miniredis.Miniredis {
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

// releaseDailyBackupLock must never delete another instance's lock — this is
// exactly the race caught in review: instance A acquires the lock, stalls
// past the TTL, instance B's SetNX then wins the now-expired key, and A
// resumes and calls its deferred release. A plain unconditional DEL would
// delete B's active lock out from under it, letting a third instance also
// acquire it while B still believes it holds it (two backups running at
// once). The fencing script must detect A's stale owner token no longer
// matches what's stored and leave B's lock alone.
func TestReleaseDailyBackupLock_DoesNotDeleteAnotherOwnersLock(t *testing.T) {
	withBackupTestRedis(t)
	ctx := context.Background()

	if !acquireDailyBackupLock() {
		t.Fatal("instance A failed to acquire the lock on an empty key")
	}
	staleOwner := dailyBackupLockOwner
	if staleOwner == "" {
		t.Fatal("acquireDailyBackupLock did not record an owner token")
	}

	// Simulate the lock expiring and a second instance (B) winning it, without
	// going through A's in-process dailyBackupLockOwner (a real second process
	// would have its own).
	newOwner := "instance-b-owner"
	if err := config.Redis.Set(ctx, dailyBackupLockKey, newOwner, dailyBackupLockTTL).Err(); err != nil {
		t.Fatalf("failed to simulate instance B taking over the lock: %v", err)
	}

	// A resumes from its stall and releases using its now-stale owner token.
	dailyBackupLockOwner = staleOwner
	releaseDailyBackupLock()

	got, err := config.Redis.Get(ctx, dailyBackupLockKey).Result()
	if err != nil {
		t.Fatalf("instance B's lock was deleted by A's stale release (expected it to survive): %v", err)
	}
	if got != newOwner {
		t.Fatalf("lock value changed after A's stale release: got %q, want %q", got, newOwner)
	}
}

// The normal case: an instance releasing its own still-valid lock must
// actually remove it, so the next tick (or a different day's run) can
// acquire cleanly instead of waiting out the full TTL.
func TestReleaseDailyBackupLock_DeletesOwnLock(t *testing.T) {
	withBackupTestRedis(t)
	ctx := context.Background()

	if !acquireDailyBackupLock() {
		t.Fatal("failed to acquire the lock on an empty key")
	}

	releaseDailyBackupLock()

	exists, err := config.Redis.Exists(ctx, dailyBackupLockKey).Result()
	if err != nil {
		t.Fatalf("unexpected error checking lock key: %v", err)
	}
	if exists != 0 {
		t.Fatal("releaseDailyBackupLock did not delete the caller's own lock")
	}
}

// A second acquire attempt while the first still holds a live lease must
// fail — this is the whole point of the lock (blue/green: both slots tick
// the same worker, only one may proceed).
func TestAcquireDailyBackupLock_SecondAttemptFailsWhileHeld(t *testing.T) {
	withBackupTestRedis(t)

	if !acquireDailyBackupLock() {
		t.Fatal("first acquire on an empty key should succeed")
	}

	// A second call from what would be a different process (this process's
	// dailyBackupLockOwner would just get overwritten in a real single-process
	// test, so check the underlying SetNX semantics directly matches what a
	// second real instance would observe).
	if acquireDailyBackupLock() {
		t.Fatal("second acquire succeeded while the first lock was still held — SetNX should have failed")
	}
}
