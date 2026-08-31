package handlers

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// =============================================================================
// Subject filter: "everything that happened to this one thing"
//
// The question an instructor actually asks of a log is rarely "what happened on
// Tuesday" and almost always "who touched this student's scores" or "what has
// been done to this assignment". A row can refer to its subject in three ways:
//
//   - as the log's own target (target_type/target_id)
//   - as a plain key in the detail JSON  ({"student_id": 42})
//   - inside a list in the detail JSON   ({"added_student_ids":[42]},
//     {"graded_scores":[{"student_id":42}]})
//
// Postgres jsonb containment matches all three list shapes with one operator,
// so the filter is an OR over the target columns and a handful of containment
// probes. It relies on the GIN index on detail declared in config/database.go.
// =============================================================================

// subjectDetailKeys lists, per subject type, the detail keys that can name it.
// Only keys that actually carry the subject belong here: every entry costs one
// containment probe per query.
var subjectDetailKeys = map[string][]string{
	"student": {
		"student_id",
		"student_ids",
		"member_ids",
		"added_student_ids",
		"moved_student_ids",
		"skipped_student_ids",
		"graded_scores",
		"record_updates",
	},
	"user": {
		"user_id",
		"instructor_ids",
		"added_user_ids",
	},
	"assignment": {
		"assignment_id",
		"linked_assignment_id",
		"origin_assignment_id",
		"ordered_ids",
	},
	"section": {
		"section_id",
		"from_section_id",
		"to_section_id",
		"target_section_id",
		"course_section_ids",
	},
	"attendance_session": {
		"attendance_session_id",
		"linked_attendance_session_id",
		"session_ids",
	},
	"group": {
		"group_id",
		"team_id",
		"team_ids",
		"created_team_ids",
		"deleted_team_ids",
	},
}

// nestedSubjectKeys are the detail keys holding a list of objects rather than a
// list of bare IDs, so containment has to be probed one level deeper.
var nestedSubjectKeys = map[string]string{
	"graded_scores":   "student_id",
	"record_updates":  "student_id",
	"sub_item_scores": "sub_item_id",
}

// applySubjectFilter narrows a query to rows referring to one entity. An unknown
// subject type or a blank ID leaves the query untouched, so a malformed filter
// shows everything rather than silently showing nothing.
func applySubjectFilter(query *gorm.DB, subjectType, subjectID string) *gorm.DB {
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	if subjectType == "" || subjectID == "" {
		return query
	}

	keys, known := subjectDetailKeys[subjectType]
	if !known {
		return query
	}

	// The subject type is the resolver's entity name, which is not always the
	// string the logs write as target_type: teams are logged as "team" and TAs
	// as "ta". Match every target type that resolves to this entity.
	targetTypes := targetTypesForSubject(subjectType)
	if len(targetTypes) == 0 {
		return query
	}

	conditions := []string{"(target_type IN ? AND target_id = ?)"}
	args := []interface{}{targetTypes, subjectID}

	// Containment compares JSON values, so a numeric ID has to be probed as a
	// number. IDs that are not numeric (a NanoID) are probed as strings.
	numeric, numericErr := strconv.ParseUint(subjectID, 10, 64)

	for _, key := range keys {
		var probe string
		if nestedKey, nested := nestedSubjectKeys[key]; nested {
			probe = buildNestedContainment(key, nestedKey, subjectID, numeric, numericErr == nil)
		} else {
			probe = buildContainment(key, subjectID, numeric, numericErr == nil)
		}
		if probe == "" {
			continue
		}
		conditions = append(conditions, "detail @> ?::jsonb")
		args = append(args, probe)
	}

	return query.Where("("+strings.Join(conditions, " OR ")+")", args...)
}

// targetTypesForSubject reverses targetTypeRefTypes: given the resolver's entity
// name it returns every target_type string the logs use for it.
func targetTypesForSubject(subjectType string) []string {
	result := make([]string, 0, 2)
	for targetType, refType := range targetTypeRefTypes {
		if refType == subjectType {
			result = append(result, targetType)
		}
	}
	sort.Strings(result)
	return result
}

// buildContainment renders the probe for a key holding either the ID directly or
// a list of IDs. The two need different shapes: {"k": 42} matches a scalar,
// {"k": [42]} matches any array containing 42. Every list key in the detail
// payloads is named with an "_ids" suffix, which is what tells the two apart.
func buildContainment(key, rawID string, numeric uint64, isNumeric bool) string {
	var value interface{} = rawID
	if isNumeric {
		value = numeric
	}

	if strings.HasSuffix(key, "_ids") {
		return marshalProbe(map[string]interface{}{key: []interface{}{value}})
	}
	return marshalProbe(map[string]interface{}{key: value})
}

// buildNestedContainment renders the probe for a list of objects, e.g.
// {"graded_scores": [{"student_id": 42}]}.
func buildNestedContainment(key, nestedKey, rawID string, numeric uint64, isNumeric bool) string {
	var value interface{} = rawID
	if isNumeric {
		value = numeric
	}
	return marshalProbe(map[string]interface{}{
		key: []interface{}{map[string]interface{}{nestedKey: value}},
	})
}

func marshalProbe(payload map[string]interface{}) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}
