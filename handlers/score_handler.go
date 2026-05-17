package handlers

import (
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

type scoreAssignmentRow struct {
	ID                        uint       `gorm:"column:id"`
	CourseID                  string     `gorm:"column:course_id"`
	Name                      string     `gorm:"column:name"`
	MaxScore                  float64    `gorm:"column:max_score"`
	AssignmentType            string     `gorm:"column:assignment_type"`
	WeekNumber                *int       `gorm:"column:week_number"`
	LinkedAttendanceSessionID *uint      `gorm:"column:linked_attendance_session_id"`
	AttendanceCondition       string     `gorm:"column:attendance_condition"`
	DueDate                   *time.Time `gorm:"column:due_date"`
	OrderIndex                int        `gorm:"column:order_index"`
}

type scoreSubItemRow struct {
	ID           uint    `gorm:"column:id"`
	AssignmentID uint    `gorm:"column:assignment_id"`
	Name         string  `gorm:"column:name"`
	MaxScore     float64 `gorm:"column:max_score"`
	OrderIndex   int     `gorm:"column:order_index"`
}

type scoreStudentRow struct {
	ID            uint   `gorm:"column:id"`
	StudentID     string `gorm:"column:student_id"`
	FullName      string `gorm:"column:full_name"`
	Email         string `gorm:"column:email"`
	SectionNumber string `gorm:"column:section_number"`
}

type scoreRecordRow struct {
	ID           uint       `gorm:"column:id"`
	AssignmentID uint       `gorm:"column:assignment_id"`
	StudentID    *uint      `gorm:"column:student_id"`
	GroupID      *uint      `gorm:"column:group_id"`
	SubItemID    *uint      `gorm:"column:sub_item_id"`
	Score        float64    `gorm:"column:score"`
	Comment      string     `gorm:"column:comment"`
	Status       string     `gorm:"column:status"`
	GradedBy     *uint      `gorm:"column:graded_by"`
	GradedAt     *time.Time `gorm:"column:graded_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

type scoreSectionRow struct {
	ID            uint   `gorm:"column:id"`
	SectionNumber string `gorm:"column:section_number"`
}

type scoreLinkedSessionRow struct {
	ID        uint      `gorm:"column:id"`
	Title     string    `gorm:"column:title"`
	StartTime time.Time `gorm:"column:start_time"`
	EndTime   time.Time `gorm:"column:end_time"`
}

func uniqueScoreUintValues(values []uint) []uint {
	seen := make(map[uint]bool, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func loadScoreAssignments(courseID string, assignmentType string) ([]scoreAssignmentRow, map[uint][]scoreSubItemRow, error) {
	query := `
		SELECT id, course_id, name, max_score, assignment_type, week_number,
		       linked_attendance_session_id, attendance_condition, due_date, order_index
		FROM assignments
		WHERE course_id = ? AND is_active = true
	`
	args := []interface{}{courseID}
	switch strings.TrimSpace(assignmentType) {
	case "individual":
		query += ` AND assignment_type = 'individual'`
	case "assignment":
		query += ` AND assignment_type = 'assignment'`
	case "group":
		query += ` AND assignment_type IN ('permanent_group', 'weekly_group')`
	}
	query += ` ORDER BY order_index ASC, id ASC`

	var assignments []scoreAssignmentRow
	if err := config.DB.Raw(query, args...).Scan(&assignments).Error; err != nil {
		return nil, nil, err
	}

	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	subItemsByAssignment := map[uint][]scoreSubItemRow{}
	if len(assignmentIDs) > 0 {
		var subItems []scoreSubItemRow
		if err := config.DB.Raw(`
			SELECT id, assignment_id, name, max_score, order_index
			FROM assignment_sub_items
			WHERE assignment_id IN ?
			ORDER BY assignment_id ASC, order_index ASC, id ASC
		`, assignmentIDs).Scan(&subItems).Error; err != nil {
			return nil, nil, err
		}
		for _, item := range subItems {
			subItemsByAssignment[item.AssignmentID] = append(subItemsByAssignment[item.AssignmentID], item)
		}
	}

	return assignments, subItemsByAssignment, nil
}

func loadScoreStudents(courseID string, sectionID string, query string) ([]scoreStudentRow, error) {
	sql := `
		SELECT DISTINCT s.id, s.student_id, s.full_name, s.email, cs.section_no AS section_number
		FROM course_sections cs
		JOIN course_section_students css ON css.course_section_id = cs.id
		JOIN students s ON s.id = css.student_id
		WHERE cs.course_id = ?
	`
	args := []interface{}{courseID}
	if sectionID != "" {
		sql += ` AND cs.id = ?`
		args = append(args, sectionID)
	}
	if strings.TrimSpace(query) != "" {
		pattern := "%" + strings.TrimSpace(query) + "%"
		sql += ` AND (s.student_id ILIKE ? OR s.full_name ILIKE ?)`
		args = append(args, pattern, pattern)
	}
	sql += ` ORDER BY cs.section_no ASC, s.student_id ASC`

	var students []scoreStudentRow
	if err := config.DB.Raw(sql, args...).Scan(&students).Error; err != nil {
		return nil, err
	}

	seen := map[uint]bool{}
	unique := make([]scoreStudentRow, 0, len(students))
	for _, student := range students {
		if seen[student.ID] {
			continue
		}
		seen[student.ID] = true
		unique = append(unique, student)
	}
	return unique, nil
}

func loadScoreRows(assignmentIDs []uint, studentIDs []uint) ([]scoreRecordRow, error) {
	if len(assignmentIDs) == 0 || len(studentIDs) == 0 {
		return []scoreRecordRow{}, nil
	}
	var rows []scoreRecordRow
	err := config.DB.Raw(`
		SELECT id, assignment_id, student_id, group_id, sub_item_id, score, comment, status, graded_by, graded_at, updated_at
		FROM scores
		WHERE assignment_id IN ? AND student_id IN ?
	`, assignmentIDs, studentIDs).Scan(&rows).Error
	return rows, err
}

func loadScoreGraderNames(userIDs []uint) (map[uint]string, error) {
	result := map[uint]string{}
	userIDs = uniqueScoreUintValues(userIDs)
	if len(userIDs) == 0 {
		return result, nil
	}
	var users []models.User
	if err := config.DB.Select("id", "full_name").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		result[user.ID] = user.FullName
	}
	return result, nil
}

func loadAssignmentLinkedSessions(assignment scoreAssignmentRow) ([]scoreLinkedSessionRow, error) {
	sessionIDs := make([]uint, 0)
	if assignment.LinkedAttendanceSessionID != nil {
		sessionIDs = append(sessionIDs, *assignment.LinkedAttendanceSessionID)
	}
	var links []models.AssignmentAttendanceLink
	if err := config.DB.Where("assignment_id = ?", assignment.ID).Find(&links).Error; err != nil {
		return nil, err
	}
	for _, link := range links {
		sessionIDs = append(sessionIDs, link.AttendanceSessionID)
	}
	sessionIDs = uniqueScoreUintValues(sessionIDs)
	if len(sessionIDs) == 0 {
		return []scoreLinkedSessionRow{}, nil
	}

	var sessions []scoreLinkedSessionRow
	if err := config.DB.Raw(`
		SELECT id, title, start_time, end_time
		FROM attendance_sessions
		WHERE id IN ?
		ORDER BY start_time ASC, id ASC
	`, sessionIDs).Scan(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func loadAttendanceStatusMap(sessionIDs []uint, studentIDs []uint) (map[uint]map[uint]string, error) {
	result := map[uint]map[uint]string{}
	if len(sessionIDs) == 0 || len(studentIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		AttendanceSessionID uint   `gorm:"column:attendance_session_id"`
		StudentID           uint   `gorm:"column:student_id"`
		Status              string `gorm:"column:status"`
	}
	if err := config.DB.Raw(`
		SELECT attendance_session_id, student_id, status
		FROM attendance_records
		WHERE attendance_session_id IN ? AND student_id IN ?
	`, sessionIDs, studentIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if result[row.StudentID] == nil {
			result[row.StudentID] = map[uint]string{}
		}
		result[row.StudentID][row.AttendanceSessionID] = row.Status
	}
	return result, nil
}

func buildScoreMatrixHandlerData(courseID string, sectionID string, assignmentType string) (fiber.Map, error) {
	var sections []scoreSectionRow
	if err := config.DB.Raw(`SELECT id, section_no AS section_number FROM course_sections WHERE course_id = ? ORDER BY section_no ASC, id ASC`, courseID).Scan(&sections).Error; err != nil {
		return nil, err
	}

	students, err := loadScoreStudents(courseID, sectionID, "")
	if err != nil {
		return nil, err
	}

	assignments, subItemsByAssignment, err := loadScoreAssignments(courseID, assignmentType)
	if err != nil {
		return nil, err
	}

	studentIDs := make([]uint, 0, len(students))
	for _, student := range students {
		studentIDs = append(studentIDs, student.ID)
	}
	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	scoreRows, err := loadScoreRows(assignmentIDs, studentIDs)
	if err != nil {
		return nil, err
	}

	gratedByIDs := make([]uint, 0, len(scoreRows))
	for _, row := range scoreRows {
		if row.GradedBy != nil {
			gratedByIDs = append(gratedByIDs, *row.GradedBy)
		}
	}
	graderNames, err := loadScoreGraderNames(gratedByIDs)
	if err != nil {
		return nil, err
	}

	// Load group names for score rows that have a group_id
	groupIDsForScores := make([]uint, 0)
	for _, row := range scoreRows {
		if row.GroupID != nil {
			groupIDsForScores = append(groupIDsForScores, *row.GroupID)
		}
	}
	groupNameMap := map[uint]string{}
	groupIDsForScores = uniqueScoreUintValues(groupIDsForScores)
	if len(groupIDsForScores) > 0 {
		var groupRows []struct {
			ID   uint   `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err := config.DB.Raw(`SELECT id, name FROM student_groups WHERE id IN ?`, groupIDsForScores).Scan(&groupRows).Error; err != nil {
			return nil, err
		}
		for _, gr := range groupRows {
			groupNameMap[gr.ID] = gr.Name
		}
	}

	// Load permanent student-group mapping for group/permanent_group assignment types
	isGroupAssignment := strings.Contains(assignmentType, "group") || assignmentType == "group" || assignmentType == "permanent_group"
	type studentGroupInfo struct {
		GroupID   uint
		GroupName string
	}
	studentGroupMap := map[uint]studentGroupInfo{}
	if isGroupAssignment && len(studentIDs) > 0 {
		var sgRows []struct {
			StudentID uint   `gorm:"column:student_id"`
			GroupID   uint   `gorm:"column:group_id"`
			GroupName string `gorm:"column:group_name"`
		}
		if err := config.DB.Raw(`
			SELECT sgm.student_id, sg.id AS group_id, sg.name AS group_name
			FROM student_group_members sgm
			JOIN student_groups sg ON sg.id = sgm.group_id
			WHERE sg.course_id = ? AND sgm.student_id IN ? AND sg.group_type = 'permanent'
			ORDER BY sg.id ASC
		`, courseID, studentIDs).Scan(&sgRows).Error; err != nil {
			return nil, err
		}
		for _, sgr := range sgRows {
			if _, exists := studentGroupMap[sgr.StudentID]; !exists {
				studentGroupMap[sgr.StudentID] = studentGroupInfo{GroupID: sgr.GroupID, GroupName: sgr.GroupName}
			}
		}
	}

	var bonusRows []struct {
		StudentID uint    `gorm:"column:student_id"`
		Total     float64 `gorm:"column:total"`
	}
	bonusMap := map[uint]float64{}
	if len(studentIDs) > 0 {
		if err := config.DB.Raw(`
			SELECT student_id, COALESCE(SUM(score), 0) AS total
			FROM bonus_scores
			WHERE course_id = ? AND student_id IN ?
			GROUP BY student_id
		`, courseID, studentIDs).Scan(&bonusRows).Error; err != nil {
			return nil, err
		}
		for _, row := range bonusRows {
			bonusMap[row.StudentID] = row.Total
		}
	}

	type scoreCell struct {
		Score     float64
		HasScore  bool
		Comment   string
		GradedBy  string
		GradedAt  *time.Time
		UpdatedAt time.Time
		ScoreID   uint
		GroupID   *uint
		GroupName string
	}
	scoreMap := map[string]scoreCell{}
	for _, row := range scoreRows {
		studentID := uint(0)
		if row.StudentID != nil {
			studentID = *row.StudentID
		}
		key := fmt.Sprintf("%d_%d_main", studentID, row.AssignmentID)
		if row.SubItemID != nil {
			key = fmt.Sprintf("%d_%d_%d", studentID, row.AssignmentID, *row.SubItemID)
		}
		graderName := ""
		if row.GradedBy != nil {
			graderName = graderNames[*row.GradedBy]
		}
		groupName := ""
		if row.GroupID != nil {
			groupName = groupNameMap[*row.GroupID]
		}
		scoreMap[key] = scoreCell{Score: row.Score, HasScore: true, Comment: row.Comment, GradedBy: graderName, GradedAt: row.GradedAt, UpdatedAt: row.UpdatedAt, ScoreID: row.ID, GroupID: row.GroupID, GroupName: groupName}
	}

	// Load approved score edit requests for all scores in this matrix
	type editRequestInfo struct {
		ScoreID       uint       `gorm:"column:score_id"`
		OldScore      *float64   `gorm:"column:old_score"`
		NewScore      float64    `gorm:"column:new_score"`
		Reason        string     `gorm:"column:reason"`
		Requester     string     `gorm:"column:requester"`
		Reviewer      string     `gorm:"column:reviewer"`
		ReviewedAt    *time.Time `gorm:"column:reviewed_at"`
		ReviewComment string     `gorm:"column:review_comment"`
	}
	editRequestsMap := map[uint][]fiber.Map{}
	allScoreIDs := make([]uint, 0, len(scoreMap))
	for _, cell := range scoreMap {
		if cell.ScoreID > 0 {
			allScoreIDs = append(allScoreIDs, cell.ScoreID)
		}
	}
	allScoreIDs = uniqueScoreUintValues(allScoreIDs)
	if len(allScoreIDs) > 0 {
		var erRows []editRequestInfo
		if err := config.DB.Raw(`
			SELECT ser.score_id,
			       ser.old_score,
			       ser.new_score,
			       ser.reason,
			       COALESCE(req.full_name, '') AS requester,
			       COALESCE(rev.full_name, '') AS reviewer,
			       ser.reviewed_at,
			       ser.review_comment
			FROM score_edit_requests ser
			LEFT JOIN users req ON req.id = ser.requested_by
			LEFT JOIN users rev ON rev.id = ser.reviewed_by
			WHERE ser.score_id IN ? AND ser.status = 'approved'
			ORDER BY ser.reviewed_at DESC
		`, allScoreIDs).Scan(&erRows).Error; err != nil {
			return nil, err
		}
		for _, er := range erRows {
			editRequestsMap[er.ScoreID] = append(editRequestsMap[er.ScoreID], fiber.Map{
				"old_score":      er.OldScore,
				"new_score":      er.NewScore,
				"reason":         er.Reason,
				"requester":      er.Requester,
				"reviewer":       er.Reviewer,
				"reviewed_at":    er.ReviewedAt,
				"review_comment": er.ReviewComment,
			})
		}
	}

	averages := fiber.Map{}
	matrixData := make([]fiber.Map, 0, len(students))
	for _, student := range students {
		groupInfo, hasGroup := studentGroupMap[student.ID]
		row := fiber.Map{
			"student_id":      student.StudentID,
			"full_name":       student.FullName,
			"section_number":  student.SectionNumber,
			"bonus_score":     bonusMap[student.ID],
			"group_id":        nil,
			"group_name":      nil,
			"scores":          fiber.Map{},
			"total_score":     0.0,
			"total_max_score": 0.0,
			"scored_count":    0,
			"total_items":     0,
		}
		if hasGroup {
			row["group_id"] = groupInfo.GroupID
			row["group_name"] = groupInfo.GroupName
		}
		scoresPayload := row["scores"].(fiber.Map)
		for _, assignment := range assignments {
			subItems := subItemsByAssignment[assignment.ID]
			if len(subItems) > 0 {
				for _, subItem := range subItems {
					key := fmt.Sprintf("%d_%d_%d", student.ID, assignment.ID, subItem.ID)
					cell, ok := scoreMap[key]
					cellKey := fmt.Sprintf("%d_%d", assignment.ID, subItem.ID)
					editReqs := editRequestsMap[cell.ScoreID]
					if editReqs == nil {
						editReqs = []fiber.Map{}
					}
					payload := fiber.Map{"score": nil, "max_score": subItem.MaxScore, "sub_item_name": subItem.Name, "graded_by": nil, "graded_at": nil, "updated_at": nil, "comment": nil, "group_name": nil, "edit_requests": editReqs}
					if ok {
						payload["score"] = cell.Score
						payload["graded_by"] = cell.GradedBy
						payload["graded_at"] = cell.GradedAt
						payload["updated_at"] = cell.UpdatedAt
						payload["comment"] = cell.Comment
						payload["group_name"] = cell.GroupName
						if ers := editRequestsMap[cell.ScoreID]; ers != nil {
							payload["edit_requests"] = ers
						}
						row["total_score"] = row["total_score"].(float64) + cell.Score
						row["scored_count"] = row["scored_count"].(int) + 1
					}
					scoresPayload[cellKey] = payload
					row["total_max_score"] = row["total_max_score"].(float64) + subItem.MaxScore
					row["total_items"] = row["total_items"].(int) + 1
				}
			} else {
				key := fmt.Sprintf("%d_%d_main", student.ID, assignment.ID)
				cell, ok := scoreMap[key]
				cellKey := fmt.Sprintf("%d_main", assignment.ID)
				editReqs := []fiber.Map{}
				if ok {
					if ers := editRequestsMap[cell.ScoreID]; ers != nil {
						editReqs = ers
					}
				}
				payload := fiber.Map{"score": nil, "max_score": assignment.MaxScore, "graded_by": nil, "graded_at": nil, "updated_at": nil, "comment": nil, "group_name": nil, "edit_requests": editReqs}
				if ok {
					payload["score"] = cell.Score
					payload["graded_by"] = cell.GradedBy
					payload["graded_at"] = cell.GradedAt
					payload["updated_at"] = cell.UpdatedAt
					payload["comment"] = cell.Comment
					payload["group_name"] = cell.GroupName
					row["total_score"] = row["total_score"].(float64) + cell.Score
					row["scored_count"] = row["scored_count"].(int) + 1
				}
				scoresPayload[cellKey] = payload
				row["total_max_score"] = row["total_max_score"].(float64) + assignment.MaxScore
				row["total_items"] = row["total_items"].(int) + 1
			}
		}
		matrixData = append(matrixData, row)
	}

	// Re-sort by group_id then student_id for group/permanent_group assignments
	if isGroupAssignment {
		sort.Slice(matrixData, func(i, j int) bool {
			gA := uint(0)
			gB := uint(0)
			if v := matrixData[i]["group_id"]; v != nil {
				if id, ok := v.(uint); ok {
					gA = id
				}
			}
			if v := matrixData[j]["group_id"]; v != nil {
				if id, ok := v.(uint); ok {
					gB = id
				}
			}
			if gA != gB {
				return gA < gB
			}
			sA, _ := matrixData[i]["student_id"].(string)
			sB, _ := matrixData[j]["student_id"].(string)
			return sA < sB
		})
	}

	for _, assignment := range assignments {
		subItems := subItemsByAssignment[assignment.ID]
		if len(subItems) > 0 {
			for _, subItem := range subItems {
				cellKey := fmt.Sprintf("%d_%d", assignment.ID, subItem.ID)
				total := 0.0
				count := 0
				for _, student := range matrixData {
					cell := student["scores"].(fiber.Map)[cellKey].(fiber.Map)
					if value, ok := cell["score"].(float64); ok {
						total += value
						count++
					}
				}
				if count > 0 {
					averages[cellKey] = fmt.Sprintf("%.2f", total/float64(count))
				} else {
					averages[cellKey] = nil
				}
			}
		} else {
			cellKey := fmt.Sprintf("%d_main", assignment.ID)
			total := 0.0
			count := 0
			for _, student := range matrixData {
				cell := student["scores"].(fiber.Map)[cellKey].(fiber.Map)
				if value, ok := cell["score"].(float64); ok {
					total += value
					count++
				}
			}
			if count > 0 {
				averages[cellKey] = fmt.Sprintf("%.2f", total/float64(count))
			} else {
				averages[cellKey] = nil
			}
		}
	}

	formattedAssignments := make([]fiber.Map, len(assignments))
	for i, assignment := range assignments {
		subItems := subItemsByAssignment[assignment.ID]
		formattedSubItems := make([]fiber.Map, len(subItems))
		for j, subItem := range subItems {
			formattedSubItems[j] = fiber.Map{"id": subItem.ID, "name": subItem.Name, "max_score": subItem.MaxScore}
		}
		formattedAssignments[i] = fiber.Map{"id": assignment.ID, "title": assignment.Name, "short_title": assignment.Name, "max_score": assignment.MaxScore, "assignment_type": assignment.AssignmentType, "subItems": formattedSubItems}
	}

	sectionsPayload := make([]fiber.Map, len(sections))
	for i, section := range sections {
		sectionsPayload[i] = fiber.Map{"id": section.ID, "section_number": section.SectionNumber}
	}

	return fiber.Map{
		"sections":    sectionsPayload,
		"assignments": formattedAssignments,
		"students":    matrixData,
		"averages":    averages,
		"summary": fiber.Map{
			"total_students":    len(matrixData),
			"total_assignments": len(assignments),
		},
	}, nil
}

// GET /api/scores?assignment_id=
func GetScoresHandler(c fiber.Ctx) error {
	assignmentIDStr := c.Query("assignment_id")
	if assignmentIDStr == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "assignment_id required"})
	}
	assignmentID, err := strconv.ParseUint(assignmentIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid assignment_id"})
	}

	var assignment scoreAssignmentRow
	if err := config.DB.Raw(`
		SELECT id, course_id, name, max_score, assignment_type, week_number,
		       linked_attendance_session_id, attendance_condition, due_date, order_index
		FROM assignments
		WHERE id = ?
	`, uint(assignmentID)).Scan(&assignment).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load assignment"})
	}
	if assignment.ID == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}

	students, err := loadScoreStudents(assignment.CourseID, "", "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}
	studentIDs := make([]uint, 0, len(students))
	for _, student := range students {
		studentIDs = append(studentIDs, student.ID)
	}

	_, subItemsByAssignment, err := loadScoreAssignments(assignment.CourseID, "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load assignment sub-items"})
	}
	subItems := subItemsByAssignment[assignment.ID]

	scoreRows, err := loadScoreRows([]uint{assignment.ID}, studentIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch scores"})
	}

	graderIDs := make([]uint, 0, len(scoreRows))
	for _, row := range scoreRows {
		if row.GradedBy != nil {
			graderIDs = append(graderIDs, *row.GradedBy)
		}
	}
	graderNames, err := loadScoreGraderNames(graderIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load grader data"})
	}

	mainScores := map[uint]scoreRecordRow{}
	subItemScores := map[string]scoreRecordRow{}
	for _, row := range scoreRows {
		if row.StudentID == nil {
			continue
		}
		if row.SubItemID != nil {
			subItemScores[fmt.Sprintf("%d_%d", *row.StudentID, *row.SubItemID)] = row
		} else {
			mainScores[*row.StudentID] = row
		}
	}

	linkedSessions, err := loadAssignmentLinkedSessions(assignment)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load linked attendance sessions"})
	}
	sessionIDs := make([]uint, 0, len(linkedSessions))
	for _, session := range linkedSessions {
		sessionIDs = append(sessionIDs, session.ID)
	}
	attendanceMap, err := loadAttendanceStatusMap(sessionIDs, studentIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load attendance records"})
	}

	condition := strings.TrimSpace(assignment.AttendanceCondition)
	if condition == "" {
		condition = "or"
	}

	studentScores := make([]fiber.Map, 0, len(students))
	for _, student := range students {
		mainScore, hasMainScore := mainScores[student.ID]
		subItemPayload := make([]fiber.Map, len(subItems))
		for i, subItem := range subItems {
			row, ok := subItemScores[fmt.Sprintf("%d_%d", student.ID, subItem.ID)]
			payload := fiber.Map{"sub_item_id": subItem.ID, "sub_item_name": subItem.Name, "max_score": subItem.MaxScore, "score": nil, "score_id": nil, "graded_by": nil, "graded_at": nil}
			if ok {
				payload["score"] = row.Score
				payload["score_id"] = row.ID
				payload["graded_at"] = row.GradedAt
				if row.GradedBy != nil {
					payload["graded_by"] = fiber.Map{"id": *row.GradedBy, "display_name": graderNames[*row.GradedBy]}
				}
			}
			subItemPayload[i] = payload
		}

		attendanceDetails := make([]fiber.Map, 0, len(linkedSessions))
		attendanceStatus := interface{}(nil)
		canScore := true
		attendedCount := 0
		if len(linkedSessions) > 0 {
			for _, session := range linkedSessions {
				status := "absent"
				if attendanceMap[student.ID] != nil {
					if current, ok := attendanceMap[student.ID][session.ID]; ok && current != "" {
						status = current
					}
				}
				attended := status != "absent"
				if attended {
					attendedCount++
				}
				attendanceDetails = append(attendanceDetails, fiber.Map{"session_id": session.ID, "session_title": session.Title, "start_time": session.StartTime, "end_time": session.EndTime, "status": status, "attended": attended})
			}
			totalCount := len(linkedSessions)
			if condition == "and" {
				canScore = attendedCount == totalCount
				if canScore {
					attendanceStatus = "present"
				} else {
					attendanceStatus = "absent"
				}
			} else {
				canScore = attendedCount > 0
				if !canScore {
					attendanceStatus = "absent"
				} else if attendedCount == totalCount {
					attendanceStatus = "present"
				} else {
					attendanceStatus = "partial"
				}
			}
			studentScores = append(studentScores, fiber.Map{
				"student": fiber.Map{"id": student.ID, "student_id": student.StudentID, "full_name": student.FullName, "email": student.Email},
				"score": func() interface{} {
					if hasMainScore {
						return mainScore.Score
					}
					return nil
				}(),
				"score_id": func() interface{} {
					if hasMainScore {
						return mainScore.ID
					}
					return nil
				}(),
				"max_score": assignment.MaxScore,
				"comment": func() interface{} {
					if hasMainScore {
						return mainScore.Comment
					}
					return nil
				}(),
				"status": func() string {
					if hasMainScore && strings.TrimSpace(mainScore.Status) != "" {
						return mainScore.Status
					}
					return "pending"
				}(),
				"graded_by": func() interface{} {
					if hasMainScore && mainScore.GradedBy != nil {
						return fiber.Map{"id": *mainScore.GradedBy, "display_name": graderNames[*mainScore.GradedBy]}
					}
					return nil
				}(),
				"graded_at": func() interface{} {
					if hasMainScore {
						return mainScore.GradedAt
					}
					return nil
				}(),
				"sub_item_scores":      subItemPayload,
				"attendance_status":    attendanceStatus,
				"can_score":            canScore,
				"attendance_details":   attendanceDetails,
				"attendance_condition": condition,
				"attended_count":       attendedCount,
				"total_count":          totalCount,
			})
			continue
		}

		studentScores = append(studentScores, fiber.Map{
			"student": fiber.Map{"id": student.ID, "student_id": student.StudentID, "full_name": student.FullName, "email": student.Email},
			"score": func() interface{} {
				if hasMainScore {
					return mainScore.Score
				}
				return nil
			}(),
			"score_id": func() interface{} {
				if hasMainScore {
					return mainScore.ID
				}
				return nil
			}(),
			"max_score": assignment.MaxScore,
			"comment": func() interface{} {
				if hasMainScore {
					return mainScore.Comment
				}
				return nil
			}(),
			"status": func() string {
				if hasMainScore && strings.TrimSpace(mainScore.Status) != "" {
					return mainScore.Status
				}
				return "pending"
			}(),
			"graded_by": func() interface{} {
				if hasMainScore && mainScore.GradedBy != nil {
					return fiber.Map{"id": *mainScore.GradedBy, "display_name": graderNames[*mainScore.GradedBy]}
				}
				return nil
			}(),
			"graded_at": func() interface{} {
				if hasMainScore {
					return mainScore.GradedAt
				}
				return nil
			}(),
			"sub_item_scores":      subItemPayload,
			"attendance_status":    nil,
			"can_score":            true,
			"attendance_details":   []fiber.Map{},
			"attendance_condition": nil,
			"attended_count":       nil,
			"total_count":          nil,
		})
	}

	assignmentPayload := fiber.Map{
		"id":                           assignment.ID,
		"course_id":                    assignment.CourseID,
		"name":                         assignment.Name,
		"max_score":                    assignment.MaxScore,
		"assignment_type":              assignment.AssignmentType,
		"week_number":                  assignment.WeekNumber,
		"linked_attendance_session_id": assignment.LinkedAttendanceSessionID,
		"attendance_condition":         condition,
		"due_date":                     assignment.DueDate,
		"order_index":                  assignment.OrderIndex,
		"subItems": func() []fiber.Map {
			items := make([]fiber.Map, len(subItems))
			for i, item := range subItems {
				items[i] = fiber.Map{"id": item.ID, "assignment_id": item.AssignmentID, "name": item.Name, "max_score": item.MaxScore, "order_index": item.OrderIndex}
			}
			return items
		}(),
		"linked_sessions_count": len(linkedSessions),
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"assignment": assignmentPayload, "student_scores": studentScores}})
}

// GET /api/scores/summary?course_id=&student_id=
func GetStudentScoresSummaryHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}

	studentIDStr := strings.TrimSpace(c.Query("student_id"))
	var filterStudentID uint64
	var filterByStudent bool
	if studentIDStr != "" {
		parsed, err := strconv.ParseUint(studentIDStr, 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid student_id"})
		}
		filterStudentID = parsed
		filterByStudent = true
	}

	assignments, subItemsByAssignment, err := loadScoreAssignments(courseID, "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load assignments"})
	}
	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	var scoreRows []scoreRecordRow
	if len(assignmentIDs) > 0 {
		query := `SELECT id, assignment_id, student_id, group_id, sub_item_id, score, comment, status, graded_by, graded_at, updated_at FROM scores WHERE assignment_id IN ?`
		args := []interface{}{assignmentIDs}
		if filterByStudent {
			query += ` AND student_id = ?`
			args = append(args, uint(filterStudentID))
		}
		if err := config.DB.Raw(query, args...).Scan(&scoreRows).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load scores summary"})
		}
	}

	scoresByAssignment := map[uint][]fiber.Map{}
	for _, row := range scoreRows {
		scoresByAssignment[row.AssignmentID] = append(scoresByAssignment[row.AssignmentID], fiber.Map{
			"id":          row.ID,
			"student_id":  row.StudentID,
			"group_id":    row.GroupID,
			"sub_item_id": row.SubItemID,
			"score":       row.Score,
			"comment":     row.Comment,
			"status":      row.Status,
			"graded_by":   row.GradedBy,
			"graded_at":   row.GradedAt,
		})
	}

	data := make([]fiber.Map, len(assignments))
	for i, assignment := range assignments {
		subItems := subItemsByAssignment[assignment.ID]
		formattedSubItems := make([]fiber.Map, len(subItems))
		for j, item := range subItems {
			formattedSubItems[j] = fiber.Map{"id": item.ID, "assignment_id": item.AssignmentID, "name": item.Name, "max_score": item.MaxScore, "order_index": item.OrderIndex}
		}
		data[i] = fiber.Map{
			"id":                           assignment.ID,
			"course_id":                    assignment.CourseID,
			"name":                         assignment.Name,
			"max_score":                    assignment.MaxScore,
			"assignment_type":              assignment.AssignmentType,
			"week_number":                  assignment.WeekNumber,
			"linked_attendance_session_id": assignment.LinkedAttendanceSessionID,
			"attendance_condition":         assignment.AttendanceCondition,
			"due_date":                     assignment.DueDate,
			"order_index":                  assignment.OrderIndex,
			"subItems":                     formattedSubItems,
			"scores":                       scoresByAssignment[assignment.ID],
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": data})
}

// GET /api/scores/matrix?course_id=
func GetScoreMatrixHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	matrix, err := buildScoreMatrixHandlerData(courseID, c.Query("section_id"), c.Query("assignment_type"))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch score matrix"})
	}
	return c.JSON(fiber.Map{"success": true, "data": matrix})
}

// GET /api/scores/students/search?course_id=&query=
func SearchScoreStudentsHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	students, err := loadScoreStudents(courseID, "", c.Query("query"))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to search students"})
	}
	data := make([]fiber.Map, len(students))
	for i, student := range students {
		data[i] = fiber.Map{"id": student.ID, "student_id": student.StudentID, "full_name": student.FullName, "email": student.Email}
	}
	return c.JSON(fiber.Map{"success": true, "data": data, "total": len(data)})
}

// GET /api/scores/groups?assignment_id=
func GetGroupsForAssignmentHandler(c fiber.Ctx) error {
	assignmentIDStr := c.Query("assignment_id")
	if assignmentIDStr == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "assignment_id required"})
	}
	assignmentID, err := strconv.ParseUint(assignmentIDStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid assignment_id"})
	}

	var assignment models.Assignment
	if err := config.DB.First(&assignment, uint(assignmentID)).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}

	if assignment.AssignmentType != "permanent_group" && assignment.AssignmentType != "weekly_group" {
		return c.JSON(fiber.Map{"success": true, "data": []fiber.Map{}})
	}

	query := config.DB.Model(&models.StudentGroup{}).Where("course_id = ?", assignment.CourseID)
	if assignment.AssignmentType == "permanent_group" {
		query = query.Where("group_type = ?", "permanent")
	} else {
		query = query.Where("group_type = ?", "temporary")
		if assignment.WeekNumber != nil {
			query = query.Where("week_number = ?", *assignment.WeekNumber)
		}
	}

	var groups []models.StudentGroup
	if err := query.Order("name ASC, id ASC").Find(&groups).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load groups"})
	}
	if len(groups) == 0 {
		return c.JSON(fiber.Map{"success": true, "data": []fiber.Map{}})
	}

	groupIDs := make([]uint, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	var memberRows []struct {
		GroupID   uint   `gorm:"column:group_id"`
		ID        uint   `gorm:"column:id"`
		StudentID string `gorm:"column:student_id"`
		FullName  string `gorm:"column:full_name"`
		Email     string `gorm:"column:email"`
	}
	if err := config.DB.Raw(`
		SELECT sgm.group_id, s.id, s.student_id, s.full_name, s.email
		FROM student_group_members sgm
		JOIN students s ON s.id = sgm.student_id
		WHERE sgm.group_id IN ?
		ORDER BY s.student_id ASC
	`, groupIDs).Scan(&memberRows).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load group members"})
	}
	membersByGroup := map[uint][]fiber.Map{}
	for _, row := range memberRows {
		membersByGroup[row.GroupID] = append(membersByGroup[row.GroupID], fiber.Map{"id": row.ID, "student_id": row.StudentID, "full_name": row.FullName, "email": row.Email})
	}
	data := make([]fiber.Map, len(groups))
	for i, group := range groups {
		data[i] = fiber.Map{"id": group.ID, "course_id": group.CourseID, "name": group.Name, "group_type": group.GroupType, "week_number": group.WeekNumber, "members": membersByGroup[group.ID]}
	}
	return c.JSON(fiber.Map{"success": true, "data": data})
}

// GET /api/scores/ungraded-summary?course_id=
func GetUngradedSummaryHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}

	assignments, _, err := loadScoreAssignments(courseID, "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load assignments"})
	}
	if len(assignments) == 0 {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{}})
	}

	students, err := loadScoreStudents(courseID, "", "")
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load students"})
	}
	if len(students) == 0 {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{}})
	}

	assignmentIDs := make([]uint, 0, len(assignments))
	studentIDs := make([]uint, 0, len(students))
	studentLookup := map[uint]fiber.Map{}
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}
	for _, student := range students {
		studentIDs = append(studentIDs, student.ID)
		studentLookup[student.ID] = fiber.Map{"student_id": student.StudentID, "full_name": student.FullName}
	}

	var scoredRows []struct {
		AssignmentID uint `gorm:"column:assignment_id"`
		StudentID    uint `gorm:"column:student_id"`
	}
	if err := config.DB.Raw(`
		SELECT DISTINCT assignment_id, student_id
		FROM scores
		WHERE assignment_id IN ? AND student_id IN ?
	`, assignmentIDs, studentIDs).Scan(&scoredRows).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to inspect graded scores"})
	}

	scoredMap := map[uint]map[uint]bool{}
	for _, row := range scoredRows {
		if scoredMap[row.AssignmentID] == nil {
			scoredMap[row.AssignmentID] = map[uint]bool{}
		}
		scoredMap[row.AssignmentID][row.StudentID] = true
	}

	summary := fiber.Map{}
	for _, assignment := range assignments {
		scoredStudents := scoredMap[assignment.ID]
		if scoredStudents == nil {
			scoredStudents = map[uint]bool{}
		}
		ungradedStudents := make([]fiber.Map, 0)
		for _, student := range students {
			if !scoredStudents[student.ID] {
				ungradedStudents = append(ungradedStudents, studentLookup[student.ID])
			}
		}
		key := strconv.FormatUint(uint64(assignment.ID), 10)
		summary[key] = fiber.Map{"ungraded_count": len(ungradedStudents), "total_students": len(students), "graded_count": len(scoredStudents), "students": ungradedStudents[:min(3, len(ungradedStudents))]}
	}

	return c.JSON(fiber.Map{"success": true, "data": summary})
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// POST /api/scores
func SubmitScoreHandler(c fiber.Ctx) error {
	var input struct {
		AssignmentID uint    `json:"assignment_id"`
		StudentID    *uint   `json:"student_id"`
		GroupID      *uint   `json:"group_id"`
		SubItemID    *uint   `json:"sub_item_id"`
		Score        float64 `json:"score"`
		Comment      string  `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.AssignmentID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "assignment_id required"})
	}

	var assignment models.Assignment
	_ = config.DB.Select("id", "course_id", "name").First(&assignment, input.AssignmentID).Error

	userID := c.Locals("user_id").(uint)
	now := time.Now()
	score := models.Score{
		AssignmentID: input.AssignmentID,
		StudentID:    input.StudentID,
		GroupID:      input.GroupID,
		SubItemID:    input.SubItemID,
		Score:        input.Score,
		Comment:      input.Comment,
		GradedBy:     &userID,
		GradedAt:     &now,
		Status:       "graded",
	}
	if err := repositories.SubmitScore(&score); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to submit score"})
	}
	logCourseActivity(c, assignment.CourseID, userID, "submit_score", "score", "assignment", input.AssignmentID, assignment.Name, fiber.Map{"student_id": input.StudentID, "group_id": input.GroupID, "sub_item_id": input.SubItemID, "score": input.Score})
	return c.Status(201).JSON(fiber.Map{"success": true, "data": score})
}

// POST /api/scores/bulk
func BulkSubmitScoresHandler(c fiber.Ctx) error {
	var input struct {
		AssignmentID uint `json:"assignment_id"`
		Scores       []struct {
			StudentID *uint   `json:"student_id"`
			GroupID   *uint   `json:"group_id"`
			SubItemID *uint   `json:"sub_item_id"`
			Score     float64 `json:"score"`
			Comment   string  `json:"comment"`
		} `json:"scores"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	var assignment models.Assignment
	_ = config.DB.Select("id", "course_id", "name").First(&assignment, input.AssignmentID).Error

	userID := c.Locals("user_id").(uint)
	now := time.Now()
	var scores []models.Score
	for _, s := range input.Scores {
		scores = append(scores, models.Score{
			AssignmentID: input.AssignmentID,
			StudentID:    s.StudentID,
			GroupID:      s.GroupID,
			SubItemID:    s.SubItemID,
			Score:        s.Score,
			Comment:      s.Comment,
			GradedBy:     &userID,
			GradedAt:     &now,
			Status:       "graded",
		})
	}
	if err := repositories.BulkUpsertScores(scores); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to bulk submit scores"})
	}
	logCourseActivity(c, assignment.CourseID, userID, "bulk_submit_scores", "score", "assignment", input.AssignmentID, assignment.Name, fiber.Map{"count": len(scores)})
	return c.JSON(fiber.Map{"success": true, "message": "Scores submitted", "count": len(scores)})
}

// POST /api/scores/group
func SubmitGroupScoreHandler(c fiber.Ctx) error {
	var input struct {
		AssignmentID uint    `json:"assignment_id"`
		GroupID      uint    `json:"group_id"`
		Score        float64 `json:"score"`
		Comment      string  `json:"comment"`
		SubItemID    *uint   `json:"sub_item_id"`
		StudentIDs   []uint  `json:"student_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.AssignmentID == 0 || input.GroupID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "assignment_id, group_id and score are required"})
	}

	var assignment models.Assignment
	if err := config.DB.First(&assignment, input.AssignmentID).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Assignment not found"})
	}
	maxScore := assignment.MaxScore
	if input.SubItemID != nil {
		var subItem models.AssignmentSubItem
		if err := config.DB.Where("id = ? AND assignment_id = ?", *input.SubItemID, input.AssignmentID).First(&subItem).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "Sub-item not found"})
		}
		maxScore = subItem.MaxScore
	}
	if input.Score < 0 || input.Score > maxScore {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("Score must be between 0 and %.2f", maxScore)})
	}

	var memberRows []struct {
		StudentID uint `gorm:"column:student_id"`
	}
	if err := config.DB.Raw(`SELECT student_id FROM student_group_members WHERE group_id = ?`, input.GroupID).Scan(&memberRows).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load group members"})
	}
	if len(memberRows) == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No members found in this group"})
	}

	memberSet := map[uint]bool{}
	memberIDs := make([]uint, 0, len(memberRows))
	for _, row := range memberRows {
		memberSet[row.StudentID] = true
		memberIDs = append(memberIDs, row.StudentID)
	}
	targetStudentIDs := memberIDs
	if len(input.StudentIDs) > 0 {
		filtered := make([]uint, 0, len(input.StudentIDs))
		for _, studentID := range input.StudentIDs {
			if !memberSet[studentID] {
				return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("Student %d is not a member of this group", studentID)})
			}
			filtered = append(filtered, studentID)
		}
		targetStudentIDs = uniqueScoreUintValues(filtered)
	}

	userID := c.Locals("user_id").(uint)
	groupID := input.GroupID
	for _, studentID := range targetStudentIDs {
		studentIDCopy := studentID
		score := models.Score{
			AssignmentID: input.AssignmentID,
			StudentID:    &studentIDCopy,
			GroupID:      &groupID,
			SubItemID:    input.SubItemID,
			Score:        input.Score,
			Comment:      input.Comment,
			GradedBy:     &userID,
		}
		if err := repositories.SubmitScore(&score); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to submit group score"})
		}
	}

	logCourseActivity(c, assignment.CourseID, userID, "submit_group_score", "score", "assignment", assignment.ID, assignment.Name, fiber.Map{"group_id": input.GroupID, "student_count": len(targetStudentIDs), "score": input.Score, "sub_item_id": input.SubItemID})

	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("Score submitted for %d group members", len(targetStudentIDs))})
}

// POST /api/scores/edit-request
func CreateScoreEditRequestHandler(c fiber.Ctx) error {
	var input struct {
		ScoreID  uint    `json:"score_id"`
		NewScore float64 `json:"new_score"`
		Reason   string  `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.ScoreID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "score_id required"})
	}

	userID := c.Locals("user_id").(uint)
	req := models.ScoreEditRequest{
		ScoreID:     input.ScoreID,
		NewScore:    input.NewScore,
		Reason:      input.Reason,
		RequestedBy: userID,
		Status:      "pending",
	}
	if err := repositories.CreateScoreEditRequest(&req); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create edit request"})
	}
	var score models.Score
	if err := config.DB.Select("id", "assignment_id").First(&score, input.ScoreID).Error; err == nil {
		var assignment models.Assignment
		if assignmentErr := config.DB.Select("id", "course_id", "name").First(&assignment, score.AssignmentID).Error; assignmentErr == nil {
			logCourseActivity(c, assignment.CourseID, userID, "create_score_edit_request", "score", "assignment", assignment.ID, assignment.Name, fiber.Map{"score_id": input.ScoreID, "new_score": input.NewScore})
		}
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": req})
}

// GET /api/scores/edit-requests?course_id=
func GetPendingEditRequestsHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	userID := c.Locals("user_id").(uint)
	role := c.Locals("user_role").(string)
	canReviewAll, err := repositories.CanReviewAllCourseScoreRequests(courseID, userID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
	}
	requests, err := loadScoreEditRequests(courseID, "pending", userID, canReviewAll)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch requests"})
	}
	return c.JSON(fiber.Map{"success": true, "data": requests})
}

// GET /api/score-edit-requests/pending-count?course_id=
func GetPendingCountHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}

	userID := c.Locals("user_id").(uint)
	role := c.Locals("user_role").(string)

	isInstructor, err := repositories.CanReviewAllCourseScoreRequests(courseID, userID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
	}

	count, err := repositories.GetPendingEditRequestCount(courseID, userID, isInstructor)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get pending count"})
	}
	return c.JSON(fiber.Map{"success": true, "count": count})
}

// PUT /api/scores/edit-requests/:id
func ReviewEditRequestHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	var existingRequest models.ScoreEditRequest
	_ = config.DB.First(&existingRequest, uint(id)).Error
	var input struct {
		Approved bool   `json:"approved"`
		Comment  string `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	reviewerID := c.Locals("user_id").(uint)
	if err2 := repositories.ReviewEditRequest(uint(id), input.Approved, reviewerID, input.Comment); err2 != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to review request"})
	}
	if existingRequest.ID != 0 {
		var score models.Score
		if err := config.DB.Select("id", "assignment_id").First(&score, existingRequest.ScoreID).Error; err == nil {
			var assignment models.Assignment
			if assignmentErr := config.DB.Select("id", "course_id", "name").First(&assignment, score.AssignmentID).Error; assignmentErr == nil {
				action := "reject_score_edit_request"
				if input.Approved {
					action = "approve_score_edit_request"
				}
				logCourseActivity(c, assignment.CourseID, reviewerID, action, "score", "assignment", assignment.ID, assignment.Name, fiber.Map{"request_id": existingRequest.ID, "score_id": existingRequest.ScoreID})
			}
		}
	}
	return c.JSON(fiber.Map{"success": true, "message": "Request reviewed"})
}
