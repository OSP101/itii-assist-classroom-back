package handlers

import (
	"encoding/json"
	"itii-assist/config"
	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
)

var scoreEvidenceName = regexp.MustCompile(`^[A-Za-z0-9_-]{10,100}\.(jpg|jpeg|png|gif|webp)$`)

// Keep existing URLs working, but authorize the request before opening either
// a new private file or a legacy file in the old public directory.
func ServeScoreEditEvidenceHandler(c fiber.Ctx) error {
	file := c.Params("file")
	if !scoreEvidenceName.MatchString(file) {
		return fiber.ErrNotFound
	}
	uid, ok := middlewares.GetUserID(c)
	if !ok {
		return fiber.ErrForbidden
	}
	var requests []models.ScoreEditRequest
	pattern := "%" + strings.ReplaceAll(file, "_", "!_") + "%"
	if err := config.DB.Where("CAST(images AS TEXT) LIKE ? ESCAPE '!'", pattern).Find(&requests).Error; err != nil {
		return fiber.ErrInternalServerError
	}
	allowed := false
	for _, request := range requests {
		var images []string
		if json.Unmarshal(request.Images, &images) != nil {
			continue
		}
		matched := false
		for _, image := range images {
			path := strings.TrimPrefix(image, "/")
			if path == "api/uploads/score-edit-requests/"+file {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		courseID, err := repositories.GetCourseIDByScoreID(request.ScoreID)
		if err != nil {
			continue
		}
		if queueStaffAllowed(c, courseID, repositories.PermissionReviewAllScoreRequests) {
			allowed = true
			break
		}
		if uid == request.RequestedBy {
			_, member, err := repositories.GetCourseAccessState(courseID, uid, "instructor", "ta")
			if err == nil && member {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return fiber.ErrNotFound
	}
	path := filepath.Join("uploads", "private", "score-edit-requests", file)
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join("uploads", "score-edit-requests", file)
	}
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	return c.SendFile(path)
}
