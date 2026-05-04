package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"
)

type FeedbackListParams struct {
	Page      int
	Limit     int
	Search    string
	Type      string
	Status    string
	Priority  string
	SortBy    string
	SortOrder string
}

type FeedbackUserBasic struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Avatar   string `json:"avatar"`
}

type FeedbackResolverBasic struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Avatar   string `json:"avatar"`
}

type FeedbackWithUsers struct {
	models.Feedback
	User     *FeedbackUserBasic     `json:"user"`
	Resolver *FeedbackResolverBasic `json:"resolver"`
}

type FeedbackPagination struct {
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

type FeedbackListResult struct {
	Feedbacks  []FeedbackWithUsers `json:"feedbacks"`
	Pagination FeedbackPagination  `json:"pagination"`
}

func buildFeedbackUsers(rawFeedbacks []models.Feedback) ([]FeedbackWithUsers, error) {
	userIDSet := map[uint]bool{}
	for _, f := range rawFeedbacks {
		if f.UserID != nil {
			userIDSet[*f.UserID] = true
		}
		if f.ResolvedBy != nil {
			userIDSet[*f.ResolvedBy] = true
		}
	}
	userMap := map[uint]models.User{}
	if len(userIDSet) > 0 {
		userIDs := make([]uint, 0, len(userIDSet))
		for id := range userIDSet {
			userIDs = append(userIDs, id)
		}
		var users []models.User
		if err := config.DB.Select("id, username, full_name, email, role, avatar").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			userMap[u.ID] = u
		}
	}

	result := make([]FeedbackWithUsers, len(rawFeedbacks))
	for i, f := range rawFeedbacks {
		fw := FeedbackWithUsers{Feedback: f}
		if f.UserID != nil {
			if u, ok := userMap[*f.UserID]; ok {
				fw.User = &FeedbackUserBasic{
					ID:       u.ID,
					Username: u.Username,
					FullName: u.FullName,
					Email:    u.Email,
					Role:     u.Role,
					Avatar:   u.Avatar,
				}
			}
		}
		if f.ResolvedBy != nil {
			if u, ok := userMap[*f.ResolvedBy]; ok {
				fw.Resolver = &FeedbackResolverBasic{
					ID:       u.ID,
					Username: u.Username,
					FullName: u.FullName,
					Avatar:   u.Avatar,
				}
			}
		}
		result[i] = fw
	}
	return result, nil
}

func CreateFeedback(f *models.Feedback) error {
	return config.DB.Create(f).Error
}

func GetFeedbacks(params FeedbackListParams) (FeedbackListResult, error) {
	db := config.DB
	query := db.Model(&models.Feedback{})

	if params.Search != "" {
		like := "%" + strings.TrimSpace(params.Search) + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", like, like)
	}
	if params.Type != "" && params.Type != "all" {
		query = query.Where("type = ?", params.Type)
	}
	if params.Status != "" && params.Status != "all" {
		query = query.Where("status = ?", params.Status)
	}
	if params.Priority != "" && params.Priority != "all" {
		query = query.Where("priority = ?", params.Priority)
	}

	var total int64
	query.Count(&total)

	sortCol := "created_at"
	if params.SortBy != "" {
		sortCol = params.SortBy
	}
	dir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		dir = "ASC"
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 10
	}
	offset := (params.Page - 1) * params.Limit

	var rawFeedbacks []models.Feedback
	if err := query.Order(sortCol + " " + dir).Limit(params.Limit).Offset(offset).Find(&rawFeedbacks).Error; err != nil {
		return FeedbackListResult{}, err
	}

	feedbacks, err := buildFeedbackUsers(rawFeedbacks)
	if err != nil {
		return FeedbackListResult{}, err
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return FeedbackListResult{
		Feedbacks: feedbacks,
		Pagination: FeedbackPagination{
			Total:      total,
			Page:       params.Page,
			Limit:      params.Limit,
			TotalPages: totalPages,
		},
	}, nil
}

func GetFeedbackByID(id uint) (*FeedbackWithUsers, error) {
	var f models.Feedback
	if err := config.DB.First(&f, id).Error; err != nil {
		return nil, err
	}
	results, err := buildFeedbackUsers([]models.Feedback{f})
	if err != nil {
		return nil, err
	}
	return &results[0], nil
}

func GetMyFeedbacks(userID uint, page, limit int) (FeedbackListResult, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	var total int64
	config.DB.Model(&models.Feedback{}).Where("user_id = ?", userID).Count(&total)

	var rawFeedbacks []models.Feedback
	if err := config.DB.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Offset(offset).Find(&rawFeedbacks).Error; err != nil {
		return FeedbackListResult{}, err
	}

	feedbacks, err := buildFeedbackUsers(rawFeedbacks)
	if err != nil {
		return FeedbackListResult{}, err
	}

	totalPages := int(total) / limit
	if int(total)%limit != 0 {
		totalPages++
	}

	return FeedbackListResult{
		Feedbacks: feedbacks,
		Pagination: FeedbackPagination{
			Total:      total,
			Page:       page,
			Limit:      limit,
			TotalPages: totalPages,
		},
	}, nil
}

func UpdateFeedback(id uint, status string, priority string, adminNotes string, resolvedBy uint) (*FeedbackWithUsers, error) {
	var f models.Feedback
	if err := config.DB.First(&f, id).Error; err != nil {
		return nil, err
	}
	if status != "" {
		f.Status = status
		if status == "resolved" || status == "rejected" {
			now := time.Now()
			f.ResolvedAt = &now
			f.ResolvedBy = &resolvedBy
		}
	}
	if priority != "" {
		f.Priority = priority
	}
	if adminNotes != "" {
		f.AdminNotes = adminNotes
	}
	if err := config.DB.Save(&f).Error; err != nil {
		return nil, err
	}
	return GetFeedbackByID(f.ID)
}

func DeleteFeedback(id uint) error {
	return config.DB.Delete(&models.Feedback{}, id).Error
}

type FeedbackByStatus struct {
	Pending   int64 `json:"pending"`
	Reviewing int64 `json:"reviewing"`
	Resolved  int64 `json:"resolved"`
	Rejected  int64 `json:"rejected"`
}

type FeedbackByType struct {
	Bugs         int64 `json:"bugs"`
	Features     int64 `json:"features"`
	Improvements int64 `json:"improvements"`
	Others       int64 `json:"others"`
}

type FeedbackStats struct {
	Total    int64            `json:"total"`
	ByStatus FeedbackByStatus `json:"byStatus"`
	ByType   FeedbackByType   `json:"byType"`
}

func GetFeedbackStats() FeedbackStats {
	db := config.DB
	var total, pending, reviewing, resolved, rejected int64
	var bugs, features, improvements, others int64
	db.Model(&models.Feedback{}).Count(&total)
	db.Model(&models.Feedback{}).Where("status = 'pending'").Count(&pending)
	db.Model(&models.Feedback{}).Where("status = 'reviewing'").Count(&reviewing)
	db.Model(&models.Feedback{}).Where("status = 'resolved'").Count(&resolved)
	db.Model(&models.Feedback{}).Where("status = 'rejected'").Count(&rejected)
	db.Model(&models.Feedback{}).Where("type = 'bug'").Count(&bugs)
	db.Model(&models.Feedback{}).Where("type = 'feature'").Count(&features)
	db.Model(&models.Feedback{}).Where("type = 'improvement'").Count(&improvements)
	db.Model(&models.Feedback{}).Where("type = 'other'").Count(&others)
	return FeedbackStats{
		Total:    total,
		ByStatus: FeedbackByStatus{Pending: pending, Reviewing: reviewing, Resolved: resolved, Rejected: rejected},
		ByType:   FeedbackByType{Bugs: bugs, Features: features, Improvements: improvements, Others: others},
	}
}
