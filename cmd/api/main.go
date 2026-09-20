package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// เปลี่ยน "itii-assist" เป็นชื่อโมดูลของคุณในไฟล์ go.mod หากคุณตั้งชื่ออื่น
	"itii-assist/config"
	"itii-assist/handlers"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/observability"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/routes"
	"itii-assist/services"
	"itii-assist/utils"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/joho/godotenv"
)

func main() {
	// Graceful shutdown (caught in review): every leader-election-gated
	// worker below is a `for range ticker.C` loop whose `defer leader.Stop()`
	// was only reachable by the goroutine returning — and with no signal
	// handling anywhere, SIGTERM (what `docker compose stop` sends during a
	// routine blue/green cutover) killed the process without ever running
	// it. That left the old slot's Redis leader keys alive until their full
	// leaderTTL expired, so every deploy left periodic workers (PIN
	// rotation, queue sweeps, ...) with no active leader for up to 15s
	// instead of the near-immediate handoff plan.md ระยะ 4.1 relies on.
	// shutdownCtx is threaded into every worker's select loop below so
	// SIGTERM makes them return (and release their lock) promptly instead
	// of being killed mid-lease; shutdownWG lets main() wait for that to
	// actually finish before the process exits.
	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	defer cancelShutdown()
	var shutdownWG sync.WaitGroup

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("🛑 Received %s, shutting down gracefully (releasing leader locks)...", sig)
		cancelShutdown()
	}()

	// 1. โหลดไฟล์ .env
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  Warning: No .env file found (using system environment variables)")
	}

	// 2. เชื่อมต่อ Database
	config.ConnectDB()
	config.ConnectRedis()
	observability.InitPrometheusMetrics(config.DB)

	// 2.5 แก้ไข column types ที่เคยสร้างเป็น bigint แต่ต้องเป็น varchar(21) สำหรับ NanoID
	// (GORM AutoMigrate ไม่เปลี่ยน type ของ column ที่มีอยู่แล้ว)
	config.MigrateColumnTypes()

	// 3. 🪄 เสกตารางด้วย AutoMigrate!
	log.Println("⏳ Running Auto Migration for all tables...")
	err = config.DB.AutoMigrate(
		// ผู้ใช้และความปลอดภัย
		&models.User{},
		&models.UserOAuthAccount{},
		&models.RefreshToken{},
		&models.PasswordResetToken{},
		&models.TwoFactorPending{},
		// ห้องเรียนและผังที่นั่ง
		&models.Classroom{},
		&models.Zone{},
		&models.Desk{},
		// รายวิชา
		&models.Course{},
		&models.CourseMember{},
		&models.CourseInstructor{},
		&models.CourseTA{},
		&models.CourseSection{},
		&models.CourseSectionStudent{},
		&models.CourseSectionStudentRemoval{},
		&models.CourseActivityLog{},
		// นักศึกษา
		&models.Student{},
		&models.StudentGroup{},
		&models.StudentGroupMember{},
		// งานมอบหมายและคะแนน
		&models.Assignment{},
		&models.AssignmentAttendanceLink{},
		&models.AssignmentSubItem{},
		&models.Score{},
		&models.ScoreEditRequest{},
		&models.BonusScore{},
		// การสอบ
		&models.ExamSetting{},
		&models.ExamScore{},
		&models.ExamSession{},
		&models.ExamSessionRoom{},
		&models.ExamSeat{},
		// เช็กชื่อ
		&models.AttendanceSession{},
		&models.AttendanceSessionSection{},
		&models.AttendanceRecord{},
		&models.AttendanceRecordHistory{},
		&models.AttendanceLeaveRequest{},
		&models.AttendanceLeaveRequestItem{},
		&models.AttendancePinHistory{},
		&models.AttendanceDisplayDevice{},
		&models.AttendanceDisplayPairing{},
		&models.AttendanceDisplayGrant{},
		&models.AttendanceDisplayAuditLog{},
		// คิว
		&models.QueueSession{},
		&models.QueueBooking{},
		&models.QueueDeskStatus{},
		&models.QueueWorker{},
		// แจ้งเตือน
		&models.FcmToken{},
		&models.PushSubscription{},
		&models.NotificationLog{},
		&models.UserNotification{},
		&models.SystemAnnouncement{},
		&models.SystemAnnouncementAck{},
		&models.SystemAnnouncementDismissal{},
		&models.DatabaseBackupRecord{},
		// Feedback และ Log
		&models.Feedback{},
		&models.SystemLog{},
		&models.AppConfig{},
	)
	if err != nil {
		log.Fatal("❌ Migration failed: ", err)
	}
	log.Println("✅ All tables migrated successfully!")

	config.MigrateAttendancePinCompatibility()
	config.MigrateAttendanceRealtimeCompatibility()
	config.MigrateScoreSchemaCompatibility()
	config.MigrateQueueSessionCounterCompatibility()
	config.MigrateUploadPathsToApiPrefix()
	config.MigrateBase64AvatarsToFiles()
	config.MigrateBase64CourseCoversToFiles()
	// Must run before the index pass: it clears the duplicate acknowledgement
	// rows that would otherwise make the new unique indexes fail to build.
	config.MigrateAnnouncementStatuses()
	config.MigratePerformanceIndexes()
	config.MigrateAutovacuumSettings()
	config.MigratePgStatStatements()
	// Must run before the lifecycle worker starts, so the worker's first tick
	// already sees a clean working set instead of every session ever stuck.
	config.MigrateCloseStaleAttendanceSessions()
	// One-time cleanup of legacy duplicate attendance_records rows that
	// predate idx_attendance_session_student — gated by a completion marker
	// so it actually runs once, not on every boot (caught in review: an
	// earlier version of this comment said "safe to redo every boot," which
	// stopped being true once the marker was added). See
	// DedupeAllAttendanceRecordsWithDB's own comment for the full reasoning.
	if cleaned, err := repositories.DedupeAllAttendanceRecordsWithDB(config.DB); err != nil {
		log.Printf("⚠️  Failed to deduplicate legacy attendance records: %v", err)
	} else if cleaned > 0 {
		log.Printf("🧹 Deduplicated attendance_records for %d session/student pair(s)", cleaned)
	}

	// Last step of the DB setup: every migration above has run its DDL as a
	// plain statement, so from here on the request path can use the prepared
	// statement cache.
	config.EnablePreparedStatements()

	// Cross-replica realtime fan-out (plan.md ระยะ 4.1) — a no-op single-
	// instance-only log line if Redis isn't configured. Started before the
	// workers below so their first broadcasts (if this replica happens to
	// win early leadership) already reach other replicas' clients.
	realtime.StartRedisBus()

	startAttendancePinLifecycleWorker(shutdownCtx, &shutdownWG)

	// Web Push (webpush-go, VAPID) needs VAPID_PUBLIC_KEY/VAPID_PRIVATE_KEY set
	// or every push subscription attempt fails silently (503 on the frontend's
	// vapid-public-key fetch, no subscription ever created).
	if strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")) == "" || strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")) == "" {
		log.Println("⚠️  Warning: VAPID_PUBLIC_KEY/VAPID_PRIVATE_KEY not set — Web Push notifications are DISABLED")
		log.Println("   Generate a key pair with:  go run ./cmd/vapid-gen  then paste the output into .env")
	} else {
		log.Println("✅ Web Push (VAPID) keys configured")
	}

	// 4. รัน Fiber Server
	app := fiber.New(fiber.Config{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		ProxyHeader:  "X-Real-IP",
		// Fiber v3 ignores ProxyHeader unless the immediate peer is trusted.
		// The backend is only ever reached through the nginx container over
		// the docker "app" network, so its peer address is always a private
		// range — trust it so c.IP() reads nginx's X-Real-IP instead of
		// falling back to nginx's own container IP (e.g. 172.18.0.6).
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Private: true, Loopback: true},
	})
	// RequestLogger already emits a structured JSON line per request (and
	// records the Prometheus metrics, and recovers panics). Fiber's logger.New()
	// on top of it wrote a second line for the same request to the same stdout,
	// doubling the blocking-write cost on the hot path for no extra
	// information. Use APP_ENABLE_REQUEST_LOGGER=false to silence access logs.
	app.Use(middlewares.RequestLogger())

	auditLogger := services.NewAuditLogger(config.DB)

	// CORS — ต้องอยู่ก่อน middleware auth ทุกตัว
	rawOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	var allowedOrigins []string
	// Header list also carries the two headers introduced for cookie-based
	// web auth: X-Client-Type (tells the backend "this is the browser, use
	// cookies" vs the mobile app's Bearer-only requests) and X-CSRF-Token
	// (the double-submit CSRF header, checked inside Protected()/
	// OptionalProtected() — see middlewares/auth_middleware.go).
	corsAllowHeaders := []string{"Origin", "Content-Type", "Accept", "Authorization", utils.WebClientHeader, utils.CSRFHeaderName, utils.DeviceHintsHeader}

	if rawOrigins == "" || rawOrigins == "*" {
		// Allow all origins via func to avoid Fiber v3 strict URL validation.
		// AllowCredentials + AllowOriginsFunc is safe (unlike a literal "*"):
		// Fiber reflects the specific request Origin, not a wildcard.
		app.Use(cors.New(cors.Config{
			AllowOriginsFunc: func(origin string) bool { return true },
			AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders:     corsAllowHeaders,
			AllowCredentials: true,
		}))
	} else {
		for _, o := range strings.Split(rawOrigins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
		app.Use(cors.New(cors.Config{
			AllowOrigins:     allowedOrigins,
			AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders:     corsAllowHeaders,
			AllowCredentials: true,
		}))
	}

	// ไฟล์ใต้ uploads/private (หลักฐานการลา ฯลฯ) ห้ามเสิร์ฟแบบสาธารณะ
	// ต้องผ่าน route ที่เช็กสิทธิ์เท่านั้น
	app.Use("/api/uploads/private", func(c fiber.Ctx) error {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Not found"})
	})
	app.Use("/api/uploads", static.New("./uploads"))

	app.Get("/api/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "success", "message": "API is Running!"})
	})
	app.Get("/metrics", middlewares.RestrictMetricsToInternalNetworks(), observability.MetricsHandler)
	app.Get("/ws", realtime.Handler())

	routes.SetupAuthRoutes(app, auditLogger)
	routes.SetupUserRoutes(app, auditLogger)
	routes.SetupStudentRoutes(app)
	routes.SetupCourseRoutes(app, auditLogger)
	routes.SetupCourseActivityLogRoutes(app)
	routes.SetupTeamRoutes(app)
	routes.SetupClassroomRoutes(app)
	routes.SetupAttendanceRoutes(app, auditLogger)
	routes.SetupAssignmentRoutes(app)
	routes.SetupScoreRoutes(app, auditLogger)
	routes.SetupExamRoutes(app, auditLogger)
	routes.SetupBonusScoreRoutes(app, auditLogger)
	routes.SetupFeedbackRoutes(app)
	routes.SetupSystemRoutes(app)
	routes.SetupSystemSettingsRoutes(app)
	routes.SetupQueueRoutes(app, auditLogger)
	routes.SetupNotificationRoutes(app)
	routes.SetupPushRoutes(app)
	routes.SetupOAuthRoutes(app)
	routes.SetupUserNotificationRoutes(app)
	routes.SetupSystemLogRoutes(app)

	log.Println("🚀 Starting server on port 8000...")
	// Background job: cleanup expired student removal archive records daily
	shutdownWG.Add(1)
	go func() {
		defer shutdownWG.Done()
		leader := config.StartLeaderElection("cleanup-expired-removals", leaderTTL)
		defer leader.Stop()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-shutdownCtx.Done():
				return
			case <-ticker.C:
				if !leader.IsLeader() {
					continue
				}
				deleted, err := repositories.CleanupExpiredRemovals()
				if err != nil {
					log.Printf("⚠️  Cleanup expired removals error: %v", err)
				} else if deleted > 0 {
					log.Printf("🧹 Cleaned up %d expired student removal record(s)", deleted)
				}
			}
		}
	}()
	// Background job: เตือนผู้สอนวันละครั้งเมื่อมีคำขอลาค้างเกิน 3 วัน
	shutdownWG.Add(1)
	go func() {
		defer shutdownWG.Done()
		leader := config.StartLeaderElection("leave-request-pending-reminder", leaderTTL)
		defer leader.Stop()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-shutdownCtx.Done():
				return
			case <-ticker.C:
				if !leader.IsLeader() {
					continue
				}
				handlers.RunLeaveRequestPendingReminder(72 * time.Hour)
			}
		}
	}()
	// Background job: ปิดคำขอลาที่ค้าง "รอพิจารณา" เกินนโยบายของวิชาอัตโนมัติวันละครั้ง (กันค้างตลอดไปถ้าผู้สอนเพิกเฉย)
	shutdownWG.Add(1)
	go func() {
		defer shutdownWG.Done()
		leader := config.StartLeaderElection("leave-request-auto-expire", leaderTTL)
		defer leader.Stop()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-shutdownCtx.Done():
				return
			case <-ticker.C:
				if !leader.IsLeader() {
					continue
				}
				handlers.RunLeaveRequestAutoExpire()
			}
		}
	}()
	startLogRetentionWorker(shutdownCtx, &shutdownWG)
	// Was written but never started: the function existed, R2 and
	// BACKUP_DAILY_HOUR/MINUTE were configured in .env, and nothing ever called
	// it — so the scheduled backup had never run once. Manual backups from the
	// admin screen were unaffected, which is why it went unnoticed.
	services.StartDailyDatabaseBackupWorker(shutdownCtx, &shutdownWG)
	startQueueMidnightWorker(shutdownCtx, &shutdownWG)
	startQueuePausedSessionLeaseWorker(shutdownCtx, &shutdownWG)
	startQueueOfferTimeoutWorker(shutdownCtx, &shutdownWG)

	// GracefulContext makes Fiber's own Listen loop stop accepting new
	// connections and drain in-flight ones as soon as shutdownCtx is
	// canceled (SIGTERM/SIGINT above), instead of the process being killed
	// out from under it. Per fasthttp's own ShutdownWithContext contract, a
	// CLEAN drain always makes Listen return nil — so any non-nil err here
	// is abnormal by construction, whether or not a shutdown signal had
	// already been received (caught in review: checking shutdownCtx.Err()
	// to decide fatal-vs-not conflated "was shutdown requested" with "did
	// Listen actually drain cleanly," which let a genuine error racing a
	// shutdown signal get silently logged as "Graceful shutdown complete").
	// So any error at all here means: cancel shutdownCtx ourselves (a
	// bind failure never got one; a harmless no-op if a signal already did),
	// wait for every worker to release its lock, and exit non-zero so a
	// process supervisor notices and can restart — matching what the old
	// log.Fatal(app.Listen(":8000")) did for every Listen failure.
	if err := app.Listen(":8000", fiber.ListenConfig{GracefulContext: shutdownCtx}); err != nil {
		cancelShutdown()
		shutdownWG.Wait()
		log.Fatalf("❌ Server stopped abnormally: %v", err)
	}
	shutdownWG.Wait()
	log.Println("✅ Graceful shutdown complete")
}

// leaderTTL is the lease length every StartLeaderElection call in this file
// uses (plan.md ระยะ 4.1). One shared value rather than per-worker tuning:
// the renewal loop runs independently of each worker's own tick cadence (at
// leaderTTL/3, regardless of whether that worker ticks every 5s or once a
// day), so the only thing this controls is failover latency if an instance
// dies — how long another replica waits before it can safely take over.
const leaderTTL = 15 * time.Second

// attendancePinTickInterval is how often the PIN lifecycle worker sweeps.
//
// It was 1 second, which meant two full queries against attendance_sessions
// every second forever — ~86,400 transactions a day even with nobody using the
// system. PINs rotate on a one-minute cadence, so a few seconds of resolution
// is far more precision than the feature needs, and 5s cuts that background
// load by 80%. Override with ATTENDANCE_PIN_TICK_SECONDS if a deployment wants
// tighter timing.
func attendancePinTickInterval() time.Duration {
	const defaultSeconds = 5

	seconds := defaultSeconds
	if raw := strings.TrimSpace(os.Getenv("ATTENDANCE_PIN_TICK_SECONDS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 30 {
			log.Printf("⚠️  Invalid ATTENDANCE_PIN_TICK_SECONDS=%q (expected 1-30), using %d", raw, defaultSeconds)
		} else {
			seconds = parsed
		}
	}

	return time.Duration(seconds) * time.Second
}

func startAttendancePinLifecycleWorker(ctx context.Context, wg *sync.WaitGroup) {
	interval := attendancePinTickInterval()
	log.Printf("⏱️  Attendance PIN lifecycle worker interval: %s", interval)
	ticker := time.NewTicker(interval)

	wg.Add(1)
	go func() {
		defer wg.Done()
		leader := config.StartLeaderElection("attendance-pin-lifecycle", leaderTTL)
		defer leader.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			// Gated: N replicas each rotating the same session's PIN and
			// broadcasting their own "new PIN" event every tick would have
			// students and the projector disagreeing about which PIN is
			// current (plan.md ระยะ 4.1). reconcileAttendanceRuntimeMode's
			// own DB condition (pin_rotates_at <= now()) is the second,
			// independent safety net if leadership ever briefly overlaps.
			if !leader.IsLeader() {
				continue
			}
			now := time.Now()
			changes := make([]repositories.AttendancePinStateChange, 0, 4)

			// Auto-open scheduled sessions whose start time has arrived, so
			// pre-created QR sessions open on time without an instructor online.
			opened, err := repositories.AutoOpenDueAttendanceSessions(context.Background(), now)
			if err != nil {
				log.Printf("⚠️  Attendance auto-open worker failed: %v", err)
			}
			for _, o := range opened {
				changes = append(changes, o.AttendancePinStateChange)
				handlers.NotifyAttendanceSessionStarted(o.CourseID, o.SessionID, o.Title)
			}

			maintained, err := repositories.MaintainAttendanceRuntimeSessions(context.Background(), now)
			if err != nil {
				log.Printf("⚠️  Attendance PIN lifecycle worker failed: %v", err)
			} else {
				changes = append(changes, maintained...)
			}

			for _, change := range changes {
				if change.Rotated || change.Released || change.StatusChanged || change.ModeChanged {
					pinMode := "static"
					if change.PinRotatesAt != nil {
						pinMode = "rotating"
					}
					payload := fiber.Map{
						"session_id":      change.SessionID,
						"auto_rotate_pin": change.PinRotatesAt != nil,
						"pin_mode":        pinMode,
						"pin_code":        change.PinCode,
						"pin_issued_at":   change.PinIssuedAt,
						"pin_rotates_at":  change.PinRotatesAt,
						"status":          change.Status,
					}
					realtime.EmitToInstructor(change.SessionID, "attendance-pin-updated", payload)
					realtime.EmitToAttendanceDisplay(change.SessionID, "attendance-pin-updated", payload)

					// Students get the rotation timings but never the code —
					// see emitAttendancePinUpdated in handlers/attendance_handler.go.
					realtime.EmitToAttendanceStudents(change.SessionID, "attendance-pin-updated", fiber.Map{
						"session_id":      change.SessionID,
						"auto_rotate_pin": change.PinRotatesAt != nil,
						"pin_mode":        pinMode,
						"pin_issued":      strings.TrimSpace(change.PinCode) != "",
						"pin_issued_at":   change.PinIssuedAt,
						"pin_rotates_at":  change.PinRotatesAt,
						"status":          change.Status,
					})
				}

				if change.Status == "closed" && change.StatusChanged {
					realtime.EmitToAttendance(change.SessionID, "session-closed", fiber.Map{"session_id": change.SessionID})
					realtime.EmitToAttendanceDisplay(change.SessionID, "session-closed", fiber.Map{"session_id": change.SessionID})
				}
			}
		}
	}()
}

// startQueueMidnightWorker auto-closes any queue sessions that are still
// active/paused but started on a previous calendar day (Asia/Bangkok time).
//
// Behaviour:
//   - Runs immediately on startup (catches sessions left open if the server
//     was restarted or was down at midnight).
//   - Then checks every minute; when the wall-clock day rolls over it runs
//     the cleanup again.
//
// Safety: only 'waiting' bookings are cancelled. Bookings that are already
// 'in_progress' are left untouched so TAs can finish grading naturally.
func startQueueMidnightWorker(ctx context.Context, wg *sync.WaitGroup) {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		log.Printf("⚠️  startQueueMidnightWorker: cannot load Asia/Bangkok, falling back to UTC: %v", err)
		loc = time.UTC
	}

	runCleanup := func() {
		closed, err := repositories.AutoCloseStaleQueueSessions(loc)
		if err != nil {
			log.Printf("⚠️  Queue midnight cleanup error: %v", err)
			return
		}
		for _, s := range closed {
			log.Printf("🌙 Auto-closed stale queue session %s (course %s, cancelled %d waiting booking(s))",
				s.SessionID, s.CourseID, s.CancelledWaiting)
			realtime.EmitToQueue(s.SessionID, "session-status-changed", fiber.Map{
				"status":    "closed",
				"reason":    "auto_midnight",
				"timestamp": time.Now().UnixMilli(),
			})
			realtime.EmitToQueue(s.SessionID, "worker-status-updated", fiber.Map{
				"scope":     "all",
				"status":    "offline",
				"reason":    "session_closed",
				"timestamp": time.Now().UnixMilli(),
			})
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		leader := config.StartLeaderElection("queue-midnight", leaderTTL)
		defer leader.Stop()

		// Run once at startup
		if leader.IsLeader() {
			runCleanup()
		}

		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		lastDate := time.Now().In(loc).YearDay()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			today := time.Now().In(loc).YearDay()
			if today != lastDate {
				lastDate = today
				if leader.IsLeader() {
					runCleanup()
				}
			}
		}
	}()
}

// startQueueOfferTimeoutWorker expires stale queue offers on a timer.
//
// Offer expiry used to run only inside request handlers, so a 90s offer could
// outlive its deadline indefinitely: the TA who received it may have closed the
// tab, and every other TA suppresses polling while their socket is healthy, so
// no request arrived to trigger the sweep. The booking then showed as assigned
// on the overview while nobody could act on it.
//
// The sweep also re-dispatches sessions holding an unassigned waiting booking,
// which covers the tasks released by a decline or by a TA closing intake.
func startQueueOfferTimeoutWorker(ctx context.Context, wg *sync.WaitGroup) {
	const queueOfferSweepInterval = 10 * time.Second

	runSweep := func() {
		sessionIDs, err := repositories.GetActiveQueueSessionIDs()
		if err != nil {
			log.Printf("⚠️  Queue offer sweep: cannot list active sessions: %v", err)
			return
		}
		for _, sessionID := range sessionIDs {
			if err := handlers.ProcessQueueOfferTimeoutsForSession(sessionID); err != nil {
				log.Printf("⚠️  Queue offer sweep failed session=%s err=%v", sessionID, err)
				continue
			}
			hasWaiting, err := repositories.HasUnassignedWaitingBooking(sessionID)
			if err != nil {
				log.Printf("⚠️  Queue offer sweep: cannot check waiting bookings session=%s err=%v", sessionID, err)
				continue
			}
			if hasWaiting {
				handlers.DispatchWaitingBookingsToAvailableWorkers(sessionID)
			}
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		leader := config.StartLeaderElection("queue-offer-timeout", leaderTTL)
		defer leader.Stop()
		ticker := time.NewTicker(queueOfferSweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !leader.IsLeader() {
				continue
			}
			runSweep()
		}
	}()
}

func startQueuePausedSessionLeaseWorker(ctx context.Context, wg *sync.WaitGroup) {
	const (
		queuePausedLeaseTimeout = 2 * time.Minute
		queuePausedLeaseCheck   = 30 * time.Second
	)

	runCleanup := func() {
		closed, err := repositories.AutoCloseAbandonedPausedQueueSessions(queuePausedLeaseTimeout)
		if err != nil {
			log.Printf("⚠️  Queue paused-session lease cleanup error: %v", err)
			return
		}
		for _, s := range closed {
			log.Printf("🛑 Auto-closed abandoned paused queue session %s (course %s, cancelled %d waiting booking(s))",
				s.SessionID, s.CourseID, s.CancelledWaiting)
			realtime.EmitToQueue(s.SessionID, "session-status-changed", fiber.Map{
				"status":    "closed",
				"reason":    "auto_paused_timeout",
				"timestamp": time.Now().UnixMilli(),
			})
			realtime.EmitToQueue(s.SessionID, "worker-status-updated", fiber.Map{
				"scope":     "all",
				"status":    "offline",
				"reason":    "session_closed",
				"timestamp": time.Now().UnixMilli(),
			})
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		leader := config.StartLeaderElection("queue-paused-lease", leaderTTL)
		defer leader.Stop()

		if leader.IsLeader() {
			runCleanup()
		}

		ticker := time.NewTicker(queuePausedLeaseCheck)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !leader.IsLeader() {
				continue
			}
			runCleanup()
		}
	}()
}

// retentionStartupDelay keeps the first purge off the startup path — see
// startLogRetentionWorker.
const retentionStartupDelay = 5 * time.Minute

// startLogRetentionWorker purges rows past their retention window from the
// append-only log tables once a day.
//
// The first run is deliberately delayed rather than firing at boot: on a
// deployment with years of accumulated logs it is the heaviest run of all, and
// competing with startup migrations plus the first wave of user traffic is the
// worst possible time for it. Batching inside PurgeExpiredLogs caps how much
// any single run can delete, so a large backlog drains over several days
// instead of in one long stall.
func startLogRetentionWorker(ctx context.Context, wg *sync.WaitGroup) {
	runPurge := func() {
		for _, result := range repositories.PurgeExpiredLogs() {
			if result.Deleted == 0 {
				continue
			}
			if result.Capped {
				log.Printf("🧹 Retention: deleted %d row(s) from %s (per-run limit reached, more remain — continuing next run)", result.Deleted, result.Table)
				continue
			}
			log.Printf("🧹 Retention: deleted %d row(s) from %s", result.Deleted, result.Table)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		leader := config.StartLeaderElection("log-retention", leaderTTL)
		defer leader.Stop()

		select {
		case <-ctx.Done():
			return
		case <-time.After(retentionStartupDelay):
		}
		if leader.IsLeader() {
			runPurge()
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !leader.IsLeader() {
				continue
			}
			runPurge()
		}
	}()
}
