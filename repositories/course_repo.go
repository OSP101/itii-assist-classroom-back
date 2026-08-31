package repositories

import (
	"errors"
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ============================================================
// Params & Results
// ============================================================

type CourseListParams struct {
	Page      int
	Limit     int
	Search    string
	Year      int
	Semester  int
	Status    string // "active" | "inactive" | ""
	SortBy    string
	SortOrder string
}

type CourseListResult struct {
	Courses     []CourseWithCounts `json:"courses"`
	Total       int64              `json:"total"`
	TotalPages  int                `json:"total_pages"`
	CurrentPage int                `json:"current_page"`
	PerPage     int                `json:"per_page"`
}

type SectionBasic struct {
	ID        uint   `json:"id"`
	SectionNo string `json:"section_no"`
	Note      string `json:"note"`
}

type CourseWithCounts struct {
	models.Course
	Instructor   *UserBasic       `json:"instructor"`
	Instructors  []UserBasic      `json:"instructors"`
	Sections     []SectionBasic   `json:"sections"`
	TAs          []UserBasic      `json:"tas"`
	TaCount      int64            `json:"taCount"`
	StudentCount int64            `json:"studentCount"`
	MySectionNo  string           `json:"my_section_no"`
	MyGroups     []StudentMyGroup `json:"my_groups"`
}

type StudentMyGroupMember struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
}

type StudentMyGroup struct {
	ID         uint                   `json:"id"`
	Name       string                 `json:"name"`
	GroupType  string                 `json:"group_type"`
	WeekNumber *int                   `json:"week_number"`
	Members    []StudentMyGroupMember `json:"members"`
}

type CourseDetail struct {
	models.Course
	Instructor  *UserBasic         `json:"instructor"` // primary instructor (single object, matches old system)
	Instructors []UserBasic        `json:"instructors"`
	TAs         []UserBasic        `json:"tas"`
	Sections    []SectionWithCount `json:"sections"`
}

type SectionWithCount struct {
	models.CourseSection
	StudentCount int64 `json:"student_count"`
}

type CourseInstructorMeta struct {
	IsPrimary   bool                    `json:"is_primary"`
	AssignedAt  time.Time               `json:"assigned_at"`
	Permissions CourseMemberPermissions `json:"permissions"`
}

type CourseTAMeta struct {
	AssignedAt  time.Time               `json:"assigned_at"`
	Permissions CourseMemberPermissions `json:"permissions"`
}

type UserBasic struct {
	ID               uint                  `json:"id"`
	FullName         string                `json:"full_name"`
	Email            string                `json:"email"`
	Username         string                `json:"username"`
	Avatar           string                `json:"avatar"`
	IsPrimary        bool                  `json:"is_primary,omitempty"`
	CourseInstructor *CourseInstructorMeta `json:"CourseInstructor,omitempty"`
	CourseTA         *CourseTAMeta         `json:"CourseTA,omitempty"`
}

type CourseStatusBreakdown struct {
	Active   int64 `json:"active"`
	Inactive int64 `json:"inactive"`
}

type CourseStats struct {
	Total    int64                 `json:"total"`
	ByStatus CourseStatusBreakdown `json:"byStatus"`
	ThisYear int64                 `json:"thisYear"`
	Years    []int                 `json:"years"`
}

type MyCourseStats struct {
	Total    int64                 `json:"total"`
	ByStatus CourseStatusBreakdown `json:"byStatus"`
	Years    []int                 `json:"years"`
}

type CourseConflictParams struct {
	Search   string
	Year     int
	Semester int
	Limit    int
}

type CourseActivationConflict struct {
	ConflictType       string `json:"conflict_type"`
	CourseCode         string `json:"course_code"`
	Year               int    `json:"year"`
	Semester           int    `json:"semester"`
	ActiveCourseID     string `json:"active_course_id"`
	ActiveCourseName   string `json:"active_course_name"`
	InactiveCourseID   string `json:"inactive_course_id"`
	InactiveCourseName string `json:"inactive_course_name"`
}

type CourseConflictListResult struct {
	Items []CourseActivationConflict `json:"items"`
	Total int64                      `json:"total"`
}

type courseMemberRow struct {
	CourseID string
	UserID   uint
}

type courseInstructorMemberRow struct {
	CourseID    string    `gorm:"column:course_id"`
	UserID      uint      `gorm:"column:user_id"`
	IsPrimary   bool      `gorm:"column:is_primary"`
	AssignedAt  time.Time `gorm:"column:assigned_at"`
	Permissions string    `gorm:"column:permissions"`
}

type courseTAMemberRow struct {
	CourseID    string    `gorm:"column:course_id"`
	UserID      uint      `gorm:"column:user_id"`
	AssignedAt  time.Time `gorm:"column:assigned_at"`
	Permissions string    `gorm:"column:permissions"`
}

func mapUsersToBasic(users []models.User) map[uint]UserBasic {
	result := make(map[uint]UserBasic, len(users))
	for _, user := range users {
		result[user.ID] = UserBasic{
			ID:       user.ID,
			FullName: user.FullName,
			Email:    user.Email,
			Username: user.Username,
			Avatar:   user.Avatar,
		}
	}
	return result
}

func getCourseTAMap(courseIDs []string) map[string][]UserBasic {
	if len(courseIDs) == 0 {
		return map[string][]UserBasic{}
	}

	var rows []courseTAMemberRow
	if err := config.DB.Raw(`SELECT course_id, user_id, assigned_at, permissions FROM course_tas WHERE course_id IN ? ORDER BY course_id ASC, assigned_at ASC`, courseIDs).Scan(&rows).Error; err != nil {
		return map[string][]UserBasic{}
	}

	userIDSet := make(map[uint]struct{}, len(rows))
	for _, row := range rows {
		userIDSet[row.UserID] = struct{}{}
	}

	userIDs := make([]uint, 0, len(userIDSet))
	for userID := range userIDSet {
		userIDs = append(userIDs, userID)
	}

	var users []models.User
	if len(userIDs) > 0 {
		if err := config.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return map[string][]UserBasic{}
		}
	}

	userMap := mapUsersToBasic(users)
	tasByCourse := make(map[string][]UserBasic)
	for _, row := range rows {
		user, exists := userMap[row.UserID]
		if !exists {
			continue
		}
		user.CourseTA = &CourseTAMeta{
			AssignedAt:  row.AssignedAt,
			Permissions: ResolveCourseMemberPermissions("ta", row.Permissions, false),
		}
		tasByCourse[row.CourseID] = append(tasByCourse[row.CourseID], user)
	}

	return tasByCourse
}

// ============================================================
// Course CRUD
// ============================================================

func GetCourses(params CourseListParams) (CourseListResult, error) {
	db := config.DB

	query := db.Model(&models.Course{})

	// Search
	if params.Search != "" {
		like := "%" + strings.TrimSpace(params.Search) + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", like, like)
	}
	// Year
	if params.Year > 0 {
		query = query.Where("year = ?", params.Year)
	}
	// Semester
	if params.Semester > 0 {
		query = query.Where("semester = ?", params.Semester)
	}
	// Status
	switch params.Status {
	case "active":
		query = query.Where("is_active = true")
	case "inactive":
		query = query.Where("is_active = false")
	}

	// Count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return CourseListResult{}, err
	}

	// Sort
	validSortCols := map[string]bool{
		"code": true, "name": true, "year": true,
		"semester": true, "is_active": true,
		"created_at": true, "updated_at": true,
	}
	col := "created_at"
	if validSortCols[params.SortBy] {
		col = params.SortBy
	}
	dir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		dir = "ASC"
	}

	// Pagination
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 10
	}
	offset := (params.Page - 1) * params.Limit

	var courses []models.Course
	if err := query.Order(col + " " + dir).
		Limit(params.Limit).Offset(offset).
		Find(&courses).Error; err != nil {
		return CourseListResult{}, err
	}

	// Gather course IDs
	courseIDs := make([]string, len(courses))
	for i, c := range courses {
		courseIDs[i] = c.ID
	}

	// Batch: instructors of each course
	var ciRows []courseInstructorMemberRow
	db.Raw(`SELECT course_id, user_id, is_primary, assigned_at, permissions FROM course_instructors WHERE course_id IN ? ORDER BY course_id ASC, is_primary DESC, assigned_at ASC`, courseIDs).Scan(&ciRows)

	// Gather user IDs for instructors
	userIDSet := map[uint]bool{}
	for _, r := range ciRows {
		userIDSet[r.UserID] = true
	}
	userIDs := make([]uint, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}

	var users []models.User
	if len(userIDs) > 0 {
		db.Where("id IN ?", userIDs).Find(&users)
	}
	userMap := map[uint]models.User{}
	for _, u := range users {
		userMap[u.ID] = u
	}

	// Batch: TA counts
	type countRow struct {
		CourseID string
		Cnt      int64
	}
	var taCounts []countRow
	db.Raw(`SELECT course_id, COUNT(*) as cnt FROM course_tas WHERE course_id IN ? GROUP BY course_id`, courseIDs).Scan(&taCounts)
	taCountMap := map[string]int64{}
	for _, r := range taCounts {
		taCountMap[r.CourseID] = r.Cnt
	}

	// Batch: student counts (via course_section_students joined with course_sections)
	var studentCounts []countRow
	db.Raw(`
		SELECT cs.course_id, COUNT(css.id) as cnt
		FROM course_sections cs
		LEFT JOIN course_section_students css ON css.course_section_id = cs.id
		WHERE cs.course_id IN ?
		GROUP BY cs.course_id
	`, courseIDs).Scan(&studentCounts)
	studentCountMap := map[string]int64{}
	for _, r := range studentCounts {
		studentCountMap[r.CourseID] = r.Cnt
	}

	// Batch: section counts
	var sectionCounts []countRow
	db.Raw(`SELECT course_id, COUNT(*) as cnt FROM course_sections WHERE course_id IN ? GROUP BY course_id`, courseIDs).Scan(&sectionCounts)
	sectionCountMap := map[string]int64{}
	for _, r := range sectionCounts {
		sectionCountMap[r.CourseID] = r.Cnt
	}

	// Batch: sections for each course (id, section_no, note)
	var allSections []models.CourseSection
	if len(courseIDs) > 0 {
		db.Where("course_id IN ?", courseIDs).Order("id ASC").Find(&allSections)
	}
	courseSectionsMap := map[string][]SectionBasic{}
	for _, s := range allSections {
		courseSectionsMap[s.CourseID] = append(courseSectionsMap[s.CourseID], SectionBasic{
			ID: s.ID, SectionNo: s.SectionNo, Note: s.Note,
		})
	}
	courseTAsMap := getCourseTAMap(courseIDs)

	// Build instructor map per course
	courseInstructorMap := map[string][]UserBasic{}
	for _, r := range ciRows {
		u := userMap[r.UserID]
		courseInstructorMap[r.CourseID] = append(courseInstructorMap[r.CourseID], UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar, IsPrimary: r.IsPrimary,
			CourseInstructor: &CourseInstructorMeta{
				IsPrimary:   r.IsPrimary,
				AssignedAt:  r.AssignedAt,
				Permissions: ResolveCourseMemberPermissions("instructor", r.Permissions, r.IsPrimary),
			},
		})
	}

	// Compose result
	result := make([]CourseWithCounts, len(courses))
	for i, c := range courses {
		instructors := courseInstructorMap[c.ID]

		// Find primary instructor (IsPrimary=true, or first one, or by Course.InstructorID)
		var primaryInstructor *UserBasic
		for j := range instructors {
			if instructors[j].IsPrimary {
				u := instructors[j]
				primaryInstructor = &u
				break
			}
		}
		if primaryInstructor == nil && c.InstructorID != nil {
			if u, ok := userMap[*c.InstructorID]; ok {
				primaryInstructor = &UserBasic{
					ID: u.ID, FullName: u.FullName, Email: u.Email,
					Username: u.Username, Avatar: u.Avatar,
				}
			}
		}
		if primaryInstructor == nil && len(instructors) > 0 {
			primaryInstructor = &instructors[0]
		}

		sections := courseSectionsMap[c.ID]
		if sections == nil {
			sections = []SectionBasic{}
		}
		tas := courseTAsMap[c.ID]
		if tas == nil {
			tas = []UserBasic{}
		}

		result[i] = CourseWithCounts{
			Course:       c,
			Instructor:   primaryInstructor,
			Instructors:  instructors,
			TAs:          tas,
			Sections:     sections,
			TaCount:      taCountMap[c.ID],
			StudentCount: studentCountMap[c.ID],
		}
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return CourseListResult{
		Courses:     result,
		Total:       total,
		TotalPages:  totalPages,
		CurrentPage: params.Page,
		PerPage:     params.Limit,
	}, nil
}

func GetCourseStats() (CourseStats, error) {
	db := config.DB

	var total, active, inactive, thisYear int64
	db.Model(&models.Course{}).Count(&total)
	db.Model(&models.Course{}).Where("is_active = true").Count(&active)
	db.Model(&models.Course{}).Where("is_active = false").Count(&inactive)

	currentYear := time.Now().Year() + 543 // พ.ศ.
	db.Model(&models.Course{}).Where("year = ?", currentYear).Count(&thisYear)

	type yearRow struct{ Year int }
	var yearRows []yearRow
	db.Raw(`SELECT DISTINCT year FROM courses ORDER BY year DESC`).Scan(&yearRows)
	years := make([]int, len(yearRows))
	for i, r := range yearRows {
		years[i] = r.Year
	}

	return CourseStats{
		Total:    total,
		ByStatus: CourseStatusBreakdown{Active: active, Inactive: inactive},
		ThisYear: thisYear,
		Years:    years,
	}, nil
}

func GetCourseActivationConflicts(params CourseConflictParams) (CourseConflictListResult, error) {
	db := config.DB

	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	baseQuery := db.Table("courses AS inactive").
		Joins("JOIN courses AS active ON active.code = inactive.code AND active.year = inactive.year AND active.semester = inactive.semester AND active.id <> inactive.id").
		Where("inactive.is_active = ?", false).
		Where("active.is_active = ?", true)

	if params.Search != "" {
		search := "%" + strings.TrimSpace(params.Search) + "%"
		baseQuery = baseQuery.Where(
			"inactive.code ILIKE ? OR inactive.name ILIKE ? OR active.name ILIKE ?",
			search,
			search,
			search,
		)
	}

	if params.Year > 0 {
		baseQuery = baseQuery.Where("inactive.year = ?", params.Year)
	}

	if params.Semester > 0 {
		baseQuery = baseQuery.Where("inactive.semester = ?", params.Semester)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return CourseConflictListResult{}, err
	}

	items := make([]CourseActivationConflict, 0)
	err := baseQuery.
		Select(`
			'duplicate_active_course' AS conflict_type,
			inactive.code AS course_code,
			inactive.year,
			inactive.semester,
			active.id AS active_course_id,
			active.name AS active_course_name,
			inactive.id AS inactive_course_id,
			inactive.name AS inactive_course_name
		`).
		Order("inactive.year DESC, inactive.semester DESC, inactive.code ASC, inactive.updated_at DESC").
		Limit(params.Limit).
		Scan(&items).Error
	if err != nil {
		return CourseConflictListResult{}, err
	}

	return CourseConflictListResult{Items: items, Total: total}, nil
}

func FindCourseByID(id string) (*CourseDetail, error) {
	db := config.DB

	var course models.Course
	if err := db.First(&course, "id = ?", id).Error; err != nil {
		return nil, err
	}

	// Instructors
	var ciRows []courseInstructorMemberRow
	db.Raw(`SELECT course_id, user_id, is_primary, assigned_at, permissions FROM course_instructors WHERE course_id = ? ORDER BY is_primary DESC, assigned_at ASC`, id).Scan(&ciRows)

	instructorUserIDs := make([]uint, len(ciRows))
	for i, r := range ciRows {
		instructorUserIDs[i] = r.UserID
	}
	var instructorUsers []models.User
	if len(instructorUserIDs) > 0 {
		db.Where("id IN ?", instructorUserIDs).Find(&instructorUsers)
	}
	instructorUserMap := make(map[uint]models.User, len(instructorUsers))
	for _, user := range instructorUsers {
		instructorUserMap[user.ID] = user
	}
	instructors := make([]UserBasic, 0, len(ciRows))
	for _, row := range ciRows {
		u, ok := instructorUserMap[row.UserID]
		if !ok {
			continue
		}
		instructors = append(instructors, UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar,
			IsPrimary: row.IsPrimary,
			CourseInstructor: &CourseInstructorMeta{
				IsPrimary:   row.IsPrimary,
				AssignedAt:  row.AssignedAt,
				Permissions: ResolveCourseMemberPermissions("instructor", row.Permissions, row.IsPrimary),
			},
		})
	}

	// TAs
	var taRows []courseTAMemberRow
	db.Raw(`SELECT course_id, user_id, assigned_at, permissions FROM course_tas WHERE course_id = ? ORDER BY assigned_at ASC`, id).Scan(&taRows)

	taUserIDs := make([]uint, len(taRows))
	for i, r := range taRows {
		taUserIDs[i] = r.UserID
	}
	var taUsers []models.User
	if len(taUserIDs) > 0 {
		db.Where("id IN ?", taUserIDs).Find(&taUsers)
	}
	taUserMap := make(map[uint]models.User, len(taUsers))
	for _, user := range taUsers {
		taUserMap[user.ID] = user
	}
	tas := make([]UserBasic, 0, len(taRows))
	for _, row := range taRows {
		u, ok := taUserMap[row.UserID]
		if !ok {
			continue
		}
		tas = append(tas, UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar,
			CourseTA: &CourseTAMeta{
				AssignedAt:  row.AssignedAt,
				Permissions: ResolveCourseMemberPermissions("ta", row.Permissions, false),
			},
		})
	}

	// Sections with student count
	var sections []models.CourseSection
	db.Where("course_id = ?", id).Order("section_no ASC").Find(&sections)

	sectionsWithCount := make([]SectionWithCount, len(sections))
	for i, s := range sections {
		var cnt int64
		db.Model(&models.CourseSectionStudent{}).Where("course_section_id = ?", s.ID).Count(&cnt)
		sectionsWithCount[i] = SectionWithCount{CourseSection: s, StudentCount: cnt}
	}

	// Resolve primary instructor (single object for backward compatibility)
	var primaryInstructor *UserBasic
	for i := range instructors {
		if instructors[i].IsPrimary {
			u := instructors[i]
			primaryInstructor = &u
			break
		}
	}
	// Fallback: first instructor, or look up by course.InstructorID
	if primaryInstructor == nil && len(instructors) > 0 {
		u := instructors[0]
		primaryInstructor = &u
	}
	if primaryInstructor == nil && course.InstructorID != nil {
		var u models.User
		if db.First(&u, *course.InstructorID).Error == nil {
			ub := UserBasic{ID: u.ID, FullName: u.FullName, Email: u.Email, Username: u.Username, Avatar: u.Avatar}
			primaryInstructor = &ub
		}
	}

	return &CourseDetail{
		Course:      course,
		Instructor:  primaryInstructor,
		Instructors: instructors,
		TAs:         tas,
		Sections:    sectionsWithCount,
	}, nil
}

func IsActiveCourseExists(code string, year, semester int, excludeID string) bool {
	var count int64
	q := config.DB.Model(&models.Course{}).
		Where("code = ? AND year = ? AND semester = ? AND is_active = true", code, year, semester)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	q.Count(&count)
	return count > 0
}

func CreateCourse(course *models.Course) error {
	defer InvalidateMyCoursesCache()

	return config.DB.Create(course).Error
}

func UpdateCourse(course *models.Course) error {
	defer InvalidateMyCoursesCache()

	return config.DB.Save(course).Error
}

func DeleteCourse(id string) error {
	defer InvalidateMyCoursesCache()

	db := config.DB

	// Get sections
	var sections []models.CourseSection
	db.Where("course_id = ?", id).Find(&sections)
	sectionIDs := make([]uint, len(sections))
	for i, s := range sections {
		sectionIDs[i] = s.ID
	}

	if len(sectionIDs) > 0 {
		db.Where("course_section_id IN ?", sectionIDs).Delete(&models.CourseSectionStudent{})
	}
	db.Where("course_id = ?", id).Delete(&models.CourseSection{})
	db.Where("course_id = ?", id).Delete(&models.CourseTA{})
	db.Where("course_id = ?", id).Delete(&models.CourseInstructor{})
	db.Where("course_id = ?", id).Delete(&models.CourseMember{})

	return db.Where("id = ?", id).Delete(&models.Course{}).Error
}

func ToggleCourseStatus(id string) (*models.Course, error) {
	defer InvalidateMyCoursesCache()

	db := config.DB
	var course models.Course
	if err := db.First(&course, "id = ?", id).Error; err != nil {
		return nil, err
	}
	course.IsActive = !course.IsActive
	if err := db.Save(&course).Error; err != nil {
		return nil, err
	}
	return &course, nil
}

// ============================================================
// Course Access
// ============================================================

func IsUserCourseInstructor(courseID string, userID uint) bool {
	var count int64
	config.DB.Model(&models.CourseInstructor{}).
		Where("course_id = ? AND user_id = ?", courseID, userID).
		Count(&count)
	return count > 0
}

func IsUserCourseTA(courseID string, userID uint) bool {
	var count int64
	config.DB.Model(&models.CourseTA{}).
		Where("course_id = ? AND user_id = ?", courseID, userID).
		Count(&count)
	return count > 0
}

// IsUserEmailStudentInCourse checks (case-insensitive) whether any student
// currently enrolled in the course shares the given email address.
func IsUserEmailStudentInCourse(courseID string, email string) bool {
	if email == "" {
		return false
	}
	var count int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM students s
		JOIN course_section_students css ON css.student_id = s.id
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ? AND s.email != '' AND LOWER(s.email) = LOWER(?)
	`, courseID, email).Scan(&count)
	return count > 0
}

// IsStudentEmailCourseTA checks (case-insensitive) whether any TA
// in the course shares the given email address.
func IsStudentEmailCourseTA(courseID string, email string) bool {
	if email == "" {
		return false
	}
	var count int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM users u
		JOIN course_tas ct ON ct.user_id = u.id
		WHERE ct.course_id = ? AND u.email != '' AND LOWER(u.email) = LOWER(?)
	`, courseID, email).Scan(&count)
	return count > 0
}

// GetUserEmailByID returns the email of a User by its primary key (empty string if not found).
func GetUserEmailByID(userID uint) string {
	var u models.User
	config.DB.Select("email").Where("id = ?", userID).First(&u)
	return u.Email
}

// GetStudentEmailByID returns the email of a Student by its primary key (empty string if not found).
func GetStudentEmailByID(studentID uint) string {
	var s models.Student
	config.DB.Select("email").Where("id = ?", studentID).First(&s)
	return s.Email
}

// ============================================================
// Section Management
// ============================================================

func IsSectionExists(courseID string, sectionNo string, excludeID uint) bool {
	var count int64
	q := config.DB.Model(&models.CourseSection{}).
		Where("course_id = ? AND section_no = ?", courseID, sectionNo)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	q.Count(&count)
	return count > 0
}

func CreateSection(section *models.CourseSection) error {
	defer InvalidateCourseOverviewCache(section.CourseID)
	defer InvalidateMyCoursesCache()

	return config.DB.Create(section).Error
}

func UpdateSection(section *models.CourseSection) error {
	defer InvalidateCourseOverviewCache(section.CourseID)

	return config.DB.Save(section).Error
}

func FindSectionByID(sectionID uint, courseID string) (*models.CourseSection, error) {
	var section models.CourseSection
	if err := config.DB.Where("id = ? AND course_id = ?", sectionID, courseID).First(&section).Error; err != nil {
		return nil, err
	}
	return &section, nil
}

func DeleteSection(sectionID uint, courseID string) error {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	db := config.DB
	db.Where("course_section_id = ?", sectionID).Delete(&models.CourseSectionStudent{})
	return db.Where("id = ? AND course_id = ?", sectionID, courseID).Delete(&models.CourseSection{}).Error
}

// ============================================================
// Instructor Management
// ============================================================

func AddCourseInstructor(courseID string, userID uint, isPrimary bool) error {
	defer InvalidateMyCoursesCache()

	return config.DB.Create(&models.CourseInstructor{
		CourseID:    courseID,
		UserID:      userID,
		IsPrimary:   isPrimary,
		Permissions: EncodeCourseMemberPermissions("instructor", nil),
		AssignedAt:  time.Now(),
	}).Error
}

func BulkAddCourseInstructors(courseID string, userIDs []uint) (addedUsers []UserBasic, skipped int, err error) {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	var existing []models.CourseInstructor
	config.DB.Where("course_id = ? AND user_id IN ?", courseID, userIDs).Find(&existing)
	existingSet := map[uint]bool{}
	for _, e := range existing {
		existingSet[e.UserID] = true
	}

	var toCreate []models.CourseInstructor
	var newUserIDs []uint
	for _, uid := range userIDs {
		if existingSet[uid] {
			skipped++
		} else {
			toCreate = append(toCreate, models.CourseInstructor{
				CourseID: courseID, UserID: uid, IsPrimary: false, Permissions: EncodeCourseMemberPermissions("instructor", nil), AssignedAt: time.Now(),
			})
			newUserIDs = append(newUserIDs, uid)
		}
	}
	if len(toCreate) > 0 {
		if err = config.DB.Create(&toCreate).Error; err != nil {
			return
		}
		var users []models.User
		config.DB.Where("id IN ?", newUserIDs).Find(&users)
		for _, u := range users {
			addedUsers = append(addedUsers, UserBasic{
				ID: u.ID, FullName: u.FullName, Email: u.Email, Username: u.Username, Avatar: u.Avatar,
			})
		}
	}
	if addedUsers == nil {
		addedUsers = []UserBasic{}
	}
	return
}

func RemoveCourseInstructor(courseID string, userID uint) (bool, error) {
	defer InvalidateMyCoursesCache()

	db := config.DB

	// Check if primary
	var ci models.CourseInstructor
	if err := db.Where("course_id = ? AND user_id = ?", courseID, userID).First(&ci).Error; err != nil {
		return false, nil // Not found
	}

	if ci.IsPrimary {
		// Count others
		var count int64
		db.Model(&models.CourseInstructor{}).
			Where("course_id = ? AND user_id <> ?", courseID, userID).Count(&count)
		if count == 0 {
			return false, nil // Can't remove last instructor — caller handles error
		}
		// Transfer primary
		var next models.CourseInstructor
		db.Where("course_id = ? AND user_id <> ?", courseID, userID).
			Order("assigned_at ASC").First(&next)
		db.Model(&next).Update("is_primary", true)
		// Update course.instructor_id for backward compat
		db.Model(&models.Course{}).Where("id = ?", courseID).Update("instructor_id", next.UserID)
	}

	db.Delete(&ci)
	return true, nil
}

func GetCourseInstructorCount(courseID string) int64 {
	var count int64
	config.DB.Model(&models.CourseInstructor{}).Where("course_id = ?", courseID).Count(&count)
	return count
}

func ReplaceAllCourseInstructors(courseID string, userIDs []uint) error {
	defer InvalidateMyCoursesCache()

	db := config.DB
	db.Where("course_id = ?", courseID).Delete(&models.CourseInstructor{})
	if len(userIDs) == 0 {
		return nil
	}
	toCreate := make([]models.CourseInstructor, len(userIDs))
	for i, uid := range userIDs {
		toCreate[i] = models.CourseInstructor{
			CourseID: courseID, UserID: uid,
			IsPrimary:   i == 0,
			Permissions: EncodeCourseMemberPermissions("instructor", nil),
			AssignedAt:  time.Now(),
		}
	}
	return db.Create(&toCreate).Error
}

// ============================================================
// TA Management
// ============================================================

func AddCourseTA(courseID string, userID uint) error {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	return config.DB.Create(&models.CourseTA{
		CourseID:    courseID,
		UserID:      userID,
		Permissions: EncodeCourseMemberPermissions("ta", nil),
		AssignedAt:  time.Now(),
	}).Error
}

func BulkAddCourseTAs(courseID string, userIDs []uint) (addedUsers []UserBasic, skipped int, err error) {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	userIDs = uniqueCourseRepoUintValues(userIDs)
	if len(userIDs) == 0 {
		return []UserBasic{}, 0, nil
	}

	var taUsers []models.User
	if err = config.DB.Where("id IN ? AND role = ? AND is_active = ?", userIDs, "ta", true).Find(&taUsers).Error; err != nil {
		return
	}
	if len(taUsers) == 0 {
		return []UserBasic{}, 0, nil
	}

	// Collect existing student emails in the course for cross-role conflict check
	var studentEmailRows []struct{ Email string }
	config.DB.Raw(`
		SELECT LOWER(s.email) as email
		FROM students s
		JOIN course_section_students css ON css.student_id = s.id
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ? AND s.email != ''
	`, courseID).Scan(&studentEmailRows)
	studentEmailSet := make(map[string]struct{}, len(studentEmailRows))
	for _, r := range studentEmailRows {
		if r.Email != "" {
			studentEmailSet[r.Email] = struct{}{}
		}
	}

	validIDs := make([]uint, 0, len(taUsers))
	for _, user := range taUsers {
		validIDs = append(validIDs, user.ID)
	}

	var existing []models.CourseTA
	config.DB.Where("course_id = ? AND user_id IN ?", courseID, validIDs).Find(&existing)
	existingSet := map[uint]bool{}
	for _, e := range existing {
		existingSet[e.UserID] = true
	}

	var toCreate []models.CourseTA
	addedUsers = []UserBasic{}
	for _, user := range taUsers {
		if existingSet[user.ID] {
			skipped++
		} else {
			if user.Email != "" {
				if _, conflict := studentEmailSet[strings.ToLower(user.Email)]; conflict {
					skipped++
					continue
				}
			}
			toCreate = append(toCreate, models.CourseTA{
				CourseID: courseID, UserID: user.ID, Permissions: EncodeCourseMemberPermissions("ta", nil), AssignedAt: time.Now(),
			})
			addedUsers = append(addedUsers, UserBasic{
				ID:       user.ID,
				FullName: user.FullName,
				Email:    user.Email,
				Username: user.Username,
				Avatar:   user.Avatar,
			})
		}
	}
	if len(toCreate) > 0 {
		if err = config.DB.Create(&toCreate).Error; err != nil {
			return
		}
	}
	if addedUsers == nil {
		addedUsers = []UserBasic{}
	}
	return
}

func uniqueCourseRepoUintValues(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func RemoveCourseTA(courseID string, userID uint) bool {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	result := config.DB.Where("course_id = ? AND user_id = ?", courseID, userID).Delete(&models.CourseTA{})
	return result.RowsAffected > 0
}

// ============================================================
// Section Student Management
// ============================================================

type SectionStudentRow struct {
	ID         uint      `gorm:"column:id" json:"id"`
	StudentID  string    `gorm:"column:student_id" json:"student_id"`
	FullName   string    `gorm:"column:full_name" json:"full_name"`
	Email      string    `gorm:"column:email" json:"email"`
	IsActive   bool      `gorm:"column:is_active" json:"is_active"`
	EnrolledAt time.Time `gorm:"column:enrolled_at" json:"enrolled_at"`
}

type RemovedSectionStudentRow struct {
	RemovalID     uint      `gorm:"column:removal_id" json:"removal_id"`
	SectionID     uint      `gorm:"column:section_id" json:"section_id"`
	SectionNo     string    `gorm:"column:section_no" json:"section_no"`
	StudentRefID  uint      `gorm:"column:student_ref_id" json:"student_ref_id"`
	StudentID     string    `gorm:"column:student_id" json:"student_id"`
	FullName      string    `gorm:"column:full_name" json:"full_name"`
	RemovedAt     time.Time `gorm:"column:removed_at" json:"removed_at"`
	RestoreUntil  time.Time `gorm:"column:restore_until" json:"restore_until"`
	RemainingDays int       `json:"remaining_days"`
}

type StudentImportConflict struct {
	StudentID        uint   `json:"student_id"`
	CurrentSectionID uint   `json:"current_section_id"`
	CurrentSectionNo string `json:"current_section_no"`
}

type BulkAddStudentsResult struct {
	Added     int                     `json:"added"`
	Moved     int                     `json:"moved"`
	Skipped   int                     `json:"skipped"`
	Conflicts []StudentImportConflict `json:"conflicts"`

	// Per-outcome student IDs, for the activity log. Excluded from the HTTP
	// response: the counts and conflicts above are what the import screen
	// shows, and these would only duplicate them on the wire.
	AddedIDs   []uint `json:"-"`
	MovedIDs   []uint `json:"-"`
	SkippedIDs []uint `json:"-"`
}

func GetSectionStudents(sectionID uint) ([]SectionStudentRow, error) {
	var rows []SectionStudentRow
	err := config.DB.Raw(`
		SELECT s.id, s.student_id, s.full_name, s.email, s.is_active, css.enrolled_at
		FROM course_section_students css
		JOIN students s ON s.id = css.student_id
		WHERE css.course_section_id = ?
		ORDER BY s.student_id ASC
	`, sectionID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []SectionStudentRow{}
	}
	return rows, nil
}

func IsStudentInCourse(courseID string, studentID uint) bool {
	var count int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ? AND css.student_id = ?
	`, courseID, studentID).Scan(&count)
	return count > 0
}

func GetStudentCurrentSectionInCourse(courseID string, studentID uint) (*SectionBasic, error) {
	var row SectionBasic
	err := config.DB.Raw(`
		SELECT cs.id, cs.section_no, cs.note
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ? AND css.student_id = ?
		ORDER BY css.enrolled_at DESC
		LIMIT 1
	`, courseID, studentID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

func AddStudentToSection(sectionID uint, studentID uint) error {
	now := time.Now()
	// After the transaction, so a concurrent read cannot repopulate the cache
	// from uncommitted state.
	defer InvalidateCourseOverviewCacheBySection(sectionID)
	defer InvalidateMyCoursesCache()

	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.CourseSectionStudent{
			CourseSectionID: sectionID,
			StudentID:       studentID,
			EnrolledAt:      now,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(&models.CourseSectionStudentRemoval{}).
			Where("course_section_id = ? AND student_id = ? AND restored_at IS NULL AND restore_until > ?", sectionID, studentID, now).
			Update("restored_at", now).Error; err != nil {
			return err
		}

		return nil
	})
}

func BulkAddStudentsToSection(courseID string, sectionID uint, studentIDs []uint, resolveConflicts string) (BulkAddStudentsResult, error) {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	result := BulkAddStudentsResult{Conflicts: []StudentImportConflict{}}

	if resolveConflicts != "move" {
		resolveConflicts = "skip"
	}

	// Deduplicate student IDs from request payload.
	uniqueStudentIDs := make([]uint, 0, len(studentIDs))
	seenStudentIDs := make(map[uint]struct{}, len(studentIDs))
	for _, sid := range studentIDs {
		if sid == 0 {
			continue
		}
		if _, exists := seenStudentIDs[sid]; exists {
			continue
		}
		seenStudentIDs[sid] = struct{}{}
		uniqueStudentIDs = append(uniqueStudentIDs, sid)
	}

	if len(uniqueStudentIDs) == 0 {
		return result, nil
	}

	// Collect TA emails in this course for cross-role conflict check
	var taEmailRows []struct{ Email string }
	config.DB.Raw(`
		SELECT LOWER(u.email) as email
		FROM users u
		JOIN course_tas ct ON ct.user_id = u.id
		WHERE ct.course_id = ? AND u.email != ''
	`, courseID).Scan(&taEmailRows)
	taEmailSet := make(map[string]struct{}, len(taEmailRows))
	for _, r := range taEmailRows {
		if r.Email != "" {
			taEmailSet[r.Email] = struct{}{}
		}
	}

	// Load student emails for the requested IDs
	var studentRecords []models.Student
	config.DB.Select("id, email").Where("id IN ?", uniqueStudentIDs).Find(&studentRecords)
	studentEmailMap := make(map[uint]string, len(studentRecords))
	for _, s := range studentRecords {
		studentEmailMap[s.ID] = s.Email
	}

	// Find current enrollment in this course for all requested students.
	type enrollmentRow struct {
		StudentID uint   `gorm:"column:student_id"`
		SectionID uint   `gorm:"column:section_id"`
		SectionNo string `gorm:"column:section_no"`
	}
	var enrollments []enrollmentRow
	config.DB.Raw(`
		SELECT css.student_id, cs.id AS section_id, cs.section_no
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ? AND css.student_id IN ?
	`, courseID, uniqueStudentIDs).Scan(&enrollments)

	currentEnrollment := make(map[uint]enrollmentRow, len(enrollments))
	for _, row := range enrollments {
		currentEnrollment[row.StudentID] = row
	}

	var toCreate []models.CourseSectionStudent
	for _, sid := range uniqueStudentIDs {
		email := studentEmailMap[sid]
		if email != "" {
			if _, conflict := taEmailSet[strings.ToLower(email)]; conflict {
				result.Skipped++
				result.SkippedIDs = append(result.SkippedIDs, sid)
				continue
			}
		}

		enrollment, alreadyInCourse := currentEnrollment[sid]
		if alreadyInCourse {
			if enrollment.SectionID == sectionID {
				result.Skipped++
				result.SkippedIDs = append(result.SkippedIDs, sid)
				continue
			}

			result.Conflicts = append(result.Conflicts, StudentImportConflict{
				StudentID:        sid,
				CurrentSectionID: enrollment.SectionID,
				CurrentSectionNo: enrollment.SectionNo,
			})

			if resolveConflicts == "move" {
				moved, moveErr := MoveStudentBetweenSections(courseID, enrollment.SectionID, sectionID, sid)
				if moveErr != nil {
					return result, moveErr
				}
				if moved {
					result.Moved++
					result.MovedIDs = append(result.MovedIDs, sid)
				} else {
					result.Skipped++
					result.SkippedIDs = append(result.SkippedIDs, sid)
				}
			} else {
				result.Skipped++
				result.SkippedIDs = append(result.SkippedIDs, sid)
			}
			continue
		}

		toCreate = append(toCreate, models.CourseSectionStudent{
			CourseSectionID: sectionID,
			StudentID:       sid,
			EnrolledAt:      time.Now(),
		})
	}

	if len(toCreate) > 0 {
		err := config.DB.Transaction(func(tx *gorm.DB) error {
			now := time.Now()
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}

			addedIDs := make([]uint, 0, len(toCreate))
			for _, student := range toCreate {
				addedIDs = append(addedIDs, student.StudentID)
			}

			if len(addedIDs) > 0 {
				if err := tx.Model(&models.CourseSectionStudentRemoval{}).
					Where("course_section_id = ? AND student_id IN ? AND restored_at IS NULL AND restore_until > ?", sectionID, addedIDs, now).
					Update("restored_at", now).Error; err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return result, err
		}
		result.Added = len(toCreate)
		for _, student := range toCreate {
			result.AddedIDs = append(result.AddedIDs, student.StudentID)
		}
	}

	return result, nil
}

func RemoveStudentFromSection(sectionID uint, studentID uint) bool {
	result := config.DB.Where("course_section_id = ? AND student_id = ?", sectionID, studentID).Delete(&models.CourseSectionStudent{})
	if result.RowsAffected > 0 {
		InvalidateCourseOverviewCacheBySection(sectionID)
		InvalidateMyCoursesCache()
	}
	return result.RowsAffected > 0
}

func MoveStudentBetweenSections(courseID string, fromSectionID uint, toSectionID uint, studentID uint) (bool, error) {
	defer InvalidateCourseOverviewCache(courseID)
	defer InvalidateMyCoursesCache()

	if fromSectionID == toSectionID {
		return false, nil
	}

	var moved bool
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var sourceSection models.CourseSection
		if err := tx.Where("id = ? AND course_id = ?", fromSectionID, courseID).First(&sourceSection).Error; err != nil {
			return err
		}

		var targetSection models.CourseSection
		if err := tx.Where("id = ? AND course_id = ?", toSectionID, courseID).First(&targetSection).Error; err != nil {
			return err
		}

		var sourceCount int64
		if err := tx.Model(&models.CourseSectionStudent{}).
			Where("course_section_id = ? AND student_id = ?", fromSectionID, studentID).
			Count(&sourceCount).Error; err != nil {
			return err
		}
		if sourceCount == 0 {
			return nil
		}

		var targetCount int64
		if err := tx.Model(&models.CourseSectionStudent{}).
			Where("course_section_id = ? AND student_id = ?", toSectionID, studentID).
			Count(&targetCount).Error; err != nil {
			return err
		}

		if targetCount == 0 {
			if err := tx.Create(&models.CourseSectionStudent{
				CourseSectionID: toSectionID,
				StudentID:       studentID,
				EnrolledAt:      time.Now(),
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("course_section_id = ? AND student_id = ?", fromSectionID, studentID).
			Delete(&models.CourseSectionStudent{}).Error; err != nil {
			return err
		}

		moved = true
		return nil
	})

	if err != nil {
		return false, err
	}

	return moved, nil
}

func ArchiveAndRemoveStudentFromSection(sectionID uint, studentID uint, removedBy uint) (bool, time.Time, error) {
	var (
		found        bool
		restoreUntil time.Time
	)

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var existing models.CourseSectionStudent
		if err := tx.Where("course_section_id = ? AND student_id = ?", sectionID, studentID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		found = true

		var section models.CourseSection
		if err := tx.Select("id", "course_id").First(&section, sectionID).Error; err != nil {
			return err
		}

		now := time.Now()
		restoreUntil = now.Add(10 * 24 * time.Hour)
		removal := models.CourseSectionStudentRemoval{
			CourseID:        section.CourseID,
			CourseSectionID: sectionID,
			StudentID:       studentID,
			RemovedAt:       now,
			RestoreUntil:    restoreUntil,
		}
		if removedBy > 0 {
			removal.RemovedBy = &removedBy
		}

		if err := tx.Create(&removal).Error; err != nil {
			return err
		}

		if err := tx.Where("course_section_id = ? AND student_id = ?", sectionID, studentID).Delete(&models.CourseSectionStudent{}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return false, time.Time{}, err
	}
	if !found {
		return false, time.Time{}, nil
	}

	return true, restoreUntil, nil
}

func GetRestorableRemovedStudents(courseID string, sectionID *uint) ([]RemovedSectionStudentRow, error) {
	var rows []RemovedSectionStudentRow

	query := `
		SELECT
			cssr.id AS removal_id,
			cssr.course_section_id AS section_id,
			cs.section_no AS section_no,
			cssr.student_id AS student_ref_id,
			s.student_id AS student_id,
			s.full_name AS full_name,
			cssr.removed_at AS removed_at,
			cssr.restore_until AS restore_until
		FROM course_section_student_removals cssr
		JOIN students s ON s.id = cssr.student_id
		JOIN course_sections cs ON cs.id = cssr.course_section_id
		WHERE cssr.course_id = ?
			AND cssr.restored_at IS NULL
			AND cssr.restore_until > NOW()
	`
	args := []any{courseID}

	if sectionID != nil {
		query += ` AND cssr.course_section_id = ?`
		args = append(args, *sectionID)
	}

	query += ` ORDER BY cssr.removed_at DESC`

	if err := config.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	now := time.Now()
	for i := range rows {
		remaining := rows[i].RestoreUntil.Sub(now)
		if remaining <= 0 {
			rows[i].RemainingDays = 0
			continue
		}
		days := int((remaining + (24*time.Hour - 1)) / (24 * time.Hour))
		if days < 1 {
			days = 1
		}
		rows[i].RemainingDays = days
	}

	if rows == nil {
		rows = []RemovedSectionStudentRow{}
	}

	return rows, nil
}

func RestoreStudentToSection(sectionID uint, studentID uint) (bool, error) {
	var restored bool

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		var removal models.CourseSectionStudentRemoval
		if err := tx.Where(
			"course_section_id = ? AND student_id = ? AND restored_at IS NULL AND restore_until > ?",
			sectionID,
			studentID,
			now,
		).Order("removed_at DESC").First(&removal).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		var count int64
		if err := tx.Model(&models.CourseSectionStudent{}).
			Where("course_section_id = ? AND student_id = ?", sectionID, studentID).
			Count(&count).Error; err != nil {
			return err
		}

		if count == 0 {
			if err := tx.Create(&models.CourseSectionStudent{
				CourseSectionID: sectionID,
				StudentID:       studentID,
				EnrolledAt:      now,
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&models.CourseSectionStudentRemoval{}).
			Where("id = ?", removal.ID).
			Update("restored_at", now).Error; err != nil {
			return err
		}

		restored = true
		return nil
	})

	if err != nil {
		return false, err
	}

	return restored, nil
}

// CleanupExpiredRemovals deletes archive records whose restore window has passed
// and that have not been restored. Returns the number of rows deleted.
func CleanupExpiredRemovals() (int64, error) {
	result := config.DB.
		Where("restore_until < ? AND restored_at IS NULL", time.Now()).
		Delete(&models.CourseSectionStudentRemoval{})
	return result.RowsAffected, result.Error
}

// ============================================================
// Dropdown helpers
// ============================================================

func GetUsersByRole(role string) ([]models.User, error) {
	var users []models.User
	err := config.DB.Where("role = ? AND is_active = true", role).
		Order("full_name ASC").Find(&users).Error
	return users, err
}

func GetAvailableTAs(courseID string) ([]models.User, error) {
	var users []models.User
	query := config.DB.Model(&models.User{}).
		Where("role = ? AND is_active = true", "ta")

	if trimmedCourseID := strings.TrimSpace(courseID); trimmedCourseID != "" {
		subQuery := config.DB.Model(&models.CourseTA{}).
			Select("user_id").
			Where("course_id = ?", trimmedCourseID)
		query = query.Where("id NOT IN (?)", subQuery)
	}

	err := query.Order("full_name ASC").Find(&users).Error
	return users, err
}

// GetMyCourses is read-through cached per (user, role, params) — it is the
// heaviest call on the home dashboard (8-11 sequential queries with no single
// dominant one to index away) and the sidebar course dropdown and the home
// page's paginated list both call it on every load with different params, so
// an uncached repeat visit paid the full cost twice.
func GetMyCourses(userID uint, role string, params CourseListParams) (CourseListResult, error) {
	cacheKey := myCoursesCacheKey(userID, role, params)
	ttl := myCoursesTTL()

	if ttl > 0 {
		var cached CourseListResult
		if config.CacheGetJSON(cacheKey, &cached) {
			return cached, nil
		}
	}

	result, err := buildMyCourses(userID, role, params)
	if err != nil {
		return CourseListResult{}, err
	}

	if ttl > 0 {
		config.CacheSetJSON(cacheKey, result, ttl)
	}

	return result, nil
}

func buildMyCourses(userID uint, role string, params CourseListParams) (CourseListResult, error) {
	db := config.DB

	query := db.Model(&models.Course{})

	var resolvedStudentID uint

	// Filter by membership
	if role == "instructor" {
		query = query.Joins(`JOIN course_instructors ci ON ci.course_id = courses.id AND ci.user_id = ?`, userID)
	} else if role == "ta" {
		query = query.Joins(`JOIN course_tas ct ON ct.course_id = courses.id AND ct.user_id = ?`, userID)
	} else if role == "student" {
		// userID here IS the student.ID (from students table) — no User lookup needed
		resolvedStudentID = userID
		query = query.Joins(`JOIN course_sections cs ON cs.course_id = courses.id`).
			Joins(`JOIN course_section_students css ON css.course_section_id = cs.id AND css.student_id = ?`, userID).
			Distinct("courses.id")
	}

	// Search
	if params.Search != "" {
		like := "%" + strings.TrimSpace(params.Search) + "%"
		query = query.Where("courses.code ILIKE ? OR courses.name ILIKE ?", like, like)
	}
	if params.Year > 0 {
		query = query.Where("courses.year = ?", params.Year)
	}
	if params.Semester > 0 {
		query = query.Where("courses.semester = ?", params.Semester)
	}
	switch params.Status {
	case "active":
		query = query.Where("courses.is_active = true")
	case "inactive":
		query = query.Where("courses.is_active = false")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return CourseListResult{}, err
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 12
	}
	offset := (params.Page - 1) * params.Limit

	col := "courses.created_at"
	validCols := map[string]string{
		"created_at": "courses.created_at",
		"code":       "courses.code",
		"name":       "courses.name",
		"year":       "courses.year",
		"semester":   "courses.semester",
	}
	if v, ok := validCols[params.SortBy]; ok {
		col = v
	}
	dir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		dir = "ASC"
	}

	var courses []models.Course
	if err := query.Select("courses.*").Order(col + " " + dir).
		Limit(params.Limit).Offset(offset).
		Find(&courses).Error; err != nil {
		return CourseListResult{}, err
	}

	courseIDs := make([]string, len(courses))
	for i, c := range courses {
		courseIDs[i] = c.ID
	}

	// Batch: instructors
	var ciRows2 []courseInstructorMemberRow
	if len(courseIDs) > 0 {
		db.Raw(`SELECT course_id, user_id, is_primary, assigned_at, permissions FROM course_instructors WHERE course_id IN ? ORDER BY course_id ASC, is_primary DESC, assigned_at ASC`, courseIDs).Scan(&ciRows2)
	}
	userIDSet2 := map[uint]bool{}
	for _, r := range ciRows2 {
		userIDSet2[r.UserID] = true
	}
	userIDs2 := make([]uint, 0, len(userIDSet2))
	for uid := range userIDSet2 {
		userIDs2 = append(userIDs2, uid)
	}
	var users2 []models.User
	if len(userIDs2) > 0 {
		db.Where("id IN ?", userIDs2).Find(&users2)
	}
	userMap2 := map[uint]models.User{}
	for _, u := range users2 {
		userMap2[u.ID] = u
	}
	courseInstructorMap2 := map[string][]UserBasic{}
	for _, r := range ciRows2 {
		u := userMap2[r.UserID]
		courseInstructorMap2[r.CourseID] = append(courseInstructorMap2[r.CourseID], UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar, IsPrimary: r.IsPrimary,
			CourseInstructor: &CourseInstructorMeta{
				IsPrimary:   r.IsPrimary,
				AssignedAt:  r.AssignedAt,
				Permissions: ResolveCourseMemberPermissions("instructor", r.Permissions, r.IsPrimary),
			},
		})
	}

	// Batch: TA counts
	type countRow struct {
		CourseID string
		Cnt      int64
	}
	var taCounts2 []countRow
	if len(courseIDs) > 0 {
		db.Raw(`SELECT course_id, COUNT(*) as cnt FROM course_tas WHERE course_id IN ? GROUP BY course_id`, courseIDs).Scan(&taCounts2)
	}
	taCountMap2 := map[string]int64{}
	for _, r := range taCounts2 {
		taCountMap2[r.CourseID] = r.Cnt
	}

	// Batch: student counts
	var studentCounts2 []countRow
	if len(courseIDs) > 0 {
		db.Raw(`
			SELECT cs.course_id, COUNT(css.id) as cnt
			FROM course_sections cs
			LEFT JOIN course_section_students css ON css.course_section_id = cs.id
			WHERE cs.course_id IN ?
			GROUP BY cs.course_id
		`, courseIDs).Scan(&studentCounts2)
	}
	studentCountMap2 := map[string]int64{}
	for _, r := range studentCounts2 {
		studentCountMap2[r.CourseID] = r.Cnt
	}

	// Batch: sections
	var allSections2 []models.CourseSection
	if len(courseIDs) > 0 {
		db.Where("course_id IN ?", courseIDs).Order("id ASC").Find(&allSections2)
	}
	courseSectionsMap2 := map[string][]SectionBasic{}
	for _, s := range allSections2 {
		courseSectionsMap2[s.CourseID] = append(courseSectionsMap2[s.CourseID], SectionBasic{
			ID: s.ID, SectionNo: s.SectionNo, Note: s.Note,
		})
	}
	courseTAsMap2 := getCourseTAMap(courseIDs)

	result := make([]CourseWithCounts, len(courses))
	for i, c := range courses {
		instructors := courseInstructorMap2[c.ID]
		var primaryInstructor *UserBasic
		for j := range instructors {
			if instructors[j].IsPrimary {
				u := instructors[j]
				primaryInstructor = &u
				break
			}
		}
		if primaryInstructor == nil && c.InstructorID != nil {
			if u, ok := userMap2[*c.InstructorID]; ok {
				primaryInstructor = &UserBasic{
					ID: u.ID, FullName: u.FullName, Email: u.Email,
					Username: u.Username, Avatar: u.Avatar,
				}
			}
		}
		if primaryInstructor == nil && len(instructors) > 0 {
			primaryInstructor = &instructors[0]
		}
		sections := courseSectionsMap2[c.ID]
		if sections == nil {
			sections = []SectionBasic{}
		}
		tas := courseTAsMap2[c.ID]
		if tas == nil {
			tas = []UserBasic{}
		}
		result[i] = CourseWithCounts{
			Course:       c,
			Instructor:   primaryInstructor,
			Instructors:  instructors,
			TAs:          tas,
			Sections:     sections,
			TaCount:      taCountMap2[c.ID],
			StudentCount: studentCountMap2[c.ID],
			MyGroups:     []StudentMyGroup{},
		}
	}

	// For student role, look up which section they're enrolled in per course
	if resolvedStudentID > 0 && len(courseIDs) > 0 {
		type studentSectionRow struct {
			CourseID  string
			SectionNo string
		}
		var sectionRows []studentSectionRow
		db.Raw(`
			SELECT cs.course_id, cs.section_no
			FROM course_sections cs
			JOIN course_section_students css ON css.course_section_id = cs.id
			WHERE css.student_id = ? AND cs.course_id IN ?
		`, resolvedStudentID, courseIDs).Scan(&sectionRows)
		sectionNoMap := map[string]string{}
		for _, row := range sectionRows {
			sectionNoMap[row.CourseID] = row.SectionNo
		}
		for i := range result {
			result[i].MySectionNo = sectionNoMap[result[i].Course.ID]
		}

		type studentGroupRow struct {
			CourseID   string `gorm:"column:course_id"`
			GroupID    uint   `gorm:"column:group_id"`
			GroupName  string `gorm:"column:group_name"`
			GroupType  string `gorm:"column:group_type"`
			WeekNumber *int   `gorm:"column:week_number"`
		}
		var studentGroupRows []studentGroupRow
		db.Raw(`
			SELECT sg.course_id, sg.id AS group_id, sg.name AS group_name, sg.group_type, sg.week_number
			FROM student_group_members sgm
			JOIN student_groups sg ON sg.id = sgm.group_id
			WHERE sgm.student_id = ? AND sg.course_id IN ?
			ORDER BY sg.course_id ASC,
			CASE WHEN sg.group_type = 'permanent' THEN 0 ELSE 1 END,
			sg.week_number ASC NULLS LAST,
			sg.id ASC
		`, resolvedStudentID, courseIDs).Scan(&studentGroupRows)

		type studentGroupMemberRow struct {
			GroupID   uint   `gorm:"column:group_id"`
			ID        uint   `gorm:"column:id"`
			StudentID string `gorm:"column:student_id"`
			FullName  string `gorm:"column:full_name"`
		}

		groupIDs := make([]uint, 0, len(studentGroupRows))
		groupByID := make(map[uint]StudentMyGroup, len(studentGroupRows))
		for _, row := range studentGroupRows {
			groupIDs = append(groupIDs, row.GroupID)
			groupByID[row.GroupID] = StudentMyGroup{
				ID:         row.GroupID,
				Name:       row.GroupName,
				GroupType:  row.GroupType,
				WeekNumber: row.WeekNumber,
				Members:    []StudentMyGroupMember{},
			}
		}

		membersByGroup := make(map[uint][]StudentMyGroupMember)
		if len(groupIDs) > 0 {
			var memberRows []studentGroupMemberRow
			db.Raw(`
				SELECT sgm.group_id, s.id, s.student_id, s.full_name
				FROM student_group_members sgm
				JOIN students s ON s.id = sgm.student_id
				WHERE sgm.group_id IN ?
				ORDER BY sgm.group_id ASC, s.full_name ASC
			`, groupIDs).Scan(&memberRows)

			for _, member := range memberRows {
				membersByGroup[member.GroupID] = append(membersByGroup[member.GroupID], StudentMyGroupMember{
					ID:        member.ID,
					StudentID: member.StudentID,
					FullName:  member.FullName,
				})
			}
		}

		groupsByCourse := make(map[string][]StudentMyGroup)
		for _, row := range studentGroupRows {
			group := groupByID[row.GroupID]
			group.Members = membersByGroup[row.GroupID]
			groupsByCourse[row.CourseID] = append(groupsByCourse[row.CourseID], group)
		}

		for i := range result {
			if groups, ok := groupsByCourse[result[i].Course.ID]; ok {
				result[i].MyGroups = groups
			}
		}
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return CourseListResult{
		Courses:     result,
		Total:       total,
		TotalPages:  totalPages,
		CurrentPage: params.Page,
		PerPage:     params.Limit,
	}, nil
}

func GetMyCourseStats(userID uint, role string) (*MyCourseStats, error) {
	buildQuery := func() *gorm.DB {
		query := config.DB.Model(&models.Course{})
		switch role {
		case "instructor":
			query = query.Joins(`JOIN course_instructors ci ON ci.course_id = courses.id AND ci.user_id = ?`, userID)
		case "ta":
			query = query.Joins(`JOIN course_tas ct ON ct.course_id = courses.id AND ct.user_id = ?`, userID)
		default:
			query = query.Where("1 = 0")
		}
		return query
	}

	stats := &MyCourseStats{}
	if err := buildQuery().Distinct("courses.id").Count(&stats.Total).Error; err != nil {
		return nil, err
	}
	if err := buildQuery().Where("courses.is_active = ?", true).Distinct("courses.id").Count(&stats.ByStatus.Active).Error; err != nil {
		return nil, err
	}
	if err := buildQuery().Where("courses.is_active = ?", false).Distinct("courses.id").Count(&stats.ByStatus.Inactive).Error; err != nil {
		return nil, err
	}

	if stats.Total > 0 {
		if err := buildQuery().Distinct("courses.year").Order("courses.year DESC").Pluck("courses.year", &stats.Years).Error; err != nil {
			return nil, err
		}
	}

	if stats.Years == nil {
		stats.Years = []int{}
	}

	return stats, nil
}

// BulkToggleCourseStatus enables or disables courses in bulk.
// For enable=true, already-active courses and courses with an activation conflict are skipped.
// For enable=false, all given courses are set to inactive.
// Returns slices of courses that were toggled and courses that were skipped.
func BulkToggleCourseStatus(ids []string, enable bool) (toggled []models.Course, skipped []models.Course, err error) {
	toggled = []models.Course{}
	skipped = []models.Course{}
	if len(ids) == 0 {
		return
	}

	var courses []models.Course
	if err = config.DB.Where("id IN ?", ids).Find(&courses).Error; err != nil {
		return
	}

	if !enable {
		if len(courses) == 0 {
			return
		}
		if err = config.DB.Model(&models.Course{}).Where("id IN ?", ids).Update("is_active", false).Error; err != nil {
			return
		}
		toggled = courses
		return
	}

	// Enable: skip already-active, skip conflicts
	toUpdateIDs := make([]string, 0, len(courses))
	for _, c := range courses {
		if c.IsActive {
			skipped = append(skipped, c)
			continue
		}
		if IsActiveCourseExists(c.Code, c.Year, c.Semester, c.ID) {
			skipped = append(skipped, c)
			continue
		}
		toUpdateIDs = append(toUpdateIDs, c.ID)
		toggled = append(toggled, c)
	}

	if len(toUpdateIDs) > 0 {
		err = config.DB.Model(&models.Course{}).Where("id IN ?", toUpdateIDs).Update("is_active", true).Error
	}
	return
}

// CourseExportRow is a course with aggregated fields for CSV export.
type CourseExportRow struct {
	models.Course
	InstructorNames string
	SectionsCount   int64
}

// GetCoursesForExport returns up to 5000 courses matching the given filter params.
func GetCoursesForExport(params CourseListParams) ([]CourseExportRow, error) {
	db := config.DB

	query := db.Model(&models.Course{})

	if params.Search != "" {
		like := "%" + strings.TrimSpace(params.Search) + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", like, like)
	}
	if params.Year > 0 {
		query = query.Where("year = ?", params.Year)
	}
	if params.Semester > 0 {
		query = query.Where("semester = ?", params.Semester)
	}
	switch params.Status {
	case "active":
		query = query.Where("is_active = true")
	case "inactive":
		query = query.Where("is_active = false")
	}

	var courses []models.Course
	if err := query.Order("created_at DESC").Limit(5000).Find(&courses).Error; err != nil {
		return nil, err
	}

	if len(courses) == 0 {
		return []CourseExportRow{}, nil
	}

	courseIDs := make([]string, len(courses))
	for i, c := range courses {
		courseIDs[i] = c.ID
	}

	// Batch: instructors
	var ciRows []courseInstructorMemberRow
	db.Raw(`SELECT course_id, user_id, is_primary, assigned_at, permissions FROM course_instructors WHERE course_id IN ? ORDER BY course_id ASC, is_primary DESC, assigned_at ASC`, courseIDs).Scan(&ciRows)

	userIDSet := map[uint]struct{}{}
	for _, r := range ciRows {
		userIDSet[r.UserID] = struct{}{}
	}
	userIDs := make([]uint, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}
	userNameMap := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		db.Where("id IN ?", userIDs).Find(&users)
		for _, u := range users {
			userNameMap[u.ID] = u.FullName
		}
	}

	instructorNamesMap := map[string][]string{}
	for _, r := range ciRows {
		if name, ok := userNameMap[r.UserID]; ok {
			instructorNamesMap[r.CourseID] = append(instructorNamesMap[r.CourseID], name)
		}
	}

	// Batch: section counts
	type secCountRow struct {
		CourseID string
		Cnt      int64
	}
	var secCounts []secCountRow
	db.Raw(`SELECT course_id, COUNT(*) as cnt FROM course_sections WHERE course_id IN ? GROUP BY course_id`, courseIDs).Scan(&secCounts)
	sectionCountMap := map[string]int64{}
	for _, r := range secCounts {
		sectionCountMap[r.CourseID] = r.Cnt
	}

	result := make([]CourseExportRow, len(courses))
	for i, c := range courses {
		result[i] = CourseExportRow{
			Course:          c,
			InstructorNames: strings.Join(instructorNamesMap[c.ID], ","),
			SectionsCount:   sectionCountMap[c.ID],
		}
	}
	return result, nil
}
