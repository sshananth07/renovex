package spatial

import (
	"errors"
	"testing"
)

// --- fixtures ---

func draftWithOneWall() RoomDraft {
	return RoomDraft{
		Walls: []RoomDraftWall{
			{ID: "wall_1", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
		},
	}
}

func draftWithTwoAdjacentWalls() RoomDraft {
	return RoomDraft{
		Walls: []RoomDraftWall{
			{ID: "wall_1", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
			{ID: "wall_2", Start: RoomLocalPoint{X: 4, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 3}},
		},
	}
}

func draftWithOneOpening() RoomDraft {
	d := draftWithOneWall()
	d.Openings = []RoomDraftOpening{
		{ID: "opening_1", ParentWallID: "wall_1", Kind: OpeningKindWindow, Profile: OpeningProfileRectangle},
	}
	return d
}

func draftWithOneDoor() RoomDraft {
	d := draftWithOneWall()
	d.Openings = []RoomDraftOpening{
		{ID: "door_1", ParentWallID: "wall_1", Kind: OpeningKindDoor, Profile: OpeningProfileRectangle},
	}
	return d
}

func draftWithOneObject() RoomDraft {
	return RoomDraft{
		Objects: []RoomDraftObject{{ID: "object_1", Category: "sofa"}},
	}
}

func draftWithOneFixture() RoomDraft {
	return RoomDraft{
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
	}
}

func draftWithOneServicePoint() RoomDraft {
	return RoomDraft{
		ServicePoints: []RoomDraftServicePoint{{ID: "sp_1", Kind: ServicePointKindPlumbing, CreatedBy: ElementOriginContractor}},
	}
}

func draftWithOneConstraint() RoomDraft {
	return RoomDraft{
		Constraints: []RoomDraftConstraint{{ID: "constraint_1", Kind: ConstraintKindColumn, CreatedBy: ElementOriginContractor}},
	}
}

// --- move_corner ---

func TestMoveCornerOperation_MovesExactlySuppliedEndpoints(t *testing.T) {
	draft := draftWithTwoAdjacentWalls()
	op := MoveCornerOperation{
		Endpoints: []WallEndpointRef{
			{WallID: "wall_1", Endpoint: "end"},
			{WallID: "wall_2", Endpoint: "start"},
		},
		NewPosition: RoomLocalPoint{X: 4.1, Y: 0, Z: 0.1},
	}

	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[0].End != op.NewPosition {
		t.Fatalf("expected wall_1.End moved, got %+v", updated.Walls[0].End)
	}
	if updated.Walls[1].Start != op.NewPosition {
		t.Fatalf("expected wall_2.Start moved, got %+v", updated.Walls[1].Start)
	}
}

func TestMoveCornerOperation_RejectsEmptyEndpointList(t *testing.T) {
	op := MoveCornerOperation{NewPosition: RoomLocalPoint{}}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestMoveCornerOperation_RejectsUnknownWall(t *testing.T) {
	draft := draftWithOneWall()
	op := MoveCornerOperation{
		Endpoints:   []WallEndpointRef{{WallID: "nonexistent", Endpoint: "start"}},
		NewPosition: RoomLocalPoint{},
	}
	_, err := op.Apply(draft)
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestMoveCornerOperation_RejectsNonCoincidentEndpoints(t *testing.T) {
	// wall_1.End is at (4,0,0); wall_2.Start is at (4,0,0) too (adjacent) —
	// but wall_2.End at (4,0,3) is NOT coincident with wall_1.End, so
	// supplying it alongside wall_1.End must be rejected as implausible.
	draft := draftWithTwoAdjacentWalls()
	op := MoveCornerOperation{
		Endpoints: []WallEndpointRef{
			{WallID: "wall_1", Endpoint: "end"},
			{WallID: "wall_2", Endpoint: "end"},
		},
		NewPosition: RoomLocalPoint{X: 5, Y: 0, Z: 0},
	}
	_, err := op.Apply(draft)
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation for non-coincident endpoints, got %v", err)
	}
}

func TestMoveCornerOperation_AcceptsEndpointsWithinTolerance(t *testing.T) {
	draft := RoomDraft{
		Walls: []RoomDraftWall{
			{ID: "wall_1", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
			// wall_2's start is 1cm off from wall_1's end — within the 2cm
			// tolerance, so this must be accepted as plausibly one corner.
			{ID: "wall_2", Start: RoomLocalPoint{X: 4.01, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 3}},
		},
	}
	op := MoveCornerOperation{
		Endpoints: []WallEndpointRef{
			{WallID: "wall_1", Endpoint: "end"},
			{WallID: "wall_2", Endpoint: "start"},
		},
		NewPosition: RoomLocalPoint{X: 4.5, Y: 0, Z: 0},
	}
	if _, err := op.Apply(draft); err != nil {
		t.Fatalf("expected endpoints within tolerance to be accepted, got %v", err)
	}
}

func TestMoveCornerOperation_MutationScopeIsExactlySuppliedSet(t *testing.T) {
	// A third wall coincident with the corner but NOT listed in Endpoints
	// must remain untouched — proves no auto-detection/scope-widening.
	draft := RoomDraft{
		Walls: []RoomDraftWall{
			{ID: "wall_1", Start: RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 0}},
			{ID: "wall_2", Start: RoomLocalPoint{X: 4, Y: 0, Z: 0}, End: RoomLocalPoint{X: 4, Y: 0, Z: 3}},
			{ID: "wall_3_unlisted", Start: RoomLocalPoint{X: 4, Y: 0, Z: 0}, End: RoomLocalPoint{X: 8, Y: 0, Z: 0}},
		},
	}
	op := MoveCornerOperation{
		Endpoints:   []WallEndpointRef{{WallID: "wall_1", Endpoint: "end"}},
		NewPosition: RoomLocalPoint{X: 4.2, Y: 0, Z: 0.1},
	}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[2].Start != (RoomLocalPoint{X: 4, Y: 0, Z: 0}) {
		t.Fatalf("expected unlisted wall_3 to remain untouched, got %+v", updated.Walls[2].Start)
	}
}

// --- move_wall / set_wall_thickness ---

func TestMoveWallOperation_TranslatesBothEndpoints(t *testing.T) {
	draft := draftWithOneWall()
	op := MoveWallOperation{WallID: "wall_1", Delta: RoomLocalPoint{X: 1, Y: 0, Z: 1}}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[0].Start != (RoomLocalPoint{X: 1, Y: 0, Z: 1}) {
		t.Fatalf("expected start translated, got %+v", updated.Walls[0].Start)
	}
	if updated.Walls[0].End != (RoomLocalPoint{X: 5, Y: 0, Z: 1}) {
		t.Fatalf("expected end translated, got %+v", updated.Walls[0].End)
	}
}

func TestMoveWallOperation_RejectsUnknownWall(t *testing.T) {
	op := MoveWallOperation{WallID: "nonexistent"}
	_, err := op.Apply(draftWithOneWall())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestSetWallThicknessOperation_SetsThicknessAndStatus(t *testing.T) {
	draft := draftWithOneWall()
	op := SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.18, Status: MeasurementStatusEstimated}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[0].Thickness == nil || *updated.Walls[0].Thickness != 0.18 {
		t.Fatalf("expected thickness 0.18, got %+v", updated.Walls[0].Thickness)
	}
	if updated.Walls[0].ThicknessStatus != MeasurementStatusEstimated {
		t.Fatalf("expected status estimated, got %s", updated.Walls[0].ThicknessStatus)
	}
}

func TestSetWallThicknessOperation_RejectsNonPositiveThickness(t *testing.T) {
	op := SetWallThicknessOperation{WallID: "wall_1", Thickness: 0, Status: MeasurementStatusEstimated}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- opening operations ---

func TestAddOpeningOperation_AddsToExistingWall(t *testing.T) {
	draft := draftWithOneWall()
	op := AddOpeningOperation{
		ID: "opening_1", ParentWallID: "wall_1", Kind: OpeningKindWindow, Profile: OpeningProfileRectangle,
		Width: 1.0, Height: 1.2,
	}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Openings) != 1 || updated.Openings[0].ID != "opening_1" {
		t.Fatalf("expected one opening added, got %+v", updated.Openings)
	}
}

func TestAddOpeningOperation_RejectsUnknownParentWall(t *testing.T) {
	op := AddOpeningOperation{ID: "o1", ParentWallID: "nonexistent", Kind: OpeningKindWindow, Profile: OpeningProfileRectangle, Width: 1, Height: 1}
	_, err := op.Apply(draftWithOneWall())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestAddOpeningOperation_RejectsDuplicateID(t *testing.T) {
	draft := draftWithOneOpening()
	op := AddOpeningOperation{ID: "opening_1", ParentWallID: "wall_1", Kind: OpeningKindWindow, Profile: OpeningProfileRectangle, Width: 1, Height: 1}
	_, err := op.Apply(draft)
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation for duplicate ID, got %v", err)
	}
}

func TestAddOpeningOperation_RejectsInvalidKind(t *testing.T) {
	op := AddOpeningOperation{ID: "o1", ParentWallID: "wall_1", Kind: OpeningKind("bogus"), Profile: OpeningProfileRectangle, Width: 1, Height: 1}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestRemoveOpeningOperation_RemovesByID(t *testing.T) {
	draft := draftWithOneOpening()
	updated, err := (RemoveOpeningOperation{OpeningID: "opening_1"}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Openings) != 0 {
		t.Fatalf("expected opening removed, got %+v", updated.Openings)
	}
}

func TestRemoveOpeningOperation_RejectsUnknownOpening(t *testing.T) {
	_, err := (RemoveOpeningOperation{OpeningID: "nonexistent"}).Apply(draftWithOneOpening())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestMoveOpeningOperation_UpdatesTransformAndOffset(t *testing.T) {
	draft := draftWithOneOpening()
	op := MoveOpeningOperation{OpeningID: "opening_1", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 2}}, OffsetAlongWall: 2.0}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].OffsetAlongWall == nil || *updated.Openings[0].OffsetAlongWall != 2.0 {
		t.Fatalf("expected offsetAlongWall 2.0, got %+v", updated.Openings[0].OffsetAlongWall)
	}
}

func TestResizeOpeningOperation_SetsWidthHeight(t *testing.T) {
	draft := draftWithOneOpening()
	updated, err := (ResizeOpeningOperation{OpeningID: "opening_1", Width: 1.5, Height: 2.0}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *updated.Openings[0].Width != 1.5 || *updated.Openings[0].Height != 2.0 {
		t.Fatalf("expected resized dimensions, got %+v/%+v", updated.Openings[0].Width, updated.Openings[0].Height)
	}
}

func TestResizeOpeningOperation_RejectsNonPositiveDimensions(t *testing.T) {
	op := ResizeOpeningOperation{OpeningID: "opening_1", Width: 0, Height: 1}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestReclassifyOpeningOperation_ChangesKindAndProfile(t *testing.T) {
	draft := draftWithOneOpening()
	updated, err := (ReclassifyOpeningOperation{OpeningID: "opening_1", Kind: OpeningKindArchway, Profile: OpeningProfileArch}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Kind != OpeningKindArchway || updated.Openings[0].Profile != OpeningProfileArch {
		t.Fatalf("expected archway/arch, got %s/%s", updated.Openings[0].Kind, updated.Openings[0].Profile)
	}
}

func TestReclassifyOpeningOperation_ReclassifyingAwayFromDoorClearsDoorMetadata(t *testing.T) {
	draft := draftWithOneDoor()
	draft.Openings[0].Door = &DoorMetadata{LeafCount: 2}
	updated, err := (ReclassifyOpeningOperation{OpeningID: "door_1", Kind: OpeningKindWindow, Profile: OpeningProfileRectangle}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Door != nil {
		t.Fatalf("expected door metadata cleared after reclassifying away from door, got %+v", updated.Openings[0].Door)
	}
}

// --- door-specific operations ---

func TestSetDoorLeafCountOperation_SetsOnDoorOpening(t *testing.T) {
	draft := draftWithOneDoor()
	updated, err := (SetDoorLeafCountOperation{OpeningID: "door_1", LeafCount: 2}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Door == nil || updated.Openings[0].Door.LeafCount != 2 {
		t.Fatalf("expected leaf count 2, got %+v", updated.Openings[0].Door)
	}
}

func TestSetDoorLeafCountOperation_RejectsNonDoorTarget(t *testing.T) {
	draft := draftWithOneOpening() // Kind = window
	_, err := (SetDoorLeafCountOperation{OpeningID: "opening_1", LeafCount: 1}).Apply(draft)
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation for non-door target, got %v", err)
	}
}

func TestSetDoorLeafCountOperation_RejectsOutOfRangeCount(t *testing.T) {
	op := SetDoorLeafCountOperation{OpeningID: "door_1", LeafCount: 3}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestSetDoorHingeOperation_SetsOnDoorOpening(t *testing.T) {
	draft := draftWithOneDoor()
	updated, err := (SetDoorHingeOperation{OpeningID: "door_1", Hinge: DoorHingeLeft}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Door == nil || updated.Openings[0].Door.Hinge != DoorHingeLeft {
		t.Fatalf("expected hinge left, got %+v", updated.Openings[0].Door)
	}
}

func TestSetDoorSwingOperation_SetsOnDoorOpening(t *testing.T) {
	draft := draftWithOneDoor()
	updated, err := (SetDoorSwingOperation{OpeningID: "door_1", Swing: DoorSwingInward}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Door == nil || updated.Openings[0].Door.Swing != DoorSwingInward {
		t.Fatalf("expected swing inward, got %+v", updated.Openings[0].Door)
	}
}

func TestSetDoorOpenDirectionOperation_SetsOnDoorOpening(t *testing.T) {
	draft := draftWithOneDoor()
	updated, err := (SetDoorOpenDirectionOperation{OpeningID: "door_1", OpenDirection: "north"}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Door == nil || updated.Openings[0].Door.OpenDirection != "north" {
		t.Fatalf("expected open direction north, got %+v", updated.Openings[0].Door)
	}
}

func TestSetDoorOpenDirectionOperation_RejectsEmptyDirection(t *testing.T) {
	op := SetDoorOpenDirectionOperation{OpeningID: "door_1", OpenDirection: ""}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- object operations ---

func TestAddObjectOperation_AddsObject(t *testing.T) {
	updated, err := (AddObjectOperation{ID: "object_1", Category: "chair"}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Objects) != 1 || updated.Objects[0].Category != "chair" {
		t.Fatalf("expected object added, got %+v", updated.Objects)
	}
}

func TestAddObjectOperation_RejectsDuplicateID(t *testing.T) {
	_, err := (AddObjectOperation{ID: "object_1", Category: "table"}).Apply(draftWithOneObject())
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestMoveObjectOperation_UpdatesPosition(t *testing.T) {
	updated, err := (MoveObjectOperation{ObjectID: "object_1", Position: RoomLocalPoint{X: 3}}).Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Transform.Position.X != 3 {
		t.Fatalf("expected position updated, got %+v", updated.Objects[0].Transform.Position)
	}
}

func TestRotateObjectOperation_UpdatesRotation(t *testing.T) {
	rotation := RoomLocalQuaternion{X: 0, Y: 1, Z: 0, W: 0}
	updated, err := (RotateObjectOperation{ObjectID: "object_1", Rotation: rotation}).Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Transform.Rotation != rotation {
		t.Fatalf("expected rotation updated, got %+v", updated.Objects[0].Transform.Rotation)
	}
}

func TestResizeObjectOperation_SetsDimensions(t *testing.T) {
	dims := RoomLocalPoint{X: 1, Y: 1, Z: 1}
	updated, err := (ResizeObjectOperation{ObjectID: "object_1", Dimensions: dims}).Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Dimensions == nil || *updated.Objects[0].Dimensions != dims {
		t.Fatalf("expected dimensions set, got %+v", updated.Objects[0].Dimensions)
	}
}

func TestReclassifyObjectOperation_ChangesCategory(t *testing.T) {
	updated, err := (ReclassifyObjectOperation{ObjectID: "object_1", Category: "armchair"}).Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Objects[0].Category != "armchair" {
		t.Fatalf("expected category armchair, got %s", updated.Objects[0].Category)
	}
}

func TestRemoveObjectOperation_RemovesByID(t *testing.T) {
	updated, err := (RemoveObjectOperation{ObjectID: "object_1"}).Apply(draftWithOneObject())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Objects) != 0 {
		t.Fatalf("expected object removed, got %+v", updated.Objects)
	}
}

// --- restore_element (M8.5C reversible-editing patch): exact-restore for
// deleting a scanned object/opening, since add_object/add_opening cannot
// preserve Provenance/VisualAsset/Appearance. ---

func TestRestoreElementOperation_Object_RestoresExactRecord(t *testing.T) {
	dims := RoomLocalPoint{X: 2, Y: 0.8, Z: 1}
	original := RoomDraftObject{
		ID:         "object_sofa_1",
		Category:   "sofa",
		Transform:  RoomLocalTransform{Position: RoomLocalPoint{X: 1, Z: 1}, Rotation: RoomLocalQuaternion{W: 1}},
		Dimensions: &dims,
		Provenance: ElementProvenance{Provider: SourceProviderRoomPlan, SourceElementIdentifier: "src-1"},
	}
	updated, err := (RestoreElementOperation{Kind: "object", Object: &original}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Objects) != 1 || updated.Objects[0] != original {
		t.Fatalf("expected exact object restored, got %+v", updated.Objects)
	}
}

func TestRestoreElementOperation_Object_RejectsIfIDAlreadyExists(t *testing.T) {
	original := RoomDraftObject{ID: "object_1", Category: "sofa"}
	_, err := (RestoreElementOperation{Kind: "object", Object: &original}).Apply(draftWithOneObject())
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestRestoreElementOperation_Opening_RestoresExactRecord(t *testing.T) {
	width := 0.9
	original := RoomDraftOpening{
		ID:           "opening_1",
		ParentWallID: "wall_1",
		Kind:         OpeningKindDoor,
		Profile:      OpeningProfileRectangle,
		Transform:    RoomLocalTransform{Position: RoomLocalPoint{X: 2}, Rotation: RoomLocalQuaternion{W: 1}},
		Width:        &width,
		Provenance:   ElementProvenance{Provider: SourceProviderRoomPlan, SourceElementIdentifier: "src-2"},
	}
	draft := RoomDraft{Walls: []RoomDraftWall{{ID: "wall_1"}}}
	updated, err := (RestoreElementOperation{Kind: "opening", Opening: &original}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Openings) != 1 || updated.Openings[0] != original {
		t.Fatalf("expected exact opening restored, got %+v", updated.Openings)
	}
}

func TestRestoreElementOperation_Opening_RejectsMissingParentWall(t *testing.T) {
	original := RoomDraftOpening{ID: "opening_1", ParentWallID: "wall_missing", Kind: OpeningKindDoor, Profile: OpeningProfileRectangle}
	_, err := (RestoreElementOperation{Kind: "opening", Opening: &original}).Apply(RoomDraft{})
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestRestoreElementOperation_RejectsUnknownKind(t *testing.T) {
	_, err := (RestoreElementOperation{Kind: "constraint"}).Apply(RoomDraft{})
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation for unsupported restore kind, got %v", err)
	}
}

func TestRestoreElementOperation_RejectsMissingPayloadForKind(t *testing.T) {
	_, err := (RestoreElementOperation{Kind: "object", Object: nil}).Apply(RoomDraft{})
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- M8.5C closure patch: restore_element gains Fixture (append-only, same
// exact-restore contract as object/opening) and Replace (in-place
// whole-record replacement for object/fixture, used by AI Use Design
// Undo/Redo — see designgeneration history docs). ---

func TestRestoreElementOperation_Fixture_RestoresExactRecord(t *testing.T) {
	dims := RoomLocalPoint{X: 0.6, Y: 0.8, Z: 0.4}
	original := RoomDraftFixture{
		ID: "fixture_1", Category: FixtureCategoryBoiler,
		Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 1, Z: 1}, Rotation: RoomLocalQuaternion{W: 1}},
		Dimensions: &dims, CreatedBy: ElementOriginContractor,
	}
	updated, err := (RestoreElementOperation{Kind: "fixture", Fixture: &original}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Fixtures) != 1 || updated.Fixtures[0] != original {
		t.Fatalf("expected exact fixture restored, got %+v", updated.Fixtures)
	}
}

func TestRestoreElementOperation_Fixture_RejectsIfIDAlreadyExists(t *testing.T) {
	original := RoomDraftFixture{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}
	_, err := (RestoreElementOperation{Kind: "fixture", Fixture: &original}).Apply(draftWithOneFixture())
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestRestoreElementOperation_Replace_Object_ReplacesExistingRecordInPlace(t *testing.T) {
	dimsBefore := RoomLocalPoint{X: 1, Y: 1, Z: 1}
	before := RoomDraftObject{ID: "object_1", Category: "sofa", Dimensions: &dimsBefore, Provenance: ElementProvenance{Provider: SourceProviderRoomPlan}}
	draft := RoomDraft{Objects: []RoomDraftObject{before}}

	dimsAfter := RoomLocalPoint{X: 2, Y: 2, Z: 2}
	appearance := validAppearance()
	after := RoomDraftObject{ID: "object_1", Category: "sofa", Dimensions: &dimsAfter, Provenance: ElementProvenance{Provider: SourceProviderRoomPlan}, Appearance: &appearance}

	updated, err := (RestoreElementOperation{Kind: "object", Object: &after, Replace: true}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Objects) != 1 || updated.Objects[0] != after {
		t.Fatalf("expected object replaced in place with exact after-record, got %+v", updated.Objects)
	}
}

func TestRestoreElementOperation_Replace_Object_RejectsWhenTargetDoesNotExist(t *testing.T) {
	original := RoomDraftObject{ID: "object_missing", Category: "sofa"}
	_, err := (RestoreElementOperation{Kind: "object", Object: &original, Replace: true}).Apply(RoomDraft{})
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestRestoreElementOperation_Replace_Fixture_ReplacesExistingRecordInPlace(t *testing.T) {
	before := RoomDraftFixture{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}
	draft := RoomDraft{Fixtures: []RoomDraftFixture{before}}

	appearance := validAppearance()
	after := RoomDraftFixture{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, Appearance: &appearance}
	updated, err := (RestoreElementOperation{Kind: "fixture", Fixture: &after, Replace: true}).Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Fixtures) != 1 || updated.Fixtures[0] != after {
		t.Fatalf("expected fixture replaced in place with exact after-record, got %+v", updated.Fixtures)
	}
}

func TestRestoreElementOperation_Replace_RejectsForOpeningKind(t *testing.T) {
	original := RoomDraftOpening{ID: "opening_1", Kind: OpeningKindDoor, Profile: OpeningProfileRectangle}
	_, err := (RestoreElementOperation{Kind: "opening", Opening: &original, Replace: true}).Apply(RoomDraft{})
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation (Replace only supported for object/fixture), got %v", err)
	}
}

// --- fixture operations ---

func TestAddFixtureOperation_CreatesContractorOriginFixture(t *testing.T) {
	updated, err := (AddFixtureOperation{ID: "fixture_1", Category: FixtureCategoryBoiler}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Fixtures) != 1 || updated.Fixtures[0].CreatedBy != ElementOriginContractor {
		t.Fatalf("expected contractor-origin fixture, got %+v", updated.Fixtures)
	}
	if updated.Fixtures[0].Provenance != nil {
		t.Fatalf("expected nil provenance for contractor-created fixture, got %+v", updated.Fixtures[0].Provenance)
	}
	if err := updated.Fixtures[0].Validate(); err != nil {
		t.Fatalf("expected valid fixture, got %v", err)
	}
}

func TestAddFixtureOperation_RejectsUnknownParentWall(t *testing.T) {
	op := AddFixtureOperation{ID: "f1", Category: FixtureCategoryWallFixture, ParentWallID: "nonexistent"}
	_, err := op.Apply(RoomDraft{})
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestAddFixtureOperation_RejectsInvalidCategory(t *testing.T) {
	op := AddFixtureOperation{ID: "f1", Category: FixtureCategory("bogus")}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestMoveFixtureOperation_UpdatesTransform(t *testing.T) {
	newTransform := RoomLocalTransform{Position: RoomLocalPoint{X: 5}}
	updated, err := (MoveFixtureOperation{FixtureID: "fixture_1", Transform: newTransform}).Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].Transform != newTransform {
		t.Fatalf("expected transform updated, got %+v", updated.Fixtures[0].Transform)
	}
}

func TestResizeFixtureOperation_SetsDimensions(t *testing.T) {
	dims := RoomLocalPoint{X: 0.6, Y: 0.6, Z: 1.5}
	updated, err := (ResizeFixtureOperation{FixtureID: "fixture_1", Dimensions: dims}).Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].Dimensions == nil || *updated.Fixtures[0].Dimensions != dims {
		t.Fatalf("expected dimensions set, got %+v", updated.Fixtures[0].Dimensions)
	}
}

func TestReclassifyFixtureOperation_ChangesCategory(t *testing.T) {
	updated, err := (ReclassifyFixtureOperation{FixtureID: "fixture_1", Category: FixtureCategoryAC}).Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Fixtures[0].Category != FixtureCategoryAC {
		t.Fatalf("expected category ac, got %s", updated.Fixtures[0].Category)
	}
}

func TestRemoveFixtureOperation_RemovesByID(t *testing.T) {
	updated, err := (RemoveFixtureOperation{FixtureID: "fixture_1"}).Apply(draftWithOneFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Fixtures) != 0 {
		t.Fatalf("expected fixture removed, got %+v", updated.Fixtures)
	}
}

func TestRemoveFixtureOperation_RejectsMissingTarget(t *testing.T) {
	_, err := (RemoveFixtureOperation{FixtureID: "nonexistent"}).Apply(draftWithOneFixture())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestRemoveFixtureOperation_RejectsEmptyID(t *testing.T) {
	if err := (RemoveFixtureOperation{}).Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- service point operations ---

func TestAddServicePointOperation_CreatesContractorOriginPoint(t *testing.T) {
	updated, err := (AddServicePointOperation{ID: "sp_1", Kind: ServicePointKindElectrical}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.ServicePoints) != 1 || updated.ServicePoints[0].Provenance != nil {
		t.Fatalf("expected contractor-origin service point with nil provenance, got %+v", updated.ServicePoints)
	}
}

func TestAddServicePointOperation_RejectsInvalidKind(t *testing.T) {
	op := AddServicePointOperation{ID: "sp1", Kind: ServicePointKind("bogus")}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestMoveServicePointOperation_UpdatesPosition(t *testing.T) {
	newPos := RoomLocalPoint{X: 2, Y: 0, Z: 1}
	updated, err := (MoveServicePointOperation{ServicePointID: "sp_1", Position: newPos}).Apply(draftWithOneServicePoint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ServicePoints[0].Position != newPos {
		t.Fatalf("expected position updated, got %+v", updated.ServicePoints[0].Position)
	}
}

func TestRemoveServicePointOperation_RemovesByID(t *testing.T) {
	updated, err := (RemoveServicePointOperation{ServicePointID: "sp_1"}).Apply(draftWithOneServicePoint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.ServicePoints) != 0 {
		t.Fatalf("expected service point removed, got %+v", updated.ServicePoints)
	}
}

func TestRemoveServicePointOperation_RejectsMissingTarget(t *testing.T) {
	_, err := (RemoveServicePointOperation{ServicePointID: "nonexistent"}).Apply(draftWithOneServicePoint())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestRemoveServicePointOperation_RejectsEmptyID(t *testing.T) {
	if err := (RemoveServicePointOperation{}).Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- constraint operations ---

func TestAddConstraintOperation_CreatesContractorOriginConstraint(t *testing.T) {
	updated, err := (AddConstraintOperation{ID: "constraint_1", Kind: ConstraintKindColumn}).Apply(RoomDraft{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Constraints) != 1 || updated.Constraints[0].Provenance != nil {
		t.Fatalf("expected contractor-origin constraint with nil provenance, got %+v", updated.Constraints)
	}
}

func TestAddConstraintOperation_RejectsInvalidKind(t *testing.T) {
	op := AddConstraintOperation{ID: "c1", Kind: ConstraintKind("bogus")}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestMoveConstraintOperation_UpdatesTransform(t *testing.T) {
	newTransform := RoomLocalTransform{Position: RoomLocalPoint{X: 1, Y: 0, Z: 1}}
	updated, err := (MoveConstraintOperation{ConstraintID: "constraint_1", Transform: newTransform}).Apply(draftWithOneConstraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Constraints[0].Transform != newTransform {
		t.Fatalf("expected transform updated, got %+v", updated.Constraints[0].Transform)
	}
}

func TestRemoveConstraintOperation_RemovesByID(t *testing.T) {
	updated, err := (RemoveConstraintOperation{ConstraintID: "constraint_1"}).Apply(draftWithOneConstraint())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Constraints) != 0 {
		t.Fatalf("expected constraint removed, got %+v", updated.Constraints)
	}
}

func TestRemoveConstraintOperation_RejectsMissingTarget(t *testing.T) {
	_, err := (RemoveConstraintOperation{ConstraintID: "nonexistent"}).Apply(draftWithOneConstraint())
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

func TestRemoveConstraintOperation_RejectsEmptyID(t *testing.T) {
	if err := (RemoveConstraintOperation{}).Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- apply_verified_measurement ---

func TestApplyVerifiedMeasurementOperation_UpdatesWallThickness(t *testing.T) {
	draft := draftWithOneWall()
	op := ApplyVerifiedMeasurementOperation{
		TargetKind: MeasurementTargetWall, TargetID: "wall_1", Field: MeasurementFieldThickness,
		Value: 0.18, Status: MeasurementStatusEstimated,
	}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Walls[0].Thickness == nil || *updated.Walls[0].Thickness != 0.18 {
		t.Fatalf("expected thickness 0.18, got %+v", updated.Walls[0].Thickness)
	}
}

func TestApplyVerifiedMeasurementOperation_UpdatesOpeningWidth(t *testing.T) {
	draft := draftWithOneOpening()
	op := ApplyVerifiedMeasurementOperation{
		TargetKind: MeasurementTargetOpening, TargetID: "opening_1", Field: MeasurementFieldWidth,
		Value: 0.95, Status: MeasurementStatusEstimated,
	}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Openings[0].Width == nil || *updated.Openings[0].Width != 0.95 {
		t.Fatalf("expected width 0.95, got %+v", updated.Openings[0].Width)
	}
}

func TestApplyVerifiedMeasurementOperation_RejectsWallWidthField(t *testing.T) {
	// "width" is not a valid field for a wall target (walls have
	// thickness/height, not width) — this proves target/field cross-validation.
	op := ApplyVerifiedMeasurementOperation{
		TargetKind: MeasurementTargetWall, TargetID: "wall_1", Field: MeasurementFieldWidth,
		Value: 1, Status: MeasurementStatusEstimated,
	}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestApplyVerifiedMeasurementOperation_RejectsOpeningThicknessField(t *testing.T) {
	op := ApplyVerifiedMeasurementOperation{
		TargetKind: MeasurementTargetOpening, TargetID: "opening_1", Field: MeasurementFieldThickness,
		Value: 1, Status: MeasurementStatusEstimated,
	}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestApplyVerifiedMeasurementOperation_RejectsNonPositiveValue(t *testing.T) {
	op := ApplyVerifiedMeasurementOperation{
		TargetKind: MeasurementTargetWall, TargetID: "wall_1", Field: MeasurementFieldThickness,
		Value: 0, Status: MeasurementStatusEstimated,
	}
	if err := op.Validate(); !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

// --- CreatedBy/Provenance discriminated invariant ---

func TestRoomDraftFixture_Validate_CaptureRequiresProvenance(t *testing.T) {
	f := RoomDraftFixture{ID: "f1", Category: FixtureCategoryAC, CreatedBy: ElementOriginCapture, Provenance: nil}
	if err := f.Validate(); !errors.Is(err, ErrInvalidElementProvenance) {
		t.Fatalf("expected ErrInvalidElementProvenance for capture-origin with nil provenance, got %v", err)
	}
}

func TestRoomDraftFixture_Validate_CaptureWithProvenanceIsValid(t *testing.T) {
	f := RoomDraftFixture{
		ID: "f1", Category: FixtureCategoryAC, CreatedBy: ElementOriginCapture,
		Provenance: &ElementProvenance{Provider: SourceProviderRoomPlan, SourceElementIdentifier: "roomplan-1"},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestRoomDraftFixture_Validate_ContractorForbidsProvenance(t *testing.T) {
	f := RoomDraftFixture{
		ID: "f1", Category: FixtureCategoryAC, CreatedBy: ElementOriginContractor,
		Provenance: &ElementProvenance{Provider: SourceProviderRoomPlan, SourceElementIdentifier: "fake"},
	}
	if err := f.Validate(); !errors.Is(err, ErrInvalidElementProvenance) {
		t.Fatalf("expected ErrInvalidElementProvenance for contractor-origin with synthesized provenance, got %v", err)
	}
}

func TestRoomDraftFixture_Validate_ContractorWithNilProvenanceIsValid(t *testing.T) {
	f := RoomDraftFixture{ID: "f1", Category: FixtureCategoryAC, CreatedBy: ElementOriginContractor, Provenance: nil}
	if err := f.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestRoomDraftServicePoint_Validate_EnforcesSameInvariant(t *testing.T) {
	sp := RoomDraftServicePoint{ID: "sp1", Kind: ServicePointKindDrain, CreatedBy: ElementOriginCapture, Provenance: nil}
	if err := sp.Validate(); !errors.Is(err, ErrInvalidElementProvenance) {
		t.Fatalf("expected ErrInvalidElementProvenance, got %v", err)
	}
}

func TestRoomDraftConstraint_Validate_EnforcesSameInvariant(t *testing.T) {
	c := RoomDraftConstraint{ID: "c1", Kind: ConstraintKindColumn, CreatedBy: ElementOriginCapture, Provenance: nil}
	if err := c.Validate(); !errors.Is(err, ErrInvalidElementProvenance) {
		t.Fatalf("expected ErrInvalidElementProvenance, got %v", err)
	}
}
