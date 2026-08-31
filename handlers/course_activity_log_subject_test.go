package handlers

import (
	"strings"
	"testing"
)

func TestBuildContainmentPicksTheShapeThatMatches(t *testing.T) {
	// A scalar key must be probed as a scalar and a list key as a list;
	// swapping the two silently matches nothing.
	if got := buildContainment("student_id", "42", 42, true); got != `{"student_id":42}` {
		t.Fatalf("scalar probe: got %s", got)
	}
	if got := buildContainment("added_student_ids", "42", 42, true); got != `{"added_student_ids":[42]}` {
		t.Fatalf("list probe: got %s", got)
	}
}

func TestBuildContainmentKeepsNonNumericIDsAsStrings(t *testing.T) {
	// Queue sessions and classrooms use NanoIDs. Probing one as a number would
	// compare a JSON string against a JSON number and never match.
	got := buildContainment("queue_session_id", "ycjMNHEVU7s2RDAe", 0, false)
	if got != `{"queue_session_id":"ycjMNHEVU7s2RDAe"}` {
		t.Fatalf("expected a string probe, got %s", got)
	}
}

func TestBuildNestedContainmentProbesOneLevelDeeper(t *testing.T) {
	got := buildNestedContainment("graded_scores", "student_id", "42", 42, true)
	if got != `{"graded_scores":[{"student_id":42}]}` {
		t.Fatalf("nested probe: got %s", got)
	}
}

func TestNestedSubjectKeysAreListedAsSubjectKeys(t *testing.T) {
	// A key in nestedSubjectKeys that no subject type references is dead code
	// that looks like coverage. sub_item_scores is deliberately absent from
	// subjectDetailKeys: nobody asks for the history of one sub-item.
	referenced := map[string]bool{}
	for _, keys := range subjectDetailKeys {
		for _, key := range keys {
			referenced[key] = true
		}
	}
	for key := range nestedSubjectKeys {
		if key == "sub_item_scores" {
			continue
		}
		if !referenced[key] {
			t.Fatalf("nested key %q is never reachable from a subject type", key)
		}
	}
}

func TestSubjectKeysAreKnownToTheResolver(t *testing.T) {
	// Every key the subject filter searches must also be a key the resolver can
	// turn into a name, or the results would list rows the UI cannot explain.
	// The nested list keys resolve through nestedListRefKeys instead.
	for subjectType, keys := range subjectDetailKeys {
		for _, key := range keys {
			if _, nested := nestedListRefKeys[key]; nested {
				continue
			}
			if _, known := detailKeyRefTypes[key]; !known {
				t.Fatalf("subject %q searches detail key %q, which the resolver does not know", subjectType, key)
			}
		}
	}
}

func TestEverySubjectTypeResolvesToATargetType(t *testing.T) {
	// The filter matches on target_type too. Subject types are the resolver's
	// entity names, which do not always equal the target_type strings the logs
	// write, so each one must map to at least one.
	for subjectType := range subjectDetailKeys {
		if got := targetTypesForSubject(subjectType); len(got) == 0 {
			t.Fatalf("subject type %q maps to no log target type", subjectType)
		}
	}
}

func TestTargetTypesForSubjectCoversRenamedTargets(t *testing.T) {
	// Teams are logged as "team", not "group"; TAs are logged as both "user"
	// and "ta". Matching only the obvious spelling would silently miss rows.
	if got := targetTypesForSubject("group"); len(got) != 1 || got[0] != "team" {
		t.Fatalf("group must match the \"team\" target type, got %v", got)
	}
	got := targetTypesForSubject("user")
	if len(got) != 2 || got[0] != "ta" || got[1] != "user" {
		t.Fatalf("user must match both \"ta\" and \"user\", got %v", got)
	}
	if got := targetTypesForSubject("nonsense"); len(got) != 0 {
		t.Fatalf("an unknown subject must match nothing, got %v", got)
	}
}

func TestMarshalProbeProducesValidJSON(t *testing.T) {
	probe := marshalProbe(map[string]interface{}{"student_id": 42})
	if !strings.HasPrefix(probe, "{") || !strings.HasSuffix(probe, "}") {
		t.Fatalf("probe is not a JSON object: %s", probe)
	}
}
