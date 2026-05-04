package handlers

import (
	"crypto/rand"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/utils"
	"math/big"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// Helper: สร้างรหัสผ่านแบบสุ่ม 12 ตัว
// =============================================================================

func generatePassword(length int) string {
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	const lower = "abcdefghjkmnpqrstuvwxyz"
	const digits = "23456789"
	const special = "!@#$%"
	const all = upper + lower + digits + special

	pick := func(charset string) byte {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		return charset[n.Int64()]
	}

	buf := []byte{pick(upper), pick(lower), pick(digits), pick(special)}
	for i := len(buf); i < length; i++ {
		buf = append(buf, pick(all))
	}

	// Fisher-Yates shuffle
	for i := len(buf) - 1; i > 0; i-- {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := n.Int64()
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// =============================================================================
// GET /api/users?page=&limit=&search=&role=&status=&sortBy=&sortOrder=
// =============================================================================

func GetUsersHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	result, err := repositories.GetUsers(repositories.UserListParams{
		Page:      page,
		Limit:     limit,
		Search:    c.Query("search"),
		Role:      c.Query("role"),
		Status:    c.Query("status"),
		SortBy:    c.Query("sortBy", "created_at"),
		SortOrder: c.Query("sortOrder", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลไม่สำเร็จ"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"users": result.Users,
			"pagination": fiber.Map{
				"total":      result.Total,
				"page":       result.Page,
				"limit":      result.Limit,
				"totalPages": result.TotalPages,
			},
		},
	})
}

// =============================================================================
// GET /api/users/stats
// =============================================================================

func GetUserStatsHandler(c fiber.Ctx) error {
	stats, err := repositories.GetUserStats()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงสถิติไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"total":    stats.Total,
			"active":   stats.Active,
			"inactive": stats.Inactive,
			"byRole": fiber.Map{
				"admin":      stats.Admin,
				"instructor": stats.Instructor,
				"ta":         stats.TA,
			},
		},
	})
}

// =============================================================================
// GET /api/users/:id
// =============================================================================

func GetUserByIDHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	user, err := repositories.FindUserByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	return c.JSON(fiber.Map{"success": true, "data": safeUser(user)})
}

// =============================================================================
// POST /api/users
// =============================================================================

type CreateUserInput struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	IsActive *bool  `json:"is_active"`
	Avatar   string `json:"avatar"`
}

func CreateUserHandler(c fiber.Ctx) error {
	var input CreateUserInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}
	if input.Username == "" || input.Role == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "กรุณากรอก username และ role"})
	}
	if input.Role != "admin" && input.Role != "instructor" && input.Role != "ta" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "role ต้องเป็น admin, instructor หรือ ta"})
	}
	if repositories.IsUsernameExists(input.Username, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ชื่อผู้ใช้นี้มีอยู่ในระบบแล้ว"})
	}
	if input.Email != "" && repositories.IsEmailExists(input.Email, 0) {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
	}

	generatedPassword := generatePassword(12)
	hashed, err := utils.HashPassword(generatedPassword)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}

	newUser := models.User{
		Username:           input.Username,
		PasswordHash:       hashed,
		Role:               input.Role,
		FullName:           input.FullName,
		Email:              input.Email,
		IsActive:           isActive,
		Provider:           "local",
		Avatar:             input.Avatar,
		MustChangePassword: true,
	}

	if err := repositories.CreateUser(&newUser); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "สร้างผู้ใช้ไม่สำเร็จ"})
	}

	actorID := c.Locals("user_id").(uint)
	config.DB.Create(&models.SystemLog{
		Action:       "create_user",
		LogType:      "auth",
		Severity:     "info",
		ActorUserID:  &actorID,
		ResourceType: "users",
		ResourceID:   strconv.FormatUint(uint64(newUser.ID), 10),
		IPAddress:    c.IP(),
	})

	return c.Status(201).JSON(fiber.Map{
		"success": true,
		"message": "สร้างผู้ใช้สำเร็จ",
		"data": fiber.Map{
			"user": safeUser(&newUser),
			"credentials": fiber.Map{
				"username": input.Username,
				"password": generatedPassword,
			},
		},
	})
}

// =============================================================================
// PUT /api/users/:id
// =============================================================================

type UpdateUserInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	IsActive *bool  `json:"is_active"`
	Avatar   string `json:"avatar"`
}

func UpdateUserHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	var input UpdateUserInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	user, err := repositories.FindUserByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	if input.Username != "" && input.Username != user.Username {
		if repositories.IsUsernameExists(input.Username, user.ID) {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "ชื่อผู้ใช้นี้มีอยู่ในระบบแล้ว"})
		}
		user.Username = input.Username
	}
	if input.Email != "" && input.Email != user.Email {
		if repositories.IsEmailExists(input.Email, user.ID) {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "อีเมลนี้มีอยู่ในระบบแล้ว"})
		}
		user.Email = input.Email
	}
	if input.Role != "" && (input.Role == "admin" || input.Role == "instructor" || input.Role == "ta") {
		user.Role = input.Role
	}
	if input.FullName != "" {
		user.FullName = input.FullName
	}
	if input.IsActive != nil {
		user.IsActive = *input.IsActive
	}
	if input.Avatar != "" {
		user.Avatar = input.Avatar
	}
	if input.Password != "" {
		hashed, err := utils.HashPassword(input.Password)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "message": "เข้ารหัสผ่านไม่สำเร็จ"})
		}
		user.PasswordHash = hashed
	}

	if err := repositories.UpdateUser(user); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "อัปเดตไม่สำเร็จ"})
	}

	actorID := c.Locals("user_id").(uint)
	config.DB.Create(&models.SystemLog{
		Action:       "update_user",
		LogType:      "auth",
		Severity:     "info",
		ActorUserID:  &actorID,
		ResourceType: "users",
		ResourceID:   c.Params("id"),
		IPAddress:    c.IP(),
	})

	return c.JSON(fiber.Map{
		"success": true,
		"message": "อัปเดตผู้ใช้สำเร็จ",
		"data":    safeUser(user),
	})
}

// =============================================================================
// PATCH /api/users/:id/status
// =============================================================================

func ToggleUserStatusHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	actorID := c.Locals("user_id").(uint)
	if uint(id) == actorID {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถเปลี่ยนสถานะบัญชีของตัวเองได้"})
	}

	user, err := repositories.ToggleUserStatus(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	action := "deactivate_user"
	msg := "ปิดใช้งานผู้ใช้สำเร็จ"
	if user.IsActive {
		action = "activate_user"
		msg = "เปิดใช้งานผู้ใช้สำเร็จ"
	}

	config.DB.Create(&models.SystemLog{
		Action:       action,
		LogType:      "auth",
		Severity:     "info",
		ActorUserID:  &actorID,
		ResourceType: "users",
		ResourceID:   c.Params("id"),
		IPAddress:    c.IP(),
	})

	return c.JSON(fiber.Map{
		"success": true,
		"message": msg,
		"data":    safeUser(user),
	})
}

// =============================================================================
// DELETE /api/users/:id
// =============================================================================

func DeleteUserHandler(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ID ไม่ถูกต้อง"})
	}

	actorID := c.Locals("user_id").(uint)
	if uint(id) == actorID {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ไม่สามารถลบบัญชีของตัวเองได้"})
	}

	user, err := repositories.FindUserByID(uint(id))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบผู้ใช้"})
	}

	username := user.Username
	if err := repositories.DeleteUser(uint(id)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบผู้ใช้ไม่สำเร็จ"})
	}

	config.DB.Create(&models.SystemLog{
		Action:       "delete_user",
		LogType:      "auth",
		Severity:     "warn",
		ActorUserID:  &actorID,
		ResourceType: "users",
		ResourceID:   c.Params("id"),
		Detail:       []byte(`{"username":"` + username + `"}`),
		IPAddress:    c.IP(),
	})

	return c.JSON(fiber.Map{"success": true, "message": "ลบผู้ใช้สำเร็จ"})
}
