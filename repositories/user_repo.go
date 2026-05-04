package repositories

import (
	"itii-assist/config"
	"itii-assist/models"
	"strings"
)

func CreateUser(user *models.User) error {
	return config.DB.Create(user).Error
}

func FindUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := config.DB.Where("username = ? AND is_active = ?", username, true).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func FindUserByID(id uint) (*models.User, error) {
	var user models.User
	err := config.DB.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func FindActiveUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := config.DB.Where("LOWER(email) = LOWER(?) AND is_active = ?", strings.TrimSpace(email), true).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

type UserListParams struct {
	Page      int
	Limit     int
	Search    string
	Role      string
	Status    string
	SortBy    string
	SortOrder string
}

type UserListResult struct {
	Users      []models.User
	Total      int64
	Page       int
	Limit      int
	TotalPages int
}

func GetUsers(params UserListParams) (*UserListResult, error) {
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

	// Whitelist สำหรับป้องกัน SQL Injection ผ่านค่า SortBy
	validSortCols := map[string]bool{
		"created_at": true, "updated_at": true, "username": true,
		"full_name": true, "email": true, "role": true, "is_active": true,
	}
	sortCol := "created_at"
	if validSortCols[params.SortBy] {
		sortCol = params.SortBy
	}
	sortDir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		sortDir = "ASC"
	}

	db := config.DB.Model(&models.User{}).Omit("PasswordHash", "TwoFactorBackupCodes", "TwoFactorSecret")

	if params.Search != "" {
		search := "%" + params.Search + "%"
		db = db.Where("username ILIKE ? OR full_name ILIKE ? OR email ILIKE ?", search, search, search)
	}
	if params.Role != "" {
		db = db.Where("role = ?", params.Role)
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

	offset := (params.Page - 1) * params.Limit

	var users []models.User
	if err := db.Order(sortCol + " " + sortDir).Limit(params.Limit).Offset(offset).Find(&users).Error; err != nil {
		return nil, err
	}

	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}

	return &UserListResult{
		Users:      users,
		Total:      total,
		Page:       params.Page,
		Limit:      params.Limit,
		TotalPages: totalPages,
	}, nil
}

func UpdateUser(user *models.User) error {
	return config.DB.Save(user).Error
}

func ToggleUserStatus(id uint) (*models.User, error) {
	user, err := FindUserByID(id)
	if err != nil {
		return nil, err
	}
	user.IsActive = !user.IsActive
	if err := config.DB.Save(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func DeleteUser(id uint) error {
	return config.DB.Delete(&models.User{}, id).Error
}

type UserStats struct {
	Total      int64
	Active     int64
	Inactive   int64
	Admin      int64
	Instructor int64
	TA         int64
}

func GetUserStats() (*UserStats, error) {
	var stats UserStats
	config.DB.Model(&models.User{}).Count(&stats.Total)
	config.DB.Model(&models.User{}).Where("is_active = ?", true).Count(&stats.Active)
	config.DB.Model(&models.User{}).Where("is_active = ?", false).Count(&stats.Inactive)
	config.DB.Model(&models.User{}).Where("role = ?", "admin").Count(&stats.Admin)
	config.DB.Model(&models.User{}).Where("role = ?", "instructor").Count(&stats.Instructor)
	config.DB.Model(&models.User{}).Where("role = ?", "ta").Count(&stats.TA)
	return &stats, nil
}

func IsUsernameExists(username string, excludeID uint) bool {
	var count int64
	db := config.DB.Model(&models.User{}).Where("username = ?", username)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	db.Count(&count)
	return count > 0
}

func IsEmailExists(email string, excludeID uint) bool {
	var count int64
	db := config.DB.Model(&models.User{}).Where("email = ?", email)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	db.Count(&count)
	return count > 0
}
