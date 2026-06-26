package repositories

import (
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupQueueRepoTestDB(t *testing.T) func() {
    t.Helper()

    db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
    if err != nil {
        t.Fatalf("open sqlite: %v", err)
    }

    if err := db.AutoMigrate(&models.QueueSession{}, &models.QueueWorker{}, &models.QueueBooking{}); err != nil {
        t.Fatalf("migrate sqlite: %v", err)
    }

    prevDB := config.DB
    config.DB = db

    return func() {
        config.DB = prevDB
    }
}

func TestCloseQueueSession_ForceWorkersOffline(t *testing.T) {
    cleanup := setupQueueRepoTestDB(t)
    defer cleanup()

    now := time.Now().UTC()
    session := models.QueueSession{
        ID:          "qs_test_close_001",
        CourseID:    "course_test",
        ClassroomID: "room_test",
        Title:       "Queue Session Test",
        PinCode:     "123456",
        Status:      "active",
        StartTime:   &now,
    }
    if err := config.DB.Create(&session).Error; err != nil {
        t.Fatalf("create session: %v", err)
    }

    currentBookingID := uint(77)
    pausedUntil := now.Add(5 * time.Minute)

    workers := []models.QueueWorker{
        {
            QueueSessionID:           session.ID,
            UserID:                   101,
            Status:                   "online",
            CurrentBookingID:         &currentBookingID,
            OfferPausedUntil:         &pausedUntil,
            ConsecutiveOfferTimeouts: 2,
            LastActiveAt:             &now,
        },
        {
            QueueSessionID:           session.ID,
            UserID:                   102,
            Status:                   "busy",
            CurrentBookingID:         &currentBookingID,
            OfferPausedUntil:         &pausedUntil,
            ConsecutiveOfferTimeouts: 1,
            LastActiveAt:             &now,
        },
        {
            QueueSessionID:           session.ID,
            UserID:                   103,
            Status:                   "paused",
            CurrentBookingID:         &currentBookingID,
            OfferPausedUntil:         &pausedUntil,
            ConsecutiveOfferTimeouts: 3,
            LastActiveAt:             &now,
        },
    }

    if err := config.DB.Create(&workers).Error; err != nil {
        t.Fatalf("create workers: %v", err)
    }

    if err := CloseQueueSession(session.ID); err != nil {
        t.Fatalf("close queue session: %v", err)
    }

    var closedSessionCount int64
    if err := config.DB.Model(&models.QueueSession{}).
        Where("id = ? AND status = 'closed' AND end_time IS NOT NULL", session.ID).
        Count(&closedSessionCount).Error; err != nil {
        t.Fatalf("verify closed session: %v", err)
    }
    if closedSessionCount != 1 {
        t.Fatalf("expected closed session count 1, got %d", closedSessionCount)
    }

    var workerCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ?", session.ID).
        Count(&workerCount).Error; err != nil {
        t.Fatalf("count workers: %v", err)
    }
    if workerCount != int64(len(workers)) {
        t.Fatalf("expected %d workers, got %d", len(workers), workerCount)
    }

    var nonOfflineCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ? AND status <> 'offline'", session.ID).
        Count(&nonOfflineCount).Error; err != nil {
        t.Fatalf("count non-offline workers: %v", err)
    }
    if nonOfflineCount != 0 {
        t.Fatalf("expected all workers offline, found %d non-offline workers", nonOfflineCount)
    }

    var hasCurrentBookingCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ? AND current_booking_id IS NOT NULL", session.ID).
        Count(&hasCurrentBookingCount).Error; err != nil {
        t.Fatalf("count workers with current booking: %v", err)
    }
    if hasCurrentBookingCount != 0 {
        t.Fatalf("expected no worker with current booking, found %d", hasCurrentBookingCount)
    }

    var hasOfferPauseCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ? AND offer_paused_until IS NOT NULL", session.ID).
        Count(&hasOfferPauseCount).Error; err != nil {
        t.Fatalf("count workers with offer pause: %v", err)
    }
    if hasOfferPauseCount != 0 {
        t.Fatalf("expected no worker with offer pause, found %d", hasOfferPauseCount)
    }

    var nonZeroTimeoutCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ? AND consecutive_offer_timeouts <> 0", session.ID).
        Count(&nonZeroTimeoutCount).Error; err != nil {
        t.Fatalf("count workers with non-zero timeout streak: %v", err)
    }
    if nonZeroTimeoutCount != 0 {
        t.Fatalf("expected all timeout streaks reset to 0, found %d", nonZeroTimeoutCount)
    }

    var missingLastActiveCount int64
    if err := config.DB.Model(&models.QueueWorker{}).
        Where("queue_session_id = ? AND last_active_at IS NULL", session.ID).
        Count(&missingLastActiveCount).Error; err != nil {
        t.Fatalf("count workers with missing last_active_at: %v", err)
    }
    if missingLastActiveCount != 0 {
        t.Fatalf("expected all workers to have last_active_at, found %d missing", missingLastActiveCount)
    }
}
