package handlers

import (
	"encoding/json"
	"strconv"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

const adminPrivilegedAuditScope = "admin_privileged"

func makeAdminAuditDetail(riskLevel string, detail fiber.Map) datatypes.JSON {
	payload := fiber.Map{
		"audit_scope":       adminPrivilegedAuditScope,
		"privileged_action": true,
		"risk_level":        riskLevel,
	}
	for key, value := range detail {
		payload[key] = value
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(raw)
}

func logPrivilegedAdminAction(c fiber.Ctx, actorID uint, action string, severity string, resourceType string, resourceID string, detail fiber.Map) {
	deviceType, browser, osName := utils.ParseUserAgent(c.Get("User-Agent"))
	logEntry := models.SystemLog{
		LogType:      "security",
		Severity:     severity,
		ActorUserID:  &actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		IPAddress:    c.IP(),
		UserAgent:    c.Get("User-Agent"),
		DeviceType:   deviceType,
		Browser:      browser,
		OS:           osName,
		HTTPMethod:   c.Method(),
		URL:          c.OriginalURL(),
		Detail:       makeAdminAuditDetail(severity, detail),
	}
	_ = config.DB.Create(&logEntry).Error
}

func userSnapshotForAudit(user *models.User) fiber.Map {
	if user == nil {
		return fiber.Map{}
	}

	return fiber.Map{
		"id":        user.ID,
		"username":  user.Username,
		"full_name": user.FullName,
		"email":     user.Email,
		"role":      user.Role,
		"is_active": user.IsActive,
	}
}

func userIDString(userID uint) string {
	return strconv.FormatUint(uint64(userID), 10)
}
