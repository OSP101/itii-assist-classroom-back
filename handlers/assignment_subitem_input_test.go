package handlers

import "testing"

// The client sends existing sub-item IDs specifically so scores stay attached
// (see assignment.service.ts). Dropping an ID here makes the repository insert a
// fresh row, and every score referencing the old ID becomes unreachable — the
// scores stay in the table but vanish from the instructor's grid.
func TestBuildUpdateSubItems_CarriesExistingIDs(t *testing.T) {
	first := uint(41)
	second := uint(42)
	input := []assignmentSubItemInput{
		{ID: &first, Name: "ข้อ 1", MaxScore: 10},
		{ID: &second, Name: "ข้อ 2", MaxScore: 10},
	}

	items := buildUpdateSubItems(&input, nil)
	if items == nil {
		t.Fatal("expected sub-items to be built")
	}
	if len(*items) != 2 {
		t.Fatalf("expected 2 sub-items, got %d", len(*items))
	}
	if (*items)[0].ID != first {
		t.Errorf("sub-item 1 lost its id: sent %d, mapped %d — its scores would be orphaned", first, (*items)[0].ID)
	}
	if (*items)[1].ID != second {
		t.Errorf("sub-item 2 lost its id: sent %d, mapped %d — its scores would be orphaned", second, (*items)[1].ID)
	}
	if (*items)[0].Name != "ข้อ 1" || (*items)[0].MaxScore != 10 {
		t.Errorf("unexpected mapping for sub-item 1: %+v", (*items)[0])
	}
}

// A newly added sub-item has no ID yet and must map to zero so the repository
// inserts it rather than trying to update a row that does not exist.
func TestBuildUpdateSubItems_NewSubItemHasNoID(t *testing.T) {
	existing := uint(41)
	input := []assignmentSubItemInput{
		{ID: &existing, Name: "ข้อ 1", MaxScore: 10},
		{ID: nil, Name: "ข้อ 2", MaxScore: 5},
	}

	items := buildUpdateSubItems(&input, nil)
	if (*items)[0].ID != existing {
		t.Errorf("expected existing id %d to survive, got %d", existing, (*items)[0].ID)
	}
	if (*items)[1].ID != 0 {
		t.Errorf("expected a new sub-item to carry id 0, got %d", (*items)[1].ID)
	}
}

// replace_sub_items is the explicit "discard and rebuild" contract and has no ID
// field, so nothing should be carried over.
func TestBuildUpdateSubItems_ReplaceBranchCarriesNoIDs(t *testing.T) {
	input := []assignmentReplaceSubItemInput{
		{Name: "ข้อ 1", MaxScore: 10},
	}

	items := buildUpdateSubItems(nil, &input)
	if items == nil {
		t.Fatal("expected sub-items to be built from replace_sub_items")
	}
	if len(*items) != 1 || (*items)[0].ID != 0 {
		t.Fatalf("expected one id-less sub-item, got %+v", *items)
	}
}

// sub_items takes precedence, and omitting both must leave sub-items untouched
// rather than deleting them.
func TestBuildUpdateSubItems_NilWhenNeitherProvided(t *testing.T) {
	if items := buildUpdateSubItems(nil, nil); items != nil {
		t.Fatalf("expected nil so the repository skips sub-item reconciliation, got %+v", *items)
	}
}
