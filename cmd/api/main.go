package main

import (
	"log"
	"os"
	"strings"
	"time"

	// เปลี่ยน "itii-assist" เป็นชื่อโมดูลของคุณในไฟล์ go.mod หากคุณตั้งชื่ออื่น
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/routes"

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
		// เช็คชื่อ
		&models.AttendanceSession{},
		&models.AttendanceSessionSection{},
		&models.AttendanceRecord{},
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
		&models.NotificationLog{},
		&models.UserNotification{},
		// Feedback และ Log
		&models.Feedback{},
		&models.SystemLog{},
		&models.AppConfig{},
	)
	if err != nil {
		log.Fatal("❌ Migration failed: ", err)
	}
	log.Println("✅ All tables migrated successfully!")

	config.MigrateScoreSchemaCompatibility()
	config.MigrateQueueSessionCounterCompatibility()
	config.MigratePerformanceIndexes()

	// 4. รัน Fiber Server
	app := fiber.New()
	app.Use(logger.New())

	// CORS — ต้องอยู่ก่อน middleware auth ทุกตัว
	rawOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	var allowedOrigins []string
	if rawOrigins == "" || rawOrigins == "*" {
		// Allow all origins via func to avoid Fiber v3 strict URL validation
		app.Use(cors.New(cors.Config{
			AllowOriginsFunc: func(origin string) bool { return true },
			AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
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
			AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
			AllowCredentials: true,
		}))
	}

	app.Use("/uploads", static.New("./uploads"))

	app.Get("/api/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "success", "message": "API is Running!"})
	})
	app.Get("/ws", realtime.Handler())

	routes.SetupAuthRoutes(app)
	routes.SetupUserRoutes(app)
	routes.SetupStudentRoutes(app)
	routes.SetupCourseRoutes(app)
	routes.SetupCourseActivityLogRoutes(app)
	routes.SetupTeamRoutes(app)
	routes.SetupClassroomRoutes(app)
	routes.SetupAttendanceRoutes(app)
	routes.SetupAssignmentRoutes(app)
	routes.SetupScoreRoutes(app)
	routes.SetupExamRoutes(app)
	routes.SetupBonusScoreRoutes(app)
	routes.SetupFeedbackRoutes(app)
	routes.SetupSystemRoutes(app)
	routes.SetupQueueRoutes(app)
	routes.SetupNotificationRoutes(app)
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
	log.Fatal(app.Listen(":8000"))
}
