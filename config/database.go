package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// สร้างตัวแปร Global เพื่อให้ไฟล์อื่นเรียกใช้ Database ได้
var DB *gorm.DB

func ConnectDB() {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")
	requestedMaxOpenConns := getEnvInt("DB_MAX_OPEN_CONNS", 100)
	maxIdleConns := getEnvInt("DB_MAX_IDLE_CONNS", 20)
	connMaxLifetimeMinutes := getEnvInt("DB_CONN_MAX_LIFETIME_MINUTES", 30)
	connMaxIdleTimeMinutes := getEnvInt("DB_CONN_MAX_IDLE_TIME_MINUTES", 10)

	// จัดรูปแบบ Data Source Name (DSN)
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Bangkok",
		host, user, password, dbname, port)

	// เปิดการเชื่อมต่อ
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// PrepareStmt stays OFF for the connection itself so the startup
		// migrations run as plain statements — see EnablePreparedStatements,
		// which turns it on once the DDL is done. These two settings are still
		// read from here when that session is created.
		//
		// The cache size MUST be bounded. GORM defaults it to MaxInt —
		// effectively unlimited — and this codebase has ~140 `IN ?` queries.
		// GORM expands those to a different SQL string for every slice length,
		// so an unbounded cache would keep accumulating distinct entries, each
		// one a real prepared statement pinned on every pooled connection:
		// exactly the slow-creep failure we are removing everywhere else.
		PrepareStmtMaxSize: getEnvInt("DB_PREPARE_STMT_MAX_SIZE", 512),
		PrepareStmtTTL:     time.Duration(getEnvInt("DB_PREPARE_STMT_TTL_MINUTES", 20)) * time.Minute,
		Logger:             newGormLogger(),
	})
	if err != nil {
		log.Fatal("❌ Failed to connect to database! \n", err)
	}

	// ตั้งค่า Connection Pool เพื่อรองรับ web + mobile พร้อมกัน
	sqlDB, err := database.DB()
	if err != nil {
		log.Fatal("❌ Failed to get underlying sql.DB: ", err)
	}
	maxOpenConns := capMaxOpenConns(database, requestedMaxOpenConns)
	if maxIdleConns > maxOpenConns {
		log.Printf("⚠️  Reducing DB_MAX_IDLE_CONNS from %d to %d so it does not exceed max open connections", maxIdleConns, maxOpenConns)
		maxIdleConns = maxOpenConns
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)                                           // จำนวน connection สูงสุดที่เปิดพร้อมกัน
	sqlDB.SetMaxIdleConns(maxIdleConns)                                           // จำนวน connection ที่เก็บไว้ใช้ซ้ำ
	sqlDB.SetConnMaxLifetime(time.Duration(connMaxLifetimeMinutes) * time.Minute) // อายุ connection สูงสุด
	sqlDB.SetConnMaxIdleTime(time.Duration(connMaxIdleTimeMinutes) * time.Minute) // ปล่อย idle connection ที่ไม่ใช้งาน

	log.Printf("✅ Database connection successfully opened (pool: max=%d, idle=%d, lifetime=%dm, idletime=%dm)", maxOpenConns, maxIdleConns, connMaxLifetimeMinutes, connMaxIdleTimeMinutes)
	DB = database
}

func getEnvInt(name string, defaultValue int) int {
	rawValue := os.Getenv(name)
	if rawValue == "" {
		return defaultValue
	}

	parsedValue, err := strconv.Atoi(rawValue)
	if err != nil || parsedValue <= 0 {
		log.Printf("⚠️  Invalid %s=%q, using default %d", name, rawValue, defaultValue)
		return defaultValue
	}

	return parsedValue
}

func capMaxOpenConns(database *gorm.DB, requestedMaxOpenConns int) int {
	var maxConnectionsRaw string
	if err := database.Raw("SHOW max_connections").Scan(&maxConnectionsRaw).Error; err != nil {
		log.Printf("⚠️  Failed to read PostgreSQL max_connections: %v", err)
		return requestedMaxOpenConns
	}

	maxConnections, err := strconv.Atoi(maxConnectionsRaw)
	if err != nil || maxConnections <= 0 {
		log.Printf("⚠️  Invalid PostgreSQL max_connections=%q", maxConnectionsRaw)
		return requestedMaxOpenConns
	}

	safeMaxOpenConns := maxConnections - 20
	if safeMaxOpenConns < 10 {
		safeMaxOpenConns = maxConnections
	}
	if requestedMaxOpenConns > safeMaxOpenConns {
		log.Printf("⚠️  Capping DB_MAX_OPEN_CONNS from %d to %d based on PostgreSQL max_connections=%d", requestedMaxOpenConns, safeMaxOpenConns, maxConnections)
		return safeMaxOpenConns
	}

	return requestedMaxOpenConns
}

// MigrateColumnTypes แก้ไข column type ที่เคยถูกสร้างเป็น bigint แต่ต้องเป็น varchar(21) สำหรับ NanoID
// GORM AutoMigrate ไม่เปลี่ยน type ของ column ที่มีอยู่แล้ว ต้องรันฟังก์ชันนี้แยกต่างหาก
func MigrateColumnTypes() {
	type alteration struct {
		table  string
		column string
	}

	targets := []alteration{
		// Primary keys ที่ใช้ NanoID (varchar(21))
		{"classrooms", "id"},
		{"zones", "id"},
		{"zones", "classroom_id"},
		{"desks", "id"},
		{"desks", "classroom_id"},
		{"courses", "id"},
		{"queue_sessions", "id"},
		{"queue_sessions", "course_id"},
		{"queue_sessions", "classroom_id"},
		{"queue_bookings", "queue_session_id"},
		{"queue_bookings", "desk_id"},
		{"queue_desk_statuses", "queue_session_id"},
		{"queue_desk_statuses", "desk_id"},
		{"queue_workers", "queue_session_id"},
	}

	for _, t := range targets {
		var dataType string
		DB.Raw(
			`SELECT data_type FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`,
			t.table, t.column,
		).Scan(&dataType)

		if dataType == "" {
			// ตารางหรือ column ยังไม่มี ข้ามไป
			continue
		}

		if dataType != "character varying" {
			log.Printf("⚙️  Altering %s.%s: %s → varchar(21)", t.table, t.column, dataType)

			// ดึง FK constraints ที่อ้างถึง column นี้เพื่อ drop ก่อน
			type fkRow struct {
				ConstraintName string `gorm:"column:constraint_name"`
				TableName      string `gorm:"column:table_name"`
			}
			var fks []fkRow
			DB.Raw(`
				SELECT tc.constraint_name, tc.table_name
				FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
				  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
				JOIN information_schema.referential_constraints rc
				  ON tc.constraint_name = rc.constraint_name
				JOIN information_schema.key_column_usage kcu2
				  ON rc.unique_constraint_name = kcu2.constraint_name
				WHERE tc.constraint_type = 'FOREIGN KEY'
				  AND kcu2.table_name = ? AND kcu2.column_name = ?
				  AND tc.table_schema = 'public'`,
				t.table, t.column,
			).Scan(&fks)

			for _, fk := range fks {
				dropSQL := fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s`,
					fk.TableName, fk.ConstraintName)
				if err := DB.Exec(dropSQL).Error; err != nil {
					log.Printf("⚠️  Could not drop FK %s on %s: %v", fk.ConstraintName, fk.TableName, err)
				}
			}

			alterSQL := fmt.Sprintf(
				`ALTER TABLE %s ALTER COLUMN %s TYPE varchar(21) USING %s::text`,
				t.table, t.column, t.column,
			)
			if err := DB.Exec(alterSQL).Error; err != nil {
				log.Printf("❌ Failed to alter %s.%s: %v", t.table, t.column, err)
			} else {
				log.Printf("✅ Altered %s.%s to varchar(21)", t.table, t.column)
			}
		}
	}
}

// MigrateScoreSchemaCompatibility fixes legacy drift in PostgreSQL databases where
// scores.user_id was created as NOT NULL even though the current score flow uses student_id/group_id.
func MigrateScoreSchemaCompatibility() {
	if DB == nil {
		return
	}

	var isNullable string
	DB.Raw(
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'scores' AND column_name = 'user_id'`,
	).Scan(&isNullable)

	if isNullable == "NO" {
		log.Println("⚙️  Altering scores.user_id to allow NULL for legacy score compatibility")
		if err := DB.Exec(`ALTER TABLE scores ALTER COLUMN user_id DROP NOT NULL`).Error; err != nil {
			log.Printf("❌ Failed to alter scores.user_id nullability: %v", err)
		} else {
			log.Println("✅ Altered scores.user_id to allow NULL")
		}
	}
}

func MigrateQueueSessionCounterCompatibility() {
	if DB == nil {
		return
	}

	statements := []string{
		`UPDATE queue_sessions SET next_queue_number = 1 WHERE next_queue_number IS NULL OR next_queue_number < 1`,
		`UPDATE queue_sessions AS qs
		 SET next_queue_number = booking_state.max_queue_number + 1
		 FROM (
		 	SELECT queue_session_id, MAX(queue_number) AS max_queue_number
		 	FROM queue_bookings
		 	GROUP BY queue_session_id
		 ) AS booking_state
		 WHERE qs.id = booking_state.queue_session_id
		   AND qs.next_queue_number <= booking_state.max_queue_number`,
	}

	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			log.Printf("⚠️  Failed to sync queue_sessions.next_queue_number: %v", err)
			return
		}
	}

	log.Println("✅ Synchronized queue_sessions.next_queue_number")
}

// MigrateUploadPathsToApiPrefix ย้าย path รูปที่เก็บใน DB จาก /uploads/... ไปเป็น /api/uploads/...
// ให้ตรงกับ static mount ใหม่ (app.Use("/api/uploads", ...)) เพื่อให้รูปเก่าที่อัปโหลดไว้ก่อนหน้ายังแสดงได้
// statement ทุกตัวเป็น idempotent (รันซ้ำได้ ไม่เพิ่ม prefix ซ้ำ) เพราะรันทุกครั้งที่ boot
func MigrateUploadPathsToApiPrefix() {
	if DB == nil {
		return
	}

	statements := []string{
		// ประกาศระบบ: image_url เป็น text ค่าเดียว เช่น "/uploads/system-announcements/xxx.png"
		`UPDATE system_announcements
		 SET image_url = '/api' || image_url
		 WHERE image_url LIKE '/uploads/%'`,
		// คำขอแก้คะแนน: images เป็น jsonb array ของ path เช่น "uploads/score-edit-requests/xxx.jpg"
		// anchor ด้วยเครื่องหมาย " หน้า path เพื่อไม่ให้ replace ซ้ำ (หลังแก้แล้ว pattern เดิมจะหายไป)
		`UPDATE score_edit_requests
		 SET images = REPLACE(images::text, '"uploads/score-edit-requests/', '"api/uploads/score-edit-requests/')::jsonb
		 WHERE images::text LIKE '%"uploads/score-edit-requests/%'`,
		// เผื่อ row เก่าที่บาง path มี leading slash ("/uploads/score-edit-requests/...")
		`UPDATE score_edit_requests
		 SET images = REPLACE(images::text, '"/uploads/score-edit-requests/', '"/api/uploads/score-edit-requests/')::jsonb
		 WHERE images::text LIKE '%"/uploads/score-edit-requests/%'`,
	}

	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			log.Printf("⚠️  Failed to migrate upload paths to /api/uploads prefix: %v", err)
			return
		}
	}

	log.Println("✅ Migrated upload paths to /api/uploads prefix")
}

// MigrateBase64AvatarsToFiles converts any users.avatar values still holding
// a base64 data URI (from the old UploadAvatarHandler, which stored the
// entire uploaded image inline before it was changed to save a file under
// uploads/avatars/ and store a URL) into a file on disk plus an
// /api/uploads/avatars/... URL. Those inline blobs rode along in every
// UserBasic embedded in course/instructor/TA list responses, so a handful of
// users with profile photos was enough to balloon endpoints like
// /api/courses/my-courses and /api/courses/instructors from a few KB to
// several MB each. Idempotent: a migrated row's avatar no longer matches the
// "data:" prefix, so re-running this on every boot is a no-op for it.
func MigrateBase64AvatarsToFiles() {
	if DB == nil {
		return
	}

	var rows []struct {
		ID     uint
		Avatar string
	}
	if err := DB.Raw(`SELECT id, avatar FROM users WHERE avatar LIKE 'data:%;base64,%'`).Scan(&rows).Error; err != nil {
		log.Printf("⚠️  Failed to query base64 avatars for migration: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	baseDir := filepath.Join("uploads", "avatars")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		log.Printf("⚠️  Failed to create uploads/avatars directory for avatar migration: %v", err)
		return
	}

	migrated := 0
	for _, row := range rows {
		comma := strings.Index(row.Avatar, ",")
		if comma < 0 {
			continue
		}
		header := row.Avatar[:comma]
		payload := row.Avatar[comma+1:]

		content, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			log.Printf("⚠️  Skipping unparseable avatar for user %d: %v", row.ID, err)
			continue
		}

		ext := ".jpg"
		switch {
		case strings.Contains(header, "image/png"):
			ext = ".png"
		case strings.Contains(header, "image/gif"):
			ext = ".gif"
		case strings.Contains(header, "image/webp"):
			ext = ".webp"
		}

		fileName := fmt.Sprintf("avatar-migrated-%d-%d%s", row.ID, time.Now().UnixNano(), ext)
		filePath := filepath.Join(baseDir, fileName)
		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			log.Printf("⚠️  Failed to write migrated avatar for user %d: %v", row.ID, err)
			continue
		}

		publicPath := filepath.ToSlash(filepath.Join("/api/uploads", "avatars", fileName))
		if err := DB.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, publicPath, row.ID).Error; err != nil {
			log.Printf("⚠️  Failed to update avatar URL for user %d: %v", row.ID, err)
			continue
		}
		migrated++
	}

	log.Printf("✅ Migrated %d base64 avatar(s) to files under uploads/avatars", migrated)
}

func MigratePerformanceIndexes() {
	if DB == nil {
		return
	}

	indexes := []struct {
		name string
		sql  string
	}{
		{
			name: "attendance_records_session_student",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_records_session_student ON attendance_records (attendance_session_id, student_id)`,
		},
		{
			name: "attendance_session_sections_session",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_session_sections_session ON attendance_session_sections (attendance_session_id)`,
		},
		{
			name: "queue_desk_statuses_session_desk",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_desk_statuses_session_desk ON queue_desk_statuses (queue_session_id, desk_id)`,
		},
		{
			name: "queue_desk_statuses_session_desk_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_desk_statuses_session_desk ON queue_desk_statuses (queue_session_id, desk_id)`,
		},
		{
			name: "queue_bookings_session_student_active",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_student_active ON queue_bookings (queue_session_id, student_id) WHERE status IN ('waiting','in_progress')`,
		},
		{
			name: "queue_bookings_session_student_active_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_bookings_session_student_active ON queue_bookings (queue_session_id, student_id) WHERE status IN ('waiting','in_progress')`,
		},
		{
			name: "queue_bookings_session_queue_number",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_queue_number ON queue_bookings (queue_session_id, queue_number DESC)`,
		},
		{
			name: "queue_bookings_session_desk_active_created",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_desk_active_created ON queue_bookings (queue_session_id, desk_id, created_at DESC) WHERE status IN ('waiting','in_progress')`,
		},
		{
			name: "course_section_students_student_section",
			sql:  `CREATE INDEX IF NOT EXISTS idx_course_section_students_student_section ON course_section_students (student_id, course_section_id)`,
		},
		{
			name: "course_sections_course_id_id",
			sql:  `CREATE INDEX IF NOT EXISTS idx_course_sections_course_id_id ON course_sections (course_id, id)`,
		},
		{
			name: "desks_classroom_number_enabled_type",
			sql:  `CREATE INDEX IF NOT EXISTS idx_desks_classroom_number_enabled_type ON desks (classroom_id, number, is_enabled, type)`,
		},
		{
			name: "attendance_sessions_pin_rotation",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_sessions_pin_rotation ON attendance_sessions (status, start_time, end_time, pin_rotates_at)`,
		},
		{
			name: "attendance_sessions_pin_code_status",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_sessions_pin_code_status ON attendance_sessions (pin_code, status)`,
		},
		{
			name: "attendance_sessions_current_pin_hash_status",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_sessions_current_pin_hash_status ON attendance_sessions (current_pin_hash, status)`,
		},
		{
			name: "attendance_sessions_previous_pin_hash_status",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_sessions_previous_pin_hash_status ON attendance_sessions (previous_pin_hash, status)`,
		},
		{
			name: "queue_sessions_pin_code_status",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_sessions_pin_code_status ON queue_sessions (pin_code, status)`,
		},
		{
			name: "queue_sessions_status_cutoff",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_sessions_status_cutoff ON queue_sessions (status, cutoff_at)`,
		},
		{
			name: "queue_bookings_session_late_created",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_late_created ON queue_bookings (queue_session_id, is_late_booking, created_at DESC)`,
		},
		{
			name: "queue_workers_session_user_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_workers_session_user ON queue_workers (queue_session_id, user_id)`,
		},
		{
			name: "exam_seats_session_student_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_exam_seats_session_student ON exam_seats (exam_session_id, student_id)`,
		},
		{
			name: "exam_seats_session_desk_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_exam_seats_session_desk ON exam_seats (exam_session_id, desk_id)`,
		},
		{
			name: "exam_session_rooms_session_classroom_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_exam_session_rooms_session_classroom ON exam_session_rooms (exam_session_id, classroom_id)`,
		},
		{
			name: "exam_seats_session_seat_number_unique",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS uq_exam_seats_session_seat_number ON exam_seats (exam_session_id, seat_number) WHERE seat_number > 0`,
		},
		{
			name: "queue_bookings_session_status_assigned_worker",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_status_assigned_worker ON queue_bookings (queue_session_id, status, assigned_worker_id, queue_number)`,
		},
		{
			name: "queue_bookings_session_worker_active",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_bookings_session_worker_active ON queue_bookings (queue_session_id, assigned_worker_id, status) WHERE status IN ('waiting','in_progress')`,
		},
		{
			name: "scores_assignment_student",
			sql:  `CREATE INDEX IF NOT EXISTS idx_scores_assignment_student ON scores (assignment_id, student_id)`,
		},
		{
			// SubmitScore upserts by finding then writing in application code, with no
			// ON CONFLICT, so two graders submitting at once can both miss and both
			// insert. This is the only thing that actually stops the duplicate.
			//
			// Creation fails while duplicate rows still exist — run
			// cmd/repair-orphan-scores first. A failure here only logs and continues.
			name: "scores_assignment_student_subitem_unique",
			sql: `CREATE UNIQUE INDEX IF NOT EXISTS uq_scores_assignment_student_subitem
			      ON scores (assignment_id, student_id, sub_item_id)
			      WHERE student_id IS NOT NULL AND sub_item_id IS NOT NULL`,
		},
		{
			name: "user_notifications_user_is_read",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_notifications_user_is_read ON user_notifications (user_id, is_read) WHERE is_read = false`,
		},
		{
			name: "fcm_tokens_session_type",
			sql:  `CREATE INDEX IF NOT EXISTS idx_fcm_tokens_session_type ON fcm_tokens (session_id, user_type)`,
		},
		{
			name: "queue_sessions_concurrent_group_id",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_sessions_concurrent_group_id ON queue_sessions (concurrent_group_id) WHERE concurrent_group_id IS NOT NULL`,
		},
		{
			name: "queue_sessions_group_pin_code",
			sql:  `CREATE INDEX IF NOT EXISTS idx_queue_sessions_group_pin_code ON queue_sessions (group_pin_code) WHERE group_pin_code IS NOT NULL`,
		},

		// ── Retention support ────────────────────────────────────────────
		//
		// The nightly purge deletes by `created_at < cutoff`. GORM's model tags
		// gave these tables indexes on their foreign keys but not on created_at,
		// so without these the purge sequentially scans the largest tables in
		// the database — on attendance_pin_histories, which gains a row every
		// minute for every open session, that is the biggest scan of all.
		//
		// system_logs and course_activity_logs already carry a created_at index
		// from their model tags and are deliberately not repeated here.
		{
			name: "attendance_pin_histories_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_pin_histories_created_at ON attendance_pin_histories (created_at)`,
		},
		{
			name: "notification_logs_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_notification_logs_created_at ON notification_logs (created_at)`,
		},
		{
			name: "attendance_display_audit_logs_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_attendance_display_audit_logs_created_at ON attendance_display_audit_logs (created_at)`,
		},
		{
			// Matches the purge predicate exactly: only read notifications are
			// ever deleted, so the partial index stays small and skips the
			// unread rows that are never candidates.
			name: "user_notifications_read_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_notifications_read_created_at ON user_notifications (created_at) WHERE is_read = true`,
		},

		// ── Filter-then-sort paths ───────────────────────────────────────
		//
		// Each of these queries filters on one column and sorts by another.
		// A single-column index on the filter alone still forces PostgreSQL to
		// fetch every matching row and sort it; putting the sort column into the
		// index lets it read the rows already ordered and stop at the LIMIT.
		{
			// GetUserNotifications: WHERE user_id = ? ORDER BY created_at DESC
			// — the notification inbox, opened constantly, and the row count per
			// user only grows.
			name: "user_notifications_user_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_notifications_user_created_at ON user_notifications (user_id, created_at DESC)`,
		},
		{
			// Course activity log listing: always scoped to a course, always
			// newest-first, always paginated.
			name: "course_activity_logs_course_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_course_activity_logs_course_created_at ON course_activity_logs (course_id, created_at DESC)`,
		},
		{
			// GetCourseOverview: WHERE course_id = ? AND is_active = true
			// ORDER BY created_at DESC. Part of the heaviest read in the app.
			name: "assignments_course_active_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_assignments_course_active_created_at ON assignments (course_id, created_at DESC) WHERE is_active = true`,
		},

		// ── My-courses list (home dashboard) ────────────────────────────
		//
		// GetMyCourses joins course_instructors/course_tas on
		// `ON x.course_id = courses.id AND x.user_id = ?`. The model tags
		// only gave each table separate single-column indexes on course_id
		// and user_id, not a composite covering the join predicate.
		{
			name: "course_instructors_user_course",
			sql:  `CREATE INDEX IF NOT EXISTS idx_course_instructors_user_course ON course_instructors (user_id, course_id)`,
		},
		{
			name: "course_tas_user_course",
			sql:  `CREATE INDEX IF NOT EXISTS idx_course_tas_user_course ON course_tas (user_id, course_id)`,
		},
		{
			// courses.is_active / year / semester are filtered directly
			// (WHERE courses.is_active = true, etc.) with no index at all.
			name: "courses_is_active",
			sql:  `CREATE INDEX IF NOT EXISTS idx_courses_is_active ON courses (is_active)`,
		},
		{
			name: "courses_year_semester",
			sql:  `CREATE INDEX IF NOT EXISTS idx_courses_year_semester ON courses (year, semester)`,
		},
	}

	ensuredCount := 0
	for _, index := range indexes {
		if err := DB.Exec(index.sql).Error; err != nil {
			log.Printf("⚠️  Failed to ensure performance index %s: %v", index.name, err)
			continue
		}
		ensuredCount++
	}

	log.Printf("✅ Ensured %d performance indexes", ensuredCount)
}

func MigrateAttendancePinCompatibility() {
	if DB == nil {
		return
	}

	statements := []string{
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS auto_rotate_pin boolean NOT NULL DEFAULT true`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS previous_pin_code varchar(50) NOT NULL DEFAULT ''`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS pin_issued_at timestamptz`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS pin_grace_until timestamptz`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS pin_rotates_at timestamptz`,
		`UPDATE attendance_sessions
		 SET previous_pin_code = COALESCE(previous_pin_code, '')
		 WHERE previous_pin_code IS NULL`,
		`UPDATE attendance_sessions
		 SET pin_issued_at = COALESCE(pin_issued_at, start_time),
		     pin_rotates_at = COALESCE(pin_rotates_at, start_time + INTERVAL '1 minute')
		 WHERE pin_code <> ''
		   AND start_time <= NOW()
		   AND end_time > NOW()`,
		`UPDATE attendance_sessions
		 SET pin_code = '',
		     previous_pin_code = '',
		     pin_issued_at = NULL,
		     pin_grace_until = NULL,
		     pin_rotates_at = NULL
		 WHERE end_time <= NOW()
		    OR (status = 'closed' AND (pin_code <> '' OR previous_pin_code <> ''))`,
	}

	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			log.Printf("⚠️  Failed attendance PIN compatibility migration: %v", err)
			return
		}
	}

	log.Println("✅ Attendance PIN compatibility synchronized")
}
func MigrateAttendanceRealtimeCompatibility() {
	if DB == nil {
		return
	}

	statements := []string{
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS pin_mode varchar(20) NOT NULL DEFAULT 'rotating'`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS pin_hash char(64) NOT NULL DEFAULT ''`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS current_pin_hash char(64) NOT NULL DEFAULT ''`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS previous_pin_hash char(64) NOT NULL DEFAULT ''`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS started_at timestamptz`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS expires_at timestamptz`,
		`ALTER TABLE attendance_sessions ADD COLUMN IF NOT EXISTS closed_at timestamptz`,
		`CREATE TABLE IF NOT EXISTS attendance_pin_histories (
			id bigserial PRIMARY KEY,
			session_id bigint NOT NULL,
			pin_hash char(64) NOT NULL,
			valid_from timestamptz NOT NULL,
			valid_until timestamptz NOT NULL,
			reason varchar(32) NOT NULL,
			created_at timestamptz NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attendance_pin_histories_session_id ON attendance_pin_histories (session_id)`,
		`UPDATE attendance_sessions
		 SET pin_mode = CASE
		     WHEN auto_rotate_pin = false THEN 'static'
		     ELSE 'rotating'
		 END
		 WHERE COALESCE(pin_mode, '') = ''`,
		`UPDATE attendance_sessions
		 SET started_at = COALESCE(started_at, CASE WHEN status = 'active' THEN start_time ELSE NULL END),
		     expires_at = COALESCE(expires_at, CASE WHEN status = 'active' THEN end_time ELSE NULL END),
		     closed_at = COALESCE(closed_at, CASE WHEN status = 'closed' THEN end_time ELSE NULL END)`,
	}

	for _, statement := range statements {
		if err := DB.Exec(statement).Error; err != nil {
			log.Printf("âš ï¸  Failed attendance realtime compatibility migration: %v", err)
			return
		}
	}

	log.Println("âœ… Attendance realtime compatibility synchronized")
}

// MigrateAutovacuumSettings tightens autovacuum for the high-churn tables.
//
// PostgreSQL's default autovacuum_vacuum_scale_factor is 0.2 — vacuum waits
// until dead tuples reach 20% of the table. That is a moving target: the bigger
// a table gets, the longer it goes between vacuums, so bloat on exactly the
// busiest tables compounds over months. These per-table overrides pin the
// threshold to a small fraction so vacuum keeps up as the tables grow.
//
// Idempotent, and a failure on one table only logs and continues — a missing
// table (fresh install, feature not yet migrated) must not block startup.
func MigrateAutovacuumSettings() {
	if DB == nil {
		return
	}

	// Log/append tables: high insert rate, deleted in bulk by the retention
	// worker, so they need aggressive vacuuming to reclaim the space.
	appendOnlyTables := []string{
		"system_logs",
		"course_activity_logs",
		"attendance_pin_histories",
		"notification_logs",
		"attendance_display_audit_logs",
		"user_notifications",
	}

	// Hot transactional tables: every UPDATE leaves a dead tuple, and these are
	// updated constantly during a class session.
	hotTables := []string{
		"queue_bookings",
		"queue_desk_statuses",
		"attendance_records",
		"attendance_sessions",
		"queue_sessions",
		"scores",
	}

	tunedCount := 0
	apply := func(tables []string, vacuumScale, analyzeScale string) {
		for _, table := range tables {
			statement := fmt.Sprintf(
				`ALTER TABLE %s SET (
					autovacuum_vacuum_scale_factor = %s,
					autovacuum_analyze_scale_factor = %s,
					autovacuum_vacuum_threshold = 1000,
					autovacuum_analyze_threshold = 1000
				)`,
				table, vacuumScale, analyzeScale,
			)
			if err := DB.Exec(statement).Error; err != nil {
				log.Printf("⚠️  Failed to tune autovacuum for %s: %v", table, err)
				continue
			}
			tunedCount++
		}
	}

	apply(appendOnlyTables, "0.02", "0.01")
	apply(hotTables, "0.05", "0.02")

	log.Printf("✅ Tuned autovacuum for %d table(s)", tunedCount)
}

// MigrateCloseStaleAttendanceSessions closes attendance sessions that are still
// marked active long after their scheduled end.
//
// These accumulate whenever a session fails to close cleanly (server restarted
// mid-session, Redis unavailable at the moment of closing, an error in the
// close path). Nothing ever cleaned them up, and because the PIN lifecycle
// worker selects every row WHERE status = 'active', each stuck row was
// reprocessed on every single tick — permanently, and the set only ever grew.
//
// The runtime close path is skipped on purpose: it exists to release Redis PIN
// state, and for sessions this old the Redis keys expired long ago. The PIN
// columns are cleared here so no stale code can be matched against.
func MigrateCloseStaleAttendanceSessions() {
	if DB == nil {
		return
	}

	// A full day past end_time — comfortably beyond any legitimately running
	// session, so this can never close one that is actually in use.
	const staleGrace = "24 hours"

	result := DB.Exec(`
		UPDATE attendance_sessions
		SET status = 'closed',
		    closed_at = COALESCE(closed_at, end_time),
		    pin_code = '',
		    previous_pin_code = '',
		    current_pin_hash = '',
		    previous_pin_hash = '',
		    pin_issued_at = NULL,
		    pin_grace_until = NULL,
		    pin_rotates_at = NULL
		WHERE status = 'active'
		  AND end_time < NOW() - INTERVAL '` + staleGrace + `'`)

	if result.Error != nil {
		log.Printf("⚠️  Failed to close stale attendance sessions: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		log.Printf("🧹 Closed %d stale attendance session(s) left active past their end time", result.RowsAffected)
	}
}

// newGormLogger configures GORM's query logger.
//
// The deployment has no pg_stat_statements, so slow-query logging is currently
// the only way to find out which queries are actually costing time. Anything
// over the threshold is logged at warn level with its duration.
//
// ParameterizedQueries is on deliberately: it logs `WHERE email = $1` instead
// of interpolating the real value. Query arguments here routinely contain
// student identifiers and emails, and those must not end up sitting in the
// container's log stream.
func newGormLogger() gormlogger.Interface {
	return gormlogger.New(
		log.New(os.Stdout, "", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold: slowQueryThreshold(),
			LogLevel:      gormlogger.Warn,
			// First()/Take() misses are a normal control-flow signal all over
			// this codebase; logging them buries the real warnings.
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		},
	)
}

// DBPoolStats exposes connection-pool utilisation for the Prometheus gauges.
// A pool sitting at max open connections with requests queueing on WaitCount is
// the difference between "a query is slow" and "there was no connection free to
// run it on" — two problems that feel identical from the browser.
func DBPoolStats() (inUse int, idle int, open int, waitCount int64) {
	if DB == nil {
		return 0, 0, 0, 0
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return 0, 0, 0, 0
	}

	stats := sqlDB.Stats()
	return stats.InUse, stats.Idle, stats.OpenConnections, stats.WaitCount
}

// EnablePreparedStatements switches DB over to a prepared-statement session.
//
// Call this once, after every migration has finished. Prepared statements are a
// straight win on the request path — PostgreSQL stops re-parsing and re-planning
// the same query text on every call — but the migrations run ALTER TABLE,
// CREATE INDEX and other utility statements, and those are not worth routing
// through the extended-query prepare path at startup for a one-time execution.
//
// Bounds come from PrepareStmtMaxSize/PrepareStmtTTL on the original config, so
// the cache is still a TTL'd LRU rather than an unbounded map.
func EnablePreparedStatements() {
	if DB == nil {
		return
	}

	DB = DB.Session(&gorm.Session{PrepareStmt: true})
	log.Printf("✅ Prepared statement cache enabled (max=%d, ttl=%s)",
		DB.PrepareStmtMaxSize, DB.PrepareStmtTTL)
}

// slowQueryThreshold is the duration above which GORM logs a query as slow.
// 200ms is GORM's own default and a reasonable bar for this workload: fast
// enough to catch real problems, slow enough not to flood the log with normal
// queries.
func slowQueryThreshold() time.Duration {
	return time.Duration(getEnvInt("DB_SLOW_QUERY_THRESHOLD_MS", 200)) * time.Millisecond
}

// MigratePgStatStatements enables the pg_stat_statements extension.
//
// Two separate things are required and it is easy to do only one: the library
// must be preloaded at server start (shared_preload_libraries, set in
// docker-compose.yml) AND the extension must be created inside the database.
// Creating it without the preload succeeds here but leaves every query against
// the view failing, so this reports which of the two is missing rather than
// silently appearing to work.
//
// Never fatal: query statistics are a diagnostic, and losing them must not stop
// the API from serving. A deployment whose database user lacks the privilege to
// create extensions simply logs and carries on.
func MigratePgStatStatements() {
	if DB == nil {
		return
	}

	// The extension can exist in the catalog while the library is not loaded,
	// which is the confusing half-configured state worth calling out explicitly.
	var preloadedLibraries string
	if err := DB.Raw("SHOW shared_preload_libraries").Scan(&preloadedLibraries).Error; err != nil {
		log.Printf("⚠️  Could not read shared_preload_libraries: %v", err)
		return
	}

	if !strings.Contains(preloadedLibraries, "pg_stat_statements") {
		log.Println("ℹ️  pg_stat_statements is not in shared_preload_libraries — query statistics are unavailable")
		log.Println("   Add it to the db service command in docker-compose.yml, then recreate the container (a reload will not apply it)")
		return
	}

	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS pg_stat_statements").Error; err != nil {
		log.Printf("⚠️  pg_stat_statements is preloaded but the extension could not be created: %v", err)
		log.Println("   Creating an extension requires a superuser; run it manually as the database owner if needed")
		return
	}

	log.Println("✅ pg_stat_statements enabled — inspect with: SELECT query, calls, mean_exec_time FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20")
}
