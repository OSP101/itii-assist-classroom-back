package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"
)

// ============================================================
// StudentGroup (Teams)
// ============================================================

type TeamMember struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	JoinedAt  string `json:"joined_at"`
}

type TeamWithMembers struct {
	ID          uint         `json:"id"`
	Name        string       `json:"name"`
	CourseID    string       `json:"course_id"`
	GroupType   string       `json:"group_type"`
	WeekNumber  *int         `json:"week_number"`
	Members     []TeamMember `json:"members"`
	MemberCount int          `json:"member_count"`
	CreatedAt   time.Time    `json:"created_at"`
}

func GetTeamsByCourse(courseID string, groupType string, weekNumber *int) ([]TeamWithMembers, error) {
	db := config.DB

	query := db.Model(&models.StudentGroup{}).Where("course_id = ?", courseID)
	if groupType == "permanent" {
		query = query.Where("group_type = 'permanent'")
	} else if groupType == "temporary" || groupType == "weekly" {
		query = query.Where("group_type = 'temporary'")
		if weekNumber != nil {
			query = query.Where("week_number = ?", *weekNumber)
		}
	}

	var groups []models.StudentGroup
	if err := query.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []TeamWithMembers{}, nil
	}

	// Collect group IDs
	groupIDs := make([]uint, len(groups))
	for i, g := range groups {
		groupIDs[i] = g.ID
	}

	// Fetch members with student info
	type memberRow struct {
		GroupID   uint   `gorm:"column:group_id"`
		JoinedAt  string `gorm:"column:joined_at"`
		StudentID uint   `gorm:"column:student_id"`
		StuID     string `gorm:"column:stu_id"`
		FullName  string `gorm:"column:full_name"`
		Email     string `gorm:"column:email"`
	}
	var rows []memberRow
	db.Raw(`
		SELECT sgm.group_id, sgm.joined_at, sgm.student_id,
		       s.student_id as stu_id, s.full_name, s.email
		FROM student_group_members sgm
		JOIN students s ON s.id = sgm.student_id
		WHERE sgm.group_id IN ?
		ORDER BY sgm.student_id ASC
	`, groupIDs).Scan(&rows)

	// Index members by group_id
	memberMap := map[uint][]TeamMember{}
	for _, r := range rows {
		memberMap[r.GroupID] = append(memberMap[r.GroupID], TeamMember{
			ID:        r.StudentID,
			StudentID: r.StuID,
			FullName:  r.FullName,
			Email:     r.Email,
			JoinedAt:  r.JoinedAt,
		})
	}

	result := make([]TeamWithMembers, len(groups))
	for i, g := range groups {
		members := memberMap[g.ID]
		if members == nil {
			members = []TeamMember{}
		}
		result[i] = TeamWithMembers{
			ID:          g.ID,
			Name:        g.Name,
			CourseID:    g.CourseID,
			GroupType:   g.GroupType,
			WeekNumber:  g.WeekNumber,
			Members:     members,
			MemberCount: len(members),
			CreatedAt:   g.CreatedAt,
		}
	}
	return result, nil
}

func GetTeamByID(teamID uint, courseID string) (*TeamWithMembers, error) {
	db := config.DB
	var g models.StudentGroup
	if err := db.Where("id = ? AND course_id = ?", teamID, courseID).First(&g).Error; err != nil {
		return nil, err
	}

	type memberRow struct {
		JoinedAt  string `gorm:"column:joined_at"`
		StudentID uint   `gorm:"column:student_id"`
		StuID     string `gorm:"column:stu_id"`
		FullName  string `gorm:"column:full_name"`
		Email     string `gorm:"column:email"`
	}
	var rows []memberRow
	db.Raw(`
		SELECT sgm.joined_at, sgm.student_id,
		       s.student_id as stu_id, s.full_name, s.email
		FROM student_group_members sgm
		JOIN students s ON s.id = sgm.student_id
		WHERE sgm.group_id = ?
		ORDER BY sgm.student_id ASC
	`, g.ID).Scan(&rows)

	members := make([]TeamMember, len(rows))
	for i, r := range rows {
		members[i] = TeamMember{
			ID:        r.StudentID,
			StudentID: r.StuID,
			FullName:  r.FullName,
			Email:     r.Email,
			JoinedAt:  r.JoinedAt,
		}
	}

	return &TeamWithMembers{
		ID:          g.ID,
		Name:        g.Name,
		CourseID:    g.CourseID,
		GroupType:   g.GroupType,
		WeekNumber:  g.WeekNumber,
		Members:     members,
		MemberCount: len(members),
		CreatedAt:   g.CreatedAt,
	}, nil
}

func CreateTeam(courseID string, name string, groupType string, weekNumber *int, memberIDs []uint) (*TeamWithMembers, error) {
	db := config.DB
	now := time.Now()

	group := models.StudentGroup{
		CourseID:  courseID,
		Name:      name,
		GroupType: groupType,
		CreatedAt: now,
	}
	if groupType == "temporary" {
		group.WeekNumber = weekNumber
	}

	if err := db.Create(&group).Error; err != nil {
		return nil, err
	}

	if len(memberIDs) > 0 {
		members := make([]models.StudentGroupMember, len(memberIDs))
		for i, sid := range memberIDs {
			members[i] = models.StudentGroupMember{
				GroupID:   group.ID,
				StudentID: sid,
				JoinedAt:  now,
			}
		}
		db.Create(&members)
	}

	return GetTeamByID(group.ID, courseID)
}

func BulkCreateTeams(courseID string, groupType string, weekNumber *int, teams []struct {
	Name      string `json:"name"`
	MemberIDs []uint `json:"member_ids"`
}) ([]TeamWithMembers, error) {
	db := config.DB
	now := time.Now()
	var created []TeamWithMembers
	for _, t := range teams {
		if t.Name == "" || len(t.MemberIDs) == 0 {
			continue
		}
		group := models.StudentGroup{
			CourseID:  courseID,
			Name:      t.Name,
			GroupType: groupType,
			CreatedAt: now,
		}
		if groupType == "temporary" {
			group.WeekNumber = weekNumber
		}
		if err := db.Create(&group).Error; err != nil {
			continue
		}
		members := make([]models.StudentGroupMember, len(t.MemberIDs))
		for i, sid := range t.MemberIDs {
			members[i] = models.StudentGroupMember{GroupID: group.ID, StudentID: sid, JoinedAt: now}
		}
		db.Create(&members)

		tw, _ := GetTeamByID(group.ID, courseID)
		if tw != nil {
			created = append(created, *tw)
		}
	}
	if created == nil {
		created = []TeamWithMembers{}
	}
	return created, nil
}

func UpdateTeam(teamID uint, courseID string, name string, memberIDs *[]uint) (*TeamWithMembers, error) {
	db := config.DB
	var group models.StudentGroup
	if err := db.Where("id = ? AND course_id = ?", teamID, courseID).First(&group).Error; err != nil {
		return nil, err
	}

	if name != "" {
		group.Name = name
		db.Save(&group)
	}

	if memberIDs != nil {
		db.Where("group_id = ?", teamID).Delete(&models.StudentGroupMember{})
		if len(*memberIDs) > 0 {
			now := time.Now()
			newMembers := make([]models.StudentGroupMember, len(*memberIDs))
			for i, sid := range *memberIDs {
				newMembers[i] = models.StudentGroupMember{GroupID: teamID, StudentID: sid, JoinedAt: now}
			}
			db.Create(&newMembers)
		}
	}

	return GetTeamByID(teamID, courseID)
}

func DeleteTeam(teamID uint, courseID string) error {
	db := config.DB
	var group models.StudentGroup
	if err := db.Where("id = ? AND course_id = ?", teamID, courseID).First(&group).Error; err != nil {
		return err
	}
	db.Where("group_id = ?", teamID).Delete(&models.StudentGroupMember{})
	return db.Delete(&group).Error
}

func BulkDeleteTeams(teamIDs []uint, courseID string) (int64, error) {
	db := config.DB
	db.Where("group_id IN ? AND group_id IN (?)",
		teamIDs,
		db.Model(&models.StudentGroup{}).Select("id").Where("course_id = ?", courseID),
	).Delete(&models.StudentGroupMember{})
	result := db.Where("id IN ? AND course_id = ?", teamIDs, courseID).Delete(&models.StudentGroup{})
	return result.RowsAffected, result.Error
}

func AddTeamMember(teamID uint, courseID string, studentID uint) error {
	db := config.DB
	var group models.StudentGroup
	if err := db.Where("id = ? AND course_id = ?", teamID, courseID).First(&group).Error; err != nil {
		return err
	}
	member := models.StudentGroupMember{
		GroupID:   teamID,
		StudentID: studentID,
		JoinedAt:  time.Now(),
	}
	return db.Where("group_id = ? AND student_id = ?", teamID, studentID).
		FirstOrCreate(&member).Error
}

func RemoveTeamMember(teamID uint, courseID string, studentID uint) error {
	db := config.DB
	var group models.StudentGroup
	if err := db.Where("id = ? AND course_id = ?", teamID, courseID).First(&group).Error; err != nil {
		return err
	}
	return db.Where("group_id = ? AND student_id = ?", teamID, studentID).
		Delete(&models.StudentGroupMember{}).Error
}

// ============================================================
// Course Overview helpers
// ============================================================

type CourseOverviewSummary struct {
	TotalStudents           int64   `json:"totalStudents"`
	TotalSections           int64   `json:"totalSections"`
	TotalTAs                int64   `json:"totalTAs"`
	TotalAssignments        int64   `json:"totalAssignments"`
	TotalMaxScore           float64 `json:"totalMaxScore"`
	SubmissionRate          int     `json:"submissionRate"`
	AttendanceRate          int     `json:"attendanceRate"`
	TotalAttendanceSessions int64   `json:"totalAttendanceSessions"`
	AverageScore            float64 `json:"averageScore"`
	Trend                   string  `json:"trend"`
	TrendValue              int     `json:"trendValue"`
}

type OverviewStudentScore struct {
	ID                uint    `json:"id"`
	StudentID         string  `json:"student_id"`
	FullName          string  `json:"full_name"`
	TotalScore        float64 `json:"totalScore"`
	AssignmentsGraded int     `json:"assignmentsGraded"`
	Percentage        int     `json:"percentage"`
}

type OverviewTAActivity struct {
	ID          uint    `json:"id"`
	FullName    string  `json:"full_name"`
	Email       string  `json:"email"`
	Avatar      string  `json:"avatar"`
	GradedCount int     `json:"gradedCount"`
	LastActive  *string `json:"lastActive"`
}

type OverviewAssignmentStat struct {
	ID             uint     `json:"id"`
	Name           string   `json:"name"`
	MaxScore       float64  `json:"max_score"`
	AssignmentType string   `json:"assignment_type"`
	IsScoreVisible bool     `json:"is_score_visible"`
	AvgScore       *float64 `json:"avgScore"`
	ScoredCount    int      `json:"scoredCount"`
	NotScoredCount int      `json:"notScoredCount"`
	SubmittedRate  int      `json:"submittedRate"`
	HasSubItems    bool     `json:"hasSubItems"`
	SubItemsCount  int      `json:"subItemsCount"`
}

type OverviewAssignmentTypeStats struct {
	Count         int     `json:"count"`
	TotalMaxScore float64 `json:"totalMaxScore"`
	TotalScored   int     `json:"totalScored"`
	TotalExpected int     `json:"totalExpected"`
	ProgressRate  int     `json:"progressRate"`
}

type OverviewRecentActivity struct {
	ID          uint    `json:"id"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	User        *struct {
		ID       uint   `json:"id"`
		FullName string `json:"full_name"`
		Avatar   string `json:"avatar"`
	} `json:"user"`
	Timestamp *string `json:"timestamp"`
}

type CourseOverview struct {
	Summary               CourseOverviewSummary                  `json:"summary"`
	TopStudents           []OverviewStudentScore                 `json:"topStudents"`
	LowPerformers         []OverviewStudentScore                 `json:"lowPerformers"`
	TAActivity            []OverviewTAActivity                   `json:"taActivity"`
	Assignments           []OverviewAssignmentStat               `json:"assignments"`
	AssignmentStatsByType map[string]OverviewAssignmentTypeStats `json:"assignmentStatsByType"`
	RecentActivities      []OverviewRecentActivity               `json:"recentActivities"`
	ScoreDistribution     map[string]int                         `json:"scoreDistribution"`
}

func GetCourseOverview(courseID string) (*CourseOverview, error) {
	db := config.DB

	// ── 1. Basic counts ──────────────────────────────────────────
	var totalSections, totalTAs, totalStudents int64
	db.Model(&models.CourseSection{}).Where("course_id = ?", courseID).Count(&totalSections)
	db.Model(&models.CourseTA{}).Where("course_id = ?", courseID).Count(&totalTAs)
	db.Raw(`
		SELECT COUNT(css.id)
		FROM course_section_students css
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ?
	`, courseID).Scan(&totalStudents)

	// ── 2. Assignments ────────────────────────────────────────────
	type assignmentRow struct {
		ID             uint    `gorm:"column:id"`
		Name           string  `gorm:"column:name"`
		MaxScore       float64 `gorm:"column:max_score"`
		AssignmentType string  `gorm:"column:assignment_type"`
		IsScoreVisible bool    `gorm:"column:is_score_visible"`
	}
	var assignments []assignmentRow
	db.Raw(`SELECT id, name, max_score, assignment_type, is_score_visible
	        FROM assignments WHERE course_id = ? AND is_active = true
	        ORDER BY created_at DESC`, courseID).Scan(&assignments)

	assignmentIDs := make([]uint, len(assignments))
	for i, a := range assignments {
		assignmentIDs[i] = a.ID
	}

	// Sub-items per assignment
	type subItemRow struct {
		AssignmentID uint    `gorm:"column:assignment_id"`
		MaxScore     float64 `gorm:"column:max_score"`
	}
	var subItems []subItemRow
	if len(assignmentIDs) > 0 {
		db.Raw(`SELECT assignment_id, max_score FROM assignment_sub_items WHERE assignment_id IN ?`, assignmentIDs).Scan(&subItems)
	}
	subItemCountMap := map[uint]int{}
	subItemScoreMap := map[uint]float64{}
	for _, si := range subItems {
		subItemCountMap[si.AssignmentID]++
		subItemScoreMap[si.AssignmentID] += si.MaxScore
	}

	// Effective max score per assignment
	assignmentMaxScoreMap := map[uint]float64{}
	var totalMaxScore float64
	for _, a := range assignments {
		score := a.MaxScore
		if subItemCountMap[a.ID] > 0 {
			score = subItemScoreMap[a.ID]
		}
		assignmentMaxScoreMap[a.ID] = score
		totalMaxScore += score
	}

	// ── 3. All scores ─────────────────────────────────────────────
	type scoreRow struct {
		ID           uint    `gorm:"column:id"`
		AssignmentID uint    `gorm:"column:assignment_id"`
		StudentID    *uint   `gorm:"column:student_id"`
		GroupID      *uint   `gorm:"column:group_id"`
		Score        float64 `gorm:"column:score"`
		GradedBy     *uint   `gorm:"column:graded_by"`
		GradedAt     *string `gorm:"column:graded_at"`
		GraderName   string  `gorm:"column:grader_name"`
		GraderAvatar string  `gorm:"column:grader_avatar"`
		StuID        string  `gorm:"column:stu_id"`
		StuFullName  string  `gorm:"column:stu_full_name"`
	}
	var scores []scoreRow
	if len(assignmentIDs) > 0 {
		db.Raw(`
			SELECT s.id, s.assignment_id, s.student_id, s.group_id, s.score,
			       s.graded_by, s.graded_at,
			       u.full_name as grader_name, u.avatar as grader_avatar,
			       st.student_id as stu_id, st.full_name as stu_full_name
			FROM scores s
			LEFT JOIN users u ON u.id = s.graded_by
			LEFT JOIN students st ON st.id = s.student_id
			WHERE s.assignment_id IN ? AND s.score IS NOT NULL
			ORDER BY s.graded_at DESC
		`, assignmentIDs).Scan(&scores)
	}

	// ── 4. Enrolled students ──────────────────────────────────────
	type enrolledStudent struct {
		ID        uint   `gorm:"column:id"`
		StudentID string `gorm:"column:student_id"`
		FullName  string `gorm:"column:full_name"`
	}
	var enrolledStudents []enrolledStudent
	db.Raw(`
		SELECT DISTINCT s.id, s.student_id, s.full_name
		FROM students s
		JOIN course_section_students css ON css.student_id = s.id
		JOIN course_sections cs ON cs.id = css.course_section_id
		WHERE cs.course_id = ?
	`, courseID).Scan(&enrolledStudents)

	// ── 5. Student score totals ───────────────────────────────────
	studentScoreMap := map[uint]*OverviewStudentScore{}
	for _, s := range enrolledStudents {
		studentScoreMap[s.ID] = &OverviewStudentScore{
			ID:        s.ID,
			StudentID: s.StudentID,
			FullName:  s.FullName,
		}
	}
	for _, sc := range scores {
		if sc.StudentID != nil {
			if data, ok := studentScoreMap[*sc.StudentID]; ok {
				data.TotalScore += sc.Score
				data.AssignmentsGraded++
			}
		}
	}

	// Rank students
	type ranked = OverviewStudentScore
	var rankedStudents []ranked
	for _, v := range studentScoreMap {
		pct := 0
		if totalMaxScore > 0 {
			pct = int((v.TotalScore / totalMaxScore) * 100)
		}
		v.Percentage = pct
		rankedStudents = append(rankedStudents, *v)
	}
	// Sort desc by TotalScore
	for i := 0; i < len(rankedStudents); i++ {
		for j := i + 1; j < len(rankedStudents); j++ {
			if rankedStudents[j].TotalScore > rankedStudents[i].TotalScore {
				rankedStudents[i], rankedStudents[j] = rankedStudents[j], rankedStudents[i]
			}
		}
	}

	// Top 5
	topStudents := []OverviewStudentScore{}
	for _, s := range rankedStudents {
		if s.TotalScore > 0 && len(topStudents) < 5 {
			topStudents = append(topStudents, s)
		}
	}

	// Low performers
	// Get course attention_threshold
	var course models.Course
	db.Select("attention_threshold").Where("id = ?", courseID).First(&course)
	threshold := course.AttentionThreshold
	if threshold == 0 {
		threshold = 60
	}
	lowPerformers := []OverviewStudentScore{}
	// iterate from end (worst performers)
	for i := len(rankedStudents) - 1; i >= 0; i-- {
		s := rankedStudents[i]
		if s.AssignmentsGraded > 0 && s.Percentage < threshold && len(lowPerformers) < 8 {
			lowPerformers = append(lowPerformers, s)
		}
	}

	// ── 6. TA activity ────────────────────────────────────────────
	type taRow struct {
		UserID   uint   `gorm:"column:user_id"`
		FullName string `gorm:"column:full_name"`
		Email    string `gorm:"column:email"`
		Avatar   string `gorm:"column:avatar"`
	}
	var taRows []taRow
	db.Raw(`
		SELECT ct.user_id, u.full_name, u.email, u.avatar
		FROM course_tas ct
		JOIN users u ON u.id = ct.user_id
		WHERE ct.course_id = ?
	`, courseID).Scan(&taRows)

	taIDs := make([]uint, len(taRows))
	for i, t := range taRows {
		taIDs[i] = t.UserID
	}

	type taStatRow struct {
		GradedBy    uint   `gorm:"column:graded_by"`
		GradedCount int    `gorm:"column:graded_count"`
		LastActive  string `gorm:"column:last_active"`
	}
	var taStats []taStatRow
	if len(taIDs) > 0 && len(assignmentIDs) > 0 {
		db.Raw(`
			SELECT graded_by, COUNT(*) as graded_count, MAX(graded_at) as last_active
			FROM scores
			WHERE assignment_id IN ? AND graded_by IN ? AND score IS NOT NULL
			GROUP BY graded_by
		`, assignmentIDs, taIDs).Scan(&taStats)
	}
	taStatMap := map[uint]taStatRow{}
	for _, ts := range taStats {
		taStatMap[ts.GradedBy] = ts
	}

	taActivity := make([]OverviewTAActivity, len(taRows))
	for i, t := range taRows {
		stat := taStatMap[t.UserID]
		var lastActive *string
		if stat.LastActive != "" {
			lastActive = &stat.LastActive
		}
		taActivity[i] = OverviewTAActivity{
			ID:          t.UserID,
			FullName:    t.FullName,
			Email:       t.Email,
			Avatar:      t.Avatar,
			GradedCount: stat.GradedCount,
			LastActive:  lastActive,
		}
	}
	// Sort by gradedCount desc
	for i := 0; i < len(taActivity); i++ {
		for j := i + 1; j < len(taActivity); j++ {
			if taActivity[j].GradedCount > taActivity[i].GradedCount {
				taActivity[i], taActivity[j] = taActivity[j], taActivity[i]
			}
		}
	}

	// ── 7. Per-assignment stats ───────────────────────────────────
	assignmentStats := make([]OverviewAssignmentStat, 0, len(assignments))
	assignmentStatsByType := map[string]OverviewAssignmentTypeStats{}

	for i, a := range assignments {
		if i >= 10 {
			break
		}
		isGroup := a.AssignmentType != "individual" && a.AssignmentType != "assignment"
		maxScore := assignmentMaxScoreMap[a.ID]

		// Scores for this assignment
		var aScores []scoreRow
		for _, sc := range scores {
			if sc.AssignmentID == a.ID {
				aScores = append(aScores, sc)
			}
		}

		var avgScore *float64
		var scoredCount int

		if isGroup {
			groupTotals := map[uint]float64{}
			for _, sc := range aScores {
				if sc.GroupID != nil {
					groupTotals[*sc.GroupID] += sc.Score
				}
			}
			scoredCount = len(groupTotals)
			if scoredCount > 0 {
				sum := 0.0
				for _, v := range groupTotals {
					sum += v
				}
				avg := sum / float64(scoredCount)
				avgScore = &avg
			}
		} else {
			studentTotals := map[uint]float64{}
			for _, sc := range aScores {
				if sc.StudentID != nil {
					studentTotals[*sc.StudentID] += sc.Score
				}
			}
			scoredCount = len(studentTotals)
			if scoredCount > 0 {
				sum := 0.0
				for _, v := range studentTotals {
					sum += v
				}
				avg := sum / float64(scoredCount)
				avgScore = &avg
			}
		}

		notScoredCount := 0
		submittedRate := 0
		if !isGroup {
			notScoredCount = int(totalStudents) - scoredCount
			if notScoredCount < 0 {
				notScoredCount = 0
			}
			if totalStudents > 0 {
				submittedRate = int(float64(scoredCount) / float64(totalStudents) * 100)
			}
		} else {
			if scoredCount > 0 {
				submittedRate = 100
			}
		}

		assignmentStats = append(assignmentStats, OverviewAssignmentStat{
			ID:             a.ID,
			Name:           a.Name,
			MaxScore:       maxScore,
			AssignmentType: a.AssignmentType,
			IsScoreVisible: a.IsScoreVisible,
			AvgScore:       avgScore,
			ScoredCount:    scoredCount,
			NotScoredCount: notScoredCount,
			SubmittedRate:  submittedRate,
			HasSubItems:    subItemCountMap[a.ID] > 0,
			SubItemsCount:  subItemCountMap[a.ID],
		})

		// By type
		typStats := assignmentStatsByType[a.AssignmentType]
		typStats.Count++
		typStats.TotalMaxScore += maxScore
		if isGroup {
			if scoredCount > 0 {
				typStats.TotalScored++
			}
			typStats.TotalExpected++
		} else {
			typStats.TotalScored += scoredCount
			typStats.TotalExpected += int(totalStudents)
		}
		assignmentStatsByType[a.AssignmentType] = typStats
	}

	// Calculate progressRate for each type
	for k, v := range assignmentStatsByType {
		if v.TotalExpected > 0 {
			v.ProgressRate = int(float64(v.TotalScored) / float64(v.TotalExpected) * 100)
		}
		assignmentStatsByType[k] = v
	}

	// ── 8. Submission rate (overall) ──────────────────────────────
	totalExpectedScores := 0
	totalReceivedScores := 0
	for _, a := range assignments {
		isGroup := a.AssignmentType != "individual" && a.AssignmentType != "assignment"
		if !isGroup {
			studentSet := map[uint]bool{}
			for _, sc := range scores {
				if sc.AssignmentID == a.ID && sc.StudentID != nil {
					studentSet[*sc.StudentID] = true
				}
			}
			totalReceivedScores += len(studentSet)
			totalExpectedScores += int(totalStudents)
		}
	}
	submissionRate := 0
	if totalExpectedScores > 0 {
		submissionRate = int(float64(totalReceivedScores) / float64(totalExpectedScores) * 100)
	}

	// ── 9. Attendance ─────────────────────────────────────────────
	type attendanceStat struct {
		TotalSessions int64 `gorm:"column:total_sessions"`
		PresentCount  int64 `gorm:"column:present_count"`
	}
	var attStat attendanceStat
	db.Raw(`
		SELECT COUNT(DISTINCT ats.id) as total_sessions,
		       SUM(CASE WHEN ar.status IN ('present','late') THEN 1 ELSE 0 END) as present_count
		FROM attendance_sessions ats
		LEFT JOIN attendance_records ar ON ats.id = ar.attendance_session_id
		WHERE ats.course_id = ?
	`, courseID).Scan(&attStat)

	attendanceRate := 0
	if attStat.TotalSessions > 0 && totalStudents > 0 {
		attendanceRate = int(float64(attStat.PresentCount) / float64(attStat.TotalSessions*totalStudents) * 100)
	}

	// ── 10. Average score ─────────────────────────────────────────
	var avgScore float64
	gradedStudents := 0
	for _, s := range rankedStudents {
		if s.AssignmentsGraded > 0 {
			avgScore += s.TotalScore
			gradedStudents++
		}
	}
	if gradedStudents > 0 {
		avgScore = float64(int(avgScore/float64(gradedStudents)*10)) / 10
	}

	// ── 11. Score distribution ────────────────────────────────────
	scoreDistribution := map[string]int{
		"excellent": 0, "good": 0, "average": 0, "poor": 0,
	}
	for _, s := range rankedStudents {
		if s.AssignmentsGraded == 0 {
			continue
		}
		switch {
		case s.Percentage >= 80:
			scoreDistribution["excellent"]++
		case s.Percentage >= 60:
			scoreDistribution["good"]++
		case s.Percentage >= 40:
			scoreDistribution["average"]++
		default:
			scoreDistribution["poor"]++
		}
	}

	// ── 12. Recent activities ─────────────────────────────────────
	recentActivities := []OverviewRecentActivity{}
	for i, sc := range scores {
		if i >= 10 {
			break
		}
		desc := "ให้คะแนน"
		if sc.StuFullName != "" {
			desc = "ให้คะแนน " + sc.StuFullName
		} else {
			desc = "ให้คะแนนกลุ่ม"
		}
		// Find assignment name
		for _, a := range assignments {
			if a.ID == sc.AssignmentID {
				desc += " - " + a.Name
				break
			}
		}

		act := OverviewRecentActivity{
			ID:          sc.ID,
			Type:        "score",
			Description: desc,
			Score:       sc.Score,
			Timestamp:   sc.GradedAt,
		}
		if sc.GradedBy != nil && sc.GraderName != "" {
			act.User = &struct {
				ID       uint   `json:"id"`
				FullName string `json:"full_name"`
				Avatar   string `json:"avatar"`
			}{
				ID:       *sc.GradedBy,
				FullName: sc.GraderName,
				Avatar:   sc.GraderAvatar,
			}
		}
		recentActivities = append(recentActivities, act)
	}

	trend := "up"
	if submissionRate <= 40 {
		trend = "down"
	} else if submissionRate <= 70 {
		trend = "stable"
	}

	return &CourseOverview{
		Summary: CourseOverviewSummary{
			TotalStudents:           totalStudents,
			TotalSections:           totalSections,
			TotalTAs:                totalTAs,
			TotalAssignments:        int64(len(assignments)),
			TotalMaxScore:           totalMaxScore,
			SubmissionRate:          submissionRate,
			AttendanceRate:          attendanceRate,
			TotalAttendanceSessions: attStat.TotalSessions,
			AverageScore:            avgScore,
			Trend:                   trend,
			TrendValue:              submissionRate,
		},
		TopStudents:           topStudents,
		LowPerformers:         lowPerformers,
		TAActivity:            taActivity,
		Assignments:           assignmentStats,
		AssignmentStatsByType: assignmentStatsByType,
		RecentActivities:      recentActivities,
		ScoreDistribution:     scoreDistribution,
	}, nil
}

// GetPendingEditRequestCount returns count of pending score edit requests for a course.
// If isInstructor is false (TA), only counts requests by that user.
func GetPendingEditRequestCount(courseID string, userID uint, isInstructor bool) (int64, error) {
	query := `
		SELECT COUNT(ser.id)
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE a.course_id = ? AND ser.status = 'pending'
	`
	args := []interface{}{courseID}
	if !isInstructor {
		query += " AND ser.requested_by = ?"
		args = append(args, userID)
	}
	var count int64
	err := config.DB.Raw(query, args...).Scan(&count).Error
	return count, err
}
