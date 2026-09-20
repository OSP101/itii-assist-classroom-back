package services

import (
	"context"
	"sync"
	"testing"
	"time"
)

// StartDailyDatabaseBackupWorker was the one worker left calling the old
// zero-argument signature when graceful shutdown was added to
// cmd/api/main.go (caught in review) — SIGTERM could kill it without ever
// giving it a chance to return, skipping tryRunDailyBackup's deferred
// releaseDailyBackupLock() if it happened to be mid-backup. This proves the
// worker now honors the same (ctx, wg) shutdown contract every other
// periodic worker uses: canceling ctx makes it return promptly, and wg
// actually tracks it so main()'s shutdownWG.Wait() won't return early
// without it.
//
// BACKUP_DAILY_HOUR/MINUTE=23:59 UTC makes tryRunDailyBackup's own "not
// scheduled yet" gate return immediately for the initial synchronous call —
// this test is about the shutdown wiring, not the backup scheduling logic.
// Not fully time-independent (caught in review: tryRunDailyBackup has no
// injectable clock, so no fixed hour/minute can be "far in the future" for
// every possible run time) — during the last minute before UTC midnight
// this gate would not short-circuit and the test would exercise real
// lock/DB logic instead, which is unconfigured here and would most likely
// just make this one run slower rather than actually break, but flagging
// the residual ~1-in-1440 flake window honestly rather than overclaiming
// determinism.
func TestStartDailyDatabaseBackupWorker_ShutdownReturnsPromptly(t *testing.T) {
	t.Setenv("R2_ENDPOINT", "https://example.invalid")
	t.Setenv("R2_BUCKET", "test-bucket")
	t.Setenv("R2_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("R2_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("BACKUP_DAILY_HOUR", "23")
	t.Setenv("BACKUP_DAILY_MINUTE", "59")
	t.Setenv("BACKUP_TIMEZONE", "UTC")

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	StartDailyDatabaseBackupWorker(ctx, &wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Sanity check: if loadR2Config() had rejected the fake-but-well-formed
	// values above, StartDailyDatabaseBackupWorker would have returned
	// without ever calling wg.Add(1), and `done` would already be closed
	// here with nothing left to prove.
	select {
	case <-done:
		t.Fatal("worker goroutine exited immediately — it was never actually started (R2 config rejected?), so this test isn't proving anything")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not return within 2s of shutdown context cancellation — select is not preferring ctx.Done()")
	}
}
