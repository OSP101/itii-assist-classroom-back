package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ─── List / Search ───────────────────────────────────────────────────────────

type StudentListParams struct {
	Page      int
	Limit     int
	Search    string
	Status    string
	SortBy    string
	SortOrder string
}

type StudentListResult struct {
	Students   []models.Student
	Total      int64
	Page       int
	Limit      int
	TotalPages int
}

func GetStudents(params StudentListParams) (*StudentListResult, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 10
	}
	if params.SortBy == "" {
		params.SortBy = "created_at"
	}
	if params.SortOrder == "" {
		params.SortOrder = "desc"
	}

	db := config.DB.Model(&models.Student{})

	if params.Search != "" {
		like := "%" + params.Search + "%"
		db = db.Where("student_id ILIKE ? OR full_name ILIKE ? OR email ILIKE ?", like, like, like)
	}
	if params.Status == "active" {
		db = db.Where("is_active = ?", true)
	} else if params.Status == "inactive" {
		db = db.Where("is_active = ?", false)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}

	// Whitelist สำหรับป้องกัน SQL Injection ผ่านค่า SortBy
	validSortCols := map[string]bool{
		"created_at": true, "updated_at": true, "student_id": true,
		"full_name": true, "email": true, "is_active": true,
	}
	sortCol := "created_at"
	if validSortCols[params.SortBy] {
		sortCol = params.SortBy
	}
	sortDir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		sortDir = "ASC"
	}
	offset := (params.Page - 1) * params.Limit

	var students []models.Student
	if err := db.Order(sortCol + " " + sortDir).Limit(params.Limit).Offset(offset).Find(&students).Error; err != nil {
		return nil, err
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return &StudentListResult{
		Students:   students,
		Total:      total,
		Page:       params.Page,
		Limit:      params.Limit,
		TotalPages: totalPages,
	}, nil
}

// ─── Single record ────────────────────────────────────────────────────────────

func FindStudentByID(id uint) (*models.Student, error) {
	var s models.Student
	err := config.DB.First(&s, id).Error
	return &s, err
}

func FindStudentByStudentID(studentID string) (*models.Student, error) {
	var s models.Student
	err := config.DB.Where("student_id = ?", studentID).First(&s).Error
	return &s, err
}

func FindStudentByEmail(email string) (*models.Student, error) {
	var s models.Student
	err := config.DB.Where("LOWER(email) = LOWER(?)", strings.TrimSpace(email)).First(&s).Error
	return &s, err
}

func ResolveStudentFromUser(user *models.User) (*models.Student, error) {
	if user == nil {
		return nil, gorm.ErrRecordNotFound
	}

	if student, err := FindStudentByStudentID(strings.TrimSpace(user.Username)); err == nil {
		return student, nil
	}

	if strings.TrimSpace(user.Email) != "" {
		if student, err := FindStudentByEmail(user.Email); err == nil {
			return student, nil
		}
	}

	return nil, gorm.ErrRecordNotFound
}

type LookupStudentStudent struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
}

type LookupStudentCourseSection struct {
	ID        uint   `json:"id"`
	SectionNo string `json:"section_no"`
}

type LookupStudentCourse struct {
	ID       string                       `json:"id"`
	Code     string                       `json:"code"`
	Name     string                       `json:"name"`
	Year     int                          `json:"year"`
	Semester int                          `json:"semester"`
	IsActive bool                         `json:"is_active"`
	Sections []LookupStudentCourseSection `json:"sections"`
}

type LookupStudentGroupInfo struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type LookupStudentAssignmentSubItem struct {
	ID       uint       `json:"id"`
	Name     string     `json:"name"`
	MaxScore float64    `json:"max_score"`
	Score    *float64   `json:"score"`
	Grader   *string    `json:"grader"`
	GradedAt *time.Time `json:"graded_at"`
}

type LookupStudentAssignment struct {
	ID                uint                             `json:"id"`
	Title             string                           `json:"title"`
	Type              string                           `json:"type"`
	MaxScore          float64                          `json:"max_score"`
	Score             *float64                         `json:"score"`
	Status            string                           `json:"status"`
	Grader            *string                          `json:"grader"`
	GradedAt          *time.Time                       `json:"graded_at"`
	Comment           *string                          `json:"comment"`
	GradedVia         *string                          `json:"graded_via"`
	IsGroupAssignment bool                             `json:"is_group_assignment"`
	GroupInfo         *LookupStudentGroupInfo          `json:"group_info"`
	SubItems          []LookupStudentAssignmentSubItem `json:"sub_items"`
}

type LookupStudentBonusRecord struct {
	Score   float64    `json:"score"`
	Reason  *string    `json:"reason"`
	GivenBy *string    `json:"given_by"`
	GivenAt *time.Time `json:"given_at"`
}

type LookupStudentBonusScore struct {
	Total   float64                    `json:"total"`
	Records []LookupStudentBonusRecord `json:"records"`
}

type LookupStudentAttendanceRecord struct {
	ID           uint       `json:"id"`
	SessionTitle string     `json:"session_title"`
	Date         *time.Time `json:"date"`
	Status       string     `json:"status"`
	CheckInTime  *time.Time `json:"check_in_time"`
	Note         *string    `json:"note"`
}

type LookupStudentAttendanceSummary struct {
	Present int `json:"present"`
	Late    int `json:"late"`
	Leave   int `json:"leave"`
	Absent  int `json:"absent"`
}

type LookupStudentAttendance struct {
	Records []LookupStudentAttendanceRecord `json:"records"`
	Summary LookupStudentAttendanceSummary  `json:"summary"`
}

type LookupStudentExamScore struct {
	ID        uint       `json:"id"`
	ExamType  string     `json:"exam_type"`
	Component string     `json:"component"`
	Score     *float64   `json:"score"`
	MaxScore  float64    `json:"max_score"`
	Grader    *string    `json:"grader"`
	GradedAt  *time.Time `json:"graded_at"`
	Comment   *string    `json:"comment"`
}

type LookupStudentCourseOverview struct {
	Course        LookupStudentCourse       `json:"course"`
	Assignments   []LookupStudentAssignment `json:"assignments"`
	TotalScore    float64                   `json:"totalScore"`
	TotalMaxScore float64                   `json:"totalMaxScore"`
	BonusScore    LookupStudentBonusScore   `json:"bonusScore"`
	Attendance    LookupStudentAttendance   `json:"attendance"`
	ExamScores    []LookupStudentExamScore  `json:"examScores"`
	Progress      int                       `json:"progress"`
}

type LookupStudentResult struct {
	Student LookupStudentStudent          `json:"student"`
	Courses []LookupStudentCourseOverview `json:"courses"`
}

type studentLookupEnrollmentRow struct {
	CourseID  string `gorm:"column:course_id"`
	Code      string `gorm:"column:code"`
	Name      string `gorm:"column:name"`
	Year      int    `gorm:"column:year"`
	Semester  int    `gorm:"column:semester"`
	IsActive  bool   `gorm:"column:is_active"`
	SectionNo string `gorm:"column:section_no"`
	SectionID uint   `gorm:"column:section_id"`
}

type studentLookupGroupMembershipRow struct {
	GroupID    uint   `gorm:"column:group_id"`
	CourseID   string `gorm:"column:course_id"`
	GroupName  string `gorm:"column:group_name"`
	GroupType  string `gorm:"column:group_type"`
	WeekNumber *int   `gorm:"column:week_number"`
}

type studentCourseGroupMembership struct {
	Permanent *LookupStudentGroupInfo
	Weekly    map[int]*LookupStudentGroupInfo
}

type studentLookupMainScoreRow struct {
	AssignmentID uint       `gorm:"column:assignment_id"`
	GroupID      *uint      `gorm:"column:group_id"`
	Score        float64    `gorm:"column:score"`
	Status       string     `gorm:"column:status"`
	GraderName   string     `gorm:"column:grader_name"`
	GradedAt     *time.Time `gorm:"column:graded_at"`
	Comment      string     `gorm:"column:comment"`
	GradedVia    string     `gorm:"column:graded_via"`
}

type studentLookupSubItemScoreRow struct {
	AssignmentID uint       `gorm:"column:assignment_id"`
	SubItemID    uint       `gorm:"column:sub_item_id"`
	Score        float64    `gorm:"column:score"`
	GraderName   string     `gorm:"column:grader_name"`
	GradedAt     *time.Time `gorm:"column:graded_at"`
}

type studentLookupBonusRow struct {
	CourseID string     `gorm:"column:course_id"`
	Score    float64    `gorm:"column:score"`
	Reason   string     `gorm:"column:reason"`
	GivenBy  string     `gorm:"column:given_by"`
	GivenAt  *time.Time `gorm:"column:given_at"`
}

type studentLookupAttendanceRow struct {
	CourseID     string     `gorm:"column:course_id"`
	ID           uint       `gorm:"column:id"`
	SessionTitle string     `gorm:"column:session_title"`
	Date         *time.Time `gorm:"column:date"`
	Status       string     `gorm:"column:status"`
	CheckInTime  *time.Time `gorm:"column:check_in_time"`
	Note         string     `gorm:"column:note"`
}

type studentLookupExamRow struct {
	CourseID   string     `gorm:"column:course_id"`
	ID         uint       `gorm:"column:id"`
	ExamType   string     `gorm:"column:exam_type"`
	Component  string     `gorm:"column:component"`
	Score      *float64   `gorm:"column:score"`
	MaxScore   float64    `gorm:"column:max_score"`
	GraderName string     `gorm:"column:grader_name"`
	GradedAt   *time.Time `gorm:"column:graded_at"`
	Comment    string     `gorm:"column:comment"`
}

type lookupStudentScoreSnapshot struct {
	Score     *float64
	Status    string
	Grader    *string
	GradedAt  *time.Time
	Comment   *string
	IsGroup   bool
	GroupInfo *LookupStudentGroupInfo
	GradedVia string
}

type lookupStudentSubItemSnapshot struct {
	Score    *float64
	Grader   *string
	GradedAt *time.Time
}

func LookupStudentScores(studentID string) (*LookupStudentResult, error) {
	return lookupStudentScores(studentID, "")
}

func LookupStudentCourseScores(studentID string, courseID string) (*LookupStudentResult, error) {
	return lookupStudentScores(studentID, courseID)
}

func lookupStudentScores(studentID string, courseID string) (*LookupStudentResult, error) {
	student, err := FindStudentByStudentID(studentID)
	if err != nil {
		return nil, err
	}

	result := &LookupStudentResult{
		Student: LookupStudentStudent{
			ID:        student.ID,
			StudentID: student.StudentID,
			FullName:  student.FullName,
			Email:     student.Email,
		},
		Courses: []LookupStudentCourseOverview{},
	}

	var enrollments []studentLookupEnrollmentRow
	enrollmentQuery := config.DB.Table("course_section_students AS css").
		Joins("JOIN course_sections AS cs ON cs.id = css.course_section_id").
		Joins("JOIN courses AS c ON c.id = cs.course_id").
		Where("css.student_id = ?", student.ID).
		Select("c.id AS course_id, c.code, c.name, c.year, c.semester, c.is_active, cs.id AS section_id, cs.section_no AS section_no").
		Order("c.id ASC, cs.id ASC")
	if strings.TrimSpace(courseID) != "" {
		enrollmentQuery = enrollmentQuery.Where("c.id = ?", strings.TrimSpace(courseID))
	}
	if err := enrollmentQuery.Scan(&enrollments).Error; err != nil {
		return nil, err
	}

	if len(enrollments) == 0 {
		return result, nil
	}

	courseOrder := make([]string, 0)
	courseMap := make(map[string]*LookupStudentCourseOverview)
	sectionSets := make(map[string]map[uint]struct{})
	for _, enrollment := range enrollments {
		courseEntry, exists := courseMap[enrollment.CourseID]
		if !exists {
			courseEntry = &LookupStudentCourseOverview{
				Course: LookupStudentCourse{
					ID:       enrollment.CourseID,
					Code:     enrollment.Code,
					Name:     enrollment.Name,
					Year:     enrollment.Year,
					Semester: enrollment.Semester,
					IsActive: enrollment.IsActive,
					Sections: []LookupStudentCourseSection{},
				},
				Assignments: []LookupStudentAssignment{},
				BonusScore:  LookupStudentBonusScore{Records: []LookupStudentBonusRecord{}},
				Attendance:  LookupStudentAttendance{Records: []LookupStudentAttendanceRecord{}, Summary: LookupStudentAttendanceSummary{}},
				ExamScores:  []LookupStudentExamScore{},
			}
			courseMap[enrollment.CourseID] = courseEntry
			sectionSets[enrollment.CourseID] = map[uint]struct{}{}
			courseOrder = append(courseOrder, enrollment.CourseID)
		}
		if _, seen := sectionSets[enrollment.CourseID][enrollment.SectionID]; !seen {
			courseEntry.Course.Sections = append(courseEntry.Course.Sections, LookupStudentCourseSection{ID: enrollment.SectionID, SectionNo: enrollment.SectionNo})
			sectionSets[enrollment.CourseID][enrollment.SectionID] = struct{}{}
		}
	}

	courseIDs := make([]string, 0, len(courseOrder))
	for _, courseID := range courseOrder {
		courseIDs = append(courseIDs, courseID)
	}

	var groupMemberships []studentLookupGroupMembershipRow
	if err := config.DB.Table("student_group_members AS sgm").
		Joins("JOIN student_groups AS sg ON sg.id = sgm.group_id").
		Where("sgm.student_id = ?", student.ID).
		Select("sgm.group_id, sg.course_id, sg.name AS group_name, sg.group_type, sg.week_number").
		Scan(&groupMemberships).Error; err != nil {
		return nil, err
	}

	groupIDs := make([]uint, 0, len(groupMemberships))
	groupInfoMap := make(map[uint]*LookupStudentGroupInfo)
	groupMembershipsByCourse := make(map[string]*studentCourseGroupMembership)
	for _, membership := range groupMemberships {
		groupIDs = append(groupIDs, membership.GroupID)
		groupInfo := &LookupStudentGroupInfo{ID: membership.GroupID, Name: membership.GroupName}
		groupInfoMap[membership.GroupID] = groupInfo

		courseGroups := groupMembershipsByCourse[membership.CourseID]
		if courseGroups == nil {
			courseGroups = &studentCourseGroupMembership{Weekly: map[int]*LookupStudentGroupInfo{}}
			groupMembershipsByCourse[membership.CourseID] = courseGroups
		}
		if membership.GroupType == "permanent" {
			courseGroups.Permanent = groupInfo
			continue
		}
		if membership.WeekNumber != nil {
			courseGroups.Weekly[*membership.WeekNumber] = groupInfo
		}
	}

	var assignments []models.Assignment
	if err := config.DB.
		Where("course_id IN ?", courseIDs).
		Where("is_active = ? AND is_draft = ? AND is_score_visible = ?", true, false, true).
		Order("order_index ASC, created_at ASC").
		Find(&assignments).Error; err != nil {
		return nil, err
	}

	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	subItemsByAssignment := make(map[uint][]models.AssignmentSubItem)
	if len(assignmentIDs) > 0 {
		var subItems []models.AssignmentSubItem
		if err := config.DB.Where("assignment_id IN ?", assignmentIDs).
			Order("assignment_id ASC, order_index ASC, id ASC").
			Find(&subItems).Error; err != nil {
			return nil, err
		}
		for _, subItem := range subItems {
			subItemsByAssignment[subItem.AssignmentID] = append(subItemsByAssignment[subItem.AssignmentID], subItem)
		}
	}

	mainScoreMap := make(map[uint]lookupStudentScoreSnapshot)
	subItemScoreMap := make(map[uint]map[uint]lookupStudentSubItemSnapshot)

	if len(assignmentIDs) > 0 {
		var individualMainScores []studentLookupMainScoreRow
		if err := config.DB.Table("scores AS s").
			Joins("LEFT JOIN users AS u ON u.id = s.graded_by").
			Where("s.assignment_id IN ? AND s.student_id = ? AND s.sub_item_id IS NULL", assignmentIDs, student.ID).
			Select(`s.assignment_id, s.group_id, s.score, s.status, s.graded_at, s.comment, u.full_name AS grader_name,
				CASE WHEN EXISTS (
					SELECT 1 FROM queue_bookings qb
					JOIN queue_sessions qs ON qs.id = qb.queue_session_id
					WHERE qb.student_id = s.student_id AND qb.booking_type = 'grading' AND qb.status = 'completed'
					AND qs.linked_assignment_id = s.assignment_id
				) THEN 'queue' ELSE 'direct' END AS graded_via`).
			Scan(&individualMainScores).Error; err != nil {
			return nil, err
		}
		for _, score := range individualMainScores {
			mainScoreMap[score.AssignmentID] = lookupStudentScoreSnapshot{
				Score:     floatPointer(score.Score),
				Status:    fallbackString(score.Status, "graded"),
				Grader:    nullableStringPointer(score.GraderName),
				GradedAt:  cloneTimePointer(score.GradedAt),
				Comment:   nullableStringPointer(score.Comment),
				IsGroup:   false,
				GradedVia: score.GradedVia,
			}
		}

		var individualSubItemScores []studentLookupSubItemScoreRow
		if err := config.DB.Table("scores AS s").
			Joins("LEFT JOIN users AS u ON u.id = s.graded_by").
			Where("s.assignment_id IN ? AND s.student_id = ? AND s.sub_item_id IS NOT NULL", assignmentIDs, student.ID).
			Select("s.assignment_id, s.sub_item_id, s.score, s.graded_at, u.full_name AS grader_name").
			Scan(&individualSubItemScores).Error; err != nil {
			return nil, err
		}
		for _, score := range individualSubItemScores {
			if subItemScoreMap[score.AssignmentID] == nil {
				subItemScoreMap[score.AssignmentID] = map[uint]lookupStudentSubItemSnapshot{}
			}
			subItemScoreMap[score.AssignmentID][score.SubItemID] = lookupStudentSubItemSnapshot{
				Score:    floatPointer(score.Score),
				Grader:   nullableStringPointer(score.GraderName),
				GradedAt: cloneTimePointer(score.GradedAt),
			}
		}

		if len(groupIDs) > 0 {
			var groupMainScores []studentLookupMainScoreRow
			if err := config.DB.Table("scores AS s").
				Joins("LEFT JOIN users AS u ON u.id = s.graded_by").
				Where("s.assignment_id IN ? AND s.group_id IN ? AND s.sub_item_id IS NULL", assignmentIDs, groupIDs).
				Select(`s.assignment_id, s.group_id, s.score, s.status, s.graded_at, s.comment, u.full_name AS grader_name,
					CASE WHEN EXISTS (
						SELECT 1 FROM student_group_members sgm
						JOIN queue_bookings qb ON qb.student_id = sgm.student_id
						JOIN queue_sessions qs ON qs.id = qb.queue_session_id
						WHERE sgm.group_id = s.group_id AND qb.booking_type = 'grading' AND qb.status = 'completed'
						AND qs.linked_assignment_id = s.assignment_id
					) THEN 'queue' ELSE 'direct' END AS graded_via`).
				Scan(&groupMainScores).Error; err != nil {
				return nil, err
			}
			for _, score := range groupMainScores {
				if _, exists := mainScoreMap[score.AssignmentID]; exists {
					continue
				}
				var groupInfo *LookupStudentGroupInfo
				if score.GroupID != nil {
					groupInfo = groupInfoMap[*score.GroupID]
				}
				mainScoreMap[score.AssignmentID] = lookupStudentScoreSnapshot{
					Score:     floatPointer(score.Score),
					Status:    fallbackString(score.Status, "graded"),
					Grader:    nullableStringPointer(score.GraderName),
					GradedAt:  cloneTimePointer(score.GradedAt),
					Comment:   nullableStringPointer(score.Comment),
					IsGroup:   true,
					GroupInfo: groupInfo,
					GradedVia: score.GradedVia,
				}
			}

			var groupSubItemScores []studentLookupSubItemScoreRow
			if err := config.DB.Table("scores AS s").
				Joins("LEFT JOIN users AS u ON u.id = s.graded_by").
				Where("s.assignment_id IN ? AND s.group_id IN ? AND s.sub_item_id IS NOT NULL", assignmentIDs, groupIDs).
				Select("s.assignment_id, s.sub_item_id, s.score, s.graded_at, u.full_name AS grader_name").
				Scan(&groupSubItemScores).Error; err != nil {
				return nil, err
			}
			for _, score := range groupSubItemScores {
				if subItemScoreMap[score.AssignmentID] == nil {
					subItemScoreMap[score.AssignmentID] = map[uint]lookupStudentSubItemSnapshot{}
				}
				if _, exists := subItemScoreMap[score.AssignmentID][score.SubItemID]; exists {
					continue
				}
				subItemScoreMap[score.AssignmentID][score.SubItemID] = lookupStudentSubItemSnapshot{
					Score:    floatPointer(score.Score),
					Grader:   nullableStringPointer(score.GraderName),
					GradedAt: cloneTimePointer(score.GradedAt),
				}
			}
		}
	}

	for _, assignment := range assignments {
		courseEntry := courseMap[assignment.CourseID]
		if courseEntry == nil {
			continue
		}

		groupInfoForAssignment := resolveGroupInfoForAssignment(groupMembershipsByCourse[assignment.CourseID], assignment.AssignmentType, assignment.WeekNumber)
		if isStudentGroupAssignmentType(assignment.AssignmentType) && groupInfoForAssignment == nil {
			continue
		}

		mainScore, hasMainScore := mainScoreMap[assignment.ID]
		assignmentScore := LookupStudentAssignment{
			ID:                assignment.ID,
			Title:             assignment.Name,
			Type:              assignment.AssignmentType,
			MaxScore:          assignment.MaxScore,
			Status:            "pending",
			Comment:           nil,
			IsGroupAssignment: assignment.AssignmentType == "permanent_group" || assignment.AssignmentType == "weekly_group",
			GroupInfo:         cloneGroupInfo(groupInfoForAssignment),
			SubItems:          []LookupStudentAssignmentSubItem{},
		}
		if hasMainScore {
			assignmentScore.Score = cloneFloatPointer(mainScore.Score)
			assignmentScore.Status = fallbackString(mainScore.Status, "graded")
			assignmentScore.Grader = cloneStringPointer(mainScore.Grader)
			assignmentScore.GradedAt = cloneTimePointer(mainScore.GradedAt)
			assignmentScore.Comment = cloneStringPointer(mainScore.Comment)
			if mainScore.GradedVia != "" {
				assignmentScore.GradedVia = &mainScore.GradedVia
			}
			assignmentScore.IsGroupAssignment = assignmentScore.IsGroupAssignment || mainScore.IsGroup
			if mainScore.GroupInfo != nil {
				assignmentScore.GroupInfo = cloneGroupInfo(mainScore.GroupInfo)
			}
		}

		subItems := subItemsByAssignment[assignment.ID]
		if len(subItems) > 0 {
			var (
				calculatedScore float64
				hasAnySubScore  bool
				latestGrader    *string
				latestGradedAt  *time.Time
				fullyGraded     = true
			)

			for _, subItem := range subItems {
				subItemData := LookupStudentAssignmentSubItem{
					ID:       subItem.ID,
					Name:     subItem.Name,
					MaxScore: subItem.MaxScore,
				}
				if scoreSnapshot, ok := subItemScoreMap[assignment.ID][subItem.ID]; ok {
					subItemData.Score = cloneFloatPointer(scoreSnapshot.Score)
					subItemData.Grader = cloneStringPointer(scoreSnapshot.Grader)
					subItemData.GradedAt = cloneTimePointer(scoreSnapshot.GradedAt)
					if scoreSnapshot.Score != nil {
						calculatedScore += *scoreSnapshot.Score
						hasAnySubScore = true
					}
					if scoreSnapshot.GradedAt != nil {
						if latestGradedAt == nil || scoreSnapshot.GradedAt.After(*latestGradedAt) {
							latestGradedAt = cloneTimePointer(scoreSnapshot.GradedAt)
							latestGrader = cloneStringPointer(scoreSnapshot.Grader)
						}
					} else if latestGrader == nil && scoreSnapshot.Grader != nil {
						latestGrader = cloneStringPointer(scoreSnapshot.Grader)
					}
				} else {
					fullyGraded = false
				}
				assignmentScore.SubItems = append(assignmentScore.SubItems, subItemData)
			}

			if assignmentScore.Score == nil && hasAnySubScore {
				assignmentScore.Score = floatPointer(calculatedScore)
				assignmentScore.Grader = latestGrader
				assignmentScore.GradedAt = latestGradedAt
				if fullyGraded {
					assignmentScore.Status = "graded"
				}
			}
		}

		courseEntry.Assignments = append(courseEntry.Assignments, assignmentScore)
		courseEntry.TotalMaxScore += assignment.MaxScore
		if assignmentScore.Score != nil {
			courseEntry.TotalScore += *assignmentScore.Score
		}
	}

	var bonusRows []studentLookupBonusRow
	if err := config.DB.Table("bonus_scores AS bs").
		Joins("LEFT JOIN users AS u ON u.id = bs.given_by").
		Where("bs.student_id = ? AND bs.course_id IN ?", student.ID, courseIDs).
		Select("bs.course_id, bs.score, bs.reason, bs.given_at, u.full_name AS given_by").
		Order("bs.given_at DESC").
		Scan(&bonusRows).Error; err != nil {
		return nil, err
	}
	for _, row := range bonusRows {
		courseEntry := courseMap[row.CourseID]
		if courseEntry == nil {
			continue
		}
		courseEntry.BonusScore.Total += row.Score
		courseEntry.BonusScore.Records = append(courseEntry.BonusScore.Records, LookupStudentBonusRecord{
			Score:   row.Score,
			Reason:  nullableStringPointer(row.Reason),
			GivenBy: nullableStringPointer(row.GivenBy),
			GivenAt: cloneTimePointer(row.GivenAt),
		})
	}

	var attendanceRows []studentLookupAttendanceRow
	if err := config.DB.Table("attendance_records AS ar").
		Joins("JOIN attendance_sessions AS s ON s.id = ar.attendance_session_id").
		Where("ar.student_id = ? AND s.course_id IN ? AND s.start_time <= ?", student.ID, courseIDs, time.Now()).
		Select("s.course_id, ar.id, s.title AS session_title, s.start_time AS date, ar.status, ar.check_in_time, ar.note").
		Order("ar.created_at DESC").
		Scan(&attendanceRows).Error; err != nil {
		return nil, err
	}
	for _, row := range attendanceRows {
		courseEntry := courseMap[row.CourseID]
		if courseEntry == nil {
			continue
		}
		courseEntry.Attendance.Records = append(courseEntry.Attendance.Records, LookupStudentAttendanceRecord{
			ID:           row.ID,
			SessionTitle: row.SessionTitle,
			Date:         cloneTimePointer(row.Date),
			Status:       row.Status,
			CheckInTime:  cloneTimePointer(row.CheckInTime),
			Note:         nullableStringPointer(row.Note),
		})
		switch row.Status {
		case "present":
			courseEntry.Attendance.Summary.Present++
		case "late":
			courseEntry.Attendance.Summary.Late++
		case "leave":
			courseEntry.Attendance.Summary.Leave++
		case "absent":
			courseEntry.Attendance.Summary.Absent++
		}
	}

	var examRows []studentLookupExamRow
	if err := config.DB.Table("exam_scores AS es").
		Joins("JOIN exam_settings AS ex ON ex.id = es.exam_setting_id").
		Joins("LEFT JOIN users AS u ON u.id = es.graded_by").
		Where("es.student_id = ? AND ex.course_id IN ? AND ex.is_active = ? AND ex.is_visible = ?", student.ID, courseIDs, true, true).
		Select("ex.course_id, es.id, ex.exam_type, ex.component, ex.max_score, es.score, es.graded_at, es.comment, u.full_name AS grader_name").
		Order("es.id ASC").
		Scan(&examRows).Error; err != nil {
		return nil, err
	}
	for _, row := range examRows {
		courseEntry := courseMap[row.CourseID]
		if courseEntry == nil {
			continue
		}
		courseEntry.ExamScores = append(courseEntry.ExamScores, LookupStudentExamScore{
			ID:        row.ID,
			ExamType:  row.ExamType,
			Component: row.Component,
			Score:     cloneFloatPointer(row.Score),
			MaxScore:  row.MaxScore,
			Grader:    nullableStringPointer(row.GraderName),
			GradedAt:  cloneTimePointer(row.GradedAt),
			Comment:   nullableStringPointer(row.Comment),
		})
	}

	for _, courseID := range courseOrder {
		courseEntry := courseMap[courseID]
		if courseEntry == nil {
			continue
		}
		if courseEntry.TotalMaxScore > 0 {
			courseEntry.Progress = int(math.Round((courseEntry.TotalScore / courseEntry.TotalMaxScore) * 100))
		}
		result.Courses = append(result.Courses, *courseEntry)
	}

	return result, nil
}

func isStudentGroupAssignmentType(assignmentType string) bool {
	return assignmentType == "permanent_group" || assignmentType == "weekly_group"
}

func resolveGroupInfoForAssignment(groups *studentCourseGroupMembership, assignmentType string, weekNumber *int) *LookupStudentGroupInfo {
	if groups == nil {
		return nil
	}
	switch assignmentType {
	case "permanent_group":
		return cloneGroupInfo(groups.Permanent)
	case "weekly_group":
		if weekNumber == nil {
			return nil
		}
		return cloneGroupInfo(groups.Weekly[*weekNumber])
	default:
		return nil
	}
}

func floatPointer(value float64) *float64 {
	copyValue := value
	return &copyValue
}

func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func nullableStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneGroupInfo(value *LookupStudentGroupInfo) *LookupStudentGroupInfo {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func IsStudentIDExists(studentID string, excludeID uint) bool {
	var count int64
	db := config.DB.Model(&models.Student{}).Where("student_id = ?", studentID)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	db.Count(&count)
	return count > 0
}

func IsStudentEmailExists(email string, excludeID uint) bool {
	var count int64
	db := config.DB.Model(&models.Student{}).Where("email = ?", email)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	db.Count(&count)
	return count > 0
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

func CreateStudent(s *models.Student) error {
	return config.DB.Create(s).Error
}

func UpdateStudent(s *models.Student) error {
	return config.DB.Save(s).Error
}

func DeleteStudent(id uint) error {
	return config.DB.Delete(&models.Student{}, id).Error
}

func ToggleStudentStatus(id uint) (*models.Student, error) {
	s, err := FindStudentByID(id)
	if err != nil {
		return nil, err
	}
	s.IsActive = !s.IsActive
	if err := config.DB.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// ─── Stats ────────────────────────────────────────────────────────────────────

type StudentStats struct {
	Total    int64
	Active   int64
	Inactive int64
}

func GetStudentStats() (*StudentStats, error) {
	var stats StudentStats
	config.DB.Model(&models.Student{}).Count(&stats.Total)
	config.DB.Model(&models.Student{}).Where("is_active = ?", true).Count(&stats.Active)
	config.DB.Model(&models.Student{}).Where("is_active = ?", false).Count(&stats.Inactive)
	return &stats, nil
}

// ─── Bulk search by student_id list ──────────────────────────────────────────

func FindStudentsByStudentIDs(studentIDs []string) ([]models.Student, error) {
	var students []models.Student
	err := config.DB.Where("student_id IN ?", studentIDs).Find(&students).Error
	return students, err
}

// ─── Bulk import ──────────────────────────────────────────────────────────────

type ImportRow struct {
	StudentID string
	FullName  string
	Email     string
	Extra     []byte // raw JSON
}

type ImportResult struct {
	Created    int
	Skipped    int
	Failed     int
	Duplicates []map[string]string
	Errors     []map[string]string
}

func ImportStudents(rows []ImportRow) (*ImportResult, error) {
	result := &ImportResult{}

	for _, row := range rows {
		if row.StudentID == "" || row.FullName == "" {
			result.Failed++
			result.Errors = append(result.Errors, map[string]string{
				"student_id": row.StudentID,
				"error":      "ข้อมูลไม่ครบถ้วน (ต้องมีรหัสนักศึกษาและชื่อ)",
			})
			continue
		}

		if IsStudentIDExists(row.StudentID, 0) {
			result.Skipped++
			result.Duplicates = append(result.Duplicates, map[string]string{
				"student_id": row.StudentID,
				"full_name":  row.FullName,
			})
			continue
		}

		s := models.Student{
			StudentID: row.StudentID,
			FullName:  row.FullName,
			Email:     row.Email,
			IsActive:  true,
		}
		if len(row.Extra) > 0 {
			s.Extra = row.Extra
		}

		if err := config.DB.Create(&s).Error; err != nil {
			result.Failed++
			result.Errors = append(result.Errors, map[string]string{
				"student_id": row.StudentID,
				"error":      err.Error(),
			})
		} else {
			result.Created++
		}
	}

	return result, nil
}
