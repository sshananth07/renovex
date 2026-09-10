package spatial

import "math"

// This file implements design spec §8.13's shared geometry-edit operation
// vocabulary (plan RP4A, extended by RP4C2 — see below). The wire
// discriminator (a "kind" string) plus each operation's typed field set is
// the actual canonical cross-platform contract — these Go structs are one
// conforming implementation, not the contract's source of truth. iOS's
// mirror types live in
// ios/RenovexCapture/Sources/RenovexCaptureCore/Capture/EditOperation.swift;
// both sides are proven to agree via matching JSON fixtures in each
// language's test suite (the same pattern RP2's CoordinateContractTests
// established for the coordinate frame), not a shared schema file.
//
// RP4A shipped 27 operations; RP4C2 added 3 more (remove_fixture/
// remove_service_point/remove_constraint, discovered missing while
// building the Web fixture/service-point/constraint editors — RP4A had
// only ever implemented add/move, plus resize/reclassify for fixtures
// only) for 30 total. Validation here is target-existence and shape
// validation only — NOT Task 14's safe/constraint_checked/
// professional_verification_required/prohibited safety classification,
// which belongs to the separate AI design-proposal pipeline (design spec
// §19-§21) and does not apply to direct contractor edits of a RoomDraft.
//
// Explicitly NOT implemented (documented gap, not silently invented):
// merge_wall/split_wall. Design spec §8.15 mentions wall merge/split "where
// supported," but §8.13's canonical operation list does not name them, and
// a topology-changing edit needs defined identity/attachment-remapping/
// constraint-migration/reset semantics this task does not attempt to
// design.

// EditOperation is the interface every one of the 30 operation types below
// satisfies: a wire-discriminator identity, shape validation, and
// application against a RoomDraft. This is the seam RP4B's HTTP handler
// decodes into and dispatches through.
type EditOperation interface {
	OperationKind() EditOperationKind
	Validate() error
	Apply(draft RoomDraft) (RoomDraft, error)
}

// EditOperationKind is the wire discriminator every edit operation payload
// carries alongside its typed fields.
type EditOperationKind string

const (
	EditOpMoveCorner               EditOperationKind = "move_corner"
	EditOpMoveWall                 EditOperationKind = "move_wall"
	EditOpSetWallThickness         EditOperationKind = "set_wall_thickness"
	EditOpAddOpening               EditOperationKind = "add_opening"
	EditOpRemoveOpening            EditOperationKind = "remove_opening"
	EditOpMoveOpening              EditOperationKind = "move_opening"
	EditOpResizeOpening            EditOperationKind = "resize_opening"
	EditOpReclassifyOpening        EditOperationKind = "reclassify_opening"
	EditOpSetDoorLeafCount         EditOperationKind = "set_door_leaf_count"
	EditOpSetDoorHinge             EditOperationKind = "set_door_hinge"
	EditOpSetDoorSwing             EditOperationKind = "set_door_swing"
	EditOpSetDoorOpenDirection     EditOperationKind = "set_door_open_direction"
	EditOpAddObject                EditOperationKind = "add_object"
	EditOpMoveObject               EditOperationKind = "move_object"
	EditOpRotateObject             EditOperationKind = "rotate_object"
	EditOpResizeObject             EditOperationKind = "resize_object"
	EditOpReclassifyObject         EditOperationKind = "reclassify_object"
	EditOpRemoveObject             EditOperationKind = "remove_object"
	EditOpAddFixture               EditOperationKind = "add_fixture"
	EditOpMoveFixture              EditOperationKind = "move_fixture"
	EditOpResizeFixture            EditOperationKind = "resize_fixture"
	EditOpReclassifyFixture        EditOperationKind = "reclassify_fixture"
	EditOpRemoveFixture            EditOperationKind = "remove_fixture"
	EditOpAddServicePoint          EditOperationKind = "add_service_point"
	EditOpMoveServicePoint         EditOperationKind = "move_service_point"
	EditOpRemoveServicePoint       EditOperationKind = "remove_service_point"
	EditOpAddConstraint            EditOperationKind = "add_constraint"
	EditOpMoveConstraint           EditOperationKind = "move_constraint"
	EditOpRemoveConstraint         EditOperationKind = "remove_constraint"
	EditOpApplyVerifiedMeasurement EditOperationKind = "apply_verified_measurement"
	EditOpAssignVisualAsset        EditOperationKind = "assign_visual_asset"
	EditOpClearVisualAsset         EditOperationKind = "clear_visual_asset"
	EditOpSetVisualAppearance      EditOperationKind = "set_visual_appearance"
	EditOpClearVisualAppearance    EditOperationKind = "clear_visual_appearance"
	// EditOpRestoreElement is the M8.5C reversible-editing patch's exact
	// delete-undo primitive for object/opening — see RestoreElementOperation.
	EditOpRestoreElement EditOperationKind = "restore_element"
)

// cornerCoincidenceEpsilonMeters is the project's canonical geometry
// tolerance for validating (never auto-detecting) that a caller-supplied
// set of wall endpoints plausibly represents one shared corner — matching
// ios/RenovexCapture/Sources/RenovexCaptureCore/Capture/CoincidentWallSanityCheck.swift's
// defaultEpsilonMeters (2cm). Go cannot import that Swift constant
// directly, so this is a separately declared value kept numerically
// identical to it by convention; if one changes, the other must too.
const cornerCoincidenceEpsilonMeters = 0.02

// --- Wall / geometry operations ---

// WallEndpointRef identifies one end of one wall — the atomic unit
// MoveCornerOperation moves. Endpoint is "start" or "end".
type WallEndpointRef struct {
	WallID   string `json:"wallId"`
	Endpoint string `json:"endpoint"`
}

// MoveCornerOperation moves a shared corner by moving every explicitly
// listed (wallID, endpoint) pair to NewPosition atomically. Endpoints are
// supplied by the caller, never auto-detected by coincidence-searching —
// mutation scope is exactly the supplied set. Validate checks that the
// supplied endpoints are plausibly coincident (within
// cornerCoincidenceEpsilonMeters of their CURRENT position, i.e. of each
// other) before Apply moves them; this is a sanity check, not scope
// discovery.
type MoveCornerOperation struct {
	Endpoints   []WallEndpointRef `json:"endpoints"`
	NewPosition RoomLocalPoint    `json:"newPosition"`
}

func (op MoveCornerOperation) OperationKind() EditOperationKind { return EditOpMoveCorner }

func (op MoveCornerOperation) Validate() error {
	if len(op.Endpoints) == 0 {
		return ErrInvalidEditOperation
	}
	for _, ref := range op.Endpoints {
		if ref.WallID == "" {
			return ErrInvalidEditOperation
		}
		if ref.Endpoint != "start" && ref.Endpoint != "end" {
			return ErrInvalidEditOperation
		}
	}
	return nil
}

// Apply moves exactly the supplied endpoints to NewPosition, after
// confirming every referenced wall exists and every supplied endpoint's
// CURRENT position is within cornerCoincidenceEpsilonMeters of the first
// endpoint's current position (a sanity check that the caller's supplied
// set plausibly is one corner, not a scope-widening search).
func (op MoveCornerOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}

	indices := make([]int, 0, len(op.Endpoints))
	var referencePosition *RoomLocalPoint
	for _, ref := range op.Endpoints {
		idx := findWallIndex(draft.Walls, ref.WallID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		current := endpointPosition(draft.Walls[idx], ref.Endpoint)
		if referencePosition == nil {
			referencePosition = &current
		} else if distance(*referencePosition, current) > cornerCoincidenceEpsilonMeters {
			return RoomDraft{}, ErrInvalidEditOperation
		}
		indices = append(indices, idx)
	}

	for i, ref := range op.Endpoints {
		setEndpointPosition(&draft.Walls[indices[i]], ref.Endpoint, op.NewPosition)
	}
	return draft, nil
}

// MoveWallOperation translates both endpoints of one wall by Delta.
type MoveWallOperation struct {
	WallID string         `json:"wallId"`
	Delta  RoomLocalPoint `json:"delta"`
}

func (op MoveWallOperation) OperationKind() EditOperationKind { return EditOpMoveWall }

func (op MoveWallOperation) Validate() error {
	if op.WallID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveWallOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findWallIndex(draft.Walls, op.WallID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Walls[idx].Start = addPoints(draft.Walls[idx].Start, op.Delta)
	draft.Walls[idx].End = addPoints(draft.Walls[idx].End, op.Delta)
	return draft, nil
}

// SetWallThicknessOperation sets a wall's Thickness under a caller-supplied
// MeasurementStatus — the contractor-correction path (design spec §8.14):
// RoomPlan's own thickness estimate never becomes authoritative merely by
// existing, but an explicit set_wall_thickness call (status=estimated or,
// per RP5's later evidence tiers, a stronger status) can update it.
type SetWallThicknessOperation struct {
	WallID    string            `json:"wallId"`
	Thickness float64           `json:"thickness"`
	Status    MeasurementStatus `json:"status"`
}

func (op SetWallThicknessOperation) OperationKind() EditOperationKind { return EditOpSetWallThickness }

func (op SetWallThicknessOperation) Validate() error {
	if op.WallID == "" || op.Thickness <= 0 {
		return ErrInvalidEditOperation
	}
	if op.Status != MeasurementStatusEstimated && op.Status != MeasurementStatusUnconfirmed {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetWallThicknessOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findWallIndex(draft.Walls, op.WallID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	thickness := op.Thickness
	draft.Walls[idx].Thickness = &thickness
	draft.Walls[idx].ThicknessStatus = op.Status
	return draft, nil
}

// --- Opening operations ---

// AddOpeningOperation adds a new opening to ParentWallID. The new opening
// is always contractor-authored (no capture provenance) — RP4A's
// add_opening always originates from a contractor action, never from
// re-running capture normalization.
type AddOpeningOperation struct {
	ID              string             `json:"id"`
	ParentWallID    string             `json:"parentWallId"`
	Kind            OpeningKind        `json:"kind"`
	Profile         OpeningProfile     `json:"profile"`
	Transform       RoomLocalTransform `json:"transform"`
	Width           float64            `json:"width"`
	Height          float64            `json:"height"`
	OffsetAlongWall float64            `json:"offsetAlongWall"`
}

func (op AddOpeningOperation) OperationKind() EditOperationKind { return EditOpAddOpening }

func (op AddOpeningOperation) Validate() error {
	if op.ID == "" || op.ParentWallID == "" {
		return ErrInvalidEditOperation
	}
	if !isValidOpeningKind(op.Kind) || !isValidOpeningProfile(op.Profile) {
		return ErrInvalidEditOperation
	}
	if op.Width <= 0 || op.Height <= 0 {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AddOpeningOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	if findWallIndex(draft.Walls, op.ParentWallID) < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	for _, o := range draft.Openings {
		if o.ID == op.ID {
			return RoomDraft{}, ErrInvalidEditOperation
		}
	}
	width, height, offset := op.Width, op.Height, op.OffsetAlongWall
	draft.Openings = append(draft.Openings, RoomDraftOpening{
		ID: op.ID, ParentWallID: op.ParentWallID, Kind: op.Kind, Profile: op.Profile,
		Transform: op.Transform, Width: &width, Height: &height, OffsetAlongWall: &offset,
	})
	return draft, nil
}

// RemoveOpeningOperation removes one opening by ID.
type RemoveOpeningOperation struct {
	OpeningID string `json:"openingId"`
}

func (op RemoveOpeningOperation) OperationKind() EditOperationKind { return EditOpRemoveOpening }

func (op RemoveOpeningOperation) Validate() error {
	if op.OpeningID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RemoveOpeningOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findOpeningIndex(draft.Openings, op.OpeningID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Openings = append(draft.Openings[:idx], draft.Openings[idx+1:]...)
	return draft, nil
}

// MoveOpeningOperation updates an opening's position along its parent wall
// and/or its room-local transform.
type MoveOpeningOperation struct {
	OpeningID       string             `json:"openingId"`
	Transform       RoomLocalTransform `json:"transform"`
	OffsetAlongWall float64            `json:"offsetAlongWall"`
}

func (op MoveOpeningOperation) OperationKind() EditOperationKind { return EditOpMoveOpening }

func (op MoveOpeningOperation) Validate() error {
	if op.OpeningID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveOpeningOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findOpeningIndex(draft.Openings, op.OpeningID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Openings[idx].Transform = op.Transform
	offset := op.OffsetAlongWall
	draft.Openings[idx].OffsetAlongWall = &offset
	return draft, nil
}

// ResizeOpeningOperation sets an opening's Width/Height.
type ResizeOpeningOperation struct {
	OpeningID string  `json:"openingId"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
}

func (op ResizeOpeningOperation) OperationKind() EditOperationKind { return EditOpResizeOpening }

func (op ResizeOpeningOperation) Validate() error {
	if op.OpeningID == "" || op.Width <= 0 || op.Height <= 0 {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ResizeOpeningOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findOpeningIndex(draft.Openings, op.OpeningID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	width, height := op.Width, op.Height
	draft.Openings[idx].Width = &width
	draft.Openings[idx].Height = &height
	return draft, nil
}

// ReclassifyOpeningOperation changes an opening's Kind and/or Profile
// (design spec §8.15: opening <-> door <-> window <-> archway
// reclassification, orthogonally to rectangle <-> arch profile change).
type ReclassifyOpeningOperation struct {
	OpeningID string         `json:"openingId"`
	Kind      OpeningKind    `json:"kind"`
	Profile   OpeningProfile `json:"profile"`
}

func (op ReclassifyOpeningOperation) OperationKind() EditOperationKind {
	return EditOpReclassifyOpening
}

func (op ReclassifyOpeningOperation) Validate() error {
	if op.OpeningID == "" {
		return ErrInvalidEditOperation
	}
	if !isValidOpeningKind(op.Kind) || !isValidOpeningProfile(op.Profile) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ReclassifyOpeningOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findOpeningIndex(draft.Openings, op.OpeningID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Openings[idx].Kind = op.Kind
	draft.Openings[idx].Profile = op.Profile
	// Reclassifying away from door discards stale door metadata — a window
	// or archway carrying leaf-count/hinge/swing would be meaningless.
	if op.Kind != OpeningKindDoor {
		draft.Openings[idx].Door = nil
	}
	return draft, nil
}

// --- Door-specific operations ---
// Each targets an opening whose Kind must already be OpeningKindDoor —
// applying a door operation to a non-door opening is a target-validity
// error, not a silent no-op or an implicit reclassification.

// ErrTargetNotADoor is returned when a door-specific operation targets an
// opening whose Kind is not OpeningKindDoor.
var errTargetNotADoor = ErrInvalidEditOperation

type SetDoorLeafCountOperation struct {
	OpeningID string `json:"openingId"`
	LeafCount int    `json:"leafCount"`
}

func (op SetDoorLeafCountOperation) OperationKind() EditOperationKind { return EditOpSetDoorLeafCount }

func (op SetDoorLeafCountOperation) Validate() error {
	if op.OpeningID == "" || op.LeafCount < 1 || op.LeafCount > 2 {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetDoorLeafCountOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx, err := findDoorIndex(draft.Openings, op.OpeningID)
	if err != nil {
		return RoomDraft{}, err
	}
	ensureDoorMetadata(&draft.Openings[idx]).LeafCount = op.LeafCount
	return draft, nil
}

type SetDoorHingeOperation struct {
	OpeningID string    `json:"openingId"`
	Hinge     DoorHinge `json:"hinge"`
}

func (op SetDoorHingeOperation) OperationKind() EditOperationKind { return EditOpSetDoorHinge }

func (op SetDoorHingeOperation) Validate() error {
	if op.OpeningID == "" {
		return ErrInvalidEditOperation
	}
	if op.Hinge != DoorHingeLeft && op.Hinge != DoorHingeRight {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetDoorHingeOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx, err := findDoorIndex(draft.Openings, op.OpeningID)
	if err != nil {
		return RoomDraft{}, err
	}
	ensureDoorMetadata(&draft.Openings[idx]).Hinge = op.Hinge
	return draft, nil
}

type SetDoorSwingOperation struct {
	OpeningID string    `json:"openingId"`
	Swing     DoorSwing `json:"swing"`
}

func (op SetDoorSwingOperation) OperationKind() EditOperationKind { return EditOpSetDoorSwing }

func (op SetDoorSwingOperation) Validate() error {
	if op.OpeningID == "" {
		return ErrInvalidEditOperation
	}
	if op.Swing != DoorSwingInward && op.Swing != DoorSwingOutward {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetDoorSwingOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx, err := findDoorIndex(draft.Openings, op.OpeningID)
	if err != nil {
		return RoomDraft{}, err
	}
	ensureDoorMetadata(&draft.Openings[idx]).Swing = op.Swing
	return draft, nil
}

type SetDoorOpenDirectionOperation struct {
	OpeningID     string `json:"openingId"`
	OpenDirection string `json:"openDirection"`
}

func (op SetDoorOpenDirectionOperation) OperationKind() EditOperationKind {
	return EditOpSetDoorOpenDirection
}

func (op SetDoorOpenDirectionOperation) Validate() error {
	if op.OpeningID == "" || op.OpenDirection == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetDoorOpenDirectionOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx, err := findDoorIndex(draft.Openings, op.OpeningID)
	if err != nil {
		return RoomDraft{}, err
	}
	ensureDoorMetadata(&draft.Openings[idx]).OpenDirection = op.OpenDirection
	return draft, nil
}

// --- Object operations ---

type AddObjectOperation struct {
	ID         string             `json:"id"`
	Category   string             `json:"category"`
	Transform  RoomLocalTransform `json:"transform"`
	Dimensions *RoomLocalPoint    `json:"dimensions,omitempty"`
}

func (op AddObjectOperation) OperationKind() EditOperationKind { return EditOpAddObject }

func (op AddObjectOperation) Validate() error {
	if op.ID == "" || op.Category == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AddObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	for _, o := range draft.Objects {
		if o.ID == op.ID {
			return RoomDraft{}, ErrInvalidEditOperation
		}
	}
	draft.Objects = append(draft.Objects, RoomDraftObject{
		ID: op.ID, Category: op.Category, Transform: op.Transform, Dimensions: op.Dimensions,
	})
	return draft, nil
}

type MoveObjectOperation struct {
	ObjectID string         `json:"objectId"`
	Position RoomLocalPoint `json:"position"`
}

func (op MoveObjectOperation) OperationKind() EditOperationKind { return EditOpMoveObject }

func (op MoveObjectOperation) Validate() error {
	if op.ObjectID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findObjectIndex(draft.Objects, op.ObjectID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Objects[idx].Transform.Position = op.Position
	return draft, nil
}

type RotateObjectOperation struct {
	ObjectID string              `json:"objectId"`
	Rotation RoomLocalQuaternion `json:"rotation"`
}

func (op RotateObjectOperation) OperationKind() EditOperationKind { return EditOpRotateObject }

func (op RotateObjectOperation) Validate() error {
	if op.ObjectID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RotateObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findObjectIndex(draft.Objects, op.ObjectID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Objects[idx].Transform.Rotation = op.Rotation
	return draft, nil
}

type ResizeObjectOperation struct {
	ObjectID   string         `json:"objectId"`
	Dimensions RoomLocalPoint `json:"dimensions"`
}

func (op ResizeObjectOperation) OperationKind() EditOperationKind { return EditOpResizeObject }

func (op ResizeObjectOperation) Validate() error {
	if op.ObjectID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ResizeObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findObjectIndex(draft.Objects, op.ObjectID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	dims := op.Dimensions
	draft.Objects[idx].Dimensions = &dims
	return draft, nil
}

type ReclassifyObjectOperation struct {
	ObjectID string `json:"objectId"`
	Category string `json:"category"`
}

func (op ReclassifyObjectOperation) OperationKind() EditOperationKind { return EditOpReclassifyObject }

func (op ReclassifyObjectOperation) Validate() error {
	if op.ObjectID == "" || op.Category == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ReclassifyObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findObjectIndex(draft.Objects, op.ObjectID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Objects[idx].Category = op.Category
	return draft, nil
}

type RemoveObjectOperation struct {
	ObjectID string `json:"objectId"`
}

func (op RemoveObjectOperation) OperationKind() EditOperationKind { return EditOpRemoveObject }

func (op RemoveObjectOperation) Validate() error {
	if op.ObjectID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RemoveObjectOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findObjectIndex(draft.Objects, op.ObjectID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Objects = append(draft.Objects[:idx], draft.Objects[idx+1:]...)
	return draft, nil
}

// RestoreElementOperation has two modes sharing one wire kind
// (M8.5C reversible-editing patch, §9 + closure patch §24-26):
//
//  1. Restore (Replace=false, the original mode): re-adds an exact element
//     removed by remove_object/remove_opening/remove_fixture — the target ID
//     must NOT currently exist. add_object/add_opening cannot serve this
//     role since neither carries Provenance/VisualAsset/Appearance, so an
//     Undo built on them would silently fabricate a NEW element identity
//     rather than restore the original one. Fixture support exists for the
//     same exact-identity guarantee on Undo-delete, even though
//     add_fixture's own caller-supplied ID already makes plain add_fixture
//     usable for redo. service_point/constraint don't need this mode at all
//     (their own add_* is already exact-restore capable end to end); walls
//     have no remove_wall operation to undo in the first place.
//  2. Replace (Replace=true): overwrites an EXISTING object/fixture's
//     entire record in place — the target ID MUST already exist. This is
//     the AI "Use Design" Undo/Redo primitive: Use Design's accepted
//     concept already mutates only transform/dimensions/appearance/
//     visualAsset through canonical operations, so a single atomic
//     whole-record replace correctly captures its Undo (restore the
//     pre-Use-Design record) and Redo (restore the accepted record)
//     without needing a second history mechanism or a new domain type.
//     Not supported for opening/service_point/constraint — Use Design never
//     targets those kinds (DesignTargetKind is object|fixture only).
type RestoreElementOperation struct {
	Kind    string            `json:"kind"`
	Object  *RoomDraftObject  `json:"object,omitempty"`
	Opening *RoomDraftOpening `json:"opening,omitempty"`
	Fixture *RoomDraftFixture `json:"fixture,omitempty"`
	Replace bool              `json:"replace,omitempty"`
}

func (op RestoreElementOperation) OperationKind() EditOperationKind { return EditOpRestoreElement }

func (op RestoreElementOperation) Validate() error {
	switch op.Kind {
	case "object":
		if op.Object == nil || op.Object.ID == "" || op.Object.Category == "" {
			return ErrInvalidEditOperation
		}
	case "fixture":
		if op.Fixture == nil || op.Fixture.ID == "" || op.Fixture.Category == "" {
			return ErrInvalidEditOperation
		}
	case "opening":
		if op.Opening == nil || op.Opening.ID == "" {
			return ErrInvalidEditOperation
		}
		if op.Replace {
			return ErrInvalidEditOperation
		}
	default:
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RestoreElementOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	switch op.Kind {
	case "object":
		idx := findObjectIndex(draft.Objects, op.Object.ID)
		if op.Replace {
			if idx < 0 {
				return RoomDraft{}, ErrEditTargetNotFound
			}
			draft.Objects[idx] = *op.Object
			return draft, nil
		}
		if idx >= 0 {
			return RoomDraft{}, ErrInvalidEditOperation
		}
		draft.Objects = append(draft.Objects, *op.Object)
	case "fixture":
		idx := findFixtureIndex(draft.Fixtures, op.Fixture.ID)
		if op.Replace {
			if idx < 0 {
				return RoomDraft{}, ErrEditTargetNotFound
			}
			draft.Fixtures[idx] = *op.Fixture
			return draft, nil
		}
		if idx >= 0 {
			return RoomDraft{}, ErrInvalidEditOperation
		}
		draft.Fixtures = append(draft.Fixtures, *op.Fixture)
	case "opening":
		if findOpeningIndex(draft.Openings, op.Opening.ID) >= 0 {
			return RoomDraft{}, ErrInvalidEditOperation
		}
		if op.Opening.ParentWallID != "" && findWallIndex(draft.Walls, op.Opening.ParentWallID) < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Openings = append(draft.Openings, *op.Opening)
	}
	return draft, nil
}

// --- Fixture operations ---
// Every fixture RP4A's vocabulary can create is contractor-authored — V1
// has no capture provider that detects FixedFixtures (design spec §8.16).

type AddFixtureOperation struct {
	ID           string             `json:"id"`
	Category     FixtureCategory    `json:"category"`
	Transform    RoomLocalTransform `json:"transform"`
	Dimensions   *RoomLocalPoint    `json:"dimensions,omitempty"`
	ParentWallID string             `json:"parentWallId,omitempty"`
}

func (op AddFixtureOperation) OperationKind() EditOperationKind { return EditOpAddFixture }

func (op AddFixtureOperation) Validate() error {
	if op.ID == "" || !isValidFixtureCategory(op.Category) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AddFixtureOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	if op.ParentWallID != "" && findWallIndex(draft.Walls, op.ParentWallID) < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	for _, f := range draft.Fixtures {
		if f.ID == op.ID {
			return RoomDraft{}, ErrInvalidEditOperation
		}
	}
	fixture := RoomDraftFixture{
		ID: op.ID, Category: op.Category, Transform: op.Transform,
		Dimensions: op.Dimensions, ParentWallID: op.ParentWallID,
		CreatedBy: ElementOriginContractor,
	}
	if err := fixture.Validate(); err != nil {
		return RoomDraft{}, err
	}
	draft.Fixtures = append(draft.Fixtures, fixture)
	return draft, nil
}

type MoveFixtureOperation struct {
	FixtureID string             `json:"fixtureId"`
	Transform RoomLocalTransform `json:"transform"`
}

func (op MoveFixtureOperation) OperationKind() EditOperationKind { return EditOpMoveFixture }

func (op MoveFixtureOperation) Validate() error {
	if op.FixtureID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveFixtureOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findFixtureIndex(draft.Fixtures, op.FixtureID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Fixtures[idx].Transform = op.Transform
	return draft, nil
}

type ResizeFixtureOperation struct {
	FixtureID  string         `json:"fixtureId"`
	Dimensions RoomLocalPoint `json:"dimensions"`
}

func (op ResizeFixtureOperation) OperationKind() EditOperationKind { return EditOpResizeFixture }

func (op ResizeFixtureOperation) Validate() error {
	if op.FixtureID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ResizeFixtureOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findFixtureIndex(draft.Fixtures, op.FixtureID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	dims := op.Dimensions
	draft.Fixtures[idx].Dimensions = &dims
	return draft, nil
}

type ReclassifyFixtureOperation struct {
	FixtureID string          `json:"fixtureId"`
	Category  FixtureCategory `json:"category"`
}

func (op ReclassifyFixtureOperation) OperationKind() EditOperationKind {
	return EditOpReclassifyFixture
}

func (op ReclassifyFixtureOperation) Validate() error {
	if op.FixtureID == "" || !isValidFixtureCategory(op.Category) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ReclassifyFixtureOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findFixtureIndex(draft.Fixtures, op.FixtureID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Fixtures[idx].Category = op.Category
	return draft, nil
}

// RemoveFixtureOperation removes one fixture by ID (RP4C2 addition — RP4A
// originally implemented only add/move/resize/reclassify for fixtures).
type RemoveFixtureOperation struct {
	FixtureID string `json:"fixtureId"`
}

func (op RemoveFixtureOperation) OperationKind() EditOperationKind { return EditOpRemoveFixture }

func (op RemoveFixtureOperation) Validate() error {
	if op.FixtureID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RemoveFixtureOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findFixtureIndex(draft.Fixtures, op.FixtureID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Fixtures = append(draft.Fixtures[:idx], draft.Fixtures[idx+1:]...)
	return draft, nil
}

// --- Service point operations ---
// §8.13 lists add/move for service points (no resize/reclassify — a
// service point is a controlled point, not a dimensioned/typed element the
// vocabulary supports resizing or reclassifying in V1); RP4C2 added remove
// (below), discovered missing when building the Web editor's service-point
// lifecycle.

type AddServicePointOperation struct {
	ID           string           `json:"id"`
	Kind         ServicePointKind `json:"kind"`
	Position     RoomLocalPoint   `json:"position"`
	ParentWallID string           `json:"parentWallId,omitempty"`
}

func (op AddServicePointOperation) OperationKind() EditOperationKind { return EditOpAddServicePoint }

func (op AddServicePointOperation) Validate() error {
	if op.ID == "" || !isValidServicePointKind(op.Kind) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AddServicePointOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	if op.ParentWallID != "" && findWallIndex(draft.Walls, op.ParentWallID) < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	for _, s := range draft.ServicePoints {
		if s.ID == op.ID {
			return RoomDraft{}, ErrInvalidEditOperation
		}
	}
	sp := RoomDraftServicePoint{
		ID: op.ID, Kind: op.Kind, Position: op.Position, ParentWallID: op.ParentWallID,
		CreatedBy: ElementOriginContractor,
	}
	if err := sp.Validate(); err != nil {
		return RoomDraft{}, err
	}
	draft.ServicePoints = append(draft.ServicePoints, sp)
	return draft, nil
}

type MoveServicePointOperation struct {
	ServicePointID string         `json:"servicePointId"`
	Position       RoomLocalPoint `json:"position"`
}

func (op MoveServicePointOperation) OperationKind() EditOperationKind { return EditOpMoveServicePoint }

func (op MoveServicePointOperation) Validate() error {
	if op.ServicePointID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveServicePointOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findServicePointIndex(draft.ServicePoints, op.ServicePointID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.ServicePoints[idx].Position = op.Position
	return draft, nil
}

// RemoveServicePointOperation removes one service point by ID (RP4C2
// addition — RP4A originally implemented only add/move for service
// points).
type RemoveServicePointOperation struct {
	ServicePointID string `json:"servicePointId"`
}

func (op RemoveServicePointOperation) OperationKind() EditOperationKind {
	return EditOpRemoveServicePoint
}

func (op RemoveServicePointOperation) Validate() error {
	if op.ServicePointID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RemoveServicePointOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findServicePointIndex(draft.ServicePoints, op.ServicePointID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.ServicePoints = append(draft.ServicePoints[:idx], draft.ServicePoints[idx+1:]...)
	return draft, nil
}

// --- Constraint operations ---
// §8.13 lists add/move for constraints, matching service points; RP4C2
// added remove (below) for the same reason.

type AddConstraintOperation struct {
	ID         string             `json:"id"`
	Kind       ConstraintKind     `json:"kind"`
	Transform  RoomLocalTransform `json:"transform"`
	Dimensions *RoomLocalPoint    `json:"dimensions,omitempty"`
}

func (op AddConstraintOperation) OperationKind() EditOperationKind { return EditOpAddConstraint }

func (op AddConstraintOperation) Validate() error {
	if op.ID == "" || !isValidConstraintKind(op.Kind) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AddConstraintOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	for _, c := range draft.Constraints {
		if c.ID == op.ID {
			return RoomDraft{}, ErrInvalidEditOperation
		}
	}
	constraint := RoomDraftConstraint{
		ID: op.ID, Kind: op.Kind, Transform: op.Transform, Dimensions: op.Dimensions,
		CreatedBy: ElementOriginContractor,
	}
	if err := constraint.Validate(); err != nil {
		return RoomDraft{}, err
	}
	draft.Constraints = append(draft.Constraints, constraint)
	return draft, nil
}

type MoveConstraintOperation struct {
	ConstraintID string             `json:"constraintId"`
	Transform    RoomLocalTransform `json:"transform"`
}

func (op MoveConstraintOperation) OperationKind() EditOperationKind { return EditOpMoveConstraint }

func (op MoveConstraintOperation) Validate() error {
	if op.ConstraintID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op MoveConstraintOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findConstraintIndex(draft.Constraints, op.ConstraintID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Constraints[idx].Transform = op.Transform
	return draft, nil
}

// RemoveConstraintOperation removes one constraint by ID (RP4C2 addition —
// RP4A originally implemented only add/move for constraints).
type RemoveConstraintOperation struct {
	ConstraintID string `json:"constraintId"`
}

func (op RemoveConstraintOperation) OperationKind() EditOperationKind { return EditOpRemoveConstraint }

func (op RemoveConstraintOperation) Validate() error {
	if op.ConstraintID == "" {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op RemoveConstraintOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	idx := findConstraintIndex(draft.Constraints, op.ConstraintID)
	if idx < 0 {
		return RoomDraft{}, ErrEditTargetNotFound
	}
	draft.Constraints = append(draft.Constraints[:idx], draft.Constraints[idx+1:]...)
	return draft, nil
}

// --- Measurement operation ---

// MeasurementTargetKind identifies which RoomDraft element kind
// ApplyVerifiedMeasurementOperation targets.
type MeasurementTargetKind string

const (
	MeasurementTargetWall    MeasurementTargetKind = "wall"
	MeasurementTargetOpening MeasurementTargetKind = "opening"
)

// MeasurementField identifies which numeric field on the target element the
// verified value applies to.
type MeasurementField string

const (
	MeasurementFieldThickness  MeasurementField = "thickness"
	MeasurementFieldHeight     MeasurementField = "height"
	MeasurementFieldWidth      MeasurementField = "width"
	MeasurementFieldSillHeight MeasurementField = "sillHeight"
)

// ApplyVerifiedMeasurementOperation is design spec §8.13/§8.14's single
// generic measurement-correction operation: RoomPlan wall thickness must
// not automatically become authoritative, and applying a verified value
// must update RoomDraft geometry (never just a displayed number). RP4A
// implements this against MeasurementStatus (estimated/unconfirmed) only —
// the full AR_VERIFIED/PHYSICAL_VERIFIED evidence-provenance model is RP5
// scope, per MeasurementStatus's own doc comment.
type ApplyVerifiedMeasurementOperation struct {
	TargetKind MeasurementTargetKind `json:"targetKind"`
	TargetID   string                `json:"targetId"`
	Field      MeasurementField      `json:"field"`
	Value      float64               `json:"value"`
	Status     MeasurementStatus     `json:"status"`
}

func (op ApplyVerifiedMeasurementOperation) OperationKind() EditOperationKind {
	return EditOpApplyVerifiedMeasurement
}

func (op ApplyVerifiedMeasurementOperation) Validate() error {
	if op.TargetID == "" || op.Value <= 0 {
		return ErrInvalidEditOperation
	}
	switch op.TargetKind {
	case MeasurementTargetWall:
		if op.Field != MeasurementFieldThickness && op.Field != MeasurementFieldHeight {
			return ErrInvalidEditOperation
		}
	case MeasurementTargetOpening:
		switch op.Field {
		case MeasurementFieldWidth, MeasurementFieldHeight, MeasurementFieldSillHeight:
		default:
			return ErrInvalidEditOperation
		}
	default:
		return ErrInvalidEditOperation
	}
	if op.Status != MeasurementStatusEstimated && op.Status != MeasurementStatusUnconfirmed {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ApplyVerifiedMeasurementOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	value := op.Value
	switch op.TargetKind {
	case MeasurementTargetWall:
		idx := findWallIndex(draft.Walls, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		switch op.Field {
		case MeasurementFieldThickness:
			draft.Walls[idx].Thickness = &value
			draft.Walls[idx].ThicknessStatus = op.Status
		case MeasurementFieldHeight:
			draft.Walls[idx].Height = &value
		}
	case MeasurementTargetOpening:
		idx := findOpeningIndex(draft.Openings, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		switch op.Field {
		case MeasurementFieldWidth:
			draft.Openings[idx].Width = &value
		case MeasurementFieldHeight:
			draft.Openings[idx].Height = &value
		case MeasurementFieldSillHeight:
			draft.Openings[idx].SillHeight = &value
		}
	}
	return draft, nil
}

// --- visual asset binding operations (RP4D) ---

// VisualAssetTargetKind identifies which RoomDraft element kind
// AssignVisualAssetOperation/ClearVisualAssetOperation targets. Narrow by
// design — RP4D's visual-asset binding applies only to fixtures and
// objects, never walls/openings/service points/constraints.
type VisualAssetTargetKind string

const (
	VisualAssetTargetFixture VisualAssetTargetKind = "fixture"
	VisualAssetTargetObject  VisualAssetTargetKind = "object"
)

func isValidVisualAssetTargetKind(kind VisualAssetTargetKind) bool {
	switch kind {
	case VisualAssetTargetFixture, VisualAssetTargetObject:
		return true
	default:
		return false
	}
}

// AssignVisualAssetOperation sets a fixture/object's canonical
// VisualAsset reference (RP4D) — identity + exact version only. This is a
// presentation-reference mutation only: it never touches geometry,
// classification, provenance, transform, or dimensions. Validate() here is
// LOCAL/structural only (shape, non-empty IDs, positive version) — it
// cannot verify the referenced asset version actually exists or belongs to
// the caller's company, which requires Mongo access and is
// Service.SubmitEditOperation's job, run before this operation's Apply.
type AssignVisualAssetOperation struct {
	TargetKind VisualAssetTargetKind `json:"targetKind"`
	TargetID   string                `json:"targetId"`
	AssetID    string                `json:"assetId"`
	Version    int                   `json:"version"`
}

func (op AssignVisualAssetOperation) OperationKind() EditOperationKind {
	return EditOpAssignVisualAsset
}

func (op AssignVisualAssetOperation) Validate() error {
	if op.TargetID == "" || !isValidVisualAssetTargetKind(op.TargetKind) {
		return ErrInvalidEditOperation
	}
	if err := (VisualAssetRef{AssetID: op.AssetID, Version: op.Version}).Validate(); err != nil {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op AssignVisualAssetOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	ref := &VisualAssetRef{AssetID: op.AssetID, Version: op.Version}
	switch op.TargetKind {
	case VisualAssetTargetFixture:
		idx := findFixtureIndex(draft.Fixtures, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Fixtures[idx].VisualAsset = ref
	case VisualAssetTargetObject:
		idx := findObjectIndex(draft.Objects, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Objects[idx].VisualAsset = ref
	}
	return draft, nil
}

// ClearVisualAssetOperation removes a fixture/object's canonical
// VisualAsset reference (RP4D), returning it to category-default/
// procedural resolution. No other field changes.
type ClearVisualAssetOperation struct {
	TargetKind VisualAssetTargetKind `json:"targetKind"`
	TargetID   string                `json:"targetId"`
}

func (op ClearVisualAssetOperation) OperationKind() EditOperationKind { return EditOpClearVisualAsset }

func (op ClearVisualAssetOperation) Validate() error {
	if op.TargetID == "" || !isValidVisualAssetTargetKind(op.TargetKind) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ClearVisualAssetOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	switch op.TargetKind {
	case VisualAssetTargetFixture:
		idx := findFixtureIndex(draft.Fixtures, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Fixtures[idx].VisualAsset = nil
	case VisualAssetTargetObject:
		idx := findObjectIndex(draft.Objects, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Objects[idx].VisualAsset = nil
	}
	return draft, nil
}

// --- visual appearance operations (RP4E2/M8.5C) ---

// SetVisualAppearanceOperation sets a fixture/object's optional material-
// only design override (RP4E2/M8.5C amendment) — canonical hex color,
// material family, roughness, metallic only. It never touches geometry,
// classification, provenance, transform, dimensions, or VisualAsset —
// material-only concepts preview and persist independently of any Hunyuan
// generation. Reuses VisualAssetTargetKind's existing fixture|object
// vocabulary rather than inventing a parallel one.
type SetVisualAppearanceOperation struct {
	TargetKind VisualAssetTargetKind `json:"targetKind"`
	TargetID   string                `json:"targetId"`
	Appearance VisualAppearance      `json:"appearance"`
}

func (op SetVisualAppearanceOperation) OperationKind() EditOperationKind {
	return EditOpSetVisualAppearance
}

func (op SetVisualAppearanceOperation) Validate() error {
	if op.TargetID == "" || !isValidVisualAssetTargetKind(op.TargetKind) {
		return ErrInvalidEditOperation
	}
	appearance := op.Appearance
	if err := appearance.Validate(); err != nil {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op SetVisualAppearanceOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	appearance := op.Appearance
	if err := appearance.Validate(); err != nil {
		return RoomDraft{}, ErrInvalidEditOperation
	}
	if op.TargetID == "" || !isValidVisualAssetTargetKind(op.TargetKind) {
		return RoomDraft{}, ErrInvalidEditOperation
	}
	switch op.TargetKind {
	case VisualAssetTargetFixture:
		idx := findFixtureIndex(draft.Fixtures, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Fixtures[idx].Appearance = &appearance
	case VisualAssetTargetObject:
		idx := findObjectIndex(draft.Objects, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Objects[idx].Appearance = &appearance
	}
	return draft, nil
}

// ClearVisualAppearanceOperation removes a fixture/object's material-only
// design override, returning it to no appearance override. No other field
// changes.
type ClearVisualAppearanceOperation struct {
	TargetKind VisualAssetTargetKind `json:"targetKind"`
	TargetID   string                `json:"targetId"`
}

func (op ClearVisualAppearanceOperation) OperationKind() EditOperationKind {
	return EditOpClearVisualAppearance
}

func (op ClearVisualAppearanceOperation) Validate() error {
	if op.TargetID == "" || !isValidVisualAssetTargetKind(op.TargetKind) {
		return ErrInvalidEditOperation
	}
	return nil
}

func (op ClearVisualAppearanceOperation) Apply(draft RoomDraft) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	switch op.TargetKind {
	case VisualAssetTargetFixture:
		idx := findFixtureIndex(draft.Fixtures, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Fixtures[idx].Appearance = nil
	case VisualAssetTargetObject:
		idx := findObjectIndex(draft.Objects, op.TargetID)
		if idx < 0 {
			return RoomDraft{}, ErrEditTargetNotFound
		}
		draft.Objects[idx].Appearance = nil
	}
	return draft, nil
}

// --- shared helpers ---

func findWallIndex(walls []RoomDraftWall, id string) int {
	for i, w := range walls {
		if w.ID == id {
			return i
		}
	}
	return -1
}

func findOpeningIndex(openings []RoomDraftOpening, id string) int {
	for i, o := range openings {
		if o.ID == id {
			return i
		}
	}
	return -1
}

func findObjectIndex(objects []RoomDraftObject, id string) int {
	for i, o := range objects {
		if o.ID == id {
			return i
		}
	}
	return -1
}

func findFixtureIndex(fixtures []RoomDraftFixture, id string) int {
	for i, f := range fixtures {
		if f.ID == id {
			return i
		}
	}
	return -1
}

func findServicePointIndex(points []RoomDraftServicePoint, id string) int {
	for i, p := range points {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func findConstraintIndex(constraints []RoomDraftConstraint, id string) int {
	for i, c := range constraints {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// findDoorIndex resolves openingID to an index in openings, requiring its
// Kind to already be OpeningKindDoor — a door-specific operation targeting
// a non-door opening is an error, not a silent no-op or implicit
// reclassification.
func findDoorIndex(openings []RoomDraftOpening, openingID string) (int, error) {
	idx := findOpeningIndex(openings, openingID)
	if idx < 0 {
		return -1, ErrEditTargetNotFound
	}
	if openings[idx].Kind != OpeningKindDoor {
		return -1, errTargetNotADoor
	}
	return idx, nil
}

// ensureDoorMetadata returns opening.Door, allocating a zero-value
// DoorMetadata first if it was nil (a door opening created via add_opening
// has no Door metadata until a door-specific operation sets one).
func ensureDoorMetadata(opening *RoomDraftOpening) *DoorMetadata {
	if opening.Door == nil {
		opening.Door = &DoorMetadata{}
	}
	return opening.Door
}

func endpointPosition(wall RoomDraftWall, endpoint string) RoomLocalPoint {
	if endpoint == "start" {
		return wall.Start
	}
	return wall.End
}

func setEndpointPosition(wall *RoomDraftWall, endpoint string, position RoomLocalPoint) {
	if endpoint == "start" {
		wall.Start = position
	} else {
		wall.End = position
	}
}

func addPoints(a, b RoomLocalPoint) RoomLocalPoint {
	return RoomLocalPoint{X: a.X + b.X, Y: a.Y + b.Y, Z: a.Z + b.Z}
}

func distance(a, b RoomLocalPoint) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func isValidOpeningKind(kind OpeningKind) bool {
	switch kind {
	case OpeningKindDoor, OpeningKindWindow, OpeningKindArchway, OpeningKindOther:
		return true
	default:
		return false
	}
}

func isValidOpeningProfile(profile OpeningProfile) bool {
	switch profile {
	case OpeningProfileRectangle, OpeningProfileArch:
		return true
	default:
		return false
	}
}

func isValidFixtureCategory(category FixtureCategory) bool {
	switch category {
	case FixtureCategoryAC, FixtureCategoryBoiler, FixtureCategoryBuiltInCabinetry,
		FixtureCategoryWallFixture, FixtureCategoryElectricalPanel, FixtureCategoryOther:
		return true
	default:
		return false
	}
}

func isValidServicePointKind(kind ServicePointKind) bool {
	switch kind {
	case ServicePointKindPlumbing, ServicePointKindElectrical, ServicePointKindDrain,
		ServicePointKindGas, ServicePointKindData:
		return true
	default:
		return false
	}
}

func isValidConstraintKind(kind ConstraintKind) bool {
	switch kind {
	case ConstraintKindColumn, ConstraintKindStaircase, ConstraintKindImmovableObstacle:
		return true
	default:
		return false
	}
}

// Compile-time proof that every operation type satisfies EditOperation —
// catches a missing/mistyped method for any of the 27 operations at build
// time rather than only at first use.
var (
	_ EditOperation = MoveCornerOperation{}
	_ EditOperation = MoveWallOperation{}
	_ EditOperation = SetWallThicknessOperation{}
	_ EditOperation = AddOpeningOperation{}
	_ EditOperation = RemoveOpeningOperation{}
	_ EditOperation = MoveOpeningOperation{}
	_ EditOperation = ResizeOpeningOperation{}
	_ EditOperation = ReclassifyOpeningOperation{}
	_ EditOperation = SetDoorLeafCountOperation{}
	_ EditOperation = SetDoorHingeOperation{}
	_ EditOperation = SetDoorSwingOperation{}
	_ EditOperation = SetDoorOpenDirectionOperation{}
	_ EditOperation = AddObjectOperation{}
	_ EditOperation = MoveObjectOperation{}
	_ EditOperation = RotateObjectOperation{}
	_ EditOperation = ResizeObjectOperation{}
	_ EditOperation = ReclassifyObjectOperation{}
	_ EditOperation = RemoveObjectOperation{}
	_ EditOperation = RestoreElementOperation{}
	_ EditOperation = AddFixtureOperation{}
	_ EditOperation = MoveFixtureOperation{}
	_ EditOperation = ResizeFixtureOperation{}
	_ EditOperation = ReclassifyFixtureOperation{}
	_ EditOperation = RemoveFixtureOperation{}
	_ EditOperation = AddServicePointOperation{}
	_ EditOperation = MoveServicePointOperation{}
	_ EditOperation = RemoveServicePointOperation{}
	_ EditOperation = AddConstraintOperation{}
	_ EditOperation = MoveConstraintOperation{}
	_ EditOperation = RemoveConstraintOperation{}
	_ EditOperation = ApplyVerifiedMeasurementOperation{}
	_ EditOperation = AssignVisualAssetOperation{}
	_ EditOperation = ClearVisualAssetOperation{}
	_ EditOperation = SetVisualAppearanceOperation{}
	_ EditOperation = ClearVisualAppearanceOperation{}
)
