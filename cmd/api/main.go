package main

import (
	"context"
	"log"
	"os"
	"strings"
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
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/joho/godotenv"
)

func main() {
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
		// เช็คชื่อ
		&models.AttendanceSession{},
		&models.AttendanceSessionSection{},
		&models.AttendanceRecord{},
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
	config.MigratePerformanceIndexes()
	startAttendancePinLifecycleWorker()

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
	})
	app.Use(middlewares.RequestLogger())
	app.Use(logger.New())

	auditLogger := services.NewAuditLogger(config.DB)

	// CORS — ต้องอยู่ก่อน middleware auth ทุกตัว
	rawOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	var allowedOrigins []string
	// Header list also carries the two headers introduced for cookie-based
	// web auth: X-Client-Type (tells the backend "this is the browser, use
	// cookies" vs the mobile app's Bearer-only requests) and X-CSRF-Token
	// (the double-submit CSRF header, checked inside Protected()/
	// OptionalProtected() — see middlewares/auth_middleware.go).
	corsAllowHeaders := []string{"Origin", "Content-Type", "Accept", "Authorization", utils.WebClientHeader, utils.CSRFHeaderName}

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
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			deleted, err := repositories.CleanupExpiredRemovals()
			if err != nil {
				log.Printf("⚠️  Cleanup expired removals error: %v", err)
			} else if deleted > 0 {
				log.Printf("🧹 Cleaned up %d expired student removal record(s)", deleted)
			}
		}
	}()
	startQueueMidnightWorker()
	startQueuePausedSessionLeaseWorker()
	log.Fatal(app.Listen(":8000"))
}

func startAttendancePinLifecycleWorker() {
	ticker := time.NewTicker(1 * time.Second)

	go func() {
		defer ticker.Stop()
		for range ticker.C {
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
				if change.Rotated || change.Released || change.StatusChanged {
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
func startQueueMidnightWorker() {
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

	go func() {
		// Run once at startup
		runCleanup()

		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		lastDate := time.Now().In(loc).YearDay()
		for range ticker.C {
			today := time.Now().In(loc).YearDay()
			if today != lastDate {
				lastDate = today
				runCleanup()
			}
		}
	}()
}

func startQueuePausedSessionLeaseWorker() {
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

	go func() {
		runCleanup()

		ticker := time.NewTicker(queuePausedLeaseCheck)
		defer ticker.Stop()

		for range ticker.C {
			runCleanup()
		}
	}()
}
