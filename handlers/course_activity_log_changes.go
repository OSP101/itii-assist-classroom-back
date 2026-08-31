package handlers

import (
	"reflect"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// Before/after field changes
//
// Update actions used to log only the new value, so the log could say a score
// is now 10 but never that it used to be 8 — which is the whole question when
// somebody disputes a grade. Handlers snapshot the fields they are about to
// change, and diffFields records the ones that actually moved under the
// "changes" key of the log detail.
// =============================================================================

// fieldChange is one field's value before and after an update.
type fieldChange struct {
	From interface{} `json:"from"`
	To   interface{} `json:"to"`
}

// diffFields returns only the fields whose value actually changed. A submit
// that re-saves the same numbers is still worth a log line ("who touched this,
// and when") but carries no changes, so an unchanged field must not appear.
//
// Keys present in only one of the two maps are treated as a change from or to
// nil, so a field that appears or disappears is not silently dropped.
func diffFields(before, after map[string]interface{}) map[string]fieldChange {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}

	// Values are stored dereferenced: a *float64 in the payload would marshal to
	// the right JSON but leave a typed nil sitting in the struct, which reads as
	// "present" to anything inspecting it in Go.
	changes := map[string]fieldChange{}
	for key, afterValue := range after {
		beforeValue := before[key]
		if !valuesDiffer(beforeValue, afterValue) {
			continue
		}
		changes[key] = fieldChange{From: derefValue(beforeValue), To: derefValue(afterValue)}
	}
	for key, beforeValue := range before {
		if _, ok := after[key]; ok {
			continue
		}
		if !valuesDiffer(beforeValue, nil) {
			continue
		}
		changes[key] = fieldChange{From: derefValue(beforeValue), To: nil}
	}

	if len(changes) == 0 {
		return nil
	}
	return changes
}

// withChanges attaches a computed diff to a log detail payload, leaving the
// payload untouched when nothing changed.
func withChanges(detail fiber.Map, before, after map[string]interface{}) fiber.Map {
	if changes := diffFields(before, after); len(changes) > 0 {
		if detail == nil {
			detail = fiber.Map{}
		}
		detail["changes"] = changes
	}
	return detail
}

// valuesDiffer compares two field values the way a reader would.
//
// Numbers are compared by value rather than by type: a score read back from the
// database is a float64 while the same score off a JSON request may arrive as
// an int, and reporting "8 changed to 8" every time would bury the real edits.
// Pointers are compared by what they point at for the same reason.
func valuesDiffer(before, after interface{}) bool {
	before = derefValue(before)
	after = derefValue(after)

	if before == nil || after == nil {
		return before != after
	}

	beforeNumber, beforeIsNumber := asFloat(before)
	afterNumber, afterIsNumber := asFloat(after)
	if beforeIsNumber && afterIsNumber {
		return beforeNumber != afterNumber
	}

	return !reflect.DeepEqual(before, after)
}

// derefValue unwraps a pointer to the value it points at, so an optional field
// compares by content. A nil pointer becomes an untyped nil, so it compares
// equal to a missing value rather than to another nil pointer of a different
// type.
func derefValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Ptr {
		return value
	}
	if reflected.IsNil() {
		return nil
	}
	return reflected.Elem().Interface()
}

func asFloat(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}
