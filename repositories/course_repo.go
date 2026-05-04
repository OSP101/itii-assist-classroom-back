package repositories

import (
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
	Instructor   *UserBasic     `json:"instructor"`
	Instructors  []UserBasic    `json:"instructors"`
	Sections     []SectionBasic `json:"sections"`
	TAs          []UserBasic    `json:"tas"`
	TaCount      int64          `json:"taCount"`
	StudentCount int64          `json:"studentCount"`
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

type UserBasic struct {
	ID        uint   `json:"id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	Avatar    string `json:"avatar"`
	IsPrimary bool   `json:"is_primary,omitempty"`
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

type courseMemberRow struct {
	CourseID string
	UserID   uint
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

	var rows []courseMemberRow
	if err := config.DB.Raw(`SELECT course_id, user_id FROM course_tas WHERE course_id IN ? ORDER BY course_id ASC, assigned_at ASC`, courseIDs).Scan(&rows).Error; err != nil {
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
	type ciRow struct {
		CourseID  string
		UserID    uint
		IsPrimary bool
	}
	var ciRows []ciRow
	db.Raw(`SELECT course_id, user_id, is_primary FROM course_instructors WHERE course_id IN ?`, courseIDs).Scan(&ciRows)

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

func FindCourseByID(id string) (*CourseDetail, error) {
	db := config.DB

	var course models.Course
	if err := db.First(&course, "id = ?", id).Error; err != nil {
		return nil, err
	}

	// Instructors
	type ciRow struct {
		UserID    uint
		IsPrimary bool
	}
	var ciRows []ciRow
	db.Raw(`SELECT user_id, is_primary FROM course_instructors WHERE course_id = ? ORDER BY is_primary DESC, assigned_at ASC`, id).Scan(&ciRows)

	instructorUserIDs := make([]uint, len(ciRows))
	for i, r := range ciRows {
		instructorUserIDs[i] = r.UserID
	}
	primaryMap := map[uint]bool{}
	for _, r := range ciRows {
		primaryMap[r.UserID] = r.IsPrimary
	}

	var instructorUsers []models.User
	if len(instructorUserIDs) > 0 {
		db.Where("id IN ?", instructorUserIDs).Find(&instructorUsers)
	}
	instructors := make([]UserBasic, len(instructorUsers))
	for i, u := range instructorUsers {
		instructors[i] = UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar,
			IsPrimary: primaryMap[u.ID],
		}
	}

	// TAs
	type taRow struct{ UserID uint }
	var taRows []taRow
	db.Raw(`SELECT user_id FROM course_tas WHERE course_id = ? ORDER BY assigned_at ASC`, id).Scan(&taRows)

	taUserIDs := make([]uint, len(taRows))
	for i, r := range taRows {
		taUserIDs[i] = r.UserID
	}
	var taUsers []models.User
	if len(taUserIDs) > 0 {
		db.Where("id IN ?", taUserIDs).Find(&taUsers)
	}
	tas := make([]UserBasic, len(taUsers))
	for i, u := range taUsers {
		tas[i] = UserBasic{
			ID: u.ID, FullName: u.FullName, Email: u.Email,
			Username: u.Username, Avatar: u.Avatar,
		}
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
	return config.DB.Create(course).Error
}

func UpdateCourse(course *models.Course) error {
	return config.DB.Save(course).Error
}

func DeleteCourse(id string) error {
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
	return config.DB.Create(section).Error
}

func UpdateSection(section *models.CourseSection) error {
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
	db := config.DB
	db.Where("course_section_id = ?", sectionID).Delete(&models.CourseSectionStudent{})
	return db.Where("id = ? AND course_id = ?", sectionID, courseID).Delete(&models.CourseSection{}).Error
}

// ============================================================
// Instructor Management
// ============================================================

func AddCourseInstructor(courseID string, userID uint, isPrimary bool) error {
	return config.DB.Create(&models.CourseInstructor{
		CourseID:   courseID,
		UserID:     userID,
		IsPrimary:  isPrimary,
		AssignedAt: time.Now(),
	}).Error
}

func BulkAddCourseInstructors(courseID string, userIDs []uint) (addedUsers []UserBasic, skipped int, err error) {
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
				CourseID: courseID, UserID: uid, IsPrimary: false, AssignedAt: time.Now(),
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
	db := config.DB
	db.Where("course_id = ?", courseID).Delete(&models.CourseInstructor{})
	if len(userIDs) == 0 {
		return nil
	}
	toCreate := make([]models.CourseInstructor, len(userIDs))
	for i, uid := range userIDs {
		toCreate[i] = models.CourseInstructor{
			CourseID: courseID, UserID: uid,
			IsPrimary:  i == 0,
			AssignedAt: time.Now(),
		}
	}
	return db.Create(&toCreate).Error
}

// ============================================================
// TA Management
// ============================================================

func AddCourseTA(courseID string, userID uint) error {
	return config.DB.Create(&models.CourseTA{
		CourseID:   courseID,
		UserID:     userID,
		AssignedAt: time.Now(),
	}).Error
}

func BulkAddCourseTAs(courseID string, userIDs []uint) (addedUsers []UserBasic, skipped int, err error) {
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
			toCreate = append(toCreate, models.CourseTA{
				CourseID: courseID, UserID: user.ID, AssignedAt: time.Now(),
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

func AddStudentToSection(sectionID uint, studentID uint) error {
	return config.DB.Create(&models.CourseSectionStudent{
		CourseSectionID: sectionID,
		StudentID:       studentID,
		EnrolledAt:      time.Now(),
	}).Error
}

func BulkAddStudentsToSection(sectionID uint, studentIDs []uint) (added, skipped int, err error) {
	// Find already enrolled in this section
	var existing []models.CourseSectionStudent
	config.DB.Where("course_section_id = ? AND student_id IN ?", sectionID, studentIDs).Find(&existing)
	existingSet := map[uint]bool{}
	for _, e := range existing {
		existingSet[e.StudentID] = true
	}

	var toCreate []models.CourseSectionStudent
	for _, sid := range studentIDs {
		if existingSet[sid] {
			skipped++
		} else {
			toCreate = append(toCreate, models.CourseSectionStudent{
				CourseSectionID: sectionID,
				StudentID:       sid,
				EnrolledAt:      time.Now(),
			})
		}
	}
	if len(toCreate) > 0 {
		err = config.DB.Create(&toCreate).Error
		added = len(toCreate)
	}
	return
}

func RemoveStudentFromSection(sectionID uint, studentID uint) bool {
	result := config.DB.Where("course_section_id = ? AND student_id = ?", sectionID, studentID).Delete(&models.CourseSectionStudent{})
	return result.RowsAffected > 0
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

func GetMyCourses(userID uint, role string, params CourseListParams) (CourseListResult, error) {
	db := config.DB

	query := db.Model(&models.Course{})

	// Filter by membership
	if role == "instructor" {
		query = query.Joins(`JOIN course_instructors ci ON ci.course_id = courses.id AND ci.user_id = ?`, userID)
	} else if role == "ta" {
		query = query.Joins(`JOIN course_tas ct ON ct.course_id = courses.id AND ct.user_id = ?`, userID)
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
	type ciRow2 struct {
		CourseID  string
		UserID    uint
		IsPrimary bool
	}
	var ciRows2 []ciRow2
	if len(courseIDs) > 0 {
		db.Raw(`SELECT course_id, user_id, is_primary FROM course_instructors WHERE course_id IN ?`, courseIDs).Scan(&ciRows2)
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
