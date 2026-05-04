package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"time"

	"gorm.io/gorm"
)

// ============================================================
// Score
// ============================================================

type ScoreRecord struct {
	Student  StudentBasic `json:"student"`
	Score    *float64     `json:"score"`
	Comment  string       `json:"comment"`
	GradedBy *uint        `json:"graded_by"`
	GradedAt *time.Time   `json:"graded_at"`
	Status   string       `json:"status"`
}

type StudentBasic struct {
	ID        uint   `json:"id"`
	StudentID string `json:"student_id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
}

func GetScoresForAssignment(assignmentID uint) ([]models.Score, error) {
	var scores []models.Score
	err := config.DB.Where("assignment_id = ?", assignmentID).Find(&scores).Error
	return scores, err
}

func SubmitScore(score *models.Score) error {
	db := config.DB
	now := time.Now()
	score.GradedAt = &now
	score.Status = "graded"

	// Upsert: try to find existing record
	var existing models.Score
	q := db.Where("assignment_id = ? AND student_id = ?", score.AssignmentID, score.StudentID)
	if score.SubItemID != nil {
		q = q.Where("sub_item_id = ?", score.SubItemID)
	} else {
		q = q.Where("sub_item_id IS NULL")
	}

	result := q.Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		// Update
		existing.Score = score.Score
		existing.Comment = score.Comment
		existing.GradedBy = score.GradedBy
		existing.GradedAt = &now
		existing.Status = "graded"
		return db.Save(&existing).Error
	}
	return db.Create(score).Error
}

func BulkUpsertScores(scores []models.Score) error {
	db := config.DB
	return db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		for i := range scores {
			scores[i].GradedAt = &now
			scores[i].Status = "graded"

			var existing models.Score
			q := tx.Where("assignment_id = ? AND student_id = ?", scores[i].AssignmentID, scores[i].StudentID)
			if scores[i].SubItemID != nil {
				q = q.Where("sub_item_id = ?", scores[i].SubItemID)
			} else {
				q = q.Where("sub_item_id IS NULL")
			}

			result := q.Limit(1).Find(&existing)
			if result.Error != nil {
				return result.Error
			}

			if result.RowsAffected > 0 {
				existing.Score = scores[i].Score
				existing.Comment = scores[i].Comment
				existing.GradedBy = scores[i].GradedBy
				existing.GradedAt = &now
				existing.Status = "graded"
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				continue
			}

			if err := tx.Create(&scores[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetScoreMatrix returns a map of studentID -> assignmentID -> score
type ScoreMatrixRow struct {
	StudentID    uint    `json:"student_id"`
	AssignmentID uint    `json:"assignment_id"`
	Score        float64 `json:"score"`
}

func GetScoreMatrix(courseID string) ([]ScoreMatrixRow, error) {
	var rows []ScoreMatrixRow
	err := config.DB.Raw(`
		SELECT sc.student_id, sc.assignment_id, sc.score
		FROM scores sc
		JOIN assignments a ON a.id = sc.assignment_id
		WHERE a.course_id = ? AND sc.sub_item_id IS NULL AND sc.student_id IS NOT NULL
	`, courseID).Scan(&rows).Error
	return rows, err
}

// ============================================================
// Score Edit Request
// ============================================================

func CreateScoreEditRequest(req *models.ScoreEditRequest) error {
	req.Status = "pending"
	req.CreatedAt = time.Now()
	return config.DB.Create(req).Error
}

func GetPendingEditRequests(courseID string) ([]models.ScoreEditRequest, error) {
	var requests []models.ScoreEditRequest
	err := config.DB.Raw(`
		SELECT ser.* FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE a.course_id = ? AND ser.status = 'pending'
		ORDER BY ser.created_at DESC
	`, courseID).Scan(&requests).Error
	return requests, err
}

func ReviewEditRequest(id uint, approved bool, reviewerID uint, comment string) error {
	db := config.DB
	var req models.ScoreEditRequest
	if err := db.First(&req, id).Error; err != nil {
		return err
	}

	now := time.Now()
	req.Status = "rejected"
	if approved {
		req.Status = "approved"
		// Apply score update
		db.Model(&models.Score{}).Where("id = ?", req.ScoreID).Updates(map[string]interface{}{
			"score":     req.NewScore,
			"graded_at": now,
		})
	}
	req.ReviewedBy = &reviewerID
	req.ReviewedAt = &now
	req.ReviewComment = comment
	return db.Save(&req).Error
}
