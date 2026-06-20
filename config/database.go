package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
			name: "user_notifications_user_is_read",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_notifications_user_is_read ON user_notifications (user_id, is_read) WHERE is_read = false`,
		},
		{
			name: "fcm_tokens_session_type",
			sql:  `CREATE INDEX IF NOT EXISTS idx_fcm_tokens_session_type ON fcm_tokens (session_id, user_type)`,
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
