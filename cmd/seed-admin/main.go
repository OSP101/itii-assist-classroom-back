package main

import (
	"errors"
	"log"
	"os"
	"strings"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"

	"gorm.io/gorm"
)

func main() {
	username := strings.TrimSpace(getEnv("SEED_ADMIN_USERNAME", "admin"))
	password := getEnv("SEED_ADMIN_PASSWORD", "admin1234")
	fullName := strings.TrimSpace(getEnv("SEED_ADMIN_FULL_NAME", "System Admin"))
	email := strings.TrimSpace(getEnv("SEED_ADMIN_EMAIL", "admin@local"))

	if username == "" {
		log.Fatal("SEED_ADMIN_USERNAME must not be empty")
	}
	if len(password) < 8 {
		log.Fatal("SEED_ADMIN_PASSWORD must be at least 8 characters")
	}

	config.ConnectDB()

	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		log.Fatalf("failed to hash seed admin password: %v", err)
	}

	var user models.User
	err = config.DB.Where("username = ?", username).First(&user).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Fatalf("failed to check existing admin user: %v", err)
		}

		user = models.User{
			Username:           username,
			PasswordHash:       hashedPassword,
			Role:               "admin",
			FullName:           fullName,
			Email:              email,
			Provider:           "local",
			IsActive:           true,
			MustChangePassword: true,
		}
		if err := config.DB.Create(&user).Error; err != nil {
			log.Fatalf("failed to create seed admin user: %v", err)
		}

		log.Printf("seed admin created: username=%s password=%s must_change_password=true", username, password)
		return
	}

	user.PasswordHash = hashedPassword
	user.Role = "admin"
	user.Provider = "local"
	user.IsActive = true
	user.MustChangePassword = true
	user.FullName = fullName
	user.Email = email

	if err := config.DB.Save(&user).Error; err != nil {
		log.Fatalf("failed to update seed admin user: %v", err)
	}

	log.Printf("seed admin reset: username=%s password=%s must_change_password=true", username, password)
}

func getEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
