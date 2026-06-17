package handlers

import (
	"encoding/json"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

func recordAuthLoginSystemLog(c fiber.Ctx, actorUserID *uint, authMethod string, detail map[string]any) {
	detailJSON, _ := json.Marshal(detail)
	dt, br, osn := utils.ParseUserAgent(c.Get("User-Agent"))

	_ = config.DB.Create(&models.SystemLog{
		LogType:     "auth",
		Severity:    "info",
		ActorUserID: actorUserID,
		Action:      "auth.login.success",
		AuthMethod:  authMethod,
		Detail:      datatypes.JSON(detailJSON),
		IPAddress:   c.IP(),
		UserAgent:   c.Get("User-Agent"),
		DeviceType:  dt,
		Browser:     br,
		OS:          osn,
		CreatedAt:   time.Now(),
	}).Error
}
