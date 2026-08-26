package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

func isPrivilegedOnlyFilterEnabled(value string) bool {
	switch value {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func attachActorUserToLog(log models.SystemLog) fiber.Map {
	raw, _ := json.Marshal(log)
	data := fiber.Map{}
	_ = json.Unmarshal(raw, &data)

	if log.ActorUserID != nil {
		user, err := repositories.FindUserByID(*log.ActorUserID)
		if err == nil {
			data["actor_user"] = fiber.Map{
				"id":        user.ID,
				"email":     user.Email,
				"full_name": user.FullName,
				"role":      user.Role,
			}
		}
		return data
	}

	// Student logins carry no ActorUserID (students live outside the users table);
	// the actor identity is embedded in Detail instead, keyed by student_id.
	var detail map[string]any
	if err := json.Unmarshal(log.Detail, &detail); err == nil {
		if rawID, ok := detail["student_id"]; ok {
			var studentID uint
			switch v := rawID.(type) {
			case float64:
				studentID = uint(v)
			case string:
				if n, err := strconv.ParseUint(v, 10, 64); err == nil {
					studentID = uint(n)
				}
			}
			if studentID > 0 {
				var student models.Student
				if err := config.DB.Select("id", "student_id", "full_name", "email").First(&student, studentID).Error; err == nil {
					data["actor_student"] = fiber.Map{
						"id":         student.ID,
						"student_no": student.StudentID,
						"full_name":  student.FullName,
						"email":      student.Email,
					}
				}
			}
		}
	}

	return data
}

// =============================================================================
// GET /api/logs
// =============================================================================

func GetLogsHandler(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))

	result, err := repositories.GetLogs(repositories.SystemLogListParams{
		LogType:        c.Query("log_type"),
		Severity:       c.Query("severity"),
		UserID:         c.Query("user_id"),
		ActionGroup:    c.Query("action_group"),
		PrivilegedOnly: isPrivilegedOnlyFilterEnabled(c.Query("privileged_only")),
		StartDate:      c.Query("start_date"),
		EndDate:        c.Query("end_date"),
		Search:         c.Query("search"),
		Page:           page,
		Limit:          limit,
		SortBy:         c.Query("sort_by", "created_at"),
		SortOrder:      c.Query("sort_order", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงข้อมูลบันทึกระบบไม่สำเร็จ"})
	}

	logs := make([]fiber.Map, len(result.Logs))
	for i, item := range result.Logs {
		logs[i] = attachActorUserToLog(item)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"logs": logs,
			"pagination": fiber.Map{
				"total":      result.Total,
				"page":       result.Page,
				"limit":      result.Limit,
				"totalPages": result.TotalPage,
			},
		},
	})
}

// =============================================================================
// GET /api/logs/:id
// =============================================================================

func GetLogByIDHandler(c fiber.Ctx) error {
	id := c.Params("id")

	log, err := repositories.GetLogByID(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "ไม่พบบันทึกระบบ"})
	}

	return c.JSON(fiber.Map{"success": true, "data": attachActorUserToLog(*log)})
}

func GetLogStatsHandler(c fiber.Ctx) error {
	stats, err := repositories.GetSystemLogStats(c.Query("start_date"), c.Query("end_date"), isPrivilegedOnlyFilterEnabled(c.Query("privileged_only")))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึงสถิติ log ไม่สำเร็จ"})
	}
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

func GetLogsTimelineHandler(c fiber.Ctx) error {
	timeline, err := repositories.GetSystemLogTimeline(
		c.Query("start_date"),
		c.Query("end_date"),
		c.Query("interval", "hour"),
		c.Query("log_type"),
		isPrivilegedOnlyFilterEnabled(c.Query("privileged_only")),
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึง timeline log ไม่สำเร็จ"})
	}

	endDate := c.Query("end_date")
	if endDate == "" {
		endDate = time.Now().Format(time.RFC3339)
	}
	startDate := c.Query("start_date")
	if startDate == "" {
		startDate = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"timeline":  timeline,
			"interval":  c.Query("interval", "hour"),
			"startDate": startDate,
			"endDate":   endDate,
		},
	})
}

func GetLogFiltersHandler(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "data": repositories.GetSystemLogFilterOptions()})
}

func ExportLogsHandler(c fiber.Ctx) error {
	result, err := repositories.GetLogs(repositories.SystemLogListParams{
		LogType:        c.Query("log_type"),
		Severity:       c.Query("severity"),
		UserID:         c.Query("user_id"),
		ActionGroup:    c.Query("action_group"),
		PrivilegedOnly: isPrivilegedOnlyFilterEnabled(c.Query("privileged_only")),
		StartDate:      c.Query("start_date"),
		EndDate:        c.Query("end_date"),
		Search:         c.Query("search"),
		Page:           1,
		Limit:          10000,
		SortBy:         c.Query("sort_by", "created_at"),
		SortOrder:      c.Query("sort_order", "desc"),
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ส่งออก log ไม่สำเร็จ"})
	}

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	_ = writer.Write([]string{"ID", "Log Type", "Severity", "Action", "HTTP Method", "URL", "Status Code", "Response Time (ms)", "IP Address", "User", "User Agent", "Browser", "OS", "Error Message", "Created At"})
	for _, item := range result.Logs {
		userLabel := ""
		if item.ActorUserID != nil {
			if user, err := repositories.FindUserByID(*item.ActorUserID); err == nil {
				userLabel = fmt.Sprintf("%s (%s)", user.FullName, user.Email)
			}
		} else {
			var detail map[string]any
			if err := json.Unmarshal(item.Detail, &detail); err == nil {
				if rawID, ok := detail["student_id"]; ok {
					var studentID uint
					if v, ok := rawID.(float64); ok {
						studentID = uint(v)
					}
					if studentID > 0 {
						var student models.Student
						if err := config.DB.Select("id", "student_id", "full_name", "email").First(&student, studentID).Error; err == nil {
							userLabel = fmt.Sprintf("%s (%s) [นักศึกษา]", student.FullName, student.Email)
						}
					}
				}
			}
		}
		statusCode := ""
		if item.StatusCode != nil {
			statusCode = strconv.Itoa(*item.StatusCode)
		}
		responseTime := ""
		if item.ResponseTimeMs != nil {
			responseTime = strconv.Itoa(*item.ResponseTimeMs)
		}
		_ = writer.Write([]string{
			strconv.FormatUint(uint64(item.ID), 10),
			item.LogType,
			item.Severity,
			item.Action,
			item.HTTPMethod,
			item.URL,
			statusCode,
			responseTime,
			item.IPAddress,
			userLabel,
			item.UserAgent,
			item.Browser,
			item.OS,
			item.ErrorMessage,
			item.CreatedAt.Format(time.RFC3339),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ส่งออก CSV ไม่สำเร็จ"})
	}

	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=system_logs_%s.csv", time.Now().Format("2006-01-02")))
	return c.SendString("\uFEFF" + buffer.String())
}

func GetRecentErrorsHandler(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	logs, err := repositories.GetRecentSystemLogs("error", limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึง error logs ไม่สำเร็จ"})
	}

	items := make([]fiber.Map, len(logs))
	for i, item := range logs {
		items[i] = attachActorUserToLog(item)
	}
	return c.JSON(fiber.Map{"success": true, "data": items})
}

func GetRecentSecurityEventsHandler(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	logs, err := repositories.GetRecentSystemLogs("security", limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ดึง security logs ไม่สำเร็จ"})
	}

	items := make([]fiber.Map, len(logs))
	for i, item := range logs {
		items[i] = attachActorUserToLog(item)
	}
	return c.JSON(fiber.Map{"success": true, "data": items})
}

func CleanupLogsHandler(c fiber.Ctx) error {
	var input struct {
		RetentionDays int `json:"retention_days"`
	}
	if err := c.Bind().JSON(&input); err != nil && c.Body() != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	result, err := repositories.CleanupOldSystemLogs(input.RetentionDays)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ลบ log เก่าไม่สำเร็จ"})
	}

	if userID, ok := c.Locals("user_id").(uint); ok {
		config.DB.Create(&models.SystemLog{
			LogType:     "security",
			Severity:    "info",
			ActorUserID: &userID,
			Action:      "admin_log_cleanup",
			IPAddress:   c.IP(),
			UserAgent:   c.Get("User-Agent"),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    result,
		"message": fmt.Sprintf("Deleted %d logs older than %d days", result.DeletedCount, result.RetentionDays),
	})
}
