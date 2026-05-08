package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type scoreEditRequestListRow struct {
	ID                 uint           `gorm:"column:id"`
	Status             string         `gorm:"column:status"`
	OldScore           *float64       `gorm:"column:old_score"`
	NewScore           float64        `gorm:"column:new_score"`
	Reason             string         `gorm:"column:reason"`
	Images             datatypes.JSON `gorm:"column:images"`
	ReviewComment      string         `gorm:"column:review_comment"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	ReviewedAt         *time.Time     `gorm:"column:reviewed_at"`
	ScoreID            uint           `gorm:"column:score_id"`
	CurrentScore       float64        `gorm:"column:current_score"`
	AssignmentID       uint           `gorm:"column:assignment_id"`
	AssignmentName     string         `gorm:"column:assignment_name"`
	AssignmentMaxScore float64        `gorm:"column:assignment_max_score"`
	SubItemID          *uint          `gorm:"column:sub_item_id"`
	SubItemName        *string        `gorm:"column:sub_item_name"`
	SubItemMaxScore    *float64       `gorm:"column:sub_item_max_score"`
	StudentRowID       *uint          `gorm:"column:student_row_id"`
	StudentCode        *string        `gorm:"column:student_code"`
	StudentName        *string        `gorm:"column:student_name"`
	RequesterID        uint           `gorm:"column:requester_id"`
	RequesterUsername  string         `gorm:"column:requester_username"`
	RequesterFullName  string         `gorm:"column:requester_full_name"`
	ReviewerID         *uint          `gorm:"column:reviewer_id"`
	ReviewerUsername   *string        `gorm:"column:reviewer_username"`
	ReviewerFullName   *string        `gorm:"column:reviewer_full_name"`
}

type scoreEditRequestContextRow struct {
	ScoreID            uint     `gorm:"column:score_id"`
	AssignmentID       uint     `gorm:"column:assignment_id"`
	CourseID           string   `gorm:"column:course_id"`
	AssignmentName     string   `gorm:"column:assignment_name"`
	StudentID          *uint    `gorm:"column:student_id"`
	CurrentScore       float64  `gorm:"column:current_score"`
	AssignmentMaxScore float64  `gorm:"column:assignment_max_score"`
	SubItemID          *uint    `gorm:"column:sub_item_id"`
	SubItemName        *string  `gorm:"column:sub_item_name"`
	SubItemMaxScore    *float64 `gorm:"column:sub_item_max_score"`
}

func scoreEditRequestImagesValue(images datatypes.JSON) interface{} {
	if len(images) == 0 || string(images) == "null" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(images, &values); err != nil {
		return []string{}
	}
	return values
}

func loadScoreEditRequestContext(scoreID uint) (*scoreEditRequestContextRow, error) {
	var row scoreEditRequestContextRow
	err := config.DB.Raw(`
		SELECT s.id AS score_id,
		       s.assignment_id,
		       a.course_id,
		       a.name AS assignment_name,
		       s.student_id,
		       s.score AS current_score,
		       a.max_score AS assignment_max_score,
		       s.sub_item_id,
		       asi.name AS sub_item_name,
		       asi.max_score AS sub_item_max_score
		FROM scores s
		JOIN assignments a ON a.id = s.assignment_id
		LEFT JOIN assignment_sub_items asi ON asi.id = s.sub_item_id
		WHERE s.id = ?
		LIMIT 1
	`, scoreID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ScoreID == 0 {
		return nil, fmt.Errorf("score not found")
	}
	return &row, nil
}

func loadScoreEditRequestContexts(scoreIDs []uint) ([]scoreEditRequestContextRow, error) {
	if len(scoreIDs) == 0 {
		return []scoreEditRequestContextRow{}, nil
	}
	var rows []scoreEditRequestContextRow
	err := config.DB.Raw(`
		SELECT s.id AS score_id,
		       s.assignment_id,
		       a.course_id,
		       a.name AS assignment_name,
		       s.student_id,
		       s.score AS current_score,
		       a.max_score AS assignment_max_score,
		       s.sub_item_id,
		       asi.name AS sub_item_name,
		       asi.max_score AS sub_item_max_score
		FROM scores s
		JOIN assignments a ON a.id = s.assignment_id
		LEFT JOIN assignment_sub_items asi ON asi.id = s.sub_item_id
		WHERE s.id IN ?
	`, scoreIDs).Scan(&rows).Error
	return rows, err
}

func findPendingScoreEditRequestByScoreID(scoreID uint) (*models.ScoreEditRequest, error) {
	var request models.ScoreEditRequest
	err := config.DB.Table("score_edit_requests AS ser").
		Select("ser.*").
		Where("ser.status = ? AND ser.score_id = ?", "pending", scoreID).
		Order("ser.created_at DESC").
		First(&request).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &request, nil
}

func saveScoreEditRequestImages(c fiber.Ctx) ([]string, error) {
	contentType := strings.ToLower(c.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, nil
	}

	form, err := c.MultipartForm()
	if err != nil || form == nil {
		return nil, nil
	}
	files := form.File["images"]
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > 3 {
		return nil, fmt.Errorf("too many files. maximum 3 images allowed")
	}

	dir := filepath.Join("uploads", "score-edit-requests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(files))
	for index, file := range files {
		if file.Size > 10*1024*1024 {
			return nil, fmt.Errorf("file size too large. maximum 10MB per file")
		}
		contentType := strings.ToLower(file.Header.Get("Content-Type"))
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("only image files are allowed")
		}
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext == "" {
			ext = ".jpg"
		}
		filename := fmt.Sprintf("edit-request-%d-%d%s", time.Now().UnixNano(), index, ext)
		destination := filepath.Join(dir, filename)
		if err := c.SaveFile(file, destination); err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(filepath.Join("uploads", "score-edit-requests", filename)))
	}

	return paths, nil
}

func imagesJSON(paths []string) datatypes.JSON {
	if len(paths) == 0 {
		return nil
	}
	payload, err := json.Marshal(paths)
	if err != nil {
		return nil
	}
	return datatypes.JSON(payload)
}

func parseSingleScoreEditRequestInput(c fiber.Ctx) (uint, float64, string, datatypes.JSON, []string, error) {
	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		scoreID, err := strconv.ParseUint(strings.TrimSpace(c.FormValue("score_id")), 10, 64)
		if err != nil {
			return 0, 0, "", nil, nil, fmt.Errorf("score_id and new_score are required")
		}
		newScore, err := strconv.ParseFloat(strings.TrimSpace(c.FormValue("new_score")), 64)
		if err != nil {
			return 0, 0, "", nil, nil, fmt.Errorf("score_id and new_score are required")
		}
		paths, err := saveScoreEditRequestImages(c)
		if err != nil {
			return 0, 0, "", nil, nil, err
		}
		return uint(scoreID), newScore, c.FormValue("reason"), imagesJSON(paths), paths, nil
	}

	var input struct {
		ScoreID  uint    `json:"score_id"`
		NewScore float64 `json:"new_score"`
		Reason   string  `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.ScoreID == 0 {
		return 0, 0, "", nil, nil, fmt.Errorf("score_id and new_score are required")
	}
	return input.ScoreID, input.NewScore, input.Reason, nil, nil, nil
}

func parseBatchScoreEditRequestInput(c fiber.Ctx) ([]uint, float64, string, datatypes.JSON, []string, error) {
	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		rawIDs := strings.TrimSpace(c.FormValue("score_ids"))
		if rawIDs == "" {
			return nil, 0, "", nil, nil, fmt.Errorf("score_ids array is required and must not be empty")
		}
		var scoreIDs []uint
		if err := json.Unmarshal([]byte(rawIDs), &scoreIDs); err != nil || len(scoreIDs) == 0 {
			return nil, 0, "", nil, nil, fmt.Errorf("invalid score_ids format")
		}
		newScore, err := strconv.ParseFloat(strings.TrimSpace(c.FormValue("new_score")), 64)
		if err != nil {
			return nil, 0, "", nil, nil, fmt.Errorf("new_score is required and must be a valid number")
		}
		paths, err := saveScoreEditRequestImages(c)
		if err != nil {
			return nil, 0, "", nil, nil, err
		}
		return uniqueScoreUintValues(scoreIDs), newScore, c.FormValue("reason"), imagesJSON(paths), paths, nil
	}

	var input struct {
		ScoreIDs []uint  `json:"score_ids"`
		NewScore float64 `json:"new_score"`
		Reason   string  `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.ScoreIDs) == 0 {
		return nil, 0, "", nil, nil, fmt.Errorf("score_ids array is required and must not be empty")
	}
	return uniqueScoreUintValues(input.ScoreIDs), input.NewScore, input.Reason, nil, nil, nil
}

type detailedScoreEditItem struct {
	ScoreID  uint    `json:"score_id"`
	NewScore float64 `json:"new_score"`
}

func parseBatchDetailedScoreEditRequestInput(c fiber.Ctx) ([]detailedScoreEditItem, string, datatypes.JSON, []string, error) {
	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		rawEdits := strings.TrimSpace(c.FormValue("edits"))
		if rawEdits == "" {
			return nil, "", nil, nil, fmt.Errorf("edits array is required and must not be empty")
		}

		var edits []detailedScoreEditItem
		if err := json.Unmarshal([]byte(rawEdits), &edits); err != nil || len(edits) == 0 {
			return nil, "", nil, nil, fmt.Errorf("invalid edits format")
		}

		paths, err := saveScoreEditRequestImages(c)
		if err != nil {
			return nil, "", nil, nil, err
		}

		return edits, c.FormValue("reason"), imagesJSON(paths), paths, nil
	}

	var input struct {
		Edits  []detailedScoreEditItem `json:"edits"`
		Reason string                  `json:"reason"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.Edits) == 0 {
		return nil, "", nil, nil, fmt.Errorf("edits array is required and must not be empty")
	}

	return input.Edits, input.Reason, nil, nil, nil
}

func loadScoreEditRequests(courseID string, status string, userID uint, isInstructor bool) ([]scoreEditRequestListRow, error) {
	query := `
		SELECT ser.id,
		       ser.status,
		       ser.old_score,
		       ser.new_score,
		       ser.reason,
		       ser.images,
		       ser.review_comment,
		       ser.created_at,
		       ser.reviewed_at,
		       ser.score_id,
		       s.score AS current_score,
		       a.id AS assignment_id,
		       a.name AS assignment_name,
		       a.max_score AS assignment_max_score,
		       asi.id AS sub_item_id,
		       asi.name AS sub_item_name,
		       asi.max_score AS sub_item_max_score,
		       st.id AS student_row_id,
		       st.student_id AS student_code,
		       st.full_name AS student_name,
		       req.id AS requester_id,
		       req.username AS requester_username,
		       req.full_name AS requester_full_name,
		       rev.id AS reviewer_id,
		       rev.username AS reviewer_username,
		       rev.full_name AS reviewer_full_name
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		LEFT JOIN assignment_sub_items asi ON asi.id = s.sub_item_id
		LEFT JOIN students st ON st.id = s.student_id
		LEFT JOIN users req ON req.id = ser.requested_by
		LEFT JOIN users rev ON rev.id = ser.reviewed_by
		WHERE a.course_id = ?
	`
	args := []interface{}{courseID}
	if status != "" {
		query += ` AND ser.status = ?`
		args = append(args, status)
	}
	if !isInstructor {
		query += ` AND ser.requested_by = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY ser.created_at DESC`

	var rows []scoreEditRequestListRow
	err := config.DB.Raw(query, args...).Scan(&rows).Error
	return rows, err
}

func loadScoreEditRequestCounts(courseID string, userID uint, isInstructor bool) (fiber.Map, error) {
	type countRow struct {
		Status string `gorm:"column:status"`
		Count  int64  `gorm:"column:count"`
	}
	query := `
		SELECT ser.status, COUNT(ser.id) AS count
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE a.course_id = ?
	`
	args := []interface{}{courseID}
	if !isInstructor {
		query += ` AND ser.requested_by = ?`
		args = append(args, userID)
	}
	query += ` GROUP BY ser.status`

	var rows []countRow
	if err := config.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := fiber.Map{"pending": int64(0), "approved": int64(0), "rejected": int64(0)}
	for _, row := range rows {
		counts[row.Status] = row.Count
	}
	return counts, nil
}

func reviewScoreEditRequests(requestIDs []uint, approved bool, reviewerID uint, comment string) (int, string, string, error) {
	if len(requestIDs) == 0 {
		return 0, "", "", fmt.Errorf("request_ids array is required")
	}

	type requestContextRow struct {
		RequestID      uint    `gorm:"column:request_id"`
		ScoreID        uint    `gorm:"column:score_id"`
		NewScore       float64 `gorm:"column:new_score"`
		Status         string  `gorm:"column:status"`
		AssignmentID   uint    `gorm:"column:assignment_id"`
		CourseID       string  `gorm:"column:course_id"`
		AssignmentName string  `gorm:"column:assignment_name"`
	}

	var contexts []requestContextRow
	if err := config.DB.Raw(`
		SELECT ser.id AS request_id,
		       ser.score_id,
		       ser.new_score,
		       ser.status,
		       a.id AS assignment_id,
		       a.course_id,
		       a.name AS assignment_name
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE ser.id IN ? AND ser.status = 'pending'
	`, requestIDs).Scan(&contexts).Error; err != nil {
		return 0, "", "", err
	}
	if len(contexts) == 0 {
		return 0, "", "", fmt.Errorf("no pending edit requests found")
	}

	courseID := contexts[0].CourseID
	assignmentName := contexts[0].AssignmentName
	for _, context := range contexts[1:] {
		if context.CourseID != courseID {
			return 0, "", "", fmt.Errorf("all requests must be from the same course")
		}
	}

	now := time.Now()
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		for _, context := range contexts {
			status := "rejected"
			if approved {
				status = "approved"
				if err := tx.Model(&models.Score{}).Where("id = ?", context.ScoreID).Updates(map[string]interface{}{"score": context.NewScore, "graded_at": now}).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&models.ScoreEditRequest{}).Where("id = ?", context.RequestID).Updates(map[string]interface{}{"status": status, "reviewed_by": reviewerID, "reviewed_at": now, "review_comment": comment}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, "", "", err
	}

	return len(contexts), courseID, assignmentName, nil
}

// GET /api/score-edit-requests?course_id=&status=
func GetScoreEditRequestsCompatHandler(c fiber.Ctx) error {
	courseID := c.Query("course_id")
	if courseID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "course_id is required"})
	}

	userID := c.Locals("user_id").(uint)
	role := c.Locals("user_role").(string)
	canReviewAll, err := repositories.CanReviewAllCourseScoreRequests(courseID, userID, role)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to resolve permissions"})
	}

	rows, err := loadScoreEditRequests(courseID, c.Query("status"), userID, canReviewAll)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch edit requests"})
	}
	counts, err := loadScoreEditRequestCounts(courseID, userID, canReviewAll)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch edit requests"})
	}

	formatted := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		item := fiber.Map{
			"id":             row.ID,
			"status":         row.Status,
			"old_score":      row.OldScore,
			"new_score":      row.NewScore,
			"reason":         row.Reason,
			"images":         scoreEditRequestImagesValue(row.Images),
			"review_comment": row.ReviewComment,
			"created_at":     row.CreatedAt,
			"reviewed_at":    row.ReviewedAt,
			"score": fiber.Map{
				"id":            row.ScoreID,
				"current_score": row.CurrentScore,
			},
			"assignment": fiber.Map{
				"id":        row.AssignmentID,
				"name":      row.AssignmentName,
				"max_score": row.AssignmentMaxScore,
			},
			"sub_item": nil,
			"student":  nil,
			"requester": fiber.Map{
				"id":        row.RequesterID,
				"username":  row.RequesterUsername,
				"full_name": row.RequesterFullName,
			},
			"reviewer": nil,
		}
		if row.SubItemID != nil {
			item["sub_item"] = fiber.Map{"id": row.SubItemID, "name": row.SubItemName, "max_score": row.SubItemMaxScore}
		}
		if row.StudentRowID != nil {
			item["student"] = fiber.Map{"id": row.StudentRowID, "student_id": row.StudentCode, "full_name": row.StudentName}
		}
		if row.ReviewerID != nil {
			item["reviewer"] = fiber.Map{"id": row.ReviewerID, "username": row.ReviewerUsername, "full_name": row.ReviewerFullName}
		}
		formatted = append(formatted, item)
	}

	return c.JSON(fiber.Map{"success": true, "data": formatted, "counts": counts, "role": func() string {
		if canReviewAll {
			return "instructor"
		}
		return "ta"
	}()})
}

// POST /api/score-edit-requests
func CreateScoreEditRequestCompatHandler(c fiber.Ctx) error {
	scoreID, newScore, reason, images, imagePaths, err := parseSingleScoreEditRequestInput(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	context, err := loadScoreEditRequestContext(scoreID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Score not found"})
	}

	maxScore := context.AssignmentMaxScore
	if context.SubItemMaxScore != nil {
		maxScore = *context.SubItemMaxScore
	}
	if newScore < 0 || newScore > maxScore {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("Score must be between 0 and %.2f", maxScore)})
	}
	if existing, pendingErr := findPendingScoreEditRequestByScoreID(scoreID); pendingErr == nil && existing != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รายการคะแนนนี้มีคำร้องแก้ไขที่รออนุมัติอยู่แล้ว"})
	}

	userID := c.Locals("user_id").(uint)
	request := models.ScoreEditRequest{
		ScoreID:     scoreID,
		OldScore:    &context.CurrentScore,
		NewScore:    newScore,
		Reason:      reason,
		RequestedBy: userID,
		Status:      "pending",
		Images:      images,
	}
	if err := repositories.CreateScoreEditRequest(&request); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create edit request"})
	}

	writeCourseActivityLog(context.CourseID, userID, "create_score_edit_request", "score", "assignment", context.AssignmentID, context.AssignmentName, fiber.Map{"score_id": scoreID, "new_score": newScore, "image_count": len(imagePaths)})
	go createNotificationsForCourseMembers(context.CourseID, userID, "score_edit_request", "ขอแก้ไขคะแนน: "+context.AssignmentName, "มีการส่งคำขอแก้ไขคะแนน", "/classroom/"+context.CourseID+"/approval", buildNotifData(context.CourseID, fmt.Sprint(context.AssignmentID), "score_edit_request", ""))

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "Edit request created successfully",
		"data": fiber.Map{
			"id":     request.ID,
			"status": request.Status,
			"images": scoreEditRequestImagesValue(request.Images),
		},
	})
}

// POST /api/score-edit-requests/batch
func CreateBatchScoreEditRequestCompatHandler(c fiber.Ctx) error {
	scoreIDs, newScore, reason, images, imagePaths, err := parseBatchScoreEditRequestInput(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	contexts, err := loadScoreEditRequestContexts(scoreIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load scores"})
	}
	if len(contexts) == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No scores found"})
	}

	assignmentID := contexts[0].AssignmentID
	courseID := contexts[0].CourseID
	assignmentName := contexts[0].AssignmentName
	maxScore := contexts[0].AssignmentMaxScore
	if contexts[0].SubItemMaxScore != nil {
		maxScore = *contexts[0].SubItemMaxScore
	}
	if newScore < 0 || newScore > maxScore {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("Score must be between 0 and %.2f", maxScore)})
	}

	// Check for duplicate pending requests — skip those, continue with the rest
	var contextsToProcess []scoreEditRequestContextRow
	skippedNames := []string{}
	type skippedItem struct {
		ScoreID     uint    `json:"score_id"`
		StudentName string  `json:"student_name"`
		SubItemName *string `json:"sub_item_name,omitempty"`
	}
	skippedItems := make([]skippedItem, 0)
	for _, context := range contexts {
		if context.AssignmentID != assignmentID {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "All score_ids must belong to the same assignment"})
		}
		if existing, pendingErr := findPendingScoreEditRequestByScoreID(context.ScoreID); pendingErr == nil && existing != nil {
			// Get student name for the skip message
			var st struct {
				FullName string `gorm:"column:full_name"`
			}
			studentName := "unknown"
			if context.StudentID != nil {
				studentName = fmt.Sprintf("student_id:%d", *context.StudentID)
				if err := config.DB.Raw("SELECT full_name FROM students WHERE id = ? LIMIT 1", *context.StudentID).Scan(&st).Error; err == nil && st.FullName != "" {
					studentName = st.FullName
				}
			}
			skippedNames = append(skippedNames, studentName)
			skippedItems = append(skippedItems, skippedItem{ScoreID: context.ScoreID, StudentName: studentName, SubItemName: context.SubItemName})
			continue
		}
		contextsToProcess = append(contextsToProcess, context)
	}

	// If ALL members already have pending requests, return error
	if len(contextsToProcess) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("มีคำร้องแก้ไขคะแนนที่รออนุมัติอยู่แล้ว: %s", strings.Join(skippedNames, ", "))})
	}

	userID := c.Locals("user_id").(uint)
	createdRequests := make([]models.ScoreEditRequest, 0, len(contextsToProcess))
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		for _, context := range contextsToProcess {
			request := models.ScoreEditRequest{
				ScoreID:     context.ScoreID,
				OldScore:    &context.CurrentScore,
				NewScore:    newScore,
				Reason:      reason,
				RequestedBy: userID,
				Status:      "pending",
				Images:      images,
			}
			if err := tx.Create(&request).Error; err != nil {
				return err
			}
			createdRequests = append(createdRequests, request)
		}
		return nil
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create batch edit requests"})
	}

	writeCourseActivityLog(courseID, userID, "create_batch_score_edit_request", "score", "assignment", assignmentID, assignmentName, fiber.Map{"count": len(createdRequests), "new_score": newScore, "image_count": len(imagePaths)})

	requests := make([]fiber.Map, 0, len(createdRequests))
	for _, request := range createdRequests {
		requests = append(requests, fiber.Map{"id": request.ID, "score_id": request.ScoreID, "status": request.Status, "images": scoreEditRequestImagesValue(request.Images)})
	}

	message := fmt.Sprintf("Created %d edit request(s) successfully", len(createdRequests))
	if len(skippedNames) > 0 {
		message = fmt.Sprintf("สร้างคำร้องแก้ไข %d รายการ (ข้าม %d รายการที่มีคำร้องรออนุมัติอยู่แล้ว: %s)", len(createdRequests), len(skippedNames), strings.Join(skippedNames, ", "))
	}

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": message,
		"data": fiber.Map{
			"count":         len(createdRequests),
			"skipped":       len(skippedNames),
			"skipped_names": skippedNames,
			"skipped_items": skippedItems,
			"requests":      requests,
		},
	})
}

// POST /api/score-edit-requests/batch-detailed
func CreateBatchDetailedScoreEditRequestCompatHandler(c fiber.Ctx) error {
	edits, reason, images, imagePaths, err := parseBatchDetailedScoreEditRequestInput(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	scoreIDToNewScore := map[uint]float64{}
	scoreIDs := make([]uint, 0, len(edits))
	for _, edit := range edits {
		if edit.ScoreID == 0 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "score_id is required"})
		}
		if _, exists := scoreIDToNewScore[edit.ScoreID]; exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("duplicate score_id: %d", edit.ScoreID)})
		}
		scoreIDToNewScore[edit.ScoreID] = edit.NewScore
		scoreIDs = append(scoreIDs, edit.ScoreID)
	}

	contexts, err := loadScoreEditRequestContexts(scoreIDs)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load scores"})
	}
	if len(contexts) == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No scores found"})
	}

	contextByScoreID := map[uint]scoreEditRequestContextRow{}
	for _, context := range contexts {
		contextByScoreID[context.ScoreID] = context
	}

	assignmentID := contexts[0].AssignmentID
	courseID := contexts[0].CourseID
	assignmentName := contexts[0].AssignmentName

	studentNameMap := map[uint]string{}
	studentIDs := make([]uint, 0)
	for _, context := range contexts {
		if context.StudentID != nil {
			studentIDs = append(studentIDs, *context.StudentID)
		}
	}
	studentIDs = uniqueScoreUintValues(studentIDs)
	if len(studentIDs) > 0 {
		var rows []struct {
			ID       uint   `gorm:"column:id"`
			FullName string `gorm:"column:full_name"`
		}
		if err := config.DB.Raw("SELECT id, full_name FROM students WHERE id IN ?", studentIDs).Scan(&rows).Error; err == nil {
			for _, row := range rows {
				studentNameMap[row.ID] = row.FullName
			}
		}
	}

	contextsToProcess := make([]scoreEditRequestContextRow, 0, len(edits))
	skippedNames := make([]string, 0)
	type skippedItem struct {
		ScoreID     uint    `json:"score_id"`
		StudentName string  `json:"student_name"`
		SubItemName *string `json:"sub_item_name,omitempty"`
	}
	skippedItems := make([]skippedItem, 0)
	for _, edit := range edits {
		context, exists := contextByScoreID[edit.ScoreID]
		if !exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("score_id %d not found", edit.ScoreID)})
		}

		if context.AssignmentID != assignmentID || context.CourseID != courseID {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "all edits must belong to the same assignment/course"})
		}

		maxScore := context.AssignmentMaxScore
		if context.SubItemMaxScore != nil {
			maxScore = *context.SubItemMaxScore
		}
		if edit.NewScore < 0 || edit.NewScore > maxScore {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("score for score_id %d must be between 0 and %.2f", edit.ScoreID, maxScore)})
		}

		if existing, pendingErr := findPendingScoreEditRequestByScoreID(edit.ScoreID); pendingErr == nil && existing != nil {
			studentName := "unknown"
			if context.StudentID != nil {
				if name, ok := studentNameMap[*context.StudentID]; ok && strings.TrimSpace(name) != "" {
					studentName = name
				} else {
					studentName = fmt.Sprintf("student_id:%d", *context.StudentID)
				}
			}
			skippedNames = append(skippedNames, studentName)
			continue
		}

		contextsToProcess = append(contextsToProcess, context)
	}

	if len(contextsToProcess) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": fmt.Sprintf("มีคำร้องแก้ไขคะแนนที่รออนุมัติอยู่แล้ว: %s", strings.Join(skippedNames, ", "))})
	}

	userID := c.Locals("user_id").(uint)
	createdRequests := make([]models.ScoreEditRequest, 0, len(contextsToProcess))
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		for _, context := range contextsToProcess {
			newScore := scoreIDToNewScore[context.ScoreID]
			request := models.ScoreEditRequest{
				ScoreID:     context.ScoreID,
				OldScore:    &context.CurrentScore,
				NewScore:    newScore,
				Reason:      reason,
				RequestedBy: userID,
				Status:      "pending",
				Images:      images,
			}
			if err := tx.Create(&request).Error; err != nil {
				return err
			}
			createdRequests = append(createdRequests, request)
		}
		return nil
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create detailed batch edit requests"})
	}

	writeCourseActivityLog(courseID, userID, "create_detailed_batch_score_edit_request", "score", "assignment", assignmentID, assignmentName, fiber.Map{"count": len(createdRequests), "image_count": len(imagePaths)})

	requests := make([]fiber.Map, 0, len(createdRequests))
	for _, request := range createdRequests {
		requests = append(requests, fiber.Map{"id": request.ID, "score_id": request.ScoreID, "status": request.Status, "images": scoreEditRequestImagesValue(request.Images)})
	}

	message := fmt.Sprintf("Created %d detailed edit request(s) successfully", len(createdRequests))
	if len(skippedNames) > 0 {
		message = fmt.Sprintf("สร้างคำร้องแก้ไข %d รายการ (ข้าม %d รายการที่มีคำร้องรออนุมัติอยู่แล้ว: %s)", len(createdRequests), len(skippedNames), strings.Join(skippedNames, ", "))
	}

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": message,
		"data": fiber.Map{
			"count":         len(createdRequests),
			"skipped":       len(skippedNames),
			"skipped_names": uniqueStrings(skippedNames),
			"skipped_items": skippedItems,
			"requests":      requests,
		},
	})
}

func uniqueStrings(input []string) []string {
	if len(input) == 0 {
		return input
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(input))
	for _, value := range input {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// DELETE /api/score-edit-requests/:id/cancel
func CancelScoreEditRequestCompatHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}

	var request models.ScoreEditRequest
	if err := config.DB.First(&request, uint(id)).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Edit request not found"})
	}

	userID := c.Locals("user_id").(uint)
	if request.RequestedBy != userID {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "Only the requester can cancel this edit request"})
	}
	if request.Status != "pending" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Only pending edit requests can be cancelled"})
	}

	context, _ := loadScoreEditRequestContext(request.ScoreID)
	if err := config.DB.Delete(&request).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to cancel edit request"})
	}
	if context != nil {
		writeCourseActivityLog(context.CourseID, userID, "cancel_score_edit_request", "score", "assignment", context.AssignmentID, context.AssignmentName, fiber.Map{"request_id": request.ID, "score_id": request.ScoreID})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Score edit request cancelled"})
}

// POST /api/score-edit-requests/:id/approve
func ApproveScoreEditRequestCompatHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	var input struct {
		Comment string `json:"comment"`
	}
	_ = c.Bind().JSON(&input)
	reviewerID := c.Locals("user_id").(uint)
	requesterID, _ := repositories.GetScoreEditRequestRequester(uint(id))
	count, courseID, assignmentName, err := reviewScoreEditRequests([]uint{uint(id)}, true, reviewerID, input.Comment)
	if err != nil {
		status := 500
		if strings.Contains(err.Error(), "no pending") {
			status = 404
		} else if strings.Contains(err.Error(), "same course") || strings.Contains(err.Error(), "required") {
			status = 400
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	writeCourseActivityLog(courseID, reviewerID, "approve_score_edit_request", "score", "assignment", "", assignmentName, fiber.Map{"count": count, "request_id": id})
	if requesterID != 0 {
		go createNotificationForUser(requesterID, courseID, "score_edit_approved", "คำขอแก้ไขคะแนนได้รับการอนุมัติ", "คำขอแก้ไขคะแนนของคุณได้รับการอนุมัติแล้ว", "/classroom/"+courseID+"/approval", buildNotifData(courseID, fmt.Sprint(id), "score_edit_request", ""))
	}
	return c.JSON(fiber.Map{"success": true, "message": "Score edit request approved"})
}

// POST /api/score-edit-requests/:id/reject
func RejectScoreEditRequestCompatHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid ID"})
	}
	var input struct {
		Comment string `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil || strings.TrimSpace(input.Comment) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Rejection reason is required"})
	}
	reviewerID := c.Locals("user_id").(uint)
	requesterID, _ := repositories.GetScoreEditRequestRequester(uint(id))
	count, courseID, assignmentName, err := reviewScoreEditRequests([]uint{uint(id)}, false, reviewerID, input.Comment)
	if err != nil {
		status := 500
		if strings.Contains(err.Error(), "no pending") {
			status = 404
		} else if strings.Contains(err.Error(), "same course") || strings.Contains(err.Error(), "required") {
			status = 400
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	writeCourseActivityLog(courseID, reviewerID, "reject_score_edit_request", "score", "assignment", "", assignmentName, fiber.Map{"count": count, "request_id": id})
	if requesterID != 0 {
		go createNotificationForUser(requesterID, courseID, "score_edit_rejected", "คำขอแก้ไขคะแนนถูกปฏิเสธ", "คำขอแก้ไขคะแนนของคุณถูกปฏิเสธ", "/classroom/"+courseID+"/approval", buildNotifData(courseID, fmt.Sprint(id), "score_edit_request", ""))
	}
	return c.JSON(fiber.Map{"success": true, "message": "Score edit request rejected"})
}

// POST /api/score-edit-requests/batch-approve
func BatchApproveScoreEditRequestsCompatHandler(c fiber.Ctx) error {
	var input struct {
		RequestIDs []uint `json:"request_ids"`
		Comment    string `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.RequestIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "request_ids array is required"})
	}
	requestIDs := uniqueScoreUintValues(input.RequestIDs)
	requesterMap, _ := repositories.GetScoreEditRequestRequesters(requestIDs)
	reviewerID := c.Locals("user_id").(uint)
	count, courseID, assignmentName, err := reviewScoreEditRequests(requestIDs, true, reviewerID, input.Comment)
	if err != nil {
		status := 500
		if strings.Contains(err.Error(), "no pending") {
			status = 404
		} else if strings.Contains(err.Error(), "same course") || strings.Contains(err.Error(), "required") {
			status = 400
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	writeCourseActivityLog(courseID, reviewerID, "batch_approve_score_edit_requests", "score", "assignment", "", assignmentName, fiber.Map{"count": count})
	notifiedUsers := map[uint]bool{}
	for reqID, requesterID := range requesterMap {
		if requesterID == 0 || notifiedUsers[requesterID] {
			continue
		}
		notifiedUsers[requesterID] = true
		go createNotificationForUser(requesterID, courseID, "score_edit_approved", "คำขอแก้ไขคะแนนได้รับการอนุมัติ", fmt.Sprintf("คำขอแก้ไขคะแนนได้รับการอนุมัติ (%d รายการ)", count), "/classroom/"+courseID+"/approval", buildNotifData(courseID, fmt.Sprint(reqID), "score_edit_request", ""))
	}
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("Approved %d edit request(s)", count), "count": count})
}

// POST /api/score-edit-requests/batch-reject
func BatchRejectScoreEditRequestsCompatHandler(c fiber.Ctx) error {
	var input struct {
		RequestIDs []uint `json:"request_ids"`
		Comment    string `json:"comment"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.RequestIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "request_ids array is required"})
	}
	if strings.TrimSpace(input.Comment) == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Rejection reason is required"})
	}
	requestIDs := uniqueScoreUintValues(input.RequestIDs)
	requesterMap, _ := repositories.GetScoreEditRequestRequesters(requestIDs)
	reviewerID := c.Locals("user_id").(uint)
	count, courseID, assignmentName, err := reviewScoreEditRequests(requestIDs, false, reviewerID, input.Comment)
	if err != nil {
		status := 500
		if strings.Contains(err.Error(), "no pending") {
			status = 404
		} else if strings.Contains(err.Error(), "same course") || strings.Contains(err.Error(), "required") {
			status = 400
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	writeCourseActivityLog(courseID, reviewerID, "batch_reject_score_edit_requests", "score", "assignment", "", assignmentName, fiber.Map{"count": count})
	notifiedUsers := map[uint]bool{}
	for reqID, requesterID := range requesterMap {
		if requesterID == 0 || notifiedUsers[requesterID] {
			continue
		}
		notifiedUsers[requesterID] = true
		go createNotificationForUser(requesterID, courseID, "score_edit_rejected", "คำขอแก้ไขคะแนนถูกปฏิเสธ", fmt.Sprintf("คำขอแก้ไขคะแนนถูกปฏิเสธ (%d รายการ)", count), "/classroom/"+courseID+"/approval", buildNotifData(courseID, fmt.Sprint(reqID), "score_edit_request", ""))
	}
	return c.JSON(fiber.Map{"success": true, "message": fmt.Sprintf("Rejected %d edit request(s)", count), "count": count})
}
