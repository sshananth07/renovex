package spatial

import (
	"encoding/json"
	"testing"
)

// A simple 4x4m rectangular room centered at origin, walls at x=0,x=4,z=0,z=4.
func rectRoomDraft() RoomDraft {
	return RoomDraft{
		ID: "roomdraft_rect",
		Walls: []RoomDraftWall{
			{ID: "wall_south", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
			{ID: "wall_east", Start: RoomLocalPoint{X: 4, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 4}},
			{ID: "wall_north", Start: RoomLocalPoint{X: 4, Y: 0, Z: 4}, End: RoomLocalPoint{X: 0, Y: 0, Z: 4}},
			{ID: "wall_west", Start: RoomLocalPoint{X: 0, Y: 0, Z: 4}, End: RoomLocalPoint{X: 0, Y: 0, Z: 0}},
		},
		Objects: []RoomDraftObject{
			{
				ID: "object_sofa", Category: "sofa",
				Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 2, Y: 0, Z: 0.5}, Rotation: RoomLocalQuaternion{W: 1}},
				Dimensions: &RoomLocalPoint{X: 2.0, Y: 0.85, Z: 0.5},
			},
			{
				ID: "object_no_dims", Category: "chair",
				Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 1, Y: 0, Z: 1}, Rotation: RoomLocalQuaternion{W: 1}},
			},
		},
	}
}

func TestResolveSpatialChanges_MoveAwayFromNearestWall(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target:   SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModePreserve},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
	}

	ops, fit, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 resolved operation, got %d", len(ops))
	}
	if ops[0].Kind != EditOpMoveObject {
		t.Fatalf("expected move_object, got %s", ops[0].Kind)
	}
	if fit.Status != FitStatusClear && fit.Status != FitStatusWarning {
		t.Fatalf("expected clear or warning fit status, got %s", fit.Status)
	}
}

func TestResolveSpatialChanges_FixtureUsesMoveFixture(t *testing.T) {
	draft := rectRoomDraft()
	draft.Fixtures = []RoomDraftFixture{
		{
			ID: "fixture_ac", Category: FixtureCategoryAC,
			// Positioned away from object_sofa (at X=2,Z=0.5) so this test
			// exercises the fixture->move_fixture resolution path without
			// tripping the (correct, separately tested) collision blocker.
			Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 3.5, Y: 2, Z: 3.5}, Rotation: RoomLocalQuaternion{W: 1}},
			Dimensions: &RoomLocalPoint{X: 0.4, Y: 0.3, Z: 0.2},
			CreatedBy:  ElementOriginContractor,
		},
	}
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "fixture_ac"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "fixture_ac"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.1,
		}},
	}

	ops, _, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != EditOpMoveFixture {
		t.Fatalf("expected move_fixture, got %+v", ops)
	}
}

func TestResolveSpatialChanges_ResizeAxisUsesResizeObject(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpResizeAxis, Axis: SpatialAxisX, TargetMeters: 2.5, HasTarget: true,
		}},
	}

	ops, _, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 || ops[0].Kind != EditOpResizeObject {
		t.Fatalf("expected resize_object, got %+v", ops)
	}
}

// The following three tests prove ResolvedSpatialOperation.Payload actually
// round-trips through decodeEditOperation + Apply against the real
// MoveObjectOperation/MoveFixtureOperation/ResizeObjectOperation wire
// shapes (editoperation.go) — the exact translation Use Design (RP4E2 Gate
// 1) depends on. Before this fix, resolveSpatialChanges built payloads with
// keys (id/newPosition/newDimensions) that did not match those operations'
// actual json tags (objectId/position/fixtureId/transform/dimensions), so
// decoding would have silently produced zero-valued fields or failed with
// ErrMalformedEditOperationPayload — a real, previously untested gap.

func TestResolveSpatialChanges_MoveObjectPayloadAppliesThroughEditOperationPipeline(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
	}
	ops, _, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("resolveSpatialChanges: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 resolved operation, got %d", len(ops))
	}

	payloadJSON, err := json.Marshal(ops[0].Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	decoded, err := decodeEditOperation(ops[0].Kind, payloadJSON)
	if err != nil {
		t.Fatalf("decodeEditOperation: %v", err)
	}
	move, ok := decoded.(*MoveObjectOperation)
	if !ok {
		t.Fatalf("expected *MoveObjectOperation, got %T", decoded)
	}
	if move.ObjectID != "object_sofa" {
		t.Fatalf("expected ObjectID populated from payload, got %q", move.ObjectID)
	}

	// Capture the original position BEFORE Apply — RoomDraft's Objects
	// slice shares a backing array with draft, so Apply mutates it in
	// place; reading draft.Objects[0] after Apply would observe the
	// already-mutated value.
	original := draft.Objects[0].Transform.Position
	updated, err := move.Apply(draft)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	moved := updated.Objects[0].Transform.Position
	if moved == original {
		t.Fatalf("expected object position to actually change, stayed at %+v", moved)
	}
	if moved.Z != 0.7 {
		t.Fatalf("expected object moved away from wall_south to Z=0.7, got %+v", moved)
	}
}

func TestResolveSpatialChanges_MoveFixturePayloadAppliesThroughEditOperationPipeline(t *testing.T) {
	draft := rectRoomDraft()
	draft.Fixtures = []RoomDraftFixture{
		{
			ID: "fixture_ac", Category: FixtureCategoryAC,
			Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 3.5, Y: 2, Z: 3.5}, Rotation: RoomLocalQuaternion{X: 0.1, Y: 0.2, Z: 0.3, W: 0.9}},
			Dimensions: &RoomLocalPoint{X: 0.4, Y: 0.3, Z: 0.2},
			CreatedBy:  ElementOriginContractor,
		},
	}
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "fixture_ac"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindFixture, ID: "fixture_ac"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.1,
		}},
	}
	ops, _, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("resolveSpatialChanges: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 resolved operation, got %d", len(ops))
	}

	payloadJSON, err := json.Marshal(ops[0].Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	decoded, err := decodeEditOperation(ops[0].Kind, payloadJSON)
	if err != nil {
		t.Fatalf("decodeEditOperation: %v", err)
	}
	move, ok := decoded.(*MoveFixtureOperation)
	if !ok {
		t.Fatalf("expected *MoveFixtureOperation, got %T", decoded)
	}
	if move.FixtureID != "fixture_ac" {
		t.Fatalf("expected FixtureID populated from payload, got %q", move.FixtureID)
	}
	originalRotation := draft.Fixtures[0].Transform.Rotation
	originalPosition := draft.Fixtures[0].Transform.Position
	// Rotation must be carried through unchanged — RP4E1's spatial
	// operations never rotate, so a MoveFixtureOperation's full-transform
	// payload must preserve the fixture's original orientation, not zero it.
	if move.Transform.Rotation != originalRotation {
		t.Fatalf("expected original rotation preserved, got %+v, want %+v", move.Transform.Rotation, originalRotation)
	}

	updated, err := move.Apply(draft)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if updated.Fixtures[0].Transform.Position == originalPosition {
		t.Fatal("expected fixture position to actually change")
	}
}

func TestResolveSpatialChanges_ResizeObjectPayloadAppliesThroughEditOperationPipeline(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpResizeAxis, Axis: SpatialAxisX, TargetMeters: 2.5, HasTarget: true,
		}},
	}
	ops, _, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("resolveSpatialChanges: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 resolved operation, got %d", len(ops))
	}

	payloadJSON, err := json.Marshal(ops[0].Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	decoded, err := decodeEditOperation(ops[0].Kind, payloadJSON)
	if err != nil {
		t.Fatalf("decodeEditOperation: %v", err)
	}
	resize, ok := decoded.(*ResizeObjectOperation)
	if !ok {
		t.Fatalf("expected *ResizeObjectOperation, got %T", decoded)
	}
	if resize.ObjectID != "object_sofa" {
		t.Fatalf("expected ObjectID populated from payload, got %q", resize.ObjectID)
	}
	if resize.Dimensions.X != 2.5 {
		t.Fatalf("expected resized X dimension 2.5, got %v", resize.Dimensions.X)
	}

	updated, err := resize.Apply(draft)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if updated.Objects[0].Dimensions.X != 2.5 {
		t.Fatalf("expected applied dimension 2.5, got %v", updated.Objects[0].Dimensions.X)
	}
}

func TestResolveSpatialChanges_MissingDimensionsBlocksSpatial(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_no_dims"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_no_dims"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
	}

	ops, fit, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no resolved operations when dimensions are missing, got %+v", ops)
	}
	if fit.Status != FitStatusBlocked {
		t.Fatalf("expected blocked fit status, got %s", fit.Status)
	}
}

func TestResolveSpatialChanges_PreserveModeReturnsNoOperations(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target:  SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModePreserve},
	}

	ops, fit, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no operations for preserve mode, got %+v", ops)
	}
	if fit.Status != FitStatusClear {
		t.Fatalf("expected clear fit status for no spatial change, got %s", fit.Status)
	}
}

func TestResolveSpatialChanges_UnresolvableRoomBoundaryBlocksMovement(t *testing.T) {
	// Open (non-closed) wall loop: only 2 of 4 walls present.
	draft := RoomDraft{
		Walls: []RoomDraftWall{
			{ID: "wall_south", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
		},
		Objects: []RoomDraftObject{
			{ID: "object_sofa", Category: "sofa", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 2, Y: 0, Z: 0.5}, Rotation: RoomLocalQuaternion{W: 1}}, Dimensions: &RoomLocalPoint{X: 2, Y: 0.85, Z: 0.5}},
		},
	}
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
	}

	ops, fit, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no resolved operations for unresolvable boundary, got %+v", ops)
	}
	if fit.Status != FitStatusBlocked {
		t.Fatalf("expected blocked fit status, got %s", fit.Status)
	}
	found := false
	for _, b := range fit.Blockers {
		if b.Code == "room_boundary_unresolvable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected room_boundary_unresolvable blocker, got %+v", fit.Blockers)
	}
}

func TestResolveSpatialChanges_OverlapWithNeighborBlocked(t *testing.T) {
	draft := rectRoomDraft()
	// Place a second object right where moving object_sofa away from the
	// wall would land it, forcing an overlap.
	draft.Objects = append(draft.Objects, RoomDraftObject{
		ID: "object_table", Category: "table",
		Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 2, Y: 0, Z: 0.7}, Rotation: RoomLocalQuaternion{W: 1}},
		Dimensions: &RoomLocalPoint{X: 3.5, Y: 0.5, Z: 3.5}, // huge footprint guarantees overlap anywhere nearby
	})
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: SpatialOpMoveRelativeToNearestWall, Relationship: SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
	}

	ops, fit, err := resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no resolved operations for an overlapping move, got %+v", ops)
	}
	if fit.Status != FitStatusBlocked {
		t.Fatalf("expected blocked fit status for overlap, got %s", fit.Status)
	}
}

func TestResolveSpatialChanges_UnsupportedOperationKindErrors(t *testing.T) {
	draft := rectRoomDraft()
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"})
	if err != nil {
		t.Fatalf("resolveDesignTarget: %v", err)
	}
	delta := ProposedSceneEditDelta{
		Target: SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa"},
		Spatial: SectionChange{Mode: SectionModeReplace, SpatialSpec: &ProposedSpatialSpec{
			Kind: "rotate_object",
		}},
	}

	_, _, err = resolveSpatialChanges(draft, target, WorkingDesign{}, delta)
	if err != ErrUnsupportedSpatialOperation {
		t.Fatalf("expected ErrUnsupportedSpatialOperation, got %v", err)
	}
}
