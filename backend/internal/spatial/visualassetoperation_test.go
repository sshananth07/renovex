package spatial

import (
	"errors"
	"testing"
)

// --- assign_visual_asset ---

func TestAssignVisualAssetOperation_AssignsToFixture(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "boiler-asset", Version: 1}
	updated, err := op.Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].VisualAsset == nil || *updated.Fixtures[0].VisualAsset != (VisualAssetRef{AssetID: "boiler-asset", Version: 1}) {
		t.Fatalf("expected VisualAsset assigned, got %+v", updated.Fixtures[0].VisualAsset)
	}
}

func TestAssignVisualAssetOperation_AssignsToObject(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1", AssetID: "sofa-asset", Version: 2}
	updated, err := op.Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].VisualAsset == nil || *updated.Objects[0].VisualAsset != (VisualAssetRef{AssetID: "sofa-asset", Version: 2}) {
		t.Fatalf("expected VisualAsset assigned, got %+v", updated.Objects[0].VisualAsset)
	}
}

func TestAssignVisualAssetOperation_RejectsMissingFixtureTarget(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "nonexistent", AssetID: "a", Version: 1}
	_, err := op.Apply(draftWithOneFixture())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestAssignVisualAssetOperation_RejectsMissingObjectTarget(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetObject, TargetID: "nonexistent", AssetID: "a", Version: 1}
	_, err := op.Apply(draftWithOneObject())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestAssignVisualAssetOperation_RejectsEmptyTargetID(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "", AssetID: "a", Version: 1}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestAssignVisualAssetOperation_RejectsEmptyAssetID(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "", Version: 1}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestAssignVisualAssetOperation_RejectsZeroVersion(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "a", Version: 0}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestAssignVisualAssetOperation_RejectsUnsupportedTargetKind(t *testing.T) {
	op := AssignVisualAssetOperation{TargetKind: "wall", TargetID: "wall_1", AssetID: "a", Version: 1}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestAssignVisualAssetOperation_LeavesUnrelatedElementsUntouched(t *testing.T) {
	draft := RoomDraft{
		Fixtures: []RoomDraftFixture{
			{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor},
			{ID: "fixture_2", Category: FixtureCategoryAC, CreatedBy: ElementOriginContractor},
		},
	}
	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "a", Version: 1}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[1].VisualAsset != nil {
		t.Fatalf("expected fixture_2 untouched, got %+v", updated.Fixtures[1].VisualAsset)
	}
}

func TestAssignVisualAssetOperation_OperationKind(t *testing.T) {
	if (AssignVisualAssetOperation{}).OperationKind() != EditOpAssignVisualAsset {
		t.Fatalf("expected EditOpAssignVisualAsset")
	}
}

// --- clear_visual_asset ---

func draftWithOneFixtureAndVisualAsset() RoomDraft {
	return RoomDraft{
		Fixtures: []RoomDraftFixture{
			{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, VisualAsset: &VisualAssetRef{AssetID: "a", Version: 1}},
		},
	}
}

func draftWithOneObjectAndVisualAsset() RoomDraft {
	return RoomDraft{
		Objects: []RoomDraftObject{
			{ID: "object_1", Category: "sofa", VisualAsset: &VisualAssetRef{AssetID: "a", Version: 1}},
		},
	}
}

func TestClearVisualAssetOperation_ClearsFixtureBinding(t *testing.T) {
	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1"}
	updated, err := op.Apply(draftWithOneFixtureAndVisualAsset())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].VisualAsset != nil {
		t.Fatalf("expected VisualAsset cleared, got %+v", updated.Fixtures[0].VisualAsset)
	}
}

func TestClearVisualAssetOperation_ClearsObjectBinding(t *testing.T) {
	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1"}
	updated, err := op.Apply(draftWithOneObjectAndVisualAsset())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].VisualAsset != nil {
		t.Fatalf("expected VisualAsset cleared, got %+v", updated.Objects[0].VisualAsset)
	}
}

func TestClearVisualAssetOperation_RejectsMissingTarget(t *testing.T) {
	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "nonexistent"}
	_, err := op.Apply(draftWithOneFixture())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestClearVisualAssetOperation_RejectsEmptyTargetID(t *testing.T) {
	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: ""}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestClearVisualAssetOperation_RejectsUnsupportedTargetKind(t *testing.T) {
	op := ClearVisualAssetOperation{TargetKind: "wall", TargetID: "wall_1"}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestClearVisualAssetOperation_LeavesUnrelatedElementsUntouched(t *testing.T) {
	draft := RoomDraft{
		Fixtures: []RoomDraftFixture{
			{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, VisualAsset: &VisualAssetRef{AssetID: "a", Version: 1}},
			{ID: "fixture_2", Category: FixtureCategoryAC, CreatedBy: ElementOriginContractor, VisualAsset: &VisualAssetRef{AssetID: "b", Version: 1}},
		},
	}
	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1"}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[1].VisualAsset == nil || updated.Fixtures[1].VisualAsset.AssetID != "b" {
		t.Fatalf("expected fixture_2 untouched, got %+v", updated.Fixtures[1].VisualAsset)
	}
}

func TestClearVisualAssetOperation_OperationKind(t *testing.T) {
	if (ClearVisualAssetOperation{}).OperationKind() != EditOpClearVisualAsset {
		t.Fatalf("expected EditOpClearVisualAsset")
	}
}
