import type { components } from "@/lib/api/generated/schema";

export type RoomLocalPoint = components["schemas"]["RoomLocalPoint"];
export type RoomLocalQuaternion = components["schemas"]["RoomLocalQuaternion"];
export type RoomLocalTransform = components["schemas"]["RoomLocalTransform"];
export type RoomDraftWall = components["schemas"]["RoomDraftWall"];
export type RoomDraftOpening = components["schemas"]["RoomDraftOpening"];
export type RoomDraftObject = components["schemas"]["RoomDraftObject"];
export type RoomDraftFixture = components["schemas"]["RoomDraftFixture"];
export type RoomDraftServicePoint = components["schemas"]["RoomDraftServicePoint"];
export type RoomDraftConstraint = components["schemas"]["RoomDraftConstraint"];
export type DoorMetadata = components["schemas"]["DoorMetadata"];

// Selection is keyed by canonical Renovex identity, never array position —
// RoomDraft arrays are replaced wholesale on every authoritative refresh, so
// an index-based selection would silently point at the wrong element (or a
// removed one) the moment the underlying array is reordered.
export type ElementKind = "wall" | "opening" | "object" | "fixture" | "servicePoint" | "constraint";

export type Selection = { kind: ElementKind; id: string } | null;

export type Viewport = {
  panX: number;
  panY: number;
  zoom: number; // screen pixels per world metre
};

export type ActiveEditorView = "2d" | "3d";

// 3D camera pose, persisted in useEditorState so remounting the 3D
// viewport (after switching to 2D and back) can restore the last SETTLED
// pose via CameraControls' imperative setLookAt, rather than re-fitting
// from scratch. Plain serializable numbers, never a Three.js/camera-controls
// object — this is UI state, not a Three.js scene reference. Written only
// at stable lifecycle boundaries (CameraControls' onRest/onSleep, or right
// before the 3D viewport unmounts) — never per animation frame.
export type CameraPose = {
  position: RoomLocalPoint;
  target: RoomLocalPoint;
};

// Canonical RP4A/RP4C2 EditOperation payloads. Exact wire shapes, no
// invented fields — mirrors backend/internal/spatial/editoperation.go.
// RP4C1 shipped the first 4 (move_corner/reclassify_opening/
// add_service_point/move_service_point); RP4C2 adds the rest of this
// slice's 2D-meaningful subset, including 3 new canonical remove
// operations (remove_fixture/remove_service_point/remove_constraint) added
// to RP4A's vocabulary as part of RP4C2 (see backend/ios changes).

export type MoveCornerPayload = {
  endpoints: { wallId: string; endpoint: "start" | "end" }[];
  newPosition: RoomLocalPoint;
};

export type MoveWallPayload = {
  wallId: string;
  delta: RoomLocalPoint;
};

export type SetWallThicknessPayload = {
  wallId: string;
  thickness: number;
  status: string; // MeasurementStatus: "estimated" | "unconfirmed"
};

export type ReclassifyOpeningPayload = {
  openingId: string;
  kind: string; // OpeningKind: "door" | "window" | "archway" | "other"
  profile: string; // OpeningProfile: "rectangle" | "arch"
};

export type AddOpeningPayload = {
  id: string;
  parentWallId: string;
  kind: string;
  profile: string;
  transform: RoomLocalTransform;
  width: number;
  height: number;
  offsetAlongWall: number;
};

export type RemoveOpeningPayload = {
  openingId: string;
};

export type MoveOpeningPayload = {
  openingId: string;
  transform: RoomLocalTransform;
  offsetAlongWall: number;
};

export type ResizeOpeningPayload = {
  openingId: string;
  width: number;
  height: number;
};

export type SetDoorLeafCountPayload = {
  openingId: string;
  leafCount: number;
};

export type SetDoorHingePayload = {
  openingId: string;
  hinge: string; // DoorHinge: "left" | "right"
};

export type SetDoorSwingPayload = {
  openingId: string;
  swing: string; // DoorSwing: "inward" | "outward"
};

export type SetDoorOpenDirectionPayload = {
  openingId: string;
  openDirection: string;
};

export type AddFixturePayload = {
  id: string;
  category: string; // FixtureCategory
  transform: RoomLocalTransform;
  dimensions?: RoomLocalPoint;
  parentWallId?: string;
};

export type MoveFixturePayload = {
  fixtureId: string;
  transform: RoomLocalTransform;
};

export type ResizeFixturePayload = {
  fixtureId: string;
  dimensions: RoomLocalPoint;
};

export type ReclassifyFixturePayload = {
  fixtureId: string;
  category: string;
};

export type RemoveFixturePayload = {
  fixtureId: string;
};

// T1B: objects gain the same move/rotate/resize vocabulary as fixtures.
// Mirrors backend/internal/spatial/editoperation.go's MoveObjectOperation/
// RotateObjectOperation/ResizeObjectOperation exactly — these already
// existed on the backend (dispatcher-wired, Apply-tested) before any Web
// caller ever built them. Reclassify/remove also exist canonically on the
// backend but are deliberately NOT exposed here — T1B's binding product
// behavior only calls for move/rotate/resize/visual-asset-follow, and
// inventing symmetry with fixtures beyond that scope is exactly what the
// task instructs against.
export type MoveObjectPayload = {
  objectId: string;
  position: RoomLocalPoint;
};

export type RemoveObjectPayload = {
  objectId: string;
};

export type RotateObjectPayload = {
  objectId: string;
  rotation: RoomLocalQuaternion;
};

export type ResizeObjectPayload = {
  objectId: string;
  dimensions: RoomLocalPoint;
};

export type AddServicePointPayload = {
  id: string;
  kind: string; // ServicePointKind: "plumbing" | "electrical" | "drain" | "gas" | "data"
  position: RoomLocalPoint;
  parentWallId?: string;
};

export type MoveServicePointPayload = {
  servicePointId: string;
  position: RoomLocalPoint;
};

export type RemoveServicePointPayload = {
  servicePointId: string;
};

export type AddConstraintPayload = {
  id: string;
  kind: string; // ConstraintKind: "column" | "staircase" | "immovable_obstacle"
  transform: RoomLocalTransform;
  dimensions?: RoomLocalPoint;
};

export type MoveConstraintPayload = {
  constraintId: string;
  transform: RoomLocalTransform;
};

export type RemoveConstraintPayload = {
  constraintId: string;
};

// RP4D: the narrow visual-asset target set — fixture | object ONLY, never
// wall/opening/service-point/constraint. Mirrors
// backend/internal/spatial/editoperation.go's VisualAssetTargetKind.
export type VisualAssetTargetKind = "fixture" | "object";

export type AssignVisualAssetPayload = {
  targetKind: VisualAssetTargetKind;
  targetId: string;
  assetId: string;
  version: number;
};

export type ClearVisualAssetPayload = {
  targetKind: VisualAssetTargetKind;
  targetId: string;
};

export type EditOperationPayload =
  | { kind: "move_corner"; payload: MoveCornerPayload }
  | { kind: "move_wall"; payload: MoveWallPayload }
  | { kind: "set_wall_thickness"; payload: SetWallThicknessPayload }
  | { kind: "reclassify_opening"; payload: ReclassifyOpeningPayload }
  | { kind: "add_opening"; payload: AddOpeningPayload }
  | { kind: "remove_opening"; payload: RemoveOpeningPayload }
  | { kind: "move_opening"; payload: MoveOpeningPayload }
  | { kind: "resize_opening"; payload: ResizeOpeningPayload }
  | { kind: "set_door_leaf_count"; payload: SetDoorLeafCountPayload }
  | { kind: "set_door_hinge"; payload: SetDoorHingePayload }
  | { kind: "set_door_swing"; payload: SetDoorSwingPayload }
  | { kind: "set_door_open_direction"; payload: SetDoorOpenDirectionPayload }
  | { kind: "move_object"; payload: MoveObjectPayload }
  | { kind: "rotate_object"; payload: RotateObjectPayload }
  | { kind: "resize_object"; payload: ResizeObjectPayload }
  | { kind: "remove_object"; payload: RemoveObjectPayload }
  | { kind: "add_fixture"; payload: AddFixturePayload }
  | { kind: "move_fixture"; payload: MoveFixturePayload }
  | { kind: "resize_fixture"; payload: ResizeFixturePayload }
  | { kind: "reclassify_fixture"; payload: ReclassifyFixturePayload }
  | { kind: "remove_fixture"; payload: RemoveFixturePayload }
  | { kind: "add_service_point"; payload: AddServicePointPayload }
  | { kind: "move_service_point"; payload: MoveServicePointPayload }
  | { kind: "remove_service_point"; payload: RemoveServicePointPayload }
  | { kind: "add_constraint"; payload: AddConstraintPayload }
  | { kind: "move_constraint"; payload: MoveConstraintPayload }
  | { kind: "remove_constraint"; payload: RemoveConstraintPayload }
  | { kind: "assign_visual_asset"; payload: AssignVisualAssetPayload }
  | { kind: "clear_visual_asset"; payload: ClearVisualAssetPayload }
  | { kind: "restore_element"; payload: RestoreElementPayload };

// M8.5C reversible-editing patch §9 + closure patch §6/§24-26: exact-restore
// for a deleted scanned object/opening, exact-restore-by-id for a deleted
// fixture (kept for delete-Undo symmetry even though add_fixture's own
// caller-supplied id already makes plain add_fixture redo-safe), and
// in-place whole-record replacement for an EXISTING object/fixture
// (`replace: true` — the AI "Use Design" Undo/Redo primitive; mirrors
// backend/internal/spatial/editoperation.go's RestoreElementOperation
// exactly, including the same Replace-only-for-object/fixture restriction).
// Never used for service_point/constraint (their own add_* with the same id
// already restores them exactly) or wall (no remove_wall operation exists
// to undo).
export type RestoreElementPayload =
  | { kind: "object"; object: RoomDraftObject; replace?: boolean }
  | { kind: "opening"; opening: RoomDraftOpening }
  | { kind: "fixture"; fixture: RoomDraftFixture; replace?: boolean };

// One immutable logical edit submission — NOT an offline queue. Built once
// when a user action begins (operationId + any client-generated element
// identity, e.g. add_service_point's new id, fixed at that moment) and
// reused verbatim across any retry of that same action, so a lost-response
// retry can never regenerate an id RP4B would then see as a conflicting
// reuse of the same operationId. baseRevision is captured here too, so a
// retry replays the exact same CAS check the action was built against
// rather than silently re-basing against whatever revision is in cache by
// the time the retry runs.
export type PendingEdit = {
  operationId: string;
  operation: EditOperationPayload;
  baseRevision: number;
  status: "submitting" | "retryable-error";
};

// Reset-to-Scan's request is transported through a DISTINCT RP4B route
// (POST /reset-to-scan, not /edits) with a structurally different wire
// shape — {operationId, expectedRevision} only, no kind/payload — so it is
// intentionally NOT folded into EditOperationPayload/PendingEdit (RP4C2
// design decision, kept separate on purpose). PendingReset follows the
// identical retry/idempotency LIFECYCLE rules as PendingEdit (operationId
// and baseRevision stable across a verbatim retry, cleared on every
// definitive response except network failure) in its own parallel slot.
export type PendingReset = {
  operationId: string;
  baseRevision: number;
  status: "submitting" | "retryable-error";
};

// Undo Reset-to-Scan (M8.5C reversible-editing patch §11): a distinct RP4B
// route (POST /undo-reset-to-scan) needing both this undo's own operationId
// AND the resetOperationId identifying which prior reset to undo — kept
// separate from PendingReset for the same "structurally distinct wire
// shape" reason PendingReset itself is separate from PendingEdit.
export type PendingUndoReset = {
  operationId: string;
  resetOperationId: string;
  baseRevision: number;
  status: "submitting" | "retryable-error";
};
