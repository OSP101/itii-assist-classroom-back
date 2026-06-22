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
	ThemePreference      string         `gorm:"type:varchar(10);default:'system'" json:"theme_preference"`
	FontSizePreference   string         `gorm:"type:varchar(10);default:'md'" json:"font_size_preference"`
	LanguagePreference   string         `gorm:"type:varchar(10);default:'th'" json:"language_preference"`
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
	Kind      string         `gorm:"type:varchar(1);not null;default:'u';index" json:"kind"` // "u" = user, "s" = student
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
	Hostname    string    `gorm:"type:varchar(100);default:''" json:"hostname"`
	IPAddress   string    `gorm:"type:varchar(50);default:''" json:"ip_address"`
	Brand       string    `gorm:"type:varchar(50);default:''" json:"brand"`
	OS          string    `gorm:"type:varchar(50);default:''" json:"os"`
	Notes       string    `gorm:"type:varchar(500);default:''" json:"notes"`
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
	CoverPositionX     float64   `gorm:"type:double precision;default:50" json:"cover_position_x"`
	CoverPositionY     float64   `gorm:"type:double precision;default:50" json:"cover_position_y"`
	CoverZoom          float64   `gorm:"type:double precision;default:1" json:"cover_zoom"`
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
	ActorEmail  string         `gorm:"type:varchar(255)" json:"actor_email"` // snapshot at event time
	ActorRole   string         `gorm:"type:varchar(30)" json:"actor_role"`   // snapshot at event time
	Action      string         `gorm:"type:varchar(100);not null;index" json:"action"`
	Category    string         `gorm:"type:varchar(30);default:'general'" json:"category"`
	TargetType  string         `gorm:"type:varchar(50)" json:"target_type"`
	TargetID    string         `gorm:"type:varchar(100)" json:"target_id"`
	TargetName  string         `gorm:"type:varchar(255)" json:"target_name"`
	Description string         `gorm:"type:text" json:"description"`
	Detail      datatypes.JSON `gorm:"type:jsonb" json:"detail,omitempty"`
	RequestID   string         `gorm:"type:varchar(128);index" json:"request_id"`
	IPAddress   string         `gorm:"type:varchar(45)" json:"ip_address"`  // IPv4 or IPv6
	UserAgent   string         `gorm:"type:varchar(512)" json:"user_agent"` // device/browser
	CreatedAt   time.Time      `gorm:"type:timestamptz;index" json:"created_at"`
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
	IsDraft                   bool       `gorm:"type:boolean;default:false" json:"is_draft"`
	PublishAt                 *time.Time `gorm:"type:timestamptz" json:"publish_at,omitempty"`
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
// 6.5 ระบบที่นั่งสอบ
// =============================================================================

type ExamSession struct {
	ID              uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID        string    `gorm:"type:varchar(21);not null;index" json:"course_id"`
	ExamSettingID   uint      `gorm:"not null;index" json:"exam_setting_id"`
	ExamDate        time.Time `gorm:"type:timestamptz;not null" json:"exam_date"`
	StartTime       string    `gorm:"type:varchar(8);not null" json:"start_time"` // e.g. "17:00"
	EndTime         string    `gorm:"type:varchar(8);not null" json:"end_time"`   // e.g. "19:00"
	Notes           string    `gorm:"type:text" json:"notes"`
	SeatNumberStart int       `gorm:"default:1" json:"seat_number_start"`
	SeatNumberStep  int       `gorm:"default:1" json:"seat_number_step"`
	CreatedAt       time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`

	ExamSetting ExamSetting       `gorm:"foreignKey:ExamSettingID" json:"exam_setting,omitempty"`
	Rooms       []ExamSessionRoom `gorm:"foreignKey:ExamSessionID" json:"rooms,omitempty"`
	Seats       []ExamSeat        `gorm:"foreignKey:ExamSessionID" json:"seats,omitempty"`
}

type ExamSessionRoom struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ExamSessionID uint      `gorm:"not null;index" json:"exam_session_id"`
	ClassroomID   string    `gorm:"type:varchar(21);not null;index" json:"classroom_id"`
	SortOrder     int       `gorm:"default:0" json:"sort_order"`
	CreatedAt     time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`

	Classroom Classroom `gorm:"foreignKey:ClassroomID;references:ID" json:"classroom,omitempty"`
}

type ExamSeat struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ExamSessionID uint      `gorm:"not null;index" json:"exam_session_id"`
	StudentRefID  uint      `gorm:"column:student_id;not null;index" json:"student_id"`
	DeskID        string    `gorm:"type:varchar(21);not null" json:"desk_id"`
	SeatNumber    int       `gorm:"default:0" json:"seat_number"`
	CreatedAt     time.Time `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`

	Student Student `gorm:"foreignKey:StudentRefID;references:ID" json:"student,omitempty"`
	Desk    Desk    `gorm:"foreignKey:DeskID;references:ID" json:"desk,omitempty"`
}

// =============================================================================
// 7. ระบบเช็คชื่อ
// =============================================================================

type AttendanceSession struct {
	ID                   uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CourseID             string     `gorm:"type:varchar(21);not null;index" json:"course_id"`
	CourseSectionID      *uint      `gorm:"index" json:"course_section_id,omitempty"`
	Title                string     `gorm:"type:varchar(255);default:'Attendance'" json:"title"`
	AutoRotatePin        *bool      `gorm:"type:boolean;default:true" json:"auto_rotate_pin"`
	PinMode              string     `gorm:"type:varchar(20);default:'rotating'" json:"pin_mode"`
	PinCode              string     `gorm:"type:varchar(50)" json:"pin_code"`
	PreviousPinCode      string     `gorm:"type:varchar(50)" json:"previous_pin_code"`
	PinHash              string     `gorm:"type:char(64)" json:"-"`
	CurrentPinHash       string     `gorm:"type:char(64)" json:"-"`
	PreviousPinHash      string     `gorm:"type:char(64)" json:"-"`
	SessionType          string     `gorm:"type:varchar(20);default:'lecture'" json:"session_type"` // lecture, lab, online
	CheckLocation        bool       `gorm:"type:boolean;default:false" json:"check_location"`
	LocationLat          *float64   `gorm:"type:decimal(10,7)" json:"location_lat,omitempty"`
	LocationLng          *float64   `gorm:"type:decimal(10,7)" json:"location_lng,omitempty"`
	RadiusMeters         int        `gorm:"default:50" json:"radius_meters"`
	StartTime            time.Time  `gorm:"type:timestamptz;not null" json:"start_time"`
	EndTime              time.Time  `gorm:"type:timestamptz;not null" json:"end_time"`
	StartedAt            *time.Time `gorm:"type:timestamptz" json:"started_at,omitempty"`
	ExpiresAt            *time.Time `gorm:"type:timestamptz" json:"expires_at,omitempty"`
	ClosedAt             *time.Time `gorm:"type:timestamptz" json:"closed_at,omitempty"`
	PinIssuedAt          *time.Time `gorm:"type:timestamptz" json:"pin_issued_at,omitempty"`
	PinGraceUntil        *time.Time `gorm:"type:timestamptz" json:"pin_grace_until,omitempty"`
	PinRotatesAt         *time.Time `gorm:"type:timestamptz" json:"pin_rotates_at,omitempty"`
	LateThresholdMinutes int        `gorm:"default:15" json:"late_threshold_minutes"`
	LateThresholdTime    string     `gorm:"type:varchar(8)" json:"late_threshold_time"`     // เวลาที่ถือว่าสาย เช่น "08:15:00"
	Status               string     `gorm:"type:varchar(20);default:'draft'" json:"status"` // draft, active, closed
	CreatedBy            *uint      `gorm:"index" json:"created_by,omitempty"`
	CreatedAt            time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
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

type AttendancePinHistory struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID  uint      `gorm:"not null;index" json:"session_id"`
	PinHash    string    `gorm:"type:char(64);not null" json:"-"`
	ValidFrom  time.Time `gorm:"type:timestamptz;not null" json:"valid_from"`
	ValidUntil time.Time `gorm:"type:timestamptz;not null" json:"valid_until"`
	Reason     string    `gorm:"type:varchar(32);not null" json:"reason"`
	CreatedAt  time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type AttendanceDisplayDevice struct {
	ID                  string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	BootstrapSecretHash string     `gorm:"type:char(64);uniqueIndex;not null" json:"-"`
	Status              string     `gorm:"type:varchar(20);default:'bootstrapped';index" json:"status"`
	LastIPHash          string     `gorm:"type:char(64)" json:"-"`
	LastUserAgent       string     `gorm:"type:text" json:"last_user_agent"`
	CreatedAt           time.Time  `gorm:"type:timestamptz" json:"created_at"`
	LastSeenAt          *time.Time `gorm:"type:timestamptz" json:"last_seen_at,omitempty"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type AttendanceDisplayPairing struct {
	ID                   string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	DisplayDeviceID      string     `gorm:"type:varchar(64);not null;index" json:"display_device_id"`
	PairingTokenHash     string     `gorm:"type:char(64);uniqueIndex;not null" json:"-"`
	VerificationCodeHash string     `gorm:"type:char(64)" json:"-"`
	CourseID             string     `gorm:"type:varchar(21);index" json:"course_id"`
	AttendanceSessionID  *uint      `gorm:"index" json:"attendance_session_id,omitempty"`
	ApprovedByUserID     *uint      `gorm:"index" json:"approved_by_user_id,omitempty"`
	Status               string     `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	ExpiresAt            time.Time  `gorm:"type:timestamptz;index" json:"expires_at"`
	ClaimedAt            *time.Time `gorm:"type:timestamptz" json:"claimed_at,omitempty"`
	ConfirmedAt          *time.Time `gorm:"type:timestamptz" json:"confirmed_at,omitempty"`
	AttemptCount         int        `gorm:"default:0" json:"attempt_count"`
	MaxAttempts          int        `gorm:"default:5" json:"max_attempts"`
	CreatedAt            time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type AttendanceDisplayGrant struct {
	ID                  string     `gorm:"primaryKey;type:varchar(64)" json:"id"`
	DisplayDeviceID     string     `gorm:"type:varchar(64);not null;index" json:"display_device_id"`
	AttendanceSessionID uint       `gorm:"not null;index" json:"attendance_session_id"`
	CourseID            string     `gorm:"type:varchar(21);not null;index" json:"course_id"`
	GrantedByUserID     uint       `gorm:"not null;index" json:"granted_by_user_id"`
	Scope               string     `gorm:"type:varchar(255);not null" json:"scope"`
	SessionSecretHash   string     `gorm:"type:char(64);uniqueIndex;not null" json:"-"`
	Status              string     `gorm:"type:varchar(20);default:'active';index" json:"status"`
	ExpiresAt           time.Time  `gorm:"type:timestamptz;index" json:"expires_at"`
	LastSeenAt          *time.Time `gorm:"type:timestamptz" json:"last_seen_at,omitempty"`
	RevokedAt           *time.Time `gorm:"type:timestamptz" json:"revoked_at,omitempty"`
	RevokeReason        string     `gorm:"type:varchar(255)" json:"revoke_reason"`
	CreatedAt           time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type AttendanceDisplayAuditLog struct {
	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	DisplayDeviceID string         `gorm:"type:varchar(64);index" json:"display_device_id"`
	PairingID       string         `gorm:"type:varchar(64);index" json:"pairing_id"`
	GrantID         string         `gorm:"type:varchar(64);index" json:"grant_id"`
	EventType       string         `gorm:"type:varchar(50);not null;index" json:"event_type"`
	ActorUserID     *uint          `gorm:"index" json:"actor_user_id,omitempty"`
	IPHash          string         `gorm:"type:char(64)" json:"-"`
	UserAgent       string         `gorm:"type:text" json:"user_agent"`
	Metadata        datatypes.JSON `gorm:"type:jsonb" json:"metadata,omitempty"`
	CreatedAt       time.Time      `gorm:"type:timestamptz" json:"created_at"`
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
	IsCutoffEnabled           bool       `gorm:"type:boolean;default:false" json:"is_cutoff_enabled"`
	CutoffAt                  *time.Time `gorm:"type:timestamptz" json:"cutoff_at,omitempty"`
	CutoffNote                string     `gorm:"type:text" json:"cutoff_note"`
	NextQueueNumber           int        `gorm:"not null;default:1" json:"-"`
	Status                    string     `gorm:"type:varchar(20);default:'draft'" json:"status"` // draft, active, paused, closed
	StartTime                 *time.Time `gorm:"type:timestamptz" json:"start_time,omitempty"`
	EndTime                   *time.Time `gorm:"type:timestamptz" json:"end_time,omitempty"`
	ProjectorLastSeenAt       *time.Time `gorm:"type:timestamptz;index" json:"projector_last_seen_at,omitempty"`
	CreatedBy                 *uint      `gorm:"index" json:"created_by,omitempty"`
	CreatedAt                 time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt                 time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type QueueBooking struct {
	ID                  uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	QueueSessionID      string     `gorm:"type:varchar(21);not null;index" json:"queue_session_id"`
	StudentID           uint       `gorm:"not null;index" json:"student_id"`
	DeskID              string     `gorm:"type:varchar(21);not null" json:"desk_id"`
	DeskNumber          int        `gorm:"not null" json:"desk_number"`
	BookingType         string     `gorm:"type:varchar(20);not null" json:"booking_type"` // grading, help
	QueueNumber         int        `gorm:"not null" json:"queue_number"`
	Note                string     `gorm:"type:text" json:"note"`
	BookingIP           string     `gorm:"type:varchar(64)" json:"booking_ip"`
	BookingUserAgent    string     `gorm:"type:text" json:"booking_user_agent"`
	BookingDevice       string     `gorm:"type:varchar(255)" json:"booking_device"`
	IsLateBooking       bool       `gorm:"type:boolean;default:false" json:"is_late_booking"`
	LateReason          string     `gorm:"type:text" json:"late_reason"`
	Status              string     `gorm:"type:varchar(20);default:'waiting'" json:"status"` // waiting, in_progress, completed, cancelled, no_show
	AssignedWorkerID    *uint      `gorm:"index" json:"assigned_worker_id,omitempty"`
	AssignedAt          *time.Time `gorm:"type:timestamptz" json:"assigned_at,omitempty"`
	OfferExpiresAt      *time.Time `gorm:"type:timestamptz" json:"offer_expires_at,omitempty"`
	LastOfferWorkerID   *uint      `gorm:"index" json:"last_offer_worker_id,omitempty"`
	LastOfferTimedOutAt *time.Time `gorm:"type:timestamptz" json:"last_offer_timed_out_at,omitempty"`
	TimeoutCount        int        `gorm:"default:0" json:"timeout_count"`
	RejectCount         int        `gorm:"default:0" json:"reject_count"`
	StartedAt           *time.Time `gorm:"type:timestamptz" json:"started_at,omitempty"`
	CompletedAt         *time.Time `gorm:"type:timestamptz" json:"completed_at,omitempty"`
	Score               *float64   `gorm:"type:decimal(5,2)" json:"score,omitempty"`
	ScoreComment        string     `gorm:"type:text" json:"score_comment"`
	WorkerNote          string     `gorm:"type:text" json:"worker_note"`
	CreatedAt           time.Time  `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
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
	OfferPausedUntil         *time.Time `gorm:"type:timestamptz" json:"offer_paused_until,omitempty"`
	ConsecutiveOfferTimeouts int        `gorm:"default:0" json:"consecutive_offer_timeouts"`
	OfferAcceptCount         int        `gorm:"default:0" json:"offer_accept_count"`
	OfferRejectCount         int        `gorm:"default:0" json:"offer_reject_count"`
	OfferTimeoutCount        int        `gorm:"default:0" json:"offer_timeout_count"`
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

// =============================================================================
// UserNotification — per-user in-app notifications
// =============================================================================

type UserNotification struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint           `gorm:"not null;index" json:"user_id"`
	Type      string         `gorm:"type:varchar(60);not null" json:"type"` // assignment_created, assignment_updated, attendance_created, attendance_started, attendance_closed, queue_created, queue_updated, queue_opened, queue_closed, score_edit_request, score_edit_approved, score_edit_rejected, admin_message
	Title     string         `gorm:"type:varchar(255);not null" json:"title"`
	Message   string         `gorm:"type:text" json:"message"`
	CourseID  string         `gorm:"type:varchar(21);index" json:"course_id"`
	Link      string         `gorm:"type:varchar(255)" json:"link"`
	Data      datatypes.JSON `gorm:"type:jsonb" json:"data,omitempty"`
	IsRead    bool           `gorm:"type:boolean;default:false;index" json:"is_read"`
	ReadAt    *time.Time     `gorm:"type:timestamptz" json:"read_at,omitempty"`
	CreatedAt time.Time      `gorm:"type:timestamptz" json:"created_at"`
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
	Type         string         `gorm:"type:varchar(20);not null;default:'other'" json:"type"` // bug, feature, improvement, other, support
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
	Action         string         `gorm:"type:varchar(255);not null;index" json:"action"`
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
	RequestID      string         `gorm:"type:varchar(128);index" json:"request_id"`
	TraceID        string         `gorm:"type:varchar(128);index" json:"trace_id"`
	CreatedAt      time.Time      `gorm:"type:timestamptz;index" json:"created_at"`
}

type AppConfig struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	CreatedAt time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type SystemAnnouncement struct {
	ID                 uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Title              string         `gorm:"type:varchar(255);not null" json:"title"`
	TitleTH            string         `gorm:"type:varchar(255)" json:"title_th"`
	TitleEN            string         `gorm:"type:varchar(255)" json:"title_en"`
	Message            string         `gorm:"type:text;not null" json:"message"`
	MessageTH          string         `gorm:"type:text" json:"message_th"`
	MessageEN          string         `gorm:"type:text" json:"message_en"`
	ContentType        string         `gorm:"type:varchar(20);default:'text'" json:"content_type"`
	DisplayMode        string         `gorm:"type:varchar(20);default:'banner_top'" json:"display_mode"`
	ImageURL           string         `gorm:"type:text" json:"image_url"`
	ActionLabel        string         `gorm:"type:varchar(255)" json:"action_label"`
	ActionLabelTH      string         `gorm:"type:varchar(255)" json:"action_label_th"`
	ActionLabelEN      string         `gorm:"type:varchar(255)" json:"action_label_en"`
	ActionURL          string         `gorm:"type:text" json:"action_url"`
	IsDismissible      bool           `gorm:"type:boolean;default:true" json:"is_dismissible"`
	DisplayPaths       datatypes.JSON `gorm:"type:jsonb" json:"display_paths,omitempty"`
	ScheduledAt        *time.Time     `gorm:"type:timestamptz" json:"scheduled_at,omitempty"`
	ExpiresAt          *time.Time     `gorm:"type:timestamptz" json:"expires_at,omitempty"`
	Audience           datatypes.JSON `gorm:"type:jsonb" json:"audience,omitempty"`
	RequireAcknowledge bool           `gorm:"type:boolean;default:false" json:"require_acknowledge"`
	IsActive           bool           `gorm:"type:boolean;default:true" json:"is_active"`
	CreatedBy          *uint          `gorm:"index" json:"created_by,omitempty"`
	CreatedAt          time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}

type SystemAnnouncementAck struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AnnouncementID uint      `gorm:"not null;index" json:"announcement_id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	StudentID      *uint     `gorm:"index" json:"student_id,omitempty"`
	AcknowledgedAt time.Time `gorm:"type:timestamptz" json:"acknowledged_at"`
	CreatedAt      time.Time `gorm:"type:timestamptz" json:"created_at"`
}

type DatabaseBackupRecord struct {
	ID              uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	BackupName      string         `gorm:"type:varchar(255);not null;index" json:"backup_name"`
	StoragePath     string         `gorm:"type:text;not null;index" json:"storage_path"`
	StorageProvider string         `gorm:"type:varchar(50);not null" json:"storage_provider"`
	StorageSlot     int            `gorm:"not null;index" json:"storage_slot"`
	ChecksumSHA256  string         `gorm:"type:varchar(64);not null" json:"checksum_sha256"`
	FileSizeBytes   int64          `gorm:"not null" json:"file_size_bytes"`
	CreatedBy       *uint          `gorm:"index" json:"created_by,omitempty"`
	RestoredAt      *time.Time     `gorm:"type:timestamptz" json:"restored_at,omitempty"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	CreatedAt       time.Time      `gorm:"type:timestamptz" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime;type:timestamptz" json:"updated_at"`
}
