package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// =============================================================================
// 1. ระบบจัดการผู้ใช้และความปลอดภัย
// =============================================================================

type User struct {
	ID                   uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Username             string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"username"`
	PasswordHash         string         `gorm:"type:char(60);not null" json:"-"`
	Role                 string         `gorm:"type:varchar(20);not null;index" json:"role"` // admin, instructor, ta
	FullName             string         `gorm:"type:varchar(255)" json:"full_name"`
	Email                string         `gorm:"type:varchar(255)" json:"email"`
	Provider             string         `gorm:"type:varchar(20);default:'local'" json:"provider"` // local, google
	GoogleID             string         `gorm:"type:varchar(255)" json:"google_id"`
	Avatar               string         `gorm:"type:text" json:"avatar"`
	IsActive             bool           `gorm:"type:boolean;default:true" json:"is_active"`
	MustChangePassword   bool           `gorm:"type:boolean;default:false" json:"must_change_password"`
	TwoFactorEnabled     bool           `gorm:"type:boolean;default:false" json:"two_factor_enabled"`
	TwoFactorMethod      string         `gorm:"type:varchar(20)" json:"two_factor_method"`
	TwoFactorSecret      string         `gorm:"type:text" json:"-"`
	TwoFactorBackupCodes datatypes.JSON `gorm:"type:jsonb" json:"-"`
	TwoFactorConfirmedAt *time.Time     `gorm:"type:timestamptz" json:"two_factor_confirmed_at,omitempty"`
	CreatedAt            time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt            time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

type UserOAuthAccount struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         uint       `gorm:"not null;index" json:"user_id"`
	Provider       string     `gorm:"type:varchar(50);not null" json:"provider"` // google, github, apple
	ProviderUserID string     `gorm:"type:varchar(255);not null" json:"provider_user_id"`
	ProviderEmail  string     `gorm:"type:varchar(255)" json:"provider_email"`
	ProviderAvatar string     `gorm:"type:varchar(500)" json:"provider_avatar"`
	ProviderName   string     `gorm:"type:varchar(255)" json:"provider_name"`
	AccessToken    string     `gorm:"type:text" json:"-"`
	RefreshToken   string     `gorm:"type:text" json:"-"`
	TokenExpiresAt *time.Time `gorm:"type:timestamptz" json:"token_expires_at,omitempty"`
	LinkedAt       time.Time  `gorm:"type:timestamptz" json:"linked_at"`
	CreatedAt      time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type RefreshToken struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	JTI       string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"jti"`
	UserID    uint           `gorm:"not null;index" json:"user_id"`
	Revoked   bool           `gorm:"type:boolean;default:false" json:"revoked"`
	Meta      datatypes.JSON `gorm:"type:jsonb" json:"meta,omitempty"`
	CreatedAt time.Time      `gorm:"type:timestamptz" json:"created_at"`
	ExpiresAt time.Time      `gorm:"type:timestamptz;not null" json:"expires_at"`
}

type PasswordResetToken struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint       `gorm:"not null;index" json:"user_id"`
	Token     string     `gorm:"type:varchar(255);not null" json:"token"`
	ExpiresAt time.Time  `gorm:"type:timestamptz;not null" json:"expires_at"`
	UsedAt    *time.Time `gorm:"type:timestamptz" json:"used_at,omitempty"`
	CreatedAt time.Time  `gorm:"type:timestamptz" json:"created_at"`
}

type TwoFactorPending struct {
	ID                 uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID             uint       `gorm:"not null;index" json:"user_id"`
	Method             string     `gorm:"type:varchar(20);not null" json:"method"`
	Secret             string     `gorm:"type:text;not null" json:"-"`
	EmailCode          string     `gorm:"type:varchar(6)" json:"-"`
	EmailCodeExpiresAt *time.Time `gorm:"type:timestamptz" json:"email_code_expires_at,omitempty"`
	CreatedAt          time.Time  `gorm:"type:timestamptz" json:"created_at"`
	ExpiresAt          time.Time  `gorm:"type:timestamptz" json:"expires_at"`
}

// =============================================================================
// 2. ระบบจัดการห้องเรียนและผังที่นั่ง
// =============================================================================

type Classroom struct {
	ID          string    `gorm:"primaryKey;type:varchar(21)" json:"id"` // NanoID
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	Building    string    `gorm:"type:varchar(100);not null" json:"building"`
	Floor       string    `gorm:"type:varchar(20);not null" json:"floor"`
	Description string    `gorm:"type:text" json:"description"`
	IsDeleted   bool      `gorm:"type:boolean;default:false" json:"is_deleted"`
	IsActive    bool      `gorm:"type:boolean;default:true" json:"is_active"`
	CreatedBy   *uint     `gorm:"index" json:"created_by,omitempty"`
	CreatedAt   time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type Zone struct {
	ID          string    `gorm:"primaryKey;type:varchar(21)" json:"id"` // NanoID
	ClassroomID string    `gorm:"type:varchar(21);not null;index" json:"classroom_id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	X           int       `gorm:"default:0" json:"x"`
	Y           int       `gorm:"default:0" json:"y"`
	Width       int       `gorm:"default:400" json:"width"`
	Height      int       `gorm:"default:300" json:"height"`
	Color       string    `gorm:"type:varchar(30);default:'rgba(99,102,241,0.15)'" json:"color"`
	CreatedAt   time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type Desk struct {
	ID          string    `gorm:"primaryKey;type:varchar(21)" json:"id"` // NanoID
	ClassroomID string    `gorm:"type:varchar(21);not null;index" json:"classroom_id"`
	Number      int       `gorm:"not null" json:"number"`
	X           int       `gorm:"default:0" json:"x"`
	Y           int       `gorm:"default:0" json:"y"`
	Type        string    `gorm:"type:varchar(20);default:'normal'" json:"type"` // computer, normal, teacher
	IsEnabled   bool      `gorm:"type:boolean;default:true" json:"is_enabled"`
	CreatedAt   time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 3. ระบบจัดการรายวิชา
// =============================================================================

type Course struct {
	ID                 string    `gorm:"primaryKey;type:varchar(21)" json:"id"` // NanoID
	Code               string    `gorm:"type:varchar(100);not null;index" json:"code"`
	Name               string    `gorm:"type:varchar(255);not null" json:"name"`
	Year               int       `gorm:"type:smallint;not null" json:"year"`
	Semester           int       `gorm:"type:smallint;not null" json:"semester"`
	InstructorID       *uint     `gorm:"index" json:"instructor_id,omitempty"`
	Instructor         *User     `gorm:"foreignKey:InstructorID" json:"-"`
	Description        string    `gorm:"type:text" json:"description"`
	Image              string    `gorm:"type:text" json:"image"`
	IsActive           bool      `gorm:"type:boolean;default:true" json:"is_active"`
	AttentionThreshold int       `gorm:"default:60" json:"attention_threshold"`
	CreatedAt          time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// CourseMember เป็น generalized model สำหรับสมาชิกในวิชา (role: student/ta/instructor)
type CourseMember struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID  string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	Role      string    `gorm:"type:varchar(20);default:'student'" json:"role"` // student, ta, instructor
	Status    string    `gorm:"type:varchar(20);default:'active'" json:"status"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type CourseInstructor struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID    string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	IsPrimary   bool      `gorm:"type:boolean;default:false" json:"is_primary"`
	Permissions string    `gorm:"type:text" json:"permissions"`
	AssignedAt  time.Time `gorm:"type:timestamptz" json:"assigned_at"`
}

type CourseTA struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID    string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	Permissions string    `gorm:"type:text" json:"permissions"`
	AssignedAt  time.Time `gorm:"type:timestamptz" json:"assigned_at"`
}

func (CourseTA) TableName() string { return "course_tas" }

type CourseSection struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID  string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	SectionNo string    `gorm:"type:varchar(50);not null" json:"section_no"`
	Note      string    `gorm:"type:varchar(255)" json:"note"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type CourseSectionStudent struct {
	ID              uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseSectionID uint      `gorm:"not null;index" json:"course_section_id"`
	StudentID       uint      `gorm:"not null;index" json:"student_id"`
	EnrolledAt      time.Time `gorm:"type:timestamptz" json:"enrolled_at"`
}

type CourseSectionStudentRemoval struct {
	ID              uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID        string     `gorm:"type:varchar(21);not null;index" json:"course_id"`
	CourseSectionID uint       `gorm:"not null;index" json:"course_section_id"`
	StudentID       uint       `gorm:"not null;index" json:"student_id"`
	RemovedBy       *uint      `gorm:"index" json:"removed_by,omitempty"`
	RemovedAt       time.Time  `gorm:"type:timestamptz;not null" json:"removed_at"`
	RestoreUntil    time.Time  `gorm:"type:timestamptz;not null;index" json:"restore_until"`
	RestoredAt      *time.Time `gorm:"type:timestamptz" json:"restored_at,omitempty"`
}

type CourseActivityLog struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID    string         `gorm:"type:varchar(100);not null;index" json:"course_id"`
	ActorUserID uint           `gorm:"not null;index" json:"actor_user_id"`
	Action      string         `gorm:"type:varchar(50);not null" json:"action"`
	Category    string         `gorm:"type:varchar(30);default:'general'" json:"category"`
	TargetType  string         `gorm:"type:varchar(50)" json:"target_type"`
	TargetID    string         `gorm:"type:varchar(100)" json:"target_id"`
	TargetName  string         `gorm:"type:varchar(255)" json:"target_name"`
	Detail      datatypes.JSON `gorm:"type:jsonb" json:"detail,omitempty"`
	CreatedAt   time.Time      `gorm:"type:timestamptz" json:"created_at"`
}

// =============================================================================
// 4. ระบบนักศึกษา (แยกจาก users)
// =============================================================================

type Student struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	StudentID string         `gorm:"type:varchar(11);uniqueIndex;not null" json:"student_id"` // รหัสนักศึกษา
	FullName  string         `gorm:"type:varchar(255);not null" json:"full_name"`
	Email     string         `gorm:"type:varchar(255)" json:"email"`
	Extra     datatypes.JSON `gorm:"type:jsonb" json:"extra,omitempty"`
	IsActive  bool           `gorm:"type:boolean;default:true" json:"is_active"`
	CreatedAt time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type StudentGroup struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID   string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	Name       string    `gorm:"type:varchar(255);not null" json:"name"`
	GroupType  string    `gorm:"type:varchar(20);default:'permanent'" json:"group_type"` // permanent, temporary
	WeekNumber *int      `gorm:"" json:"week_number,omitempty"`
	CreatedAt  time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type StudentGroupMember struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID   uint      `gorm:"not null;index" json:"group_id"`
	StudentID uint      `gorm:"not null;index" json:"student_id"`
	JoinedAt  time.Time `gorm:"type:timestamptz" json:"joined_at"`
}

// =============================================================================
// 5. ระบบงานมอบหมายและคะแนน
// =============================================================================

type Assignment struct {
	ID                        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID                  string     `gorm:"type:varchar(21);not null;index" json:"course_id"`
	Name                      string     `gorm:"type:varchar(255);not null" json:"name"`
	Description               string     `gorm:"type:text" json:"description"`
	AssignmentType            string     `gorm:"type:varchar(50);default:'individual'" json:"assignment_type"` // individual, permanent_group, weekly_group, assignment
	WeekNumber                *int       `gorm:"" json:"week_number,omitempty"`
	LinkedAttendanceSessionID *uint      `gorm:"index" json:"linked_attendance_session_id,omitempty"`
	AttendanceCondition       string     `gorm:"type:varchar(10);default:'or'" json:"attendance_condition"` // and, or
	MaxScore                  float64    `gorm:"type:decimal(5,2);default:10" json:"max_score"`
	DueDate                   *time.Time `gorm:"type:timestamptz" json:"due_date,omitempty"`
	IsActive                  bool       `gorm:"type:boolean;default:true" json:"is_active"`
	IsScoreVisible            bool       `gorm:"type:boolean;default:true" json:"is_score_visible"`
	CreatedBy                 uint       `gorm:"not null;index" json:"created_by"`
	OrderIndex                int        `gorm:"default:0" json:"order_index"`
	CreatedAt                 time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt                 time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type AssignmentAttendanceLink struct {
	ID                  uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AssignmentID        uint      `gorm:"not null;index" json:"assignment_id"`
	AttendanceSessionID uint      `gorm:"not null;index" json:"attendance_session_id"`
	CreatedAt           time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type AssignmentSubItem struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AssignmentID uint      `gorm:"not null;index" json:"assignment_id"`
	Name         string    `gorm:"type:varchar(255);not null" json:"name"`
	MaxScore     float64   `gorm:"type:decimal(10,2);default:10" json:"max_score"`
	OrderIndex   int       `gorm:"default:0" json:"order_index"`
	CreatedAt    time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type Score struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	AssignmentID uint       `gorm:"not null;index" json:"assignment_id"`
	StudentID    *uint      `gorm:"index" json:"student_id,omitempty"` // สำหรับงานเดี่ยว
	GroupID      *uint      `gorm:"index" json:"group_id,omitempty"`   // สำหรับงานกลุ่ม
	SubItemID    *uint      `gorm:"index" json:"sub_item_id,omitempty"`
	Score        float64    `gorm:"type:decimal(5,2)" json:"score"`
	Comment      string     `gorm:"type:text" json:"comment"`
	GradedBy     *uint      `gorm:"index" json:"graded_by,omitempty"`
	GradedAt     *time.Time `gorm:"type:timestamptz" json:"graded_at,omitempty"`
	Status       string     `gorm:"type:varchar(20);default:'pending'" json:"status"` // pending, graded
	CreatedAt    time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type ScoreEditRequest struct {
	ID            uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	ScoreID       uint           `gorm:"not null;index" json:"score_id"`
	OldScore      *float64       `gorm:"type:decimal(5,2)" json:"old_score,omitempty"`
	NewScore      float64        `gorm:"type:decimal(5,2);not null" json:"new_score"`
	Reason        string         `gorm:"type:text" json:"reason"`
	RequestedBy   uint           `gorm:"not null;index" json:"requested_by"`
	Status        string         `gorm:"type:varchar(20);default:'pending'" json:"status"` // pending, approved, rejected
	ReviewedBy    *uint          `gorm:"index" json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time     `gorm:"type:timestamptz" json:"reviewed_at,omitempty"`
	ReviewComment string         `gorm:"type:text" json:"review_comment"`
	Images        datatypes.JSON `gorm:"type:jsonb" json:"images,omitempty"`
	CreatedAt     time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type BonusScore struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID  string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	StudentID uint      `gorm:"not null;index" json:"student_id"`
	Score     float64   `gorm:"type:decimal(5,2);not null;default:1" json:"score"`
	Reason    string    `gorm:"type:varchar(255)" json:"reason"`
	GivenBy   uint      `gorm:"not null;index" json:"given_by"`
	GivenAt   time.Time `gorm:"type:timestamptz" json:"given_at"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 6. ระบบสอบ
// =============================================================================

type ExamSetting struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID  string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	ExamType  string    `gorm:"type:varchar(20);not null" json:"exam_type"` // midterm, final
	Component string    `gorm:"type:varchar(20);not null" json:"component"` // lab, lecture
	MaxScore  float64   `gorm:"type:decimal(5,2);not null;default:0" json:"max_score"`
	IsVisible bool      `gorm:"type:boolean;default:false" json:"is_visible"`
	IsActive  bool      `gorm:"type:boolean;default:false" json:"is_active"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type ExamScore struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ExamSettingID uint       `gorm:"not null;index" json:"exam_setting_id"`
	StudentID     uint       `gorm:"not null;index" json:"student_id"`
	Score         *float64   `gorm:"type:decimal(5,2)" json:"score,omitempty"`
	Comment       string     `gorm:"type:text" json:"comment"`
	GradedBy      *uint      `gorm:"index" json:"graded_by,omitempty"`
	GradedAt      *time.Time `gorm:"type:timestamptz" json:"graded_at,omitempty"`
	CreatedAt     time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 7. ระบบเช็คชื่อ
// =============================================================================

type AttendanceSession struct {
	ID                   uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID             string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	CourseSectionID      *uint     `gorm:"index" json:"course_section_id,omitempty"`
	Title                string    `gorm:"type:varchar(255);default:'Attendance'" json:"title"`
	PinCode              string    `gorm:"type:varchar(50)" json:"pin_code"`
	SessionType          string    `gorm:"type:varchar(20);default:'lecture'" json:"session_type"` // lecture, lab, online
	CheckLocation        bool      `gorm:"type:boolean;default:false" json:"check_location"`
	LocationLat          *float64  `gorm:"type:decimal(10,7)" json:"location_lat,omitempty"`
	LocationLng          *float64  `gorm:"type:decimal(10,7)" json:"location_lng,omitempty"`
	RadiusMeters         int       `gorm:"default:50" json:"radius_meters"`
	StartTime            time.Time `gorm:"type:timestamptz;not null" json:"start_time"`
	EndTime              time.Time `gorm:"type:timestamptz;not null" json:"end_time"`
	LateThresholdMinutes int       `gorm:"default:15" json:"late_threshold_minutes"`
	LateThresholdTime    string    `gorm:"type:varchar(8)" json:"late_threshold_time"`     // เวลาที่ถือว่าสาย เช่น "08:15:00"
	Status               string    `gorm:"type:varchar(20);default:'draft'" json:"status"` // draft, active, closed
	CreatedBy            *uint     `gorm:"index" json:"created_by,omitempty"`
	CreatedAt            time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type AttendanceSessionSection struct {
	ID                  uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AttendanceSessionID uint      `gorm:"not null;index" json:"attendance_session_id"`
	CourseSectionID     uint      `gorm:"not null;index" json:"course_section_id"`
	CreatedAt           time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type AttendanceRecord struct {
	ID                  uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	AttendanceSessionID uint       `gorm:"not null;index" json:"attendance_session_id"`
	StudentID           uint       `gorm:"not null;index" json:"student_id"`
	CheckInTime         *time.Time `gorm:"type:timestamptz" json:"check_in_time,omitempty"`
	Status              string     `gorm:"type:varchar(20);default:'absent'" json:"status"` // present, late, leave, absent
	GoogleEmail         string     `gorm:"type:varchar(255)" json:"google_email"`
	GoogleID            string     `gorm:"type:varchar(255)" json:"google_id"`
	PinVerified         bool       `gorm:"type:boolean;default:false" json:"pin_verified"`
	LocationVerified    bool       `gorm:"type:boolean;default:false" json:"location_verified"`
	Note                string     `gorm:"type:text" json:"note"`
	LocationLat         *float64   `gorm:"type:decimal(10,7)" json:"location_lat,omitempty"`
	LocationLng         *float64   `gorm:"type:decimal(10,7)" json:"location_lng,omitempty"`
	DistanceMeters      *int       `gorm:"" json:"distance_meters,omitempty"`
	UpdatedBy           *uint      `gorm:"index" json:"updated_by,omitempty"`
	CreatedAt           time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 8. ระบบคิว
// =============================================================================

type QueueSession struct {
	ID                        string     `gorm:"primaryKey;type:varchar(21)" json:"id"` // NanoID
	CourseID                  string     `gorm:"type:varchar(21);not null;index" json:"course_id"`
	ClassroomID               string     `gorm:"type:varchar(21);not null" json:"classroom_id"`
	Title                     string     `gorm:"type:varchar(255);not null" json:"title"`
	Description               string     `gorm:"type:text" json:"description"`
	PinCode                   string     `gorm:"type:varchar(10);not null" json:"pin_code"`
	LinkedAssignmentID        *uint      `gorm:"index" json:"linked_assignment_id,omitempty"`
	RequireAttendance         bool       `gorm:"type:boolean;default:false" json:"require_attendance"`
	LinkedAttendanceSessionID *uint      `gorm:"index" json:"linked_attendance_session_id,omitempty"`
	NextQueueNumber           int        `gorm:"not null;default:1" json:"-"`
	Status                    string     `gorm:"type:varchar(20);default:'draft'" json:"status"` // draft, active, paused, closed
	StartTime                 *time.Time `gorm:"type:timestamptz" json:"start_time,omitempty"`
	EndTime                   *time.Time `gorm:"type:timestamptz" json:"end_time,omitempty"`
	CreatedBy                 *uint      `gorm:"index" json:"created_by,omitempty"`
	CreatedAt                 time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt                 time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type QueueBooking struct {
	ID               uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	QueueSessionID   string     `gorm:"type:varchar(21);not null;index" json:"queue_session_id"`
	StudentID        uint       `gorm:"not null;index" json:"student_id"`
	DeskID           string     `gorm:"type:varchar(21);not null" json:"desk_id"`
	DeskNumber       int        `gorm:"not null" json:"desk_number"`
	BookingType      string     `gorm:"type:varchar(20);not null" json:"booking_type"` // grading, help
	QueueNumber      int        `gorm:"not null" json:"queue_number"`
	Note             string     `gorm:"type:text" json:"note"`
	Status           string     `gorm:"type:varchar(20);default:'waiting'" json:"status"` // waiting, in_progress, completed, cancelled, no_show
	AssignedWorkerID *uint      `gorm:"index" json:"assigned_worker_id,omitempty"`
	AssignedAt       *time.Time `gorm:"type:timestamptz" json:"assigned_at,omitempty"`
	StartedAt        *time.Time `gorm:"type:timestamptz" json:"started_at,omitempty"`
	CompletedAt      *time.Time `gorm:"type:timestamptz" json:"completed_at,omitempty"`
	Score            *float64   `gorm:"type:decimal(5,2)" json:"score,omitempty"`
	ScoreComment     string     `gorm:"type:text" json:"score_comment"`
	WorkerNote       string     `gorm:"type:text" json:"worker_note"`
	CreatedAt        time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type QueueDeskStatus struct {
	ID               uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	QueueSessionID   string    `gorm:"type:varchar(21);not null;index" json:"queue_session_id"`
	DeskID           string    `gorm:"type:varchar(21);not null" json:"desk_id"`
	GradingStatus    string    `gorm:"type:varchar(20);default:'not_started'" json:"grading_status"` // not_started, waiting, in_progress, completed
	GradingBookingID *uint     `gorm:"index" json:"grading_booking_id,omitempty"`
	HelpStatus       string    `gorm:"type:varchar(20);default:'none'" json:"help_status"` // none, waiting, in_progress
	HelpBookingID    *uint     `gorm:"index" json:"help_booking_id,omitempty"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type QueueWorker struct {
	ID                       uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	QueueSessionID           string     `gorm:"type:varchar(21);not null;index" json:"queue_session_id"`
	UserID                   uint       `gorm:"not null;index" json:"user_id"`
	AcceptGrading            bool       `gorm:"type:boolean;default:true" json:"accept_grading"`
	AcceptHelp               bool       `gorm:"type:boolean;default:true" json:"accept_help"`
	PushNotificationsEnabled bool       `gorm:"type:boolean;default:true" json:"push_notifications_enabled"`
	Status                   string     `gorm:"type:varchar(20);default:'offline'" json:"status"` // online, busy, offline
	CurrentBookingID         *uint      `gorm:"index" json:"current_booking_id,omitempty"`
	TotalGradingCompleted    int        `gorm:"default:0" json:"total_grading_completed"`
	TotalHelpCompleted       int        `gorm:"default:0" json:"total_help_completed"`
	LastActiveAt             *time.Time `gorm:"type:timestamptz" json:"last_active_at,omitempty"`
	CreatedAt                time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt                time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 9. ระบบแจ้งเตือน (Push Notification)
// =============================================================================

type FcmToken struct {
	ID         uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	FcmToken   string         `gorm:"type:varchar(500);not null" json:"fcm_token"`
	UserType   string         `gorm:"type:varchar(20);not null" json:"user_type"` // worker, student
	UserID     *uint          `gorm:"index" json:"user_id,omitempty"`
	StudentID  string         `gorm:"type:varchar(20)" json:"student_id"` // รหัสนักศึกษา (string)
	BookingID  *uint          `gorm:"index" json:"booking_id,omitempty"`
	SessionID  string         `gorm:"type:varchar(21)" json:"session_id"`
	DeviceInfo datatypes.JSON `gorm:"type:jsonb" json:"device_info,omitempty"`
	IsActive   bool           `gorm:"type:boolean;default:true" json:"is_active"`
	LastUsedAt *time.Time     `gorm:"type:timestamptz" json:"last_used_at,omitempty"`
	CreatedAt  time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type NotificationLog struct {
	ID               uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	FcmTokenID       *uint          `gorm:"index" json:"fcm_token_id,omitempty"`
	NotificationType string         `gorm:"type:varchar(50);not null" json:"notification_type"` // new-task, queue-ready, booking-completed, session-closed, other
	Title            string         `gorm:"type:varchar(255);not null" json:"title"`
	Body             string         `gorm:"type:text" json:"body"`
	Data             datatypes.JSON `gorm:"type:jsonb" json:"data,omitempty"`
	Status           string         `gorm:"type:varchar(20);default:'pending'" json:"status"` // pending, sent, failed, delivered
	ErrorMessage     string         `gorm:"type:text" json:"error_message"`
	SentAt           *time.Time     `gorm:"type:timestamptz" json:"sent_at,omitempty"`
	DeliveredAt      *time.Time     `gorm:"type:timestamptz" json:"delivered_at,omitempty"`
	CreatedAt        time.Time      `gorm:"type:timestamptz" json:"created_at"`
}

// =============================================================================
// 10. ระบบ Feedback
// =============================================================================

type Feedback struct {
	ID           uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       *uint          `gorm:"index" json:"user_id,omitempty"`
	Type         string         `gorm:"type:varchar(20);not null;default:'other'" json:"type"` // bug, feature, improvement, other
	Title        string         `gorm:"type:varchar(255);not null" json:"title"`
	Description  string         `gorm:"type:text;not null" json:"description"`
	Attachments  datatypes.JSON `gorm:"type:jsonb" json:"attachments,omitempty"`
	Status       string         `gorm:"type:varchar(20);default:'pending'" json:"status"`  // pending, reviewing, resolved, rejected
	Priority     string         `gorm:"type:varchar(20);default:'medium'" json:"priority"` // low, medium, high, critical
	AdminNotes   string         `gorm:"type:text" json:"admin_notes"`
	ResolvedAt   *time.Time     `gorm:"type:timestamptz" json:"resolved_at,omitempty"`
	ResolvedBy   *uint          `gorm:"index" json:"resolved_by,omitempty"`
	ContactEmail string         `gorm:"type:varchar(255)" json:"contact_email"`
	CreatedAt    time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

// =============================================================================
// 11. ระบบบันทึก Log และ Config
// =============================================================================

type SystemLog struct {
	ID             uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	LogType        string         `gorm:"type:varchar(20);index" json:"log_type"` // access, error, auth, security
	Severity       string         `gorm:"type:varchar(20)" json:"severity"`       // debug, info, warn, error, critical
	ActorUserID    *uint          `gorm:"index" json:"actor_user_id,omitempty"`
	SessionID      string         `gorm:"type:varchar(128)" json:"session_id"`
	AuthMethod     string         `gorm:"type:varchar(50)" json:"auth_method"`
	Action         string         `gorm:"type:varchar(255);not null" json:"action"`
	HTTPMethod     string         `gorm:"type:varchar(10)" json:"http_method"`
	URL            string         `gorm:"type:varchar(2048)" json:"url"`
	QueryParams    datatypes.JSON `gorm:"type:jsonb" json:"query_params,omitempty"`
	StatusCode     *int           `gorm:"" json:"status_code,omitempty"`
	ResponseTimeMs *int           `gorm:"" json:"response_time_ms,omitempty"`
	Detail         datatypes.JSON `gorm:"type:jsonb" json:"detail,omitempty"`
	ErrorMessage   string         `gorm:"type:text" json:"error_message"`
	ErrorStack     string         `gorm:"type:text" json:"error_stack"`
	ErrorCode      string         `gorm:"type:varchar(50)" json:"error_code"`
	ResourceType   string         `gorm:"type:varchar(100)" json:"resource_type"`
	ResourceID     string         `gorm:"type:varchar(255)" json:"resource_id"`
	RequestBody    datatypes.JSON `gorm:"type:jsonb" json:"request_body,omitempty"`
	RequestSize    *int           `gorm:"" json:"request_size,omitempty"`
	ResponseSize   *int           `gorm:"" json:"response_size,omitempty"`
	IPAddress      string         `gorm:"type:varchar(64)" json:"ip_address"`
	UserAgent      string         `gorm:"type:varchar(512)" json:"user_agent"`
	Referer        string         `gorm:"type:varchar(2048)" json:"referer"`
	DeviceType     string         `gorm:"type:varchar(50)" json:"device_type"`
	Browser        string         `gorm:"type:varchar(100)" json:"browser"`
	OS             string         `gorm:"type:varchar(100)" json:"os"`
	CreatedAt      time.Time      `gorm:"type:timestamptz" json:"created_at"`
}

type AppConfig struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
}
