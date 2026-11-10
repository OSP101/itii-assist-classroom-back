package handlers

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"itii-assist/config"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func TestScoreEvidenceServesAuthorizedPrivateAndLegacyFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previousDB := config.DB
	config.DB = db
	t.Cleanup(func() { config.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	for _, sql := range []string{
		`CREATE TABLE assignments (id INTEGER PRIMARY KEY, course_id TEXT)`,
		`CREATE TABLE scores (id INTEGER PRIMARY KEY, assignment_id INTEGER)`,
		`CREATE TABLE score_edit_requests (id INTEGER PRIMARY KEY, score_id INTEGER, requested_by INTEGER, images TEXT)`,
		`INSERT INTO assignments VALUES (1, 'course-a')`,
		`INSERT INTO scores VALUES (1, 1)`,
		`INSERT INTO score_edit_requests VALUES (1, 1, 7, '["/api/uploads/score-edit-requests/evidence_private_123.jpg","/api/uploads/score-edit-requests/evidence_legacy_123.jpg"]')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"uploads/private/score-edit-requests/evidence_private_123.jpg", "uploads/score-edit-requests/evidence_legacy_123.jpg"} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	app.Get("/api/uploads/score-edit-requests/:file", func(c fiber.Ctx) error {
		c.Locals("user_id", uint(7))
		c.Locals("user_role", "admin")
		return ServeScoreEditEvidenceHandler(c)
	})
	for _, file := range []string{"evidence_private_123.jpg", "evidence_legacy_123.jpg"} {
		response, err := app.Test(httptest.NewRequest("GET", "/api/uploads/score-edit-requests/"+file, nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || string(body) != "evidence" {
			t.Fatalf("authorized evidence %s: status=%d body=%s", file, response.StatusCode, body)
		}
		if response.Header.Get("Cache-Control") != "private, no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("unsafe evidence headers: %v", response.Header)
		}
	}
}
