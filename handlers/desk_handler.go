package handlers

import (
	"itii-assist/config"
	"itii-assist/models"

	"errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// GET /api/desks/:deskId  (public – no auth required)
// Returns desk info + any active/paused queue sessions running in its classroom.
// Used by the student QR scanner when the scanned code points to a specific desk.
func GetDeskPublicHandler(c fiber.Ctx) error {
	deskID := c.Params("deskId")
	if deskID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "รหัสโต๊ะไม่ถูกต้อง"})
	}

	var desk models.Desk
	if err := config.DB.First(&desk, "id = ?", deskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบข้อมูลโต๊ะนี้"})
		}
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	var classroom models.Classroom
	if err := config.DB.First(&classroom, "id = ?", desk.ClassroomID).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	// Find active/paused queue sessions for this classroom
	type sessionRow struct {
		ID       string `gorm:"column:id"`
		Title    string `gorm:"column:title"`
		PinCode  string `gorm:"column:pin_code"`
		Status   string `gorm:"column:status"`
		CourseID string `gorm:"column:course_id"`
	}
	var sessionRows []sessionRow
	if err := config.DB.
		Model(&models.QueueSession{}).
		Select("id, title, pin_code, status, course_id").
		Where("classroom_id = ? AND status IN ?", desk.ClassroomID, []string{"active", "paused"}).
		Order("created_at DESC").
		Scan(&sessionRows).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	// Collect course IDs
	courseIDSet := map[string]struct{}{}
	for _, s := range sessionRows {
		courseIDSet[s.CourseID] = struct{}{}
	}
	courseIDs := make([]string, 0, len(courseIDSet))
	for cid := range courseIDSet {
		courseIDs = append(courseIDs, cid)
	}

	// Load courses
	type courseRow struct {
		ID   string `gorm:"column:id"`
		Code string `gorm:"column:code"`
		Name string `gorm:"column:name"`
	}
	courseMap := map[string]courseRow{}
	if len(courseIDs) > 0 {
		var courses []courseRow
		if err := config.DB.
			Model(&models.Course{}).
			Select("id, code, name").
			Where("id IN ?", courseIDs).
			Scan(&courses).Error; err == nil {
			for _, cr := range courses {
				courseMap[cr.ID] = cr
			}
		}
	}

	// Build session list
	activeSessions := make([]fiber.Map, 0, len(sessionRows))
	for _, s := range sessionRows {
		cr := courseMap[s.CourseID]
		activeSessions = append(activeSessions, fiber.Map{
			"id":       s.ID,
			"title":    s.Title,
			"pin_code": s.PinCode,
			"status":   s.Status,
			"course": fiber.Map{
				"id":   cr.ID,
				"code": cr.Code,
				"name": cr.Name,
			},
		})
	}

	deskTypeLabel := map[string]string{
		"computer": "คอมพิวเตอร์",
		"normal":   "โต๊ะทั่วไป",
		"teacher":  "โต๊ะอาจารย์",
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"desk": fiber.Map{
				"id":         desk.ID,
				"number":     desk.Number,
				"type":       desk.Type,
				"type_label": deskTypeLabel[desk.Type],
				"is_enabled": desk.IsEnabled,
				"hostname":   desk.Hostname,
				"ip_address": desk.IPAddress,
				"brand":      desk.Brand,
				"os":         desk.OS,
				"notes":      desk.Notes,
			},
			"classroom": fiber.Map{
				"id":       classroom.ID,
				"name":     classroom.Name,
				"building": classroom.Building,
				"floor":    classroom.Floor,
			},
			"active_sessions": activeSessions,
		},
	})
}
