package spatial

import (
	"errors"
	"testing"
)

// --- set_visual_appearance ---

func TestSetVisualAppearanceOperation_SetsOnFixture(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", Appearance: validAppearance()}
	updated, err := op.Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].Appearance == nil {
		t.Fatal("expected appearance set")
	}
	if updated.Fixtures[0].Appearance.BaseColor != "#2f4f3a" {
		t.Fatalf("expected normalized lowercase color, got %q", updated.Fixtures[0].Appearance.BaseColor)
	}
}

func TestSetVisualAppearanceOperation_SetsOnObject(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1", Appearance: validAppearance()}
	updated, err := op.Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Appearance == nil || updated.Objects[0].Appearance.MaterialFamily != MaterialFamilyFabric {
		t.Fatalf("expected appearance set, got %+v", updated.Objects[0].Appearance)
	}
}

func TestSetVisualAppearanceOperation_RejectsMissingFixtureTarget(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "nonexistent", Appearance: validAppearance()}
	if _, err := op.Apply(draftWithOneFixture()); !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestSetVisualAppearanceOperation_RejectsMissingObjectTarget(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetObject, TargetID: "nonexistent", Appearance: validAppearance()}
	if _, err := op.Apply(draftWithOneObject()); !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestSetVisualAppearanceOperation_RejectsEmptyTargetID(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "", Appearance: validAppearance()}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestSetVisualAppearanceOperation_RejectsUnsupportedTargetKind(t *testing.T) {
	op := SetVisualAppearanceOperation{TargetKind: "wall", TargetID: "wall_1", Appearance: validAppearance()}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestSetVisualAppearanceOperation_RejectsInvalidAppearance(t *testing.T) {
	bad := validAppearance()
	bad.BaseColor = "dark green"
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", Appearance: bad}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestSetVisualAppearanceOperation_LeavesUnrelatedElementsUntouched(t *testing.T) {
	draft := RoomDraft{
		Fixtures: []RoomDraftFixture{
			{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor},
			{ID: "fixture_2", Category: FixtureCategoryAC, CreatedBy: ElementOriginContractor},
		},
	}
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", Appearance: validAppearance()}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[1].Appearance != nil {
		t.Fatalf("expected fixture_2 untouched, got %+v", updated.Fixtures[1].Appearance)
	}
}

func TestSetVisualAppearanceOperation_DoesNotTouchVisualAsset(t *testing.T) {
	draft := draftWithOneObjectAndVisualAsset()
	op := SetVisualAppearanceOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1", Appearance: validAppearance()}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].VisualAsset == nil || updated.Objects[0].VisualAsset.AssetID != "a" {
		t.Fatalf("expected VisualAsset untouched, got %+v", updated.Objects[0].VisualAsset)
	}
	if updated.Objects[0].Appearance == nil {
		t.Fatal("expected appearance also set")
	}
}

func TestSetVisualAppearanceOperation_OperationKind(t *testing.T) {
	if (SetVisualAppearanceOperation{}).OperationKind() != EditOpSetVisualAppearance {
		t.Fatal("expected EditOpSetVisualAppearance")
	}
}

// --- clear_visual_appearance ---

func draftWithOneFixtureAndAppearance() RoomDraft {
	return RoomDraft{
		Fixtures: []RoomDraftFixture{
			{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, Appearance: appearancePtr(validAppearance())},
		},
	}
}

func draftWithOneObjectAndAppearance() RoomDraft {
	return RoomDraft{
		Objects: []RoomDraftObject{
			{ID: "object_1", Category: "sofa", Appearance: appearancePtr(validAppearance())},
		},
	}
}

func appearancePtr(a VisualAppearance) *VisualAppearance { return &a }

func TestClearVisualAppearanceOperation_ClearsFixtureAppearance(t *testing.T) {
	op := ClearVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1"}
	updated, err := op.Apply(draftWithOneFixtureAndAppearance())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].Appearance != nil {
		t.Fatalf("expected appearance cleared, got %+v", updated.Fixtures[0].Appearance)
	}
}

func TestClearVisualAppearanceOperation_ClearsObjectAppearance(t *testing.T) {
	op := ClearVisualAppearanceOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1"}
	updated, err := op.Apply(draftWithOneObjectAndAppearance())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Appearance != nil {
		t.Fatalf("expected appearance cleared, got %+v", updated.Objects[0].Appearance)
	}
}

func TestClearVisualAppearanceOperation_RejectsMissingTarget(t *testing.T) {
	op := ClearVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: "nonexistent"}
	if _, err := op.Apply(draftWithOneFixtureAndAppearance()); !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestClearVisualAppearanceOperation_RejectsEmptyTargetID(t *testing.T) {
	op := ClearVisualAppearanceOperation{TargetKind: VisualAssetTargetFixture, TargetID: ""}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestClearVisualAppearanceOperation_RejectsUnsupportedTargetKind(t *testing.T) {
	op := ClearVisualAppearanceOperation{TargetKind: "wall", TargetID: "wall_1"}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestClearVisualAppearanceOperation_DoesNotTouchVisualAsset(t *testing.T) {
	draft := draftWithOneObjectAndVisualAsset()
	draft.Objects[0].Appearance = appearancePtr(validAppearance())
	op := ClearVisualAppearanceOperation{TargetKind: VisualAssetTargetObject, TargetID: "object_1"}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].VisualAsset == nil {
		t.Fatal("expected VisualAsset untouched")
	}
	if updated.Objects[0].Appearance != nil {
		t.Fatal("expected appearance cleared")
	}
}

func TestClearVisualAppearanceOperation_OperationKind(t *testing.T) {
	if (ClearVisualAppearanceOperation{}).OperationKind() != EditOpClearVisualAppearance {
		t.Fatal("expected EditOpClearVisualAppearance")
	}
}
