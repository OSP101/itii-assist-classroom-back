package middlewares

import (
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/repositories"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type CourseAccessResolver func(c fiber.Ctx) ([]string, error)

type courseAccessError struct {
	status  int
	message string
}

func (err *courseAccessError) Error() string {
	return err.message
}

func newCourseAccessError(status int, message string) error {
	return &courseAccessError{status: status, message: message}
}

func RequireCourseAccess(resolver CourseAccessResolver, allowedCourseRoles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		courseIDs, err := resolver(c)
		if err != nil {
			return writeCourseAccessError(c, err)
		}

		userRole, ok := GetUserRole(c)
		if !ok {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "ไม่สามารถตรวจสอบสิทธิ์ได้"})
		}

		if userRole == "admin" {
			return c.Next()
		}

		userID, ok := GetUserID(c)
		if !ok {
			return c.Status(403).JSON(fiber.Map{"success": false, "message": "ไม่สามารถตรวจสอบสิทธิ์ได้"})
		}

		for _, courseID := range uniqueCourseIDs(courseIDs) {
			courseExists, allowed, err := repositories.GetCourseAccessState(courseID, userID, allowedCourseRoles...)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course access"})
			}
			if !courseExists {
				return c.Status(404).JSON(fiber.Map{"success": false, "message": "Course not found"})
			}
			if !allowed {
				return c.Status(403).JSON(fiber.Map{"success": false, "message": "คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้"})
			}
		}

		return c.Next()
	}
}

func writeCourseAccessError(c fiber.Ctx, err error) error {
	var courseErr *courseAccessError
	if errors.As(err, &courseErr) {
		return c.Status(courseErr.status).JSON(fiber.Map{"success": false, "message": courseErr.message})
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Resource not found"})
	}
	return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to validate course access"})
}

func uniqueCourseIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func isFormRequest(c fiber.Ctx) bool {
	contentType := strings.ToLower(strings.TrimSpace(c.Get("Content-Type")))
	return strings.HasPrefix(contentType, "multipart/form-data") || strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

func rawMessageToString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	return strings.Trim(strings.TrimSpace(trimmed), `"`)
}

func rawMessageToUint(raw json.RawMessage) (uint, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, nil
	}

	var asUint uint
	if err := json.Unmarshal(raw, &asUint); err == nil {
		return asUint, nil
	}

	value := rawMessageToString(raw)
	if value == "" {
		return 0, nil
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}

func rawMessageToUintSlice(raw json.RawMessage) ([]uint, error) {
	var values []uint
	if err := json.Unmarshal(raw, &values); err == nil {
		return uniqueUintValues(values), nil
	}

	var stringValues []string
	if err := json.Unmarshal(raw, &stringValues); err == nil {
		values = make([]uint, 0, len(stringValues))
		for _, value := range stringValues {
			parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return nil, err
			}
			values = append(values, uint(parsed))
		}
		return uniqueUintValues(values), nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	values = make([]uint, 0, len(items))
	for _, item := range items {
		value, err := rawMessageToUint(item)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	return uniqueUintValues(values), nil
}

func uniqueUintValues(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func jsonBody(c fiber.Ctx) (map[string]json.RawMessage, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func requiredStringField(c fiber.Ctx, field string) (string, error) {
	if isFormRequest(c) {
		value := strings.TrimSpace(c.FormValue(field))
		if value == "" {
			return "", newCourseAccessError(400, field+" required")
		}
		return value, nil
	}

	payload, err := jsonBody(c)
	if err != nil {
		return "", newCourseAccessError(400, "Invalid input")
	}
	raw, exists := payload[field]
	if !exists {
		return "", newCourseAccessError(400, field+" required")
	}
	value := rawMessageToString(raw)
	if value == "" {
		return "", newCourseAccessError(400, field+" required")
	}
	return value, nil
}

func requiredUintField(c fiber.Ctx, field string) (uint, error) {
	if isFormRequest(c) {
		value := strings.TrimSpace(c.FormValue(field))
		if value == "" {
			return 0, newCourseAccessError(400, field+" required")
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return 0, newCourseAccessError(400, field+" required")
		}
		return uint(parsed), nil
	}

	payload, err := jsonBody(c)
	if err != nil {
		return 0, newCourseAccessError(400, "Invalid input")
	}
	raw, exists := payload[field]
	if !exists {
		return 0, newCourseAccessError(400, field+" required")
	}
	parsed, err := rawMessageToUint(raw)
	if err != nil || parsed == 0 {
		return 0, newCourseAccessError(400, field+" required")
	}
	return parsed, nil
}

func requiredUintQuery(c fiber.Ctx, field string) (uint, error) {
	value := strings.TrimSpace(c.Query(field))
	if value == "" {
		return 0, newCourseAccessError(400, field+" required")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, newCourseAccessError(400, field+" required")
	}
	return uint(parsed), nil
}

func requiredUintSliceField(c fiber.Ctx, field string) ([]uint, error) {
	if isFormRequest(c) {
		value := strings.TrimSpace(c.FormValue(field))
		if value == "" {
			return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
		}
		parsed, err := rawMessageToUintSlice(json.RawMessage(value))
		if err != nil || len(parsed) == 0 {
			return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
		}
		return parsed, nil
	}

	payload, err := jsonBody(c)
	if err != nil {
		return nil, newCourseAccessError(400, "Invalid input")
	}
	raw, exists := payload[field]
	if !exists {
		return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
	}
	parsed, err := rawMessageToUintSlice(raw)
	if err != nil || len(parsed) == 0 {
		return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
	}
	return parsed, nil
}

func CourseIDFromParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		courseID := strings.TrimSpace(c.Params(param))
		if courseID == "" {
			return nil, newCourseAccessError(400, "course_id required")
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromQuery(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		courseID := strings.TrimSpace(c.Query(param))
		if courseID == "" {
			return nil, newCourseAccessError(400, param+" required")
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		courseID, err := requiredStringField(c, field)
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromAssignmentParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		assignmentID, err := strconv.ParseUint(strings.TrimSpace(c.Params(param)), 10, 64)
		if err != nil {
			return nil, newCourseAccessError(400, "Invalid assignment ID")
		}
		courseID, err := repositories.GetCourseIDByAssignmentID(uint(assignmentID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Assignment not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromAssignmentQuery(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		assignmentID, err := requiredUintQuery(c, param)
		if err != nil {
			return nil, err
		}
		courseID, err := repositories.GetCourseIDByAssignmentID(assignmentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Assignment not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromAssignmentBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		assignmentID, err := requiredUintField(c, field)
		if err != nil {
			return nil, err
		}
		courseID, err := repositories.GetCourseIDByAssignmentID(assignmentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Assignment not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromAttendanceSessionParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		sessionID, err := strconv.ParseUint(strings.TrimSpace(c.Params(param)), 10, 64)
		if err != nil {
			return nil, newCourseAccessError(400, "Invalid session ID")
		}
		courseID, err := repositories.GetCourseIDByAttendanceSessionID(uint(sessionID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Attendance session not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromScoreBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		scoreID, err := requiredUintField(c, field)
		if err != nil {
			return nil, err
		}
		courseID, err := repositories.GetCourseIDByScoreID(scoreID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Score not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDsFromScoreBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		scoreIDs, err := requiredUintSliceField(c, field)
		if err != nil {
			return nil, err
		}
		courseIDs, err := repositories.GetCourseIDsByScoreIDs(scoreIDs)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Score not found")
		}
		return courseIDs, err
	}
}

func CourseIDsFromScoreEditBatchDetailedBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		type editPayload struct {
			ScoreID uint `json:"score_id"`
		}

		edits := make([]editPayload, 0)
		contentType := strings.ToLower(c.Get("Content-Type"))
		if strings.HasPrefix(contentType, "multipart/form-data") {
			raw := strings.TrimSpace(c.FormValue(field))
			if raw == "" {
				return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
			}
			if err := json.Unmarshal([]byte(raw), &edits); err != nil {
				return nil, newCourseAccessError(400, fmt.Sprintf("Invalid %s format", field))
			}
		} else {
			var body map[string]json.RawMessage
			if err := c.Bind().JSON(&body); err != nil {
				return nil, newCourseAccessError(400, "Invalid request body")
			}
			raw, ok := body[field]
			if !ok {
				return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
			}
			if err := json.Unmarshal(raw, &edits); err != nil {
				return nil, newCourseAccessError(400, fmt.Sprintf("Invalid %s format", field))
			}
		}

		if len(edits) == 0 {
			return nil, newCourseAccessError(400, fmt.Sprintf("%s array is required", field))
		}

		scoreIDs := make([]uint, 0, len(edits))
		seen := map[uint]struct{}{}
		for _, edit := range edits {
			if edit.ScoreID == 0 {
				return nil, newCourseAccessError(400, "score_id is required")
			}
			if _, exists := seen[edit.ScoreID]; exists {
				continue
			}
			seen[edit.ScoreID] = struct{}{}
			scoreIDs = append(scoreIDs, edit.ScoreID)
		}

		courseIDs, err := repositories.GetCourseIDsByScoreIDs(scoreIDs)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Score not found")
		}
		return courseIDs, err
	}
}

func CourseIDFromScoreEditRequestParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		requestID, err := strconv.ParseUint(strings.TrimSpace(c.Params(param)), 10, 64)
		if err != nil {
			return nil, newCourseAccessError(400, "Invalid ID")
		}
		courseID, err := repositories.GetCourseIDByScoreEditRequestID(uint(requestID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Edit request not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDsFromScoreEditRequestBody(field string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		requestIDs, err := requiredUintSliceField(c, field)
		if err != nil {
			return nil, err
		}
		courseIDs, err := repositories.GetCourseIDsByScoreEditRequestIDs(requestIDs)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Edit request not found")
		}
		return courseIDs, err
	}
}

func CourseIDFromBonusScoreParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		bonusScoreID, err := strconv.ParseUint(strings.TrimSpace(c.Params(param)), 10, 64)
		if err != nil {
			return nil, newCourseAccessError(400, "Invalid bonus score ID")
		}
		courseID, err := repositories.GetCourseIDByBonusScoreID(uint(bonusScoreID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Bonus score not found")
		}
		if err != nil {
			return nil, err
		}
		return []string{courseID}, nil
	}
}

func CourseIDFromQueueSessionParam(param string) CourseAccessResolver {
	return func(c fiber.Ctx) ([]string, error) {
		sessionID := strings.TrimSpace(c.Params(param))
		if sessionID == "" {
			return nil, newCourseAccessError(400, "Queue session ID is required")
		}
		courseID, err := repositories.GetCourseIDByQueueSessionID(sessionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newCourseAccessError(404, "Queue session not found")
		}
		if err != nil {
			return nil, err
		}
		if scopedCourseID := strings.TrimSpace(c.Params("courseId")); scopedCourseID != "" && scopedCourseID != courseID {
			return nil, newCourseAccessError(404, "Queue session not found")
		}
		return []string{courseID}, nil
	}
}
