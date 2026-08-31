package handlers

import (
	"encoding/json"
	"testing"

	"gorm.io/datatypes"
)

// Payloads below are copied from real rows in course_activity_logs so the
// resolver is exercised against the shapes it actually receives.

func detailOf(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	decoded := decodeActivityDetail(datatypes.JSON([]byte(raw)))
	if decoded == nil {
		t.Fatalf("failed to decode detail: %s", raw)
	}
	return decoded
}

func collectorFor(t *testing.T, detail map[string]interface{}) *refCollector {
	t.Helper()
	collector := newRefCollector()
	for key, value := range detail {
		if rule, ok := nestedListRefKeys[key]; ok {
			collector.addNestedList(rule, value)
			continue
		}
		if refType, ok := detailKeyRefTypes[key]; ok {
			collector.addValue(refType, value)
		}
	}
	return collector
}

func TestCollectorGathersScalarListAndNestedIDs(t *testing.T) {
	detail := detailOf(t, `{"end_time":"2026-06-21T14:10:25.72Z","check_location":false,"course_section_ids":[2,3]}`)
	collector := collectorFor(t, detail)

	got := collector.numericList(refSection)
	if len(got) != 2 {
		t.Fatalf("expected 2 section ids, got %v", got)
	}

	detail = detailOf(t, `{"score":null,"booking_type":"grading","sub_item_scores":[{"score":5,"sub_item_id":11},{"score":5,"sub_item_id":12}]}`)
	collector = collectorFor(t, detail)
	if got := collector.numericList(refSubItem); len(got) != 2 {
		t.Fatalf("expected 2 sub item ids from nested list, got %v", got)
	}

	// NanoID primary keys must survive as strings, not be dropped by numeric parsing.
	detail = detailOf(t, `{"classroom_id":"ycjMNHEVU7s2RDAe5ehG8","linked_assignment_id":8}`)
	collector = collectorFor(t, detail)
	if got := collector.list(refClassroom); len(got) != 1 || got[0] != "ycjMNHEVU7s2RDAe5ehG8" {
		t.Fatalf("expected classroom NanoID to be collected, got %v", got)
	}
	if got := collector.numericList(refAssignment); len(got) != 1 || got[0] != 8 {
		t.Fatalf("expected assignment id 8, got %v", got)
	}
}

func TestCollectorIgnoresZeroAndUnknownKeys(t *testing.T) {
	detail := detailOf(t, `{"student_id":0,"week_number":null,"assignment_type":"individual"}`)
	collector := collectorFor(t, detail)

	if got := collector.numericList(refStudent); len(got) != 0 {
		t.Fatalf("expected zero id to be skipped, got %v", got)
	}
	if got := collector.list(refAssignment); len(got) != 0 {
		t.Fatalf("assignment_type is not a foreign key, got %v", got)
	}
}

func TestResolveDetailReplacesKnownKeysOnly(t *testing.T) {
	index := refIndex{}
	index.put(refStudent, "42", resolvedRef{Type: refStudent, ID: "42", Label: "สมชาย ใจดี", Sub: "643020123-4"})
	index.put(refSection, "2", resolvedRef{Type: refSection, ID: "2", Label: "1"})
	index.put(refSection, "3", resolvedRef{Type: refSection, ID: "3", Label: "2"})

	detail := detailOf(t, `{"student_id":42,"course_section_ids":[2,3],"status":"present","score":8}`)
	resolved := resolveDetail(detail, index)

	if _, ok := resolved["status"]; ok {
		t.Fatalf("plain values must not appear in resolved: %v", resolved)
	}
	if _, ok := resolved["score"]; ok {
		t.Fatalf("plain values must not appear in resolved: %v", resolved)
	}

	student, ok := resolved["student_id"].(resolvedRef)
	if !ok || student.Label != "สมชาย ใจดี" || student.Sub != "643020123-4" {
		t.Fatalf("student was not resolved: %#v", resolved["student_id"])
	}

	sections, ok := resolved["course_section_ids"].([]resolvedRef)
	if !ok || len(sections) != 2 {
		t.Fatalf("sections were not resolved as a list: %#v", resolved["course_section_ids"])
	}
}

func TestResolveDetailKeepsNestedListPositions(t *testing.T) {
	index := refIndex{}
	// Only the second sub item still exists; the first was deleted.
	index.put(refSubItem, "12", resolvedRef{Type: refSubItem, ID: "12", Label: "ส่วนที่ 2"})

	detail := detailOf(t, `{"sub_item_scores":[{"score":5,"sub_item_id":11},{"score":5,"sub_item_id":12}]}`)
	resolved := resolveDetail(detail, index)

	entries, ok := resolved["sub_item_scores"].([]interface{})
	if !ok || len(entries) != 2 {
		t.Fatalf("expected 2 positional entries, got %#v", resolved["sub_item_scores"])
	}
	if entries[0] != nil {
		t.Fatalf("missing sub item must stay nil so the score pairing does not shift: %#v", entries[0])
	}
	second, ok := entries[1].(resolvedRef)
	if !ok || second.Label != "ส่วนที่ 2" {
		t.Fatalf("second sub item was not resolved: %#v", entries[1])
	}
}

func TestResolveDetailReturnsNilWhenNothingResolves(t *testing.T) {
	detail := detailOf(t, `{"status":"offline","accept_help":true}`)
	if resolved := resolveDetail(detail, refIndex{}); resolved != nil {
		t.Fatalf("expected nil, got %#v", resolved)
	}
}

func TestResolvedRefJSONShapeMatchesFrontendContract(t *testing.T) {
	raw, err := json.Marshal(resolvedRef{Type: refStudent, ID: "7", Label: "ก", Sub: ""})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"type":"student","id":"7","label":"ก"}` {
		t.Fatalf("unexpected JSON shape: %s", raw)
	}
}
