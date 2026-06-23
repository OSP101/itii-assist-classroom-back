package repositories

import (
	"encoding/json"
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"strings"

	"gorm.io/gorm"
)

const (
	PermissionUpdateCourse             = "update_course"
	PermissionManagePeople             = "manage_people"
	PermissionManageSections           = "manage_sections"
	PermissionManageTeams              = "manage_teams"
	PermissionManageAssignments        = "manage_assignments"
	PermissionManageExamScores         = "manage_exam_scores"
	PermissionManageAttendanceSessions = "manage_attendance_sessions"
	PermissionManageQueue              = "manage_queue"
	PermissionViewPeople               = "view_people"
	PermissionAddPeople                = "add_people"
	PermissionRemovePeople             = "remove_people"
	PermissionEditMemberPermissions    = "edit_member_permissions"
	PermissionViewSections             = "view_sections"
	PermissionCreateSections           = "create_sections"
	PermissionUpdateSections           = "update_sections"
	PermissionDeleteSections           = "delete_sections"
	PermissionManageSectionStudents    = "manage_section_students"
	PermissionViewTeams                = "view_teams"
	PermissionCreateTeams              = "create_teams"
	PermissionUpdateTeams              = "update_teams"
	PermissionDeleteTeams              = "delete_teams"
	PermissionManageTeamMembers        = "manage_team_members"
	PermissionViewAssignments          = "view_assignments"
	PermissionCreateAssignments        = "create_assignments"
	PermissionUpdateAssignments        = "update_assignments"
	PermissionDeleteAssignments        = "delete_assignments"
	PermissionGradeAssignments         = "grade_assignments"
	PermissionEditScores               = "edit_scores"
	PermissionViewScoreSummary         = "view_score_summary"
	PermissionViewExamScores           = "view_exam_scores"
	PermissionCreateExamScores         = "create_exam_scores"
	PermissionUpdateExamScores         = "update_exam_scores"
	PermissionDeleteExamScores         = "delete_exam_scores"
	PermissionUpdateExamSettings       = "update_exam_settings"
	PermissionReviewOwnScoreRequests   = "review_own_score_requests"
	PermissionReviewAllScoreRequests   = "review_all_score_requests"
	PermissionViewAttendance           = "view_attendance"
	PermissionCreateAttendanceSessions = "create_attendance_sessions"
	PermissionUpdateAttendanceSessions = "update_attendance_sessions"
	PermissionDeleteAttendanceSessions = "delete_attendance_sessions"
	PermissionUpdateAttendanceStatus   = "update_attendance_status"
	PermissionViewQueue                = "view_queue"
	PermissionCreateQueueSessions      = "create_queue_sessions"
	PermissionUpdateQueueSessions      = "update_queue_sessions"
	PermissionDeleteQueueSessions      = "delete_queue_sessions"
	PermissionManageQueueBookings      = "manage_queue_bookings"
)

var ErrPrimaryInstructorPermissionsImmutable = errors.New("primary instructor permissions cannot be changed")

type CourseMemberPermissions struct {
	UpdateCourse             bool `json:"update_course"`
	ViewPeople               bool `json:"view_people"`
	AddPeople                bool `json:"add_people"`
	RemovePeople             bool `json:"remove_people"`
	EditMemberPermissions    bool `json:"edit_member_permissions"`
	ViewSections             bool `json:"view_sections"`
	CreateSections           bool `json:"create_sections"`
	UpdateSections           bool `json:"update_sections"`
	DeleteSections           bool `json:"delete_sections"`
	ManageSectionStudents    bool `json:"manage_section_students"`
	ViewTeams                bool `json:"view_teams"`
	CreateTeams              bool `json:"create_teams"`
	UpdateTeams              bool `json:"update_teams"`
	DeleteTeams              bool `json:"delete_teams"`
	ManageTeamMembers        bool `json:"manage_team_members"`
	ViewAssignments          bool `json:"view_assignments"`
	CreateAssignments        bool `json:"create_assignments"`
	UpdateAssignments        bool `json:"update_assignments"`
	DeleteAssignments        bool `json:"delete_assignments"`
	GradeAssignments         bool `json:"grade_assignments"`
	EditScores               bool `json:"edit_scores"`
	ViewScoreSummary         bool `json:"view_score_summary"`
	ViewExamScores           bool `json:"view_exam_scores"`
	CreateExamScores         bool `json:"create_exam_scores"`
	UpdateExamScores         bool `json:"update_exam_scores"`
	DeleteExamScores         bool `json:"delete_exam_scores"`
	UpdateExamSettings       bool `json:"update_exam_settings"`
	ReviewOwnScoreRequests   bool `json:"review_own_score_requests"`
	ReviewAllScoreRequests   bool `json:"review_all_score_requests"`
	ViewAttendance           bool `json:"view_attendance"`
	CreateAttendanceSessions bool `json:"create_attendance_sessions"`
	UpdateAttendanceSessions bool `json:"update_attendance_sessions"`
	DeleteAttendanceSessions bool `json:"delete_attendance_sessions"`
	UpdateAttendanceStatus   bool `json:"update_attendance_status"`
	ViewQueue                bool `json:"view_queue"`
	CreateQueueSessions      bool `json:"create_queue_sessions"`
	UpdateQueueSessions      bool `json:"update_queue_sessions"`
	DeleteQueueSessions      bool `json:"delete_queue_sessions"`
	ManageQueueBookings      bool `json:"manage_queue_bookings"`
}

type legacyCourseMemberPermissions struct {
	ManagePeople             bool `json:"manage_people"`
	ManageSections           bool `json:"manage_sections"`
	ManageTeams              bool `json:"manage_teams"`
	ManageAssignments        bool `json:"manage_assignments"`
	GradeAssignments         bool `json:"grade_assignments"`
	EditScores               bool `json:"edit_scores"`
	ViewScoreSummary         bool `json:"view_score_summary"`
	ViewExamScores           bool `json:"view_exam_scores"`
	ManageExamScores         bool `json:"manage_exam_scores"`
	ReviewOwnScoreRequests   bool `json:"review_own_score_requests"`
	ReviewAllScoreRequests   bool `json:"review_all_score_requests"`
	ManageAttendanceSessions bool `json:"manage_attendance_sessions"`
	UpdateAttendanceStatus   bool `json:"update_attendance_status"`
	ManageQueue              bool `json:"manage_queue"`
}

type CourseMemberPermissionProfile struct {
	Role        string
	IsPrimary   bool
	Permissions CourseMemberPermissions
}

func DefaultInstructorCoursePermissions() CourseMemberPermissions {
	return CourseMemberPermissions{
		UpdateCourse:             true,
		ViewPeople:               true,
		AddPeople:                true,
		RemovePeople:             true,
		EditMemberPermissions:    true,
		ViewSections:             true,
		CreateSections:           true,
		UpdateSections:           true,
		DeleteSections:           true,
		ManageSectionStudents:    true,
		ViewTeams:                true,
		CreateTeams:              true,
		UpdateTeams:              true,
		DeleteTeams:              true,
		ManageTeamMembers:        true,
		ViewAssignments:          true,
		CreateAssignments:        true,
		UpdateAssignments:        true,
		DeleteAssignments:        true,
		GradeAssignments:         true,
		EditScores:               true,
		ViewScoreSummary:         true,
		ViewExamScores:           true,
		CreateExamScores:         true,
		UpdateExamScores:         true,
		DeleteExamScores:         true,
		UpdateExamSettings:       true,
		ReviewOwnScoreRequests:   true,
		ReviewAllScoreRequests:   true,
		ViewAttendance:           true,
		CreateAttendanceSessions: true,
		UpdateAttendanceSessions: true,
		DeleteAttendanceSessions: true,
		UpdateAttendanceStatus:   true,
		ViewQueue:                true,
		CreateQueueSessions:      true,
		UpdateQueueSessions:      true,
		DeleteQueueSessions:      true,
		ManageQueueBookings:      true,
	}
}

func DefaultTACoursePermissions() CourseMemberPermissions {
	return CourseMemberPermissions{
		UpdateCourse:             false,
		ViewPeople:               false,
		AddPeople:                false,
		RemovePeople:             false,
		EditMemberPermissions:    false,
		ViewSections:             false,
		CreateSections:           false,
		UpdateSections:           false,
		DeleteSections:           false,
		ManageSectionStudents:    false,
		ViewTeams:                false,
		CreateTeams:              false,
		UpdateTeams:              false,
		DeleteTeams:              false,
		ManageTeamMembers:        false,
		ViewAssignments:          true,
		CreateAssignments:        false,
		UpdateAssignments:        false,
		DeleteAssignments:        false,
		GradeAssignments:         true,
		EditScores:               true,
		ViewScoreSummary:         true,
		ViewExamScores:           false,
		CreateExamScores:         false,
		UpdateExamScores:         false,
		DeleteExamScores:         false,
		UpdateExamSettings:       false,
		ReviewOwnScoreRequests:   true,
		ReviewAllScoreRequests:   false,
		ViewAttendance:           true,
		CreateAttendanceSessions: true,
		UpdateAttendanceSessions: true,
		DeleteAttendanceSessions: true,
		UpdateAttendanceStatus:   false,
		ViewQueue:                true,
		CreateQueueSessions:      true,
		UpdateQueueSessions:      true,
		DeleteQueueSessions:      true,
		ManageQueueBookings:      true,
	}
}

func NormalizeCourseMemberPermissions(role string, permissions *CourseMemberPermissions) CourseMemberPermissions {
	if permissions == nil {
		if strings.EqualFold(strings.TrimSpace(role), "ta") {
			return DefaultTACoursePermissions()
		}
		return DefaultInstructorCoursePermissions()
	}

	normalized := *permissions
	if normalized.AddPeople || normalized.RemovePeople || normalized.EditMemberPermissions {
		normalized.ViewPeople = true
	}
	if normalized.CreateSections || normalized.UpdateSections || normalized.DeleteSections || normalized.ManageSectionStudents {
		normalized.ViewSections = true
	}
	if normalized.CreateTeams || normalized.UpdateTeams || normalized.DeleteTeams || normalized.ManageTeamMembers {
		normalized.ViewTeams = true
	}
	if normalized.CreateAssignments || normalized.UpdateAssignments || normalized.DeleteAssignments || normalized.GradeAssignments || normalized.EditScores {
		normalized.ViewAssignments = true
	}
	if normalized.ReviewAllScoreRequests {
		normalized.ReviewOwnScoreRequests = true
	}
	if normalized.CreateExamScores || normalized.UpdateExamScores || normalized.DeleteExamScores || normalized.UpdateExamSettings {
		normalized.ViewExamScores = true
	}
	if normalized.CreateAttendanceSessions || normalized.UpdateAttendanceSessions || normalized.DeleteAttendanceSessions || normalized.UpdateAttendanceStatus {
		normalized.ViewAttendance = true
	}
	if normalized.CreateQueueSessions || normalized.UpdateQueueSessions || normalized.DeleteQueueSessions || normalized.ManageQueueBookings {
		normalized.ViewQueue = true
	}
	return normalized
}

func applyBoolOverride[T any](payload map[string]json.RawMessage, key string, update func(bool)) {
	raw, ok := payload[key]
	if !ok {
		return
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		update(value)
	}
}

func applyLegacyCourseMemberPermissions(normalized *CourseMemberPermissions, payload map[string]json.RawMessage) {
	if normalized == nil {
		return
	}

	applyBoolOverride[CourseMemberPermissions](payload, "manage_people", func(value bool) {
		normalized.ViewPeople = value
		normalized.AddPeople = value
		normalized.RemovePeople = value
		normalized.EditMemberPermissions = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_sections", func(value bool) {
		normalized.ViewSections = value
		normalized.CreateSections = value
		normalized.UpdateSections = value
		normalized.DeleteSections = value
		normalized.ManageSectionStudents = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_teams", func(value bool) {
		normalized.ViewTeams = value
		normalized.CreateTeams = value
		normalized.UpdateTeams = value
		normalized.DeleteTeams = value
		normalized.ManageTeamMembers = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_assignments", func(value bool) {
		normalized.ViewAssignments = value
		normalized.CreateAssignments = value
		normalized.UpdateAssignments = value
		normalized.DeleteAssignments = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_exam_scores", func(value bool) {
		normalized.ViewExamScores = value
		normalized.CreateExamScores = value
		normalized.UpdateExamScores = value
		normalized.DeleteExamScores = value
		normalized.UpdateExamSettings = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_attendance_sessions", func(value bool) {
		normalized.ViewAttendance = value
		normalized.CreateAttendanceSessions = value
		normalized.UpdateAttendanceSessions = value
		normalized.DeleteAttendanceSessions = value
	})
	applyBoolOverride[CourseMemberPermissions](payload, "manage_queue", func(value bool) {
		normalized.ViewQueue = value
		normalized.CreateQueueSessions = value
		normalized.UpdateQueueSessions = value
		normalized.DeleteQueueSessions = value
		normalized.ManageQueueBookings = value
	})
}

func ResolveCourseMemberPermissions(role string, raw string, isPrimary bool) CourseMemberPermissions {
	if strings.EqualFold(strings.TrimSpace(role), "instructor") && isPrimary {
		return DefaultInstructorCoursePermissions()
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return NormalizeCourseMemberPermissions(role, nil)
	}

	base := NormalizeCourseMemberPermissions(role, nil)
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return NormalizeCourseMemberPermissions(role, nil)
	}

	applyBoolOverride[CourseMemberPermissions](payload, "view_people", func(value bool) { base.ViewPeople = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_course", func(value bool) { base.UpdateCourse = value })
	applyBoolOverride[CourseMemberPermissions](payload, "add_people", func(value bool) { base.AddPeople = value })
	applyBoolOverride[CourseMemberPermissions](payload, "remove_people", func(value bool) { base.RemovePeople = value })
	applyBoolOverride[CourseMemberPermissions](payload, "edit_member_permissions", func(value bool) { base.EditMemberPermissions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_sections", func(value bool) { base.ViewSections = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_sections", func(value bool) { base.CreateSections = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_sections", func(value bool) { base.UpdateSections = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_sections", func(value bool) { base.DeleteSections = value })
	applyBoolOverride[CourseMemberPermissions](payload, "manage_section_students", func(value bool) { base.ManageSectionStudents = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_teams", func(value bool) { base.ViewTeams = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_teams", func(value bool) { base.CreateTeams = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_teams", func(value bool) { base.UpdateTeams = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_teams", func(value bool) { base.DeleteTeams = value })
	applyBoolOverride[CourseMemberPermissions](payload, "manage_team_members", func(value bool) { base.ManageTeamMembers = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_assignments", func(value bool) { base.ViewAssignments = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_assignments", func(value bool) { base.CreateAssignments = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_assignments", func(value bool) { base.UpdateAssignments = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_assignments", func(value bool) { base.DeleteAssignments = value })
	applyBoolOverride[CourseMemberPermissions](payload, "grade_assignments", func(value bool) { base.GradeAssignments = value })
	applyBoolOverride[CourseMemberPermissions](payload, "edit_scores", func(value bool) { base.EditScores = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_score_summary", func(value bool) { base.ViewScoreSummary = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_exam_scores", func(value bool) { base.ViewExamScores = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_exam_scores", func(value bool) { base.CreateExamScores = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_exam_scores", func(value bool) { base.UpdateExamScores = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_exam_scores", func(value bool) { base.DeleteExamScores = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_exam_settings", func(value bool) { base.UpdateExamSettings = value })
	applyBoolOverride[CourseMemberPermissions](payload, "review_own_score_requests", func(value bool) { base.ReviewOwnScoreRequests = value })
	applyBoolOverride[CourseMemberPermissions](payload, "review_all_score_requests", func(value bool) { base.ReviewAllScoreRequests = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_attendance", func(value bool) { base.ViewAttendance = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_attendance_sessions", func(value bool) { base.CreateAttendanceSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_attendance_sessions", func(value bool) { base.UpdateAttendanceSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_attendance_sessions", func(value bool) { base.DeleteAttendanceSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_attendance_status", func(value bool) { base.UpdateAttendanceStatus = value })
	applyBoolOverride[CourseMemberPermissions](payload, "view_queue", func(value bool) { base.ViewQueue = value })
	applyBoolOverride[CourseMemberPermissions](payload, "create_queue_sessions", func(value bool) { base.CreateQueueSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "update_queue_sessions", func(value bool) { base.UpdateQueueSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "delete_queue_sessions", func(value bool) { base.DeleteQueueSessions = value })
	applyBoolOverride[CourseMemberPermissions](payload, "manage_queue_bookings", func(value bool) { base.ManageQueueBookings = value })

	applyLegacyCourseMemberPermissions(&base, payload)

	return NormalizeCourseMemberPermissions(role, &base)
}

func EncodeCourseMemberPermissions(role string, permissions *CourseMemberPermissions) string {
	normalized := NormalizeCourseMemberPermissions(role, permissions)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(payload)
}

func (permissions CourseMemberPermissions) Has(permissionKey string) bool {
	switch permissionKey {
	case PermissionManagePeople:
		return permissions.ViewPeople || permissions.AddPeople || permissions.RemovePeople || permissions.EditMemberPermissions
	case PermissionUpdateCourse:
		return permissions.UpdateCourse
	case PermissionViewPeople:
		return permissions.ViewPeople
	case PermissionAddPeople:
		return permissions.AddPeople
	case PermissionRemovePeople:
		return permissions.RemovePeople
	case PermissionEditMemberPermissions:
		return permissions.EditMemberPermissions
	case PermissionManageSections:
		return permissions.ViewSections || permissions.CreateSections || permissions.UpdateSections || permissions.DeleteSections || permissions.ManageSectionStudents
	case PermissionViewSections:
		return permissions.ViewSections
	case PermissionCreateSections:
		return permissions.CreateSections
	case PermissionUpdateSections:
		return permissions.UpdateSections
	case PermissionDeleteSections:
		return permissions.DeleteSections
	case PermissionManageSectionStudents:
		return permissions.ManageSectionStudents
	case PermissionViewTeams:
		return permissions.ViewTeams
	case PermissionCreateTeams:
		return permissions.CreateTeams
	case PermissionUpdateTeams:
		return permissions.UpdateTeams
	case PermissionDeleteTeams:
		return permissions.DeleteTeams
	case PermissionManageTeamMembers:
		return permissions.ManageTeamMembers
	case PermissionManageTeams:
		return permissions.ViewTeams || permissions.CreateTeams || permissions.UpdateTeams || permissions.DeleteTeams || permissions.ManageTeamMembers
	case PermissionViewAssignments:
		return permissions.ViewAssignments
	case PermissionCreateAssignments:
		return permissions.CreateAssignments
	case PermissionUpdateAssignments:
		return permissions.UpdateAssignments
	case PermissionDeleteAssignments:
		return permissions.DeleteAssignments
	case PermissionManageAssignments:
		return permissions.ViewAssignments || permissions.CreateAssignments || permissions.UpdateAssignments || permissions.DeleteAssignments
	case PermissionGradeAssignments:
		return permissions.GradeAssignments
	case PermissionEditScores:
		return permissions.EditScores
	case PermissionViewScoreSummary:
		return permissions.ViewScoreSummary
	case PermissionViewExamScores:
		return permissions.ViewExamScores
	case PermissionCreateExamScores:
		return permissions.CreateExamScores
	case PermissionUpdateExamScores:
		return permissions.UpdateExamScores
	case PermissionDeleteExamScores:
		return permissions.DeleteExamScores
	case PermissionUpdateExamSettings:
		return permissions.UpdateExamSettings
	case PermissionManageExamScores:
		return permissions.ViewExamScores || permissions.CreateExamScores || permissions.UpdateExamScores || permissions.DeleteExamScores || permissions.UpdateExamSettings
	case PermissionReviewOwnScoreRequests:
		return permissions.ReviewOwnScoreRequests
	case PermissionReviewAllScoreRequests:
		return permissions.ReviewAllScoreRequests
	case PermissionViewAttendance:
		return permissions.ViewAttendance
	case PermissionCreateAttendanceSessions:
		return permissions.CreateAttendanceSessions
	case PermissionUpdateAttendanceSessions:
		return permissions.UpdateAttendanceSessions
	case PermissionDeleteAttendanceSessions:
		return permissions.DeleteAttendanceSessions
	case PermissionManageAttendanceSessions:
		return permissions.ViewAttendance || permissions.CreateAttendanceSessions || permissions.UpdateAttendanceSessions || permissions.DeleteAttendanceSessions
	case PermissionUpdateAttendanceStatus:
		return permissions.UpdateAttendanceStatus
	case PermissionViewQueue:
		return permissions.ViewQueue
	case PermissionCreateQueueSessions:
		return permissions.CreateQueueSessions
	case PermissionUpdateQueueSessions:
		return permissions.UpdateQueueSessions
	case PermissionDeleteQueueSessions:
		return permissions.DeleteQueueSessions
	case PermissionManageQueueBookings:
		return permissions.ManageQueueBookings
	case PermissionManageQueue:
		return permissions.ViewQueue || permissions.CreateQueueSessions || permissions.UpdateQueueSessions || permissions.DeleteQueueSessions || permissions.ManageQueueBookings
	default:
		return false
	}
}

func GetCourseMemberPermissionProfile(courseID string, userID uint) (*CourseMemberPermissionProfile, error) {
	if strings.TrimSpace(courseID) == "" || userID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var instructor models.CourseInstructor
	if err := config.DB.Where("course_id = ? AND user_id = ?", courseID, userID).First(&instructor).Error; err == nil {
		return &CourseMemberPermissionProfile{
			Role:        "instructor",
			IsPrimary:   instructor.IsPrimary,
			Permissions: ResolveCourseMemberPermissions("instructor", instructor.Permissions, instructor.IsPrimary),
		}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var ta models.CourseTA
	if err := config.DB.Where("course_id = ? AND user_id = ?", courseID, userID).First(&ta).Error; err == nil {
		return &CourseMemberPermissionProfile{
			Role:        "ta",
			Permissions: ResolveCourseMemberPermissions("ta", ta.Permissions, false),
		}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Backward compatibility: older courses may only set courses.instructor_id
	// without creating a course_instructors row. Treat that owner as primary instructor.
	var course models.Course
	if err := config.DB.Select("id", "instructor_id").Where("id = ?", courseID).First(&course).Error; err == nil {
		if course.InstructorID != nil && *course.InstructorID == userID {
			return &CourseMemberPermissionProfile{
				Role:        "instructor",
				IsPrimary:   true,
				Permissions: DefaultInstructorCoursePermissions(),
			}, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return nil, gorm.ErrRecordNotFound
}

func HasCoursePermission(courseID string, userID uint, userRole string, permissionKey string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(userRole), "admin") {
		return true, nil
	}

	profile, err := GetCourseMemberPermissionProfile(courseID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	return profile.Permissions.Has(permissionKey), nil
}

func CanReviewAllCourseScoreRequests(courseID string, userID uint, userRole string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(userRole), "admin") {
		return true, nil
	}
	return HasCoursePermission(courseID, userID, userRole, PermissionReviewAllScoreRequests)
}

func UpdateCourseInstructorPermissions(courseID string, userID uint, permissions *CourseMemberPermissions) error {
	var instructor models.CourseInstructor
	if err := config.DB.Where("course_id = ? AND user_id = ?", courseID, userID).First(&instructor).Error; err != nil {
		return err
	}
	if instructor.IsPrimary {
		return ErrPrimaryInstructorPermissionsImmutable
	}
	return config.DB.Model(&instructor).Update("permissions", EncodeCourseMemberPermissions("instructor", permissions)).Error
}

func UpdateCourseTAPermissions(courseID string, userID uint, permissions *CourseMemberPermissions) error {
	var ta models.CourseTA
	if err := config.DB.Where("course_id = ? AND user_id = ?", courseID, userID).First(&ta).Error; err != nil {
		return err
	}
	return config.DB.Model(&ta).Update("permissions", EncodeCourseMemberPermissions("ta", permissions)).Error
}
