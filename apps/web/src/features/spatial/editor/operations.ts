import type {
  AddConstraintPayload,
  AddFixturePayload,
  AddOpeningPayload,
  AddServicePointPayload,
  AssignVisualAssetPayload,
  ClearVisualAssetPayload,
  EditOperationPayload,
  MoveConstraintPayload,
  MoveCornerPayload,
  MoveFixturePayload,
  MoveObjectPayload,
  MoveOpeningPayload,
  MoveServicePointPayload,
  MoveWallPayload,
  PendingEdit,
  PendingReset,
  PendingUndoReset,
  ReclassifyFixturePayload,
  ReclassifyOpeningPayload,
  RemoveConstraintPayload,
  RemoveFixturePayload,
  RemoveObjectPayload,
  RemoveOpeningPayload,
  RemoveServicePointPayload,
  ResizeFixturePayload,
  ResizeObjectPayload,
  ResizeOpeningPayload,
  RestoreElementPayload,
  RotateObjectPayload,
  SetDoorHingePayload,
  SetDoorLeafCountPayload,
  SetDoorOpenDirectionPayload,
  SetDoorSwingPayload,
  SetWallThicknessPayload,
} from "./types";

// Builds one immutable PendingEdit per logical user action: a fresh
// operationId, and — for the "add" operations specifically — a fresh
// client-generated Renovex id, both fixed at construction time and reused
// verbatim across any retry of this same action (see PendingEdit's doc
// comment in types.ts). Call this ONCE when a logical action begins; never
// call it again for a retry of that same action.
export function buildPendingEdit(operation: EditOperationPayload, baseRevision: number): PendingEdit {
  return {
    operationId: crypto.randomUUID(),
    operation,
    baseRevision,
    status: "submitting",
  };
}

// --- RP4C1 operations ---

export function moveCornerOperation(payload: MoveCornerPayload): EditOperationPayload {
  return { kind: "move_corner", payload };
}

export function reclassifyOpeningOperation(payload: ReclassifyOpeningPayload): EditOperationPayload {
  return { kind: "reclassify_opening", payload };
}

// Generates the new service point's id here, at the moment the logical
// create action begins — one of RP4C2's several "add" operations where
// RP4A's contract requires the caller to supply a new Renovex id. The
// caller must NOT regenerate this id on retry; it lives inside the
// returned operation, which buildPendingEdit's caller stores once in
// PendingEdit and reuses verbatim.
export function addServicePointOperation(
  payload: Omit<AddServicePointPayload, "id">,
): EditOperationPayload {
  return { kind: "add_service_point", payload: { ...payload, id: crypto.randomUUID() } };
}

export function moveServicePointOperation(payload: MoveServicePointPayload): EditOperationPayload {
  return { kind: "move_service_point", payload };
}

// --- RP4C2 operations ---

export function moveWallOperation(payload: MoveWallPayload): EditOperationPayload {
  return { kind: "move_wall", payload };
}

export function setWallThicknessOperation(payload: SetWallThicknessPayload): EditOperationPayload {
  return { kind: "set_wall_thickness", payload };
}

export function addOpeningOperation(payload: Omit<AddOpeningPayload, "id">): EditOperationPayload {
  return { kind: "add_opening", payload: { ...payload, id: crypto.randomUUID() } };
}

export function removeOpeningOperation(payload: RemoveOpeningPayload): EditOperationPayload {
  return { kind: "remove_opening", payload };
}

export function moveOpeningOperation(payload: MoveOpeningPayload): EditOperationPayload {
  return { kind: "move_opening", payload };
}

export function resizeOpeningOperation(payload: ResizeOpeningPayload): EditOperationPayload {
  return { kind: "resize_opening", payload };
}

export function setDoorLeafCountOperation(payload: SetDoorLeafCountPayload): EditOperationPayload {
  return { kind: "set_door_leaf_count", payload };
}

export function setDoorHingeOperation(payload: SetDoorHingePayload): EditOperationPayload {
  return { kind: "set_door_hinge", payload };
}

export function setDoorSwingOperation(payload: SetDoorSwingPayload): EditOperationPayload {
  return { kind: "set_door_swing", payload };
}

export function setDoorOpenDirectionOperation(payload: SetDoorOpenDirectionPayload): EditOperationPayload {
  return { kind: "set_door_open_direction", payload };
}

export function addFixtureOperation(payload: Omit<AddFixturePayload, "id">): EditOperationPayload {
  return { kind: "add_fixture", payload: { ...payload, id: crypto.randomUUID() } };
}

export function moveFixtureOperation(payload: MoveFixturePayload): EditOperationPayload {
  return { kind: "move_fixture", payload };
}

export function resizeFixtureOperation(payload: ResizeFixturePayload): EditOperationPayload {
  return { kind: "resize_fixture", payload };
}

export function reclassifyFixtureOperation(payload: ReclassifyFixturePayload): EditOperationPayload {
  return { kind: "reclassify_fixture", payload };
}

export function removeFixtureOperation(payload: RemoveFixturePayload): EditOperationPayload {
  return { kind: "remove_fixture", payload };
}

// --- T1B operations (object move/rotate/resize — backend already had
// these canonical operations dispatcher-wired; only the Web builders were
// missing) ---

export function moveObjectOperation(payload: MoveObjectPayload): EditOperationPayload {
  return { kind: "move_object", payload };
}

export function rotateObjectOperation(payload: RotateObjectPayload): EditOperationPayload {
  return { kind: "rotate_object", payload };
}

export function resizeObjectOperation(payload: ResizeObjectPayload): EditOperationPayload {
  return { kind: "resize_object", payload };
}

export function removeObjectOperation(payload: RemoveObjectPayload): EditOperationPayload {
  return { kind: "remove_object", payload };
}

// M8.5C reversible-editing patch §9: exact-restore for a deleted scanned
// object/opening (RestoreElementPayload — mirrors backend
// RestoreElementOperation exactly).
export function restoreElementOperation(payload: RestoreElementPayload): EditOperationPayload {
  return { kind: "restore_element", payload };
}

export function removeServicePointOperation(payload: RemoveServicePointPayload): EditOperationPayload {
  return { kind: "remove_service_point", payload };
}

export function addConstraintOperation(payload: Omit<AddConstraintPayload, "id">): EditOperationPayload {
  return { kind: "add_constraint", payload: { ...payload, id: crypto.randomUUID() } };
}

export function moveConstraintOperation(payload: MoveConstraintPayload): EditOperationPayload {
  return { kind: "move_constraint", payload };
}

export function removeConstraintOperation(payload: RemoveConstraintPayload): EditOperationPayload {
  return { kind: "remove_constraint", payload };
}

// --- RP4D operations (fixture/object visual-asset binding only) ---

export function assignVisualAssetOperation(payload: AssignVisualAssetPayload): EditOperationPayload {
  return { kind: "assign_visual_asset", payload };
}

export function clearVisualAssetOperation(payload: ClearVisualAssetPayload): EditOperationPayload {
  return { kind: "clear_visual_asset", payload };
}

// Converts a PendingEdit into the exact SubmitRoomDraftEditInputBody wire
// shape RP4B expects. This is the ONLY place that assembles the HTTP body —
// callers never construct it inline, so the operationId/baseRevision/payload
// stay byte-for-byte what was fixed when the PendingEdit was built, retry or
// not.
export function toSubmitEditBody(pendingEdit: PendingEdit) {
  return {
    operationId: pendingEdit.operationId,
    kind: pendingEdit.operation.kind,
    payload: pendingEdit.operation.payload,
    expectedRevision: pendingEdit.baseRevision,
  };
}

// --- Reset-to-Scan (RP4C2) ---
// Deliberately separate from buildPendingEdit/PendingEdit — see PendingReset's
// doc comment in types.ts. No element-ID generation: reset carries no
// payload, only operationId + the base revision it was confirmed against.
export function buildPendingReset(baseRevision: number): PendingReset {
  return {
    operationId: crypto.randomUUID(),
    baseRevision,
    status: "submitting",
  };
}

// Converts a PendingReset into the exact ResetRoomDraftInputBody wire shape
// RP4B's dedicated reset-to-scan route expects.
export function toSubmitResetBody(pendingReset: PendingReset) {
  return {
    operationId: pendingReset.operationId,
    expectedRevision: pendingReset.baseRevision,
  };
}

// --- Undo Reset-to-Scan (M8.5C reversible-editing patch §11) ---

export function buildPendingUndoReset(resetOperationId: string, baseRevision: number): PendingUndoReset {
  return {
    operationId: crypto.randomUUID(),
    resetOperationId,
    baseRevision,
    status: "submitting",
  };
}

export function toSubmitUndoResetBody(pendingUndoReset: PendingUndoReset) {
  return {
    operationId: pendingUndoReset.operationId,
    resetOperationId: pendingUndoReset.resetOperationId,
    expectedRevision: pendingUndoReset.baseRevision,
  };
}
