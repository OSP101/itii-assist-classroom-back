package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"itii-assist/middlewares"
	"itii-assist/repositories"
	"itii-assist/services"

	"github.com/gofiber/fiber/v3"
)

const systemOperationsHistoryConfigKey = "system_operations_history_v1"
const maxSystemOperationsHistory = 100

var systemOperationsHistoryMu sync.Mutex

type systemOperationRecord struct {
	ID          string                 `json:"id"`
	Action      string                 `json:"action"`
	Target      string                 `json:"target"`
	Status      string                 `json:"status"`
	Reason      string                 `json:"reason"`
	RequestedBy uint                   `json:"requested_by"`
	RequestedAt time.Time              `json:"requested_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	DurationMs  int64                  `json:"duration_ms"`
	DryRun      bool                   `json:"dry_run"`
	Output      string                 `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

type restartServicePayload struct {
	Service string `json:"service"`
	Reason  string `json:"reason"`
	DryRun  bool   `json:"dry_run"`
	Force   bool   `json:"force"`
}

type rebootHostPayload struct {
	Reason       string `json:"reason"`
	DelaySeconds int    `json:"delay_seconds"`
	DryRun       bool   `json:"dry_run"`
	Force        bool   `json:"force"`
}

type cancelOperationPayload struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason"`
}

func operationsEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_SYSTEM_OPERATIONS")), "true")
}

func loadSystemOperationHistory() ([]systemOperationRecord, error) {
	raw, err := repositories.GetAppConfigValue(systemOperationsHistoryConfigKey)
	if err != nil {
		return []systemOperationRecord{}, nil
	}
	if strings.TrimSpace(raw) == "" {
		return []systemOperationRecord{}, nil
	}

	var records []systemOperationRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return []systemOperationRecord{}, err
	}
	return records, nil
}

func saveSystemOperationHistory(records []systemOperationRecord) error {
	if len(records) > maxSystemOperationsHistory {
		records = records[:maxSystemOperationsHistory]
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return repositories.SetAppConfigValue(systemOperationsHistoryConfigKey, string(raw))
}

func appendSystemOperationHistory(record systemOperationRecord) {
	systemOperationsHistoryMu.Lock()
	defer systemOperationsHistoryMu.Unlock()

	records, err := loadSystemOperationHistory()
	if err != nil {
		records = []systemOperationRecord{}
	}
	records = append([]systemOperationRecord{record}, records...)
	_ = saveSystemOperationHistory(records)
}

func updateSystemOperationRecord(operationID string, mutate func(*systemOperationRecord) error) (systemOperationRecord, error) {
	systemOperationsHistoryMu.Lock()
	defer systemOperationsHistoryMu.Unlock()

	records, err := loadSystemOperationHistory()
	if err != nil {
		return systemOperationRecord{}, err
	}

	for idx := range records {
		if records[idx].ID != operationID {
			continue
		}
		if err := mutate(&records[idx]); err != nil {
			return systemOperationRecord{}, err
		}
		if err := saveSystemOperationHistory(records); err != nil {
			return systemOperationRecord{}, err
		}
		return records[idx], nil
	}

	return systemOperationRecord{}, errors.New("operation not found")
}

func readAllowedServices() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("OPS_ALLOWED_SERVICES"))
	if raw == "" {
		raw = "api,frontend,database"
	}

	allowed := map[string]struct{}{}
	for _, value := range strings.Split(raw, ",") {
		service := normalizeServiceName(value)
		if service != "" {
			allowed[service] = struct{}{}
		}
	}
	return allowed
}

func normalizeServiceName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return ""
	}
	return value
}

func ensureOperationsPreflight(force bool) error {
	if services.IsBackupOperationRunning() {
		return errors.New("cannot run operation while backup is running")
	}
	if force {
		return nil
	}

	cfg, err := repositories.GetMaintenanceModeConfig()
	if err != nil {
		return errors.New("maintenance mode must be enabled before running this action")
	}
	if !repositories.IsMaintenanceActive(cfg) {
		return errors.New("maintenance mode must be enabled before running this action")
	}

	return nil
}

func runOperationCommand(timeout time.Duration, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	outputText := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		if outputText == "" {
			outputText = "operation timed out"
		}
		return outputText, errors.New(outputText)
	}
	if err != nil {
		if outputText == "" {
			outputText = err.Error()
		}
		return outputText, errors.New(outputText)
	}
	return outputText, nil
}

func buildRestartCommand(service string) (string, []string) {
	custom := strings.TrimSpace(os.Getenv("OPS_RESTART_COMMAND"))
	if custom != "" {
		parts := strings.Fields(custom)
		if len(parts) > 0 {
			return parts[0], append(parts[1:], service)
		}
	}

	return "docker", []string{"compose", "restart", service}
}

func buildRebootCommand(delaySeconds int) (string, []string) {
	if delaySeconds < 0 {
		delaySeconds = 0
	}

	if runtime.GOOS == "windows" {
		return "shutdown", []string{"/r", "/t", strconv.Itoa(delaySeconds)}
	}

	delayMinutes := delaySeconds / 60
	if delaySeconds%60 != 0 {
		delayMinutes++
	}
	if delayMinutes < 0 {
		delayMinutes = 0
	}
	return "shutdown", []string{"-r", fmt.Sprintf("+%d", delayMinutes)}
}

func buildCancelRebootCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "shutdown", []string{"/a"}
	}
	return "shutdown", []string{"-c"}
}

func readMetaInt(meta map[string]interface{}, key string) int {
	if meta == nil {
		return 0
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return 0
	}

	switch value := raw.(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}

	return 0
}

func GetSystemOperationHistoryHandler(c fiber.Ctx) error {
	records, err := loadSystemOperationHistory()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "ไม่สามารถอ่านประวัติการปฏิบัติการระบบได้"})
	}
	return c.JSON(fiber.Map{"success": true, "data": records})
}

func RestartSystemServiceHandler(c fiber.Ctx) error {
	var payload restartServicePayload
	if err := c.Bind().Body(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	payload.Service = normalizeServiceName(payload.Service)
	payload.Reason = strings.TrimSpace(payload.Reason)
	if payload.Service == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "service is required"})
	}
	if len(payload.Reason) < 5 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "reason must be at least 5 characters"})
	}

	allowed := readAllowedServices()
	if _, ok := allowed[payload.Service]; !ok {
		return c.Status(403).JSON(fiber.Map{"success": false, "message": "service is not allowed"})
	}

	if err := ensureOperationsPreflight(payload.Force); err != nil {
		return c.Status(409).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if !operationsEnabled() && !payload.DryRun {
		return c.Status(503).JSON(fiber.Map{"success": false, "code": "OPERATIONS_DISABLED", "message": "operations are disabled in this environment"})
	}

	startedAt := time.Now().UTC()
	record := systemOperationRecord{
		ID:          fmt.Sprintf("op_%d", startedAt.UnixNano()),
		Action:      "restart_service",
		Target:      payload.Service,
		Status:      "success",
		Reason:      payload.Reason,
		RequestedBy: actorID,
		RequestedAt: startedAt,
		DryRun:      payload.DryRun,
		Meta: map[string]interface{}{
			"force": payload.Force,
		},
	}

	output := "dry-run mode"
	if !payload.DryRun {
		command, args := buildRestartCommand(payload.Service)
		execOutput, execErr := runOperationCommand(20*time.Second, command, args...)
		output = execOutput
		if execErr != nil {
			record.Status = "failed"
			record.Error = execErr.Error()
		}
	}

	completedAt := time.Now().UTC()
	record.CompletedAt = &completedAt
	record.DurationMs = completedAt.Sub(startedAt).Milliseconds()
	record.Output = output

	appendSystemOperationHistory(record)
	logPrivilegedAdminAction(c, actorID, "restart_system_service", "critical", "system_operations", payload.Service, fiber.Map{
		"reason":  payload.Reason,
		"dry_run": payload.DryRun,
		"force":   payload.Force,
		"status":  record.Status,
	})

	statusCode := 200
	if record.Status != "success" {
		statusCode = 500
	}
	return c.Status(statusCode).JSON(fiber.Map{"success": record.Status == "success", "data": record})
}

func RebootSystemHostHandler(c fiber.Ctx) error {
	var payload rebootHostPayload
	if err := c.Bind().Body(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	payload.Reason = strings.TrimSpace(payload.Reason)
	if len(payload.Reason) < 5 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "reason must be at least 5 characters"})
	}
	if payload.DelaySeconds < 0 {
		payload.DelaySeconds = 0
	}
	if payload.DelaySeconds > 3600 {
		payload.DelaySeconds = 3600
	}

	if err := ensureOperationsPreflight(payload.Force); err != nil {
		return c.Status(409).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if !operationsEnabled() && !payload.DryRun {
		return c.Status(503).JSON(fiber.Map{"success": false, "code": "OPERATIONS_DISABLED", "message": "operations are disabled in this environment"})
	}

	startedAt := time.Now().UTC()
	record := systemOperationRecord{
		ID:          fmt.Sprintf("op_%d", startedAt.UnixNano()),
		Action:      "reboot_host",
		Target:      "host",
		Status:      "success",
		Reason:      payload.Reason,
		RequestedBy: actorID,
		RequestedAt: startedAt,
		DryRun:      payload.DryRun,
		Meta: map[string]interface{}{
			"delay_seconds": payload.DelaySeconds,
			"force":         payload.Force,
		},
	}

	output := "dry-run mode"
	if !payload.DryRun {
		command, args := buildRebootCommand(payload.DelaySeconds)
		execOutput, execErr := runOperationCommand(10*time.Second, command, args...)
		output = execOutput
		if execErr != nil {
			record.Status = "failed"
			record.Error = execErr.Error()
		}
	}

	completedAt := time.Now().UTC()
	record.CompletedAt = &completedAt
	record.DurationMs = completedAt.Sub(startedAt).Milliseconds()
	record.Output = output

	appendSystemOperationHistory(record)
	logPrivilegedAdminAction(c, actorID, "reboot_system_host", "critical", "system_operations", "host", fiber.Map{
		"reason":        payload.Reason,
		"delay_seconds": payload.DelaySeconds,
		"dry_run":       payload.DryRun,
		"force":         payload.Force,
		"status":        record.Status,
	})

	statusCode := 200
	if record.Status != "success" {
		statusCode = 500
	}
	return c.Status(statusCode).JSON(fiber.Map{"success": record.Status == "success", "data": record})
}

func CancelSystemOperationHandler(c fiber.Ctx) error {
	var payload cancelOperationPayload
	if err := c.Bind().Body(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "ข้อมูลไม่ถูกต้อง"})
	}

	actorID, ok := middlewares.GetUserID(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}

	payload.OperationID = strings.TrimSpace(payload.OperationID)
	payload.Reason = strings.TrimSpace(payload.Reason)
	if payload.OperationID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "operation_id is required"})
	}
	if len(payload.Reason) < 5 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "reason must be at least 5 characters"})
	}

	updatedRecord, err := updateSystemOperationRecord(payload.OperationID, func(record *systemOperationRecord) error {
		if record.Action != "reboot_host" {
			return errors.New("only reboot_host operation can be cancelled")
		}
		if record.Status == "cancelled" {
			return nil
		}
		if record.Status != "success" {
			return errors.New("operation is not cancellable")
		}

		delaySeconds := readMetaInt(record.Meta, "delay_seconds")
		if !record.DryRun {
			if delaySeconds <= 0 {
				return errors.New("operation cancel window has already elapsed")
			}

			cancelDeadline := record.RequestedAt.Add(time.Duration(delaySeconds) * time.Second)
			if time.Now().UTC().After(cancelDeadline) {
				return errors.New("operation cancel window has already elapsed")
			}
		}

		output := "dry-run cancellation"
		if !record.DryRun {
			command, args := buildCancelRebootCommand()
			execOutput, execErr := runOperationCommand(6*time.Second, command, args...)
			output = execOutput
			if execErr != nil {
				return execErr
			}
		}

		now := time.Now().UTC()
		record.Status = "cancelled"
		record.CompletedAt = &now
		record.DurationMs = now.Sub(record.RequestedAt).Milliseconds()
		record.Output = strings.TrimSpace(strings.Join([]string{record.Output, output}, "\n"))
		record.Error = ""
		if record.Meta == nil {
			record.Meta = map[string]interface{}{}
		}
		record.Meta["cancelled_by"] = actorID
		record.Meta["cancel_reason"] = payload.Reason
		record.Meta["cancelled_at"] = now.Format(time.RFC3339)
		return nil
	})
	if err != nil {
		status := 409
		if strings.Contains(err.Error(), "not found") {
			status = 404
		}
		return c.Status(status).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	logPrivilegedAdminAction(c, actorID, "cancel_system_operation", "critical", "system_operations", payload.OperationID, fiber.Map{
		"reason": payload.Reason,
		"status": updatedRecord.Status,
	})

	return c.JSON(fiber.Map{"success": true, "data": updatedRecord})
}
