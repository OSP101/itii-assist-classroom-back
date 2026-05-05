package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestParseBatchDetailedScoreEditRequestInput_JSONSuccess(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		edits, reason, _, _, err := parseBatchDetailedScoreEditRequestInput(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
		}

		return c.JSON(fiber.Map{
			"success":         true,
			"count":           len(edits),
			"reason":          reason,
			"first_score_id":  edits[0].ScoreID,
			"first_new_score": edits[0].NewScore,
		})
	})

	payload := map[string]any{
		"edits": []map[string]any{
			{"score_id": 101, "new_score": 9.5},
			{"score_id": 202, "new_score": 7},
		},
		"reason": "ปรับคะแนนตาม rubric ใหม่",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Success       bool    `json:"success"`
		Count         int     `json:"count"`
		Reason        string  `json:"reason"`
		FirstScoreID  uint    `json:"first_score_id"`
		FirstNewScore float64 `json:"first_new_score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected success=true")
	}
	if result.Count != 2 {
		t.Fatalf("expected count=2, got %d", result.Count)
	}
	if result.Reason != "ปรับคะแนนตาม rubric ใหม่" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
	if result.FirstScoreID != 101 {
		t.Fatalf("expected first_score_id=101, got %d", result.FirstScoreID)
	}
	if result.FirstNewScore != 9.5 {
		t.Fatalf("expected first_new_score=9.5, got %v", result.FirstNewScore)
	}
}

func TestParseBatchDetailedScoreEditRequestInput_JSONEmptyEdits(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		_, _, _, _, err := parseBatchDetailedScoreEditRequestInput(c)
		if err != nil {
			return c.Status(400).SendString(err.Error())
		}
		return c.SendStatus(200)
	})

	payload := `{"edits":[],"reason":"x"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestUniqueStrings(t *testing.T) {
	input := []string{"A", "B", "A", "C", "B", "D"}
	result := uniqueStrings(input)

	expected := []string{"A", "B", "C", "D"}
	if len(result) != len(expected) {
		t.Fatalf("expected len=%d, got len=%d", len(expected), len(result))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Fatalf("expected result[%d]=%s, got %s", i, expected[i], result[i])
		}
	}
}
