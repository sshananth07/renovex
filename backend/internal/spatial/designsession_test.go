package spatial

import (
	"math"
	"testing"
)

func draftWithSofaAndFixture() RoomDraft {
	return RoomDraft{
		ID:        "roomdraft_001",
		CompanyID: "company_1",
		CaptureID: "capture_1",
		Objects: []RoomDraftObject{
			{
				ID:         "object_sofa_123",
				Category:   "sofa",
				Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 1.2, Y: 0, Z: 2.4}, Rotation: RoomLocalQuaternion{W: 1}},
				Dimensions: &RoomLocalPoint{X: 2.0, Y: 0.85, Z: 0.95},
			},
			{
				ID:        "object_no_dimensions",
				Category:  "chair",
				Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 0.5, Y: 0, Z: 0.5}, Rotation: RoomLocalQuaternion{W: 1}},
			},
			{
				ID:        "object_nonfinite_transform",
				Category:  "table",
				Transform: RoomLocalTransform{Position: RoomLocalPoint{X: math.NaN(), Y: 0, Z: 0}, Rotation: RoomLocalQuaternion{W: 1}},
			},
		},
		Fixtures: []RoomDraftFixture{
			{
				ID:        "fixture_ac_001",
				Category:  FixtureCategoryAC,
				Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 3, Y: 2, Z: 0}, Rotation: RoomLocalQuaternion{W: 1}},
				CreatedBy: ElementOriginContractor,
			},
		},
		Walls: []RoomDraftWall{
			{ID: "wall_1", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
		},
		Revision: 17,
	}
}

func TestResolveDesignTarget_ValidObject(t *testing.T) {
	draft := draftWithSofaAndFixture()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Category != "sofa" {
		t.Fatalf("expected category sofa, got %s", target.Category)
	}
	if target.Dimensions == nil {
		t.Fatal("expected dimensions to be present")
	}
}

func TestResolveDesignTarget_ValidFixture(t *testing.T) {
	draft := draftWithSofaAndFixture()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "fixture_ac_001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Category != string(FixtureCategoryAC) {
		t.Fatalf("expected category ac, got %s", target.Category)
	}
}

func TestResolveDesignTarget_WrongKindRejected(t *testing.T) {
	draft := draftWithSofaAndFixture()
	_, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: "wall", ID: "wall_1"})
	if err != ErrUnsupportedDesignTargetKind {
		t.Fatalf("expected ErrUnsupportedDesignTargetKind, got %v", err)
	}
}

func TestResolveDesignTarget_UnknownIDNotFound(t *testing.T) {
	draft := draftWithSofaAndFixture()
	_, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_does_not_exist"})
	if err != ErrDesignTargetNotFound {
		t.Fatalf("expected ErrDesignTargetNotFound, got %v", err)
	}
}

func TestResolveDesignTarget_CrossKindCollisionRejected(t *testing.T) {
	// object_sofa_123 exists as an OBJECT; requesting it as a FIXTURE must
	// not silently find it via ID collision across kinds.
	draft := draftWithSofaAndFixture()
	_, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "object_sofa_123"})
	if err != ErrDesignTargetNotFound {
		t.Fatalf("expected ErrDesignTargetNotFound for cross-kind ID collision, got %v", err)
	}
}

func TestResolveDesignTarget_MissingDimensionsStillResolves(t *testing.T) {
	draft := draftWithSofaAndFixture()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_no_dimensions"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Dimensions != nil {
		t.Fatalf("expected nil dimensions, got %+v", target.Dimensions)
	}
}

func TestResolveDesignTarget_NonfiniteTransformRejected(t *testing.T) {
	draft := draftWithSofaAndFixture()
	_, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_nonfinite_transform"})
	if err != ErrInvalidDesignTargetTransform {
		t.Fatalf("expected ErrInvalidDesignTargetTransform, got %v", err)
	}
}
