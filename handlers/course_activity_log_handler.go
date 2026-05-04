package handlers

import (
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func parsePositiveIntQuery(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func fetchUsersByID(userIDs []uint) (map[uint]models.User, error) {
	result := map[uint]models.User{}
	if len(userIDs) == 0 {
		return result, nil
	}

	var users []models.User
	if err := config.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}

	for _, user := range users {
		result[user.ID] = user
	}
	return result, nil
}

func buildActorPayload(user models.User) fiber.Map {
	return fiber.Map{
		"id":        user.ID,
		"full_name": user.FullName,
		"email":     user.Email,
		"role":      user.Role,
		"avatar":    user.Avatar,
	}
}

func buildScoreDistribution(scores []models.Score, maxScore float64) []fiber.Map {
	if len(scores) == 0 {
		return []fiber.Map{}
	}
	if maxScore <= 0 {
		maxScore = 100
	}

	bucketSize := maxScore / 5
	if bucketSize <= 0 {
		bucketSize = 1
	}

	buckets := make([]int, 5)
	for _, score := range scores {
		index := int(score.Score / bucketSize)
		if index >= 5 {
			index = 4
		}
		if index < 0 {
			index = 0
		}
		buckets[index]++
	}

	result := make([]fiber.Map, 0, 5)
	for index, count := range buckets {
		low := int(float64(index) * bucketSize)
		high := int(float64(index+1) * bucketSize)
		result = append(result, fiber.Map{
			"range": fmt.Sprintf("%d-%d", low, high),
			"count": count,
		})
	}

	return result
}

// GET /api/courses/:courseId/activity-logs
func GetCourseActivityLogsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	page := parsePositiveIntQuery(c.Query("page"), 1)
	limit := parsePositiveIntQuery(c.Query("limit"), 30)
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	query := config.DB.Model(&models.CourseActivityLog{}).Where("course_id = ?", courseID)
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}
	if action := c.Query("action"); action != "" {
		query = query.Where("action = ?", action)
	}
	if actorID := c.Query("actorId"); actorID != "" {
		query = query.Where("actor_user_id = ?", actorID)
	}
	if startDate := c.Query("startDate"); startDate != "" {
		if parsed, err := time.Parse("2006-01-02", startDate); err == nil {
			query = query.Where("created_at >= ?", parsed)
		}
	}
	if endDate := c.Query("endDate"); endDate != "" {
		if parsed, err := time.Parse("2006-01-02", endDate); err == nil {
			query = query.Where("created_at <= ?", parsed.Add(24*time.Hour-time.Nanosecond))
		}
	}
	if search := c.Query("search"); search != "" {
		like := "%" + search + "%"
		query = query.Where("target_name ILIKE ? OR action ILIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity logs"})
	}

	var logs []models.CourseActivityLog
	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity logs"})
	}

	actorIDs := make([]uint, 0, len(logs))
	actorSet := map[uint]struct{}{}
	for _, log := range logs {
		if _, ok := actorSet[log.ActorUserID]; !ok {
			actorSet[log.ActorUserID] = struct{}{}
			actorIDs = append(actorIDs, log.ActorUserID)
		}
	}

	usersByID, err := fetchUsersByID(actorIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity logs"})
	}

	items := make([]fiber.Map, 0, len(logs))
	for _, log := range logs {
		item := fiber.Map{
			"id":            log.ID,
			"course_id":     log.CourseID,
			"actor_user_id": log.ActorUserID,
			"action":        log.Action,
			"category":      log.Category,
			"target_type":   log.TargetType,
			"target_id":     log.TargetID,
			"target_name":   log.TargetName,
			"detail":        log.Detail,
			"created_at":    log.CreatedAt,
		}
		if actor, ok := usersByID[log.ActorUserID]; ok {
			item["actor"] = buildActorPayload(actor)
		}
		items = append(items, item)
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"logs": items,
			"pagination": fiber.Map{
				"total":      total,
				"page":       page,
				"limit":      limit,
				"totalPages": totalPages,
			},
		},
	})
}

// GET /api/courses/:courseId/activity-logs/stats
func GetCourseActivityStatsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	days := parsePositiveIntQuery(c.Query("days"), 30)
	sinceDate := time.Now().AddDate(0, 0, -days)

	type categoryStatRow struct {
		Category string `gorm:"column:category"`
		Count    int64  `gorm:"column:count"`
	}
	type actionStatRow struct {
		Action string `gorm:"column:action"`
		Count  int64  `gorm:"column:count"`
	}
	type actorStatRow struct {
		ActorUserID uint  `gorm:"column:actor_user_id"`
		Count       int64 `gorm:"column:count"`
	}
	type timelineRow struct {
		Date  string `gorm:"column:date"`
		Count int64  `gorm:"column:count"`
	}

	var categoryStats []categoryStatRow
	var actionStats []actionStatRow
	var actorStats []actorStatRow
	var timeline []timelineRow
	var totalLogs int64

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Select("category, COUNT(id) AS count").
		Where("course_id = ? AND created_at >= ?", courseID, sinceDate).
		Group("category").
		Scan(&categoryStats).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Select("action, COUNT(id) AS count").
		Where("course_id = ? AND created_at >= ?", courseID, sinceDate).
		Group("action").
		Order("count DESC").
		Limit(10).
		Scan(&actionStats).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Select("actor_user_id, COUNT(id) AS count").
		Where("course_id = ? AND created_at >= ?", courseID, sinceDate).
		Group("actor_user_id").
		Order("count DESC").
		Scan(&actorStats).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Select("DATE(created_at) AS date, COUNT(id) AS count").
		Where("course_id = ? AND created_at >= ?", courseID, sinceDate).
		Group("DATE(created_at)").
		Order("DATE(created_at) ASC").
		Scan(&timeline).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).Where("course_id = ?", courseID).Count(&totalLogs).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	actorIDs := make([]uint, 0, len(actorStats))
	for _, stat := range actorStats {
		actorIDs = append(actorIDs, stat.ActorUserID)
	}

	usersByID, err := fetchUsersByID(actorIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity stats"})
	}

	actorItems := make([]fiber.Map, 0, len(actorStats))
	for _, stat := range actorStats {
		actor := fiber.Map{
			"userId": stat.ActorUserID,
			"count":  stat.Count,
		}
		if user, ok := usersByID[stat.ActorUserID]; ok {
			actor["fullName"] = user.FullName
			actor["role"] = user.Role
			actor["avatar"] = user.Avatar
		} else {
			actor["fullName"] = "Unknown"
			actor["role"] = "unknown"
			actor["avatar"] = nil
		}
		actorItems = append(actorItems, actor)
	}

	categoryItems := make([]fiber.Map, 0, len(categoryStats))
	for _, stat := range categoryStats {
		categoryItems = append(categoryItems, fiber.Map{"category": stat.Category, "count": stat.Count})
	}

	actionItems := make([]fiber.Map, 0, len(actionStats))
	for _, stat := range actionStats {
		actionItems = append(actionItems, fiber.Map{"action": stat.Action, "count": stat.Count})
	}

	timelineItems := make([]fiber.Map, 0, len(timeline))
	for _, row := range timeline {
		timelineItems = append(timelineItems, fiber.Map{"date": row.Date, "count": row.Count})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"total":         totalLogs,
			"period":        days,
			"categoryStats": categoryItems,
			"actionStats":   actionItems,
			"actorStats":    actorItems,
			"timeline":      timelineItems,
		},
	})
}

// GET /api/courses/:courseId/activity-logs/filters
func GetCourseActivityFiltersHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")

	type categoryRow struct {
		Category string `gorm:"column:category"`
	}
	type actionRow struct {
		Action   string `gorm:"column:action"`
		Category string `gorm:"column:category"`
	}
	type actorRow struct {
		ActorUserID uint `gorm:"column:actor_user_id"`
	}

	var categories []categoryRow
	var actions []actionRow
	var actors []actorRow

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Distinct("category").
		Where("course_id = ?", courseID).
		Order("category ASC").
		Scan(&categories).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity filters"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Select("DISTINCT action, category").
		Where("course_id = ?", courseID).
		Order("category ASC, action ASC").
		Scan(&actions).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity filters"})
	}

	if err := config.DB.Model(&models.CourseActivityLog{}).
		Distinct("actor_user_id").
		Where("course_id = ?", courseID).
		Scan(&actors).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity filters"})
	}

	actorIDs := make([]uint, 0, len(actors))
	for _, actor := range actors {
		actorIDs = append(actorIDs, actor.ActorUserID)
	}

	usersByID, err := fetchUsersByID(actorIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch activity filters"})
	}

	categoryItems := make([]string, 0, len(categories))
	for _, category := range categories {
		if category.Category != "" {
			categoryItems = append(categoryItems, category.Category)
		}
	}

	actionItems := make([]fiber.Map, 0, len(actions))
	for _, action := range actions {
		actionItems = append(actionItems, fiber.Map{"action": action.Action, "category": action.Category})
	}

	actorItems := make([]fiber.Map, 0, len(actorIDs))
	for _, actorID := range actorIDs {
		item := fiber.Map{"id": actorID, "fullName": "Unknown", "role": "unknown", "avatar": nil}
		if user, ok := usersByID[actorID]; ok {
			item["fullName"] = user.FullName
			item["role"] = user.Role
			item["avatar"] = user.Avatar
		}
		actorItems = append(actorItems, item)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"categories": categoryItems,
			"actions":    actionItems,
			"actors":     actorItems,
		},
	})
}

// GET /api/courses/:courseId/activity-logs/ta-stats
func GetCourseActivityTAStatsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")

	var course models.Course
	if err := config.DB.First(&course, "id = ?", courseID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบรายวิชา"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
	}

	var tas []models.CourseTA
	if err := config.DB.Where("course_id = ?", courseID).Find(&tas).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
	}

	var assignments []models.Assignment
	if err := config.DB.Where("course_id = ?", courseID).Order("created_at ASC").Find(&assignments).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
	}

	taUserIDs := make([]uint, 0, len(tas))
	for _, ta := range tas {
		taUserIDs = append(taUserIDs, ta.UserID)
	}

	usersByID, err := fetchUsersByID(taUserIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
	}

	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	var scores []models.Score
	if len(assignmentIDs) > 0 && len(taUserIDs) > 0 {
		if err := config.DB.Where("assignment_id IN ? AND graded_by IN ? AND status = ?", assignmentIDs, taUserIDs, "graded").Order("graded_at DESC").Find(&scores).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
		}
	}

	var queueSessions []models.QueueSession
	if err := config.DB.Where("course_id = ?", courseID).Find(&queueSessions).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
	}

	sessionIDs := make([]string, 0, len(queueSessions))
	for _, session := range queueSessions {
		sessionIDs = append(sessionIDs, session.ID)
	}

	type queueAggRow struct {
		AssignedWorkerID uint     `gorm:"column:assigned_worker_id"`
		TotalCompleted   int64    `gorm:"column:total_completed"`
		AvgScore         *float64 `gorm:"column:avg_score"`
		MinScore         *float64 `gorm:"column:min_score"`
		MaxScore         *float64 `gorm:"column:max_score"`
	}
	var queueAggs []queueAggRow
	if len(sessionIDs) > 0 && len(taUserIDs) > 0 {
		if err := config.DB.Model(&models.QueueBooking{}).
			Select("assigned_worker_id, COUNT(*) AS total_completed, AVG(score) AS avg_score, MIN(score) AS min_score, MAX(score) AS max_score").
			Where("queue_session_id IN ? AND assigned_worker_id IN ? AND status = ?", sessionIDs, taUserIDs, "completed").
			Group("assigned_worker_id").
			Scan(&queueAggs).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA stats"})
		}
	}

	scoresByTA := map[uint][]models.Score{}
	scoresByAssignment := map[uint][]models.Score{}
	scoresByTAAssignment := map[string][]models.Score{}
	for _, score := range scores {
		if score.GradedBy == nil {
			continue
		}
		taID := *score.GradedBy
		scoresByTA[taID] = append(scoresByTA[taID], score)
		scoresByAssignment[score.AssignmentID] = append(scoresByAssignment[score.AssignmentID], score)
		key := fmt.Sprintf("%d:%d", taID, score.AssignmentID)
		scoresByTAAssignment[key] = append(scoresByTAAssignment[key], score)
	}

	queueAggByUser := map[uint]queueAggRow{}
	totalQueueCompleted := int64(0)
	for _, row := range queueAggs {
		queueAggByUser[row.AssignedWorkerID] = row
		totalQueueCompleted += row.TotalCompleted
	}

	overallAssignmentStats := make([]fiber.Map, 0, len(assignments))
	overallAverageByAssignment := map[uint]float64{}
	for _, assignment := range assignments {
		assignmentScores := scoresByAssignment[assignment.ID]
		avg := 0.0
		if len(assignmentScores) > 0 {
			totalScore := 0.0
			for _, score := range assignmentScores {
				totalScore += score.Score
			}
			avg = totalScore / float64(len(assignmentScores))
			overallAverageByAssignment[assignment.ID] = avg
		}
		overallAssignmentStats = append(overallAssignmentStats, fiber.Map{
			"assignmentId":   assignment.ID,
			"assignmentName": assignment.Name,
			"maxScore":       assignment.MaxScore,
			"totalGraded":    len(assignmentScores),
			"avgScore": func() interface{} {
				if len(assignmentScores) == 0 {
					return nil
				}
				return avg
			}(),
		})
	}

	expectedShare := 0.0
	if len(tas) > 0 {
		expectedShare = float64(len(scores)) / float64(len(tas))
	}
	avgQueuePerTA := 0.0
	if len(tas) > 0 {
		avgQueuePerTA = float64(totalQueueCompleted) / float64(len(tas))
	}

	taItems := make([]fiber.Map, 0, len(tas))
	for _, ta := range tas {
		user := usersByID[ta.UserID]
		taScores := scoresByTA[ta.UserID]
		perAssignment := make([]fiber.Map, 0)
		consistencyTotal := 0.0
		consistencyCount := 0
		anomalies := make([]fiber.Map, 0)

		for _, assignment := range assignments {
			key := fmt.Sprintf("%d:%d", ta.UserID, assignment.ID)
			assignmentScores := scoresByTAAssignment[key]
			if len(assignmentScores) == 0 {
				continue
			}

			mainScores := make([]models.Score, 0)
			subItemCount := 0
			totalScore := 0.0
			minScore := 0.0
			maxScore := 0.0
			for index, score := range assignmentScores {
				if score.SubItemID == nil {
					mainScores = append(mainScores, score)
				}
				if score.SubItemID != nil {
					subItemCount++
				}
				totalScore += score.Score
				if index == 0 || score.Score < minScore {
					minScore = score.Score
				}
				if index == 0 || score.Score > maxScore {
					maxScore = score.Score
				}
			}

			avgScore := totalScore / float64(len(assignmentScores))
			if overallAvg, ok := overallAverageByAssignment[assignment.ID]; ok && assignment.MaxScore > 0 {
				deviation := 1 - (absFloat(avgScore-overallAvg) / assignment.MaxScore)
				if deviation < 0 {
					deviation = 0
				}
				consistencyTotal += deviation
				consistencyCount++
				if absFloat(avgScore-overallAvg) > assignment.MaxScore*0.3 && len(assignmentScores) >= 3 {
					anomalies = append(anomalies, fiber.Map{
						"kind":           "score_deviation",
						"severity":       "warning",
						"message":        fmt.Sprintf("ค่าเฉลี่ยงาน \"%s\" ต่างจากค่าเฉลี่ยรวมค่อนข้างมาก", assignment.Name),
						"assignmentId":   assignment.ID,
						"assignmentName": assignment.Name,
					})
				}
			}

			perAssignment = append(perAssignment, fiber.Map{
				"assignmentId":       assignment.ID,
				"assignmentName":     assignment.Name,
				"maxScore":           assignment.MaxScore,
				"totalGraded":        len(assignmentScores),
				"mainScores":         len(mainScores),
				"subItemScoresCount": subItemCount,
				"avgScore":           avgScore,
				"minScore":           minScore,
				"maxScore_given":     maxScore,
				"scoreDistribution":  buildScoreDistribution(mainScoresOrAll(mainScores, assignmentScores), assignment.MaxScore),
			})
		}

		assignmentsGraded := len(perAssignment)
		kpiWorkload := 50.0
		if expectedShare > 0 {
			kpiWorkload = minFloat((float64(len(taScores))/expectedShare)*100, 100)
		}
		kpiCoverage := 0.0
		if len(assignments) > 0 {
			kpiCoverage = (float64(assignmentsGraded) / float64(len(assignments))) * 100
		}
		kpiConsistency := 50.0
		if consistencyCount > 0 {
			kpiConsistency = (consistencyTotal / float64(consistencyCount)) * 100
		}
		kpiQueue := 50.0
		if queueStat, ok := queueAggByUser[ta.UserID]; ok && avgQueuePerTA > 0 {
			kpiQueue = minFloat((float64(queueStat.TotalCompleted)/avgQueuePerTA)*100, 100)
		}
		kpiSpread := 50.0
		kpiAnomaly := maxFloat(0, 100-float64(len(anomalies))*20)

		if len(assignments) > 0 && float64(assignmentsGraded)/float64(len(assignments)) < 0.3 {
			anomalies = append(anomalies, fiber.Map{
				"kind":     "low_coverage",
				"severity": "warning",
				"message":  fmt.Sprintf("ตรวจงานเพียง %d จาก %d งาน", assignmentsGraded, len(assignments)),
			})
		}
		if expectedShare > 0 && float64(len(taScores))/expectedShare < 0.3 {
			anomalies = append(anomalies, fiber.Map{
				"kind":     "low_volume",
				"severity": "warning",
				"message":  fmt.Sprintf("ตรวจงานเพียง %d รายการ เทียบกับค่าเฉลี่ยที่คาดหวัง %.0f รายการ", len(taScores), expectedShare),
			})
		}

		performanceScore := roundFloat(
			kpiWorkload*0.30 +
				kpiCoverage*0.15 +
				kpiConsistency*0.25 +
				kpiSpread*0.10 +
				kpiQueue*0.15 +
				kpiAnomaly*0.05,
		)

		confidenceLevel := "low"
		switch {
		case len(taScores) >= 20:
			confidenceLevel = "high"
		case len(taScores) >= 10:
			confidenceLevel = "medium"
		}

		item := fiber.Map{
			"userId":            ta.UserID,
			"fullName":          user.FullName,
			"email":             user.Email,
			"avatar":            user.Avatar,
			"totalScoresGraded": len(taScores),
			"assignmentsGraded": assignmentsGraded,
			"perAssignment":     perAssignment,
			"performanceScore":  performanceScore,
			"confidenceLevel":   confidenceLevel,
			"confidence": fiber.Map{
				"level":          confidenceLevel,
				"sampleSize":     len(taScores),
				"minRecommended": 20,
			},
			"kpiBreakdown": fiber.Map{
				"workload":    fiber.Map{"score": roundFloat(kpiWorkload), "weight": 0.30, "label": "ปริมาณงาน", "description": "สัดส่วนงานที่ตรวจเทียบกับค่าเฉลี่ยต่อ TA"},
				"coverage":    fiber.Map{"score": roundFloat(kpiCoverage), "weight": 0.15, "label": "ความครอบคลุม", "description": "จำนวนงานที่ตรวจเทียบกับงานทั้งหมด"},
				"consistency": fiber.Map{"score": roundFloat(kpiConsistency), "weight": 0.25, "label": "ความสม่ำเสมอ", "description": "ค่าเฉลี่ยคะแนนใกล้เคียงภาพรวมแค่ไหน"},
				"spread":      fiber.Map{"score": roundFloat(kpiSpread), "weight": 0.10, "label": "การกระจายคะแนน", "description": "ใช้ค่าเป็นกลางเมื่อข้อมูลยังไม่พอ"},
				"queue":       fiber.Map{"score": roundFloat(kpiQueue), "weight": 0.15, "label": "คิวตรวจงาน", "description": "จำนวนคิวที่ทำสำเร็จเทียบกับค่าเฉลี่ยต่อ TA"},
				"anomaly":     fiber.Map{"score": roundFloat(kpiAnomaly), "weight": 0.05, "label": "ตรวจพบสิ่งผิดปกติ", "description": "ลดลงเมื่อพบรูปแบบที่น่าสงสัย"},
			},
			"anomalies": anomalies,
		}

		if queueStat, ok := queueAggByUser[ta.UserID]; ok {
			item["queueStats"] = fiber.Map{
				"totalCompleted": queueStat.TotalCompleted,
				"avgScore":       queueStat.AvgScore,
				"minScore":       queueStat.MinScore,
				"maxScore":       queueStat.MaxScore,
			}
		} else {
			item["queueStats"] = nil
		}

		taItems = append(taItems, item)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"taStats":     taItems,
			"assignments": overallAssignmentStats,
			"summary": fiber.Map{
				"totalTAs":          len(tas),
				"totalAssignments":  len(assignments),
				"totalScoresGraded": len(scores),
			},
		},
	})
}

// GET /api/courses/:courseId/activity-logs/ta-stats/:userId
func GetCourseActivityTADetailHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	userID, err := strconv.ParseUint(c.Params("userId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid user id"})
	}

	page := parsePositiveIntQuery(c.Query("page"), 1)
	limit := parsePositiveIntQuery(c.Query("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	offset := (page - 1) * limit

	var user models.User
	if err := config.DB.First(&user, uint(userID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้งาน"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
	}

	assignmentQuery := config.DB.Model(&models.Assignment{}).Where("course_id = ?", courseID)
	if assignmentID := c.Query("assignmentId"); assignmentID != "" {
		assignmentQuery = assignmentQuery.Where("id = ?", assignmentID)
	}

	var assignments []models.Assignment
	if err := assignmentQuery.Find(&assignments).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
	}

	assignmentIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs = append(assignmentIDs, assignment.ID)
	}

	assignmentMap := map[uint]models.Assignment{}
	for _, assignment := range assignments {
		assignmentMap[assignment.ID] = assignment
	}

	detailQuery := config.DB.Model(&models.Score{}).
		Where("graded_by = ? AND status = ?", uint(userID), "graded")
	if len(assignmentIDs) > 0 {
		detailQuery = detailQuery.Where("assignment_id IN ?", assignmentIDs)
	} else {
		detailQuery = detailQuery.Where("1 = 0")
	}

	var total int64
	if err := detailQuery.Count(&total).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
	}

	var detailScores []models.Score
	if err := detailQuery.Order("graded_at DESC").Limit(limit).Offset(offset).Find(&detailScores).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
	}

	subItemIDs := make([]uint, 0)
	studentIDs := make([]uint, 0)
	subItemSet := map[uint]struct{}{}
	studentSet := map[uint]struct{}{}
	for _, score := range detailScores {
		if score.SubItemID != nil {
			if _, ok := subItemSet[*score.SubItemID]; !ok {
				subItemSet[*score.SubItemID] = struct{}{}
				subItemIDs = append(subItemIDs, *score.SubItemID)
			}
		}
		if score.StudentID != nil {
			if _, ok := studentSet[*score.StudentID]; !ok {
				studentSet[*score.StudentID] = struct{}{}
				studentIDs = append(studentIDs, *score.StudentID)
			}
		}
	}

	subItemMap := map[uint]models.AssignmentSubItem{}
	if len(subItemIDs) > 0 {
		var subItems []models.AssignmentSubItem
		if err := config.DB.Where("id IN ?", subItemIDs).Find(&subItems).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
		}
		for _, subItem := range subItems {
			subItemMap[subItem.ID] = subItem
		}
	}

	studentMap := map[uint]models.Student{}
	if len(studentIDs) > 0 {
		var students []models.Student
		if err := config.DB.Where("id IN ?", studentIDs).Find(&students).Error; err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
		}
		for _, student := range students {
			studentMap[student.ID] = student
		}
	}

	items := make([]fiber.Map, 0, len(detailScores))
	for _, score := range detailScores {
		item := fiber.Map{
			"id":            score.ID,
			"assignment_id": score.AssignmentID,
			"student_id":    score.StudentID,
			"sub_item_id":   score.SubItemID,
			"score":         score.Score,
			"comment":       score.Comment,
			"graded_by":     score.GradedBy,
			"graded_at":     score.GradedAt,
			"status":        score.Status,
			"created_at":    score.CreatedAt,
			"updated_at":    score.UpdatedAt,
		}
		if assignment, ok := assignmentMap[score.AssignmentID]; ok {
			item["assignment"] = fiber.Map{
				"id":        assignment.ID,
				"name":      assignment.Name,
				"max_score": assignment.MaxScore,
			}
		}
		if score.SubItemID != nil {
			if subItem, ok := subItemMap[*score.SubItemID]; ok {
				item["subItem"] = fiber.Map{
					"id":        subItem.ID,
					"name":      subItem.Name,
					"max_score": subItem.MaxScore,
				}
			}
		}
		if score.StudentID != nil {
			if student, ok := studentMap[*score.StudentID]; ok {
				item["student"] = fiber.Map{
					"id":         student.ID,
					"student_id": student.StudentID,
					"full_name":  student.FullName,
				}
			}
		}
		items = append(items, item)
	}

	type timelineRow struct {
		Date     string   `gorm:"column:date"`
		Count    int64    `gorm:"column:count"`
		AvgScore *float64 `gorm:"column:avg_score"`
	}
	var timeline []timelineRow
	timelineQuery := config.DB.Model(&models.Score{}).
		Select("DATE(graded_at) AS date, COUNT(id) AS count, AVG(score) AS avg_score").
		Where("graded_by = ? AND status = ?", uint(userID), "graded")
	if len(assignmentIDs) > 0 {
		timelineQuery = timelineQuery.Where("assignment_id IN ?", assignmentIDs)
	} else {
		timelineQuery = timelineQuery.Where("1 = 0")
	}
	if err := timelineQuery.Group("DATE(graded_at)").Order("DATE(graded_at) ASC").Scan(&timeline).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch TA detail"})
	}

	timelineItems := make([]fiber.Map, 0, len(timeline))
	for _, row := range timeline {
		timelineItems = append(timelineItems, fiber.Map{
			"date":      row.Date,
			"count":     row.Count,
			"avg_score": row.AvgScore,
		})
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"user": fiber.Map{
				"id":        user.ID,
				"full_name": user.FullName,
				"email":     user.Email,
			},
			"scores":   items,
			"timeline": timelineItems,
			"pagination": fiber.Map{
				"total":      total,
				"page":       page,
				"limit":      limit,
				"totalPages": totalPages,
			},
		},
	})
}

func mainScoresOrAll(mainScores []models.Score, allScores []models.Score) []models.Score {
	if len(mainScores) > 0 {
		return mainScores
	}
	return allScores
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func roundFloat(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}
