package spatial

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestVisualAssetRef_Validate_RejectsEmptyAssetID(t *testing.T) {
	ref := VisualAssetRef{AssetID: "", Version: 1}
	if err := ref.Validate(); !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef, got %v", err)
	}
}

func TestVisualAssetRef_Validate_RejectsZeroVersion(t *testing.T) {
	ref := VisualAssetRef{AssetID: "chair-123", Version: 0}
	if err := ref.Validate(); !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef, got %v", err)
	}
}

func TestVisualAssetRef_Validate_RejectsNegativeVersion(t *testing.T) {
	ref := VisualAssetRef{AssetID: "chair-123", Version: -1}
	if err := ref.Validate(); !errors.Is(err, ErrInvalidVisualAssetRef) {
		t.Fatalf("expected ErrInvalidVisualAssetRef, got %v", err)
	}
}

func TestVisualAssetRef_Validate_AcceptsValidRef(t *testing.T) {
	ref := VisualAssetRef{AssetID: "chair-123", Version: 1}
	if err := ref.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoomDraftFixture_DecodesOldJSONWithoutVisualAsset(t *testing.T) {
	oldJSON := `{"id":"fixture_1","category":"boiler","transform":{"position":{"x":0,"y":0,"z":0},"rotation":{"x":0,"y":0,"z":0,"w":1}},"createdBy":"contractor"}`
	var fixture RoomDraftFixture
	if err := json.Unmarshal([]byte(oldJSON), &fixture); err != nil {
		t.Fatalf("unexpected error decoding old JSON: %v", err)
	}
	if fixture.VisualAsset != nil {
		t.Fatalf("expected nil VisualAsset for old JSON missing the field, got %+v", fixture.VisualAsset)
	}
}

func TestRoomDraftObject_DecodesOldJSONWithoutVisualAsset(t *testing.T) {
	oldJSON := `{"id":"object_1","category":"sofa","transform":{"position":{"x":0,"y":0,"z":0},"rotation":{"x":0,"y":0,"z":0,"w":1}},"provenance":{"provider":"roomplan","sourceElementIdentifier":"o1"}}`
	var object RoomDraftObject
	if err := json.Unmarshal([]byte(oldJSON), &object); err != nil {
		t.Fatalf("unexpected error decoding old JSON: %v", err)
	}
	if object.VisualAsset != nil {
		t.Fatalf("expected nil VisualAsset for old JSON missing the field, got %+v", object.VisualAsset)
	}
}

func TestRoomDraftFixture_RoundTripsWithVisualAsset(t *testing.T) {
	fixture := RoomDraftFixture{
		ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor,
		VisualAsset: &VisualAssetRef{AssetID: "boiler-asset", Version: 2},
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("unexpected error marshaling: %v", err)
	}
	var decoded RoomDraftFixture
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unexpected error unmarshaling: %v", err)
	}
	if decoded.VisualAsset == nil || *decoded.VisualAsset != *fixture.VisualAsset {
		t.Fatalf("expected VisualAsset to round-trip exactly, got %+v", decoded.VisualAsset)
	}
}
