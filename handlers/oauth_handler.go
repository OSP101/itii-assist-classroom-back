package handlers

import (
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// GET /api/oauth/linked
// =============================================================================

func GetLinkedAccountsHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var accounts []models.UserOAuthAccount
	config.DB.Where("user_id = ?", userID).
		Order("linked_at ASC").
		Find(&accounts)

	type safeAccount struct {
		ID             uint      `json:"id"`
		Provider       string    `json:"provider"`
		ProviderEmail  string    `json:"provider_email"`
		ProviderName   string    `json:"provider_name"`
		ProviderAvatar string    `json:"provider_avatar"`
		LinkedAt       time.Time `json:"linked_at"`
	}

	result := make([]safeAccount, len(accounts))
	for i, a := range accounts {
		result[i] = safeAccount{
			ID:             a.ID,
			Provider:       a.Provider,
			ProviderEmail:  a.ProviderEmail,
			ProviderName:   a.ProviderName,
			ProviderAvatar: a.ProviderAvatar,
			LinkedAt:       a.LinkedAt,
		}
	}

	return c.JSON(fiber.Map{"success": true, "data": result})
}

// =============================================================================
// POST /api/oauth/link
// =============================================================================

type LinkAccountInput struct {
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	ProviderEmail  string `json:"provider_email"`
	ProviderName   string `json:"provider_name"`
	ProviderAvatar string `json:"provider_avatar"`
}

var validProviders = map[string]bool{"google": true, "github": true, "apple": true}

func LinkAccountHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	var input LinkAccountInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid request body"})
	}

	if input.Provider == "" || input.ProviderUserID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Provider and provider_user_id are required"})
	}

	if !validProviders[input.Provider] {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid provider"})
	}

	// Check if this provider account is already linked to any user
	var existing models.UserOAuthAccount
	if err := config.DB.Where("provider = ? AND provider_user_id = ?", input.Provider, input.ProviderUserID).First(&existing).Error; err == nil {
		if existing.UserID == userID {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "This account is already linked to your profile"})
		}
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "This account is already linked to another user"})
	}

	// Check if user already has this provider linked
	var userProvider models.UserOAuthAccount
	if err := config.DB.Where("user_id = ? AND provider = ?", userID, input.Provider).First(&userProvider).Error; err == nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "You already have a " + input.Provider + " account linked",
		})
	}

	account := models.UserOAuthAccount{
		UserID:         userID,
		Provider:       input.Provider,
		ProviderUserID: input.ProviderUserID,
		ProviderEmail:  input.ProviderEmail,
		ProviderName:   input.ProviderName,
		ProviderAvatar: input.ProviderAvatar,
		LinkedAt:       time.Now(),
	}

	if err := config.DB.Create(&account).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to link account"})
	}

	logPrivilegedAdminAction(c, userID, "link_oauth_account", "info", "users", strconv.FormatUint(uint64(userID), 10), fiber.Map{"provider": input.Provider})

	return c.JSON(fiber.Map{
		"success": true,
		"message": input.Provider + " account linked successfully",
		"data": fiber.Map{
			"id":              account.ID,
			"provider":        account.Provider,
			"provider_email":  account.ProviderEmail,
			"provider_name":   account.ProviderName,
			"provider_avatar": account.ProviderAvatar,
			"linked_at":       account.LinkedAt,
		},
	})
}

// =============================================================================
// DELETE /api/oauth/unlink/:provider
// =============================================================================

func UnlinkAccountHandler(c fiber.Ctx) error {
	userID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	provider := c.Params("provider")
	if !validProviders[provider] {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid provider"})
	}

	var account models.UserOAuthAccount
	if err := config.DB.Where("user_id = ? AND provider = ?", userID, provider).First(&account).Error; err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No " + provider + " account is linked"})
	}

	// Ensure user has another auth method before unlinking
	var user models.User
	config.DB.Select("id, password_hash, google_id").First(&user, userID)
	hasPassword := user.PasswordHash != ""

	var remaining int64
	config.DB.Model(&models.UserOAuthAccount{}).Where("user_id = ?", userID).Count(&remaining)

	if !hasPassword && remaining <= 1 {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Cannot unlink the only authentication method. Please set a password first.",
		})
	}

	if err := config.DB.Delete(&account).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to unlink account"})
	}

	// Clear legacy google_id field if unlinking Google
	if provider == "google" && user.GoogleID != "" {
		config.DB.Model(&user).Update("google_id", "")
	}

	logPrivilegedAdminAction(c, userID, "unlink_oauth_account", "info", "users", strconv.FormatUint(uint64(userID), 10), fiber.Map{"provider": provider})

	return c.JSON(fiber.Map{"success": true, "message": provider + " account unlinked successfully"})
}

// =============================================================================
// GET /api/oauth/admin/user/:userId  (admin only)
// =============================================================================

func AdminGetLinkedAccountsHandler(c fiber.Ctx) error {
	targetUserIDStr := c.Params("userId")
	targetUserID, err := strconv.Atoi(targetUserIDStr)
	if err != nil || targetUserID <= 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid userId"})
	}

	var accounts []models.UserOAuthAccount
	config.DB.Where("user_id = ?", uint(targetUserID)).Order("linked_at ASC").Find(&accounts)

	return c.JSON(fiber.Map{"success": true, "data": accounts})
}

// =============================================================================
// DELETE /api/oauth/admin/user/:userId/:provider  (admin only)
// =============================================================================

func AdminUnlinkAccountHandler(c fiber.Ctx) error {
	targetUserIDStr := c.Params("userId")
	targetUserID, err := strconv.Atoi(targetUserIDStr)
	if err != nil || targetUserID <= 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid userId"})
	}

	provider := c.Params("provider")
	if !validProviders[provider] {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid provider"})
	}

	result := config.DB.Where("user_id = ? AND provider = ?", uint(targetUserID), provider).
		Delete(&models.UserOAuthAccount{})

	if result.RowsAffected == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "No " + provider + " account linked to this user"})
	}

	actorID, _ := c.Locals("user_id").(uint)
	logPrivilegedAdminAction(c, actorID, "admin_unlink_oauth_account", "warn", "users", targetUserIDStr, fiber.Map{"provider": provider})

	return c.JSON(fiber.Map{"success": true, "message": provider + " account unlinked successfully"})
}
