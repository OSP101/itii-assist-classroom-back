package handlers

import (
	"encoding/json"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestDiffFieldsReportsOnlyRealChanges(t *testing.T) {
	before := map[string]interface{}{"score": 8.0, "comment": "ok", "is_visible": false}
	after := map[string]interface{}{"score": 10.0, "comment": "ok", "is_visible": true}

	changes := diffFields(before, after)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %#v", len(changes), changes)
	}
	if changes["score"].From != 8.0 || changes["score"].To != 10.0 {
		t.Fatalf("unexpected score change: %#v", changes["score"])
	}
	if _, ok := changes["comment"]; ok {
		t.Fatal("an unchanged field must not appear in the diff")
	}
}

func TestDiffFieldsIgnoresNumericTypeDifferences(t *testing.T) {
	// A score read back from the database is a float64; the same score off the
	// request may be an int. Reporting "8 changed to 8" would bury real edits.
	before := map[string]interface{}{"score": 8.0, "max_score": 30}
	after := map[string]interface{}{"score": 8, "max_score": 30.0}

	if changes := diffFields(before, after); changes != nil {
		t.Fatalf("numerically equal values must not be reported: %#v", changes)
	}
}

func TestDiffFieldsComparesPointersByValue(t *testing.T) {
	eight, ten := 8.0, 10.0
	same := diffFields(
		map[string]interface{}{"score": &eight},
		map[string]interface{}{"score": 8.0},
	)
	if same != nil {
		t.Fatalf("a pointer and its value must compare equal: %#v", same)
	}

	moved := diffFields(
		map[string]interface{}{"score": &eight},
		map[string]interface{}{"score": &ten},
	)
	if len(moved) != 1 {
		t.Fatalf("expected the score change to be reported: %#v", moved)
	}
}

func TestDiffFieldsTreatsNilPointerAsAbsent(t *testing.T) {
	var missing *float64
	ten := 10.0

	// Grading a previously ungraded field is a change from nothing to a score.
	changes := diffFields(
		map[string]interface{}{"score": missing},
		map[string]interface{}{"score": &ten},
	)
	if len(changes) != 1 || changes["score"].From != nil || changes["score"].To != 10.0 {
		t.Fatalf("unexpected change: %#v", changes)
	}

	// Two nil pointers of different types are both "no value", not a change.
	var otherMissing *int
	if unchanged := diffFields(
		map[string]interface{}{"score": missing},
		map[string]interface{}{"score": otherMissing},
	); unchanged != nil {
		t.Fatalf("two absent values must not be a change: %#v", unchanged)
	}
}

func TestDiffFieldsReportsAppearingAndDisappearingKeys(t *testing.T) {
	changes := diffFields(
		map[string]interface{}{"due_date": "2026-09-01T00:00:00Z"},
		map[string]interface{}{"max_score": 20.0},
	)
	if len(changes) != 2 {
		t.Fatalf("expected both keys reported, got %#v", changes)
	}
	if changes["due_date"].To != nil {
		t.Fatalf("a removed field must report a nil destination: %#v", changes["due_date"])
	}
	if changes["max_score"].From != nil {
		t.Fatalf("an added field must report a nil origin: %#v", changes["max_score"])
	}
}

func TestWithChangesLeavesDetailAloneWhenNothingMoved(t *testing.T) {
	detail := fiber.Map{"score": 8}
	same := map[string]interface{}{"score": 8.0}

	result := withChanges(detail, same, same)
	if _, ok := result["changes"]; ok {
		t.Fatalf("a re-save with identical values must not carry a diff: %#v", result)
	}
	if result["score"] != 8 {
		t.Fatal("withChanges must not disturb the existing payload")
	}
}

func TestFieldChangeSerialisesForTheFrontend(t *testing.T) {
	raw, err := json.Marshal(map[string]fieldChange{"score": {From: 8.0, To: 10.0}})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"score":{"from":8,"to":10}}` {
		t.Fatalf("unexpected JSON shape: %s", raw)
	}
}

func TestResolveChangesHydratesBothSides(t *testing.T) {
	index := refIndex{}
	index.put(refSection, "2", resolvedRef{Type: refSection, ID: "2", Label: "1"})
	index.put(refSection, "5", resolvedRef{Type: refSection, ID: "5", Label: "2"})

	detail := detailOf(t, `{"changes":{"section_id":{"from":2,"to":5},"score":{"from":8,"to":10}}}`)
	resolved := resolveDetail(detail, index)

	changes, ok := resolved["changes"].(map[string]interface{})
	if !ok {
		t.Fatalf("changes were not resolved: %#v", resolved["changes"])
	}
	if _, ok := changes["score"]; ok {
		t.Fatal("a plain numeric field has nothing to resolve and must be left out")
	}

	sides, ok := changes["section_id"].(map[string]interface{})
	if !ok {
		t.Fatalf("section_id sides missing: %#v", changes["section_id"])
	}
	from, _ := sides["from"].(resolvedRef)
	to, _ := sides["to"].(resolvedRef)
	if from.Label != "1" || to.Label != "2" {
		t.Fatalf("unexpected resolved sides: from=%#v to=%#v", sides["from"], sides["to"])
	}
}

func TestResolveChangesKeepsOneSideWhenTheOtherIsGone(t *testing.T) {
	index := refIndex{}
	index.put(refSection, "5", resolvedRef{Type: refSection, ID: "5", Label: "2"})

	detail := detailOf(t, `{"changes":{"section_id":{"from":2,"to":5}}}`)
	resolved := resolveDetail(detail, index)

	changes := resolved["changes"].(map[string]interface{})
	sides := changes["section_id"].(map[string]interface{})
	if _, ok := sides["from"]; ok {
		t.Fatal("a deleted section must not be invented on the from side")
	}
	if to, _ := sides["to"].(resolvedRef); to.Label != "2" {
		t.Fatalf("the surviving side must still resolve: %#v", sides["to"])
	}
}

func TestCollectorGathersIDsFromBothSidesOfAChange(t *testing.T) {
	detail := detailOf(t, `{"changes":{"section_id":{"from":2,"to":5},"score":{"from":8,"to":10}}}`)
	collector := newRefCollector()
	for key, value := range detail {
		if key == changesDetailKey {
			collector.addChanges(value)
		}
	}

	got := collector.numericList(refSection)
	if len(got) != 2 {
		t.Fatalf("expected both section ids collected, got %v", got)
	}
}
