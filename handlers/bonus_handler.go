package handlers

import (
	"fmt"
	"itii-assist/repositories"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func getBonusCourseID(c fiber.Ctx) string {
	if courseID := strings.TrimSpace(c.Params("courseId")); courseID != "" {
		return courseID
	}
	return strings.TrimSpace(c.Query("course_id"))
}

func getBonusStudentID(c fiber.Ctx) (uint, error) {
	raw := strings.TrimSpace(c.Params("studentId"))
	if raw == "" {
		raw = strings.TrimSpace(c.Query("student_id"))
	}
	if raw == "" {
		return 0, fmt.Errorf("student_id required")
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}

func buildBonusRecord(row repositories.BonusScoreWithNames) fiber.Map {
	return fiber.Map{
		"id":         row.ID,
		"course_id":  row.CourseID,
		"student_id": row.StudentID,
		"score":      row.Score,
		"reason":     row.Reason,
		"given_by":   row.GivenBy,
		"given_at":   row.GivenAt,
		"giver": fiber.Map{
			"id":        row.GivenBy,
			"full_name": row.GiverName,
		},
	}
}

// POST /api/bonus-scores
func GiveBonusScoreHandler(c fiber.Ctx) error {
	var input struct {
		CourseID  string  `json:"course_id"`
		StudentID uint    `json:"student_id"`
		Score     float64 `json:"score"`
		Reason    string  `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.CourseID == "" || input.StudentID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id and student_id required"})
	}
	score := input.Score
	if score == 0 {
		score = 1
	}
	if strings.TrimSpace(input.Reason) == "" {
		input.Reason = "ตอบคำถามในห้องเรียน"
	}

	givenBy := c.Locals("user_id").(uint)
	bonus, err := repositories.GiveBonusScore(input.CourseID, input.StudentID, score, input.Reason, givenBy)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to give bonus score"})
	}

	totalBonus, _ := repositories.GetBonusTotalForStudent(input.CourseID, input.StudentID)
	student, _ := repositories.FindStudentByID(input.StudentID)
	giver, _ := repositories.FindUserByID(givenBy)

	bonusScore := fiber.Map{
		"id":         bonus.ID,
		"course_id":  bonus.CourseID,
		"student_id": bonus.StudentID,
		"score":      bonus.Score,
		"reason":     bonus.Reason,
		"given_by":   bonus.GivenBy,
		"given_at":   bonus.GivenAt,
	}
	if student != nil {
		bonusScore["student"] = fiber.Map{
			"id":         student.ID,
			"student_id": student.StudentID,
			"full_name":  student.FullName,
		}
	}
	if giver != nil {
		bonusScore["giver"] = fiber.Map{
			"id":        giver.ID,
			"full_name": giver.FullName,
		}
	}

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": fmt.Sprintf("ให้คะแนนพิเศษ %s คะแนนสำเร็จ", strconv.FormatFloat(score, 'f', -1, 64)),
		"data": fiber.Map{
			"bonusScore": bonusScore,
			"totalBonus": totalBonus,
		},
	})
}

// GET /api/bonus-scores?course_id=
func GetBonusScoresHandler(c fiber.Ctx) error {
	courseID := getBonusCourseID(c)
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	bonuses, err := repositories.GetBonusScoresByCourse(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch bonus scores"})
	}

	type groupedBonus struct {
		Student    fiber.Map   `json:"student"`
		TotalScore float64     `json:"totalScore"`
		Records    []fiber.Map `json:"records"`
	}

	groupedMap := map[uint]*groupedBonus{}
	for _, row := range bonuses {
		entry, exists := groupedMap[row.StudentID]
		if !exists {
			entry = &groupedBonus{
				Student: fiber.Map{
					"id":         row.StudentID,
					"student_id": row.StudentNumber,
					"full_name":  row.StudentName,
				},
				Records: []fiber.Map{},
			}
			groupedMap[row.StudentID] = entry
		}
		entry.TotalScore += row.Score
		entry.Records = append(entry.Records, buildBonusRecord(row))
	}

	grouped := make([]groupedBonus, 0, len(groupedMap))
	for _, entry := range groupedMap {
		grouped = append(grouped, *entry)
	}
	sort.Slice(grouped, func(i, j int) bool {
		return grouped[i].TotalScore > grouped[j].TotalScore
	})

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"studentBonusScores": grouped,
			"totalRecords":       len(bonuses),
		},
	})
}

// GET /api/bonus-scores/summary?course_id=
func GetBonusScoreSummaryHandler(c fiber.Ctx) error {
	courseID := getBonusCourseID(c)
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}
	bonuses, err := repositories.GetBonusScoresByCourse(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch summary"})
	}

	totalGiven := 0.0
	studentTotals := map[uint]float64{}
	studentNames := map[uint]struct {
		StudentNumber string
		FullName      string
	}{}
	for _, row := range bonuses {
		totalGiven += row.Score
		studentTotals[row.StudentID] += row.Score
		studentNames[row.StudentID] = struct {
			StudentNumber string
			FullName      string
		}{StudentNumber: row.StudentNumber, FullName: row.StudentName}
	}

	type topStudent struct {
		StudentID string  `json:"student_id"`
		FullName  string  `json:"full_name"`
		Total     float64 `json:"total"`
	}

	topStudents := make([]topStudent, 0, len(studentTotals))
	for studentID, total := range studentTotals {
		student := studentNames[studentID]
		topStudents = append(topStudents, topStudent{
			StudentID: student.StudentNumber,
			FullName:  student.FullName,
			Total:     total,
		})
	}
	sort.Slice(topStudents, func(i, j int) bool {
		return topStudents[i].Total > topStudents[j].Total
	})
	if len(topStudents) > 5 {
		topStudents = topStudents[:5]
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"totalGiven":     totalGiven,
			"totalRecords":   len(bonuses),
			"uniqueStudents": len(studentTotals),
			"topStudents":    topStudents,
		},
	})
}

// GET /api/bonus-scores/student?course_id=&student_id=
func GetStudentBonusHistoryHandler(c fiber.Ctx) error {
	courseID := getBonusCourseID(c)
	studentID, err := getBonusStudentID(c)
	if courseID == "" || err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id and student_id required"})
	}
	history, err := repositories.GetStudentBonusHistory(courseID, studentID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch history"})
	}

	totalScore := 0.0
	records := make([]fiber.Map, 0, len(history))
	for _, row := range history {
		totalScore += row.Score
		records = append(records, buildBonusRecord(row))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"records":    records,
			"totalScore": totalScore,
		},
	})
}

// GET /api/bonus-scores/course/:courseId/students
func GetEnrolledStudentsForBonusHandler(c fiber.Ctx) error {
	courseID := getBonusCourseID(c)
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id required"})
	}

	students, err := repositories.GetEnrolledStudentsForBonus(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch enrolled students"})
	}
	if students == nil {
		students = []repositories.BonusEnrolledStudent{}
	}

	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"students": students}})
}

// DELETE /api/bonus-scores/:id
func DeleteBonusScoreHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	if err := repositories.DeleteBonusScore(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete bonus score"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Bonus score deleted"})
}
