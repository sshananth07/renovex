import { useCallback, useState } from "react";
import {
  addConstraintOperation,
  addFixtureOperation,
  addServicePointOperation,
  assignVisualAssetOperation,
  clearVisualAssetOperation,
  moveConstraintOperation,
  moveCornerOperation,
  moveFixtureOperation,
  moveObjectOperation,
  moveServicePointOperation,
  moveWallOperation,
  reclassifyFixtureOperation,
  reclassifyOpeningOperation,
  removeConstraintOperation,
  removeFixtureOperation,
  removeServicePointOperation,
  resizeFixtureOperation,
  resizeObjectOperation,
  resizeOpeningOperation,
  restoreElementOperation,
  rotateObjectOperation,
  setDoorHingeOperation,
  setDoorLeafCountOperation,
  setDoorOpenDirectionOperation,
  setDoorSwingOperation,
  setWallThicknessOperation,
} from "./operations";
import type { EditOperationPayload, RoomDraftConstraint, RoomDraftFixture, RoomDraftServicePoint, RoomLocalPoint } from "./types";
import type { RoomDraft } from "../api";

// M8.5C reversible-editing patch (§1-3): session-only Undo/Redo, scoped to
// the mounted editor — cleared on refresh/reopen, which is accepted. This is
// NOT persistent history, NOT a revision browser, and NOT event sourcing —
// it stores exactly enough to resubmit the existing canonical
// EditOperationPayload vocabulary through the existing mutation pipeline.
// revision itself remains a pure CAS token throughout: undo/redo always
// submit a FRESH operation against the CURRENT authoritative revision (never
// decrement, never replay an old operationId — see SpatialEditor.tsx's
// handleUndo/handleRedo).

export type HistoryCommand =
  | { kind: "none" }
  // Closure patch §3: a genuinely new user edit doesn't know its committed
  // HistoryEntry until the server confirms success (add_* needs the
  // server-assigned id) — this carries just enough to build one in
  // onSuccess via buildCommittedHistoryEntry(operation, before, after).
  | { kind: "recordPending"; operation: EditOperationPayload; before: RoomDraft }
  | { kind: "record"; entry: HistoryEntry }
  | { kind: "undo"; entry: HistoryEntry }
  | { kind: "redo"; entry: HistoryEntry };

// The actual undo/redo primitive an entry replays — distinct from
// HistoryCommand (which additionally carries "none"/"record"/"undo"/"redo"
// bookkeeping for pendingHistoryRef).
export type HistoryAction =
  | { kind: "edit"; operation: EditOperationPayload }
  | { kind: "resetToScan" }
  | { kind: "restoreAfterReset"; resetOperationId: string };

export type HistoryEntry = {
  id: string;
  label: string;
  undo: HistoryAction;
  redo: HistoryAction;
};

// Builds the operation that reverses `operation`, reading every "previous
// value" field from `before` — the authoritative RoomDraft captured at the
// moment the logical user action BEGAN, never from the post-mutation
// result. Returns null when the target element cannot be found in `before`
// (nothing safe to invert against) — callers must treat null as "this edit
// is not undoable," never fabricate a best-guess inverse.
//
// remove_fixture/remove_service_point/remove_constraint invert to their own
// add_* operation with the SAME id: exact-restore for these three, since
// they are always contractor-created (never RoomPlan-provenance) and
// add_fixture/add_service_point/add_constraint already reproduce every
// field they can carry. remove_object/remove_opening invert to
// restore_element instead, because add_object/add_opening cannot preserve
// Provenance/VisualAsset/Appearance — see editoperation.go's
// RestoreElementOperation doc comment. There is no remove_wall/remove_corner
// operation to invert.
// Closure-patch tolerance for "these coincident endpoints all shared one
// corner before the drag" — mirrors SpatialEditor.tsx's
// CORNER_COINCIDENCE_EPSILON_METERS exactly (the same tolerance that built
// the endpoint list moveCornerOperation's forward payload carries).
const CORNER_COINCIDENCE_EPSILON_METERS = 0.02;

function distance3D(a: RoomLocalPoint, b: RoomLocalPoint): number {
  return Math.sqrt((a.x - b.x) ** 2 + (a.y - b.y) ** 2 + (a.z - b.z) ** 2);
}

// RP4D's narrow visual-asset target set is object|fixture only (mirrors
// VisualAssetTargetKind in types.ts) — reads either array's matching
// element's own visualAsset field, whichever kind applies.
function findVisualAssetTarget(
  draft: RoomDraft,
  targetKind: "object" | "fixture",
  targetId: string,
): { visualAsset?: { assetId: string; version: number } } | null {
  if (targetKind === "object") return draft.objects?.find((o) => o.id === targetId) ?? null;
  return draft.fixtures?.find((f) => f.id === targetId) ?? null;
}

export function buildInverseEdit(operation: EditOperationPayload, before: RoomDraft): EditOperationPayload | null {
  switch (operation.kind) {
    case "move_corner": {
      // Every listed endpoint's CURRENT (pre-drag) position, read from
      // `before` — never inferred from the forward operation's own
      // newPosition, and never negated/computed geometrically. If they are
      // not actually coincident in `before` (nothing dragged them there as
      // one corner), there is no single safe previous corner to restore.
      const positions: RoomLocalPoint[] = [];
      for (const endpoint of operation.payload.endpoints) {
        const wall = before.walls?.find((w) => w.id === endpoint.wallId);
        if (!wall) return null;
        positions.push(endpoint.endpoint === "start" ? wall.start : wall.end);
      }
      if (positions.length === 0) return null;
      const [first, ...rest] = positions;
      if (!first || rest.some((p) => distance3D(p, first) > CORNER_COINCIDENCE_EPSILON_METERS)) return null;
      return moveCornerOperation({ endpoints: operation.payload.endpoints, newPosition: first });
    }
    case "set_door_leaf_count": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening?.door) return null;
      return setDoorLeafCountOperation({ openingId: operation.payload.openingId, leafCount: opening.door.leafCount });
    }
    case "set_door_hinge": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening?.door) return null;
      // Matches ElementInspector.tsx's own displayed default exactly — a
      // door record with no hinge recorded yet reads as "left" there too.
      return setDoorHingeOperation({ openingId: operation.payload.openingId, hinge: opening.door.hinge ?? "left" });
    }
    case "set_door_swing": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening?.door) return null;
      return setDoorSwingOperation({ openingId: operation.payload.openingId, swing: opening.door.swing ?? "inward" });
    }
    case "set_door_open_direction": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening?.door) return null;
      return setDoorOpenDirectionOperation({ openingId: operation.payload.openingId, openDirection: opening.door.openDirection ?? "" });
    }
    case "reclassify_fixture": {
      const fixture = before.fixtures?.find((f) => f.id === operation.payload.fixtureId);
      if (!fixture) return null;
      return reclassifyFixtureOperation({ fixtureId: operation.payload.fixtureId, category: fixture.category });
    }
    case "clear_visual_asset": {
      const previous = findVisualAssetTarget(before, operation.payload.targetKind, operation.payload.targetId);
      if (!previous) return null;
      // No previous binding at all → nothing to restore (clearing an
      // already-unbound target is a real no-op with no safe inverse).
      if (!previous.visualAsset) return null;
      return assignVisualAssetOperation({
        targetKind: operation.payload.targetKind,
        targetId: operation.payload.targetId,
        assetId: previous.visualAsset.assetId,
        version: previous.visualAsset.version,
      });
    }
    case "assign_visual_asset": {
      const previous = findVisualAssetTarget(before, operation.payload.targetKind, operation.payload.targetId);
      if (!previous) return null;
      if (!previous.visualAsset) {
        return clearVisualAssetOperation({ targetKind: operation.payload.targetKind, targetId: operation.payload.targetId });
      }
      return assignVisualAssetOperation({
        targetKind: operation.payload.targetKind,
        targetId: operation.payload.targetId,
        assetId: previous.visualAsset.assetId,
        version: previous.visualAsset.version,
      });
    }
    case "remove_constraint": {
      const constraint = before.constraints?.find((c) => c.id === operation.payload.constraintId);
      if (!constraint) return null;
      return {
        kind: "add_constraint",
        payload: {
          id: constraint.id,
          kind: constraint.kind,
          transform: constraint.transform,
          ...(constraint.dimensions ? { dimensions: constraint.dimensions } : {}),
        },
      };
    }
    case "move_object": {
      const object = before.objects?.find((o) => o.id === operation.payload.objectId);
      if (!object) return null;
      return moveObjectOperation({ objectId: operation.payload.objectId, position: object.transform.position });
    }
    case "rotate_object": {
      const object = before.objects?.find((o) => o.id === operation.payload.objectId);
      if (!object) return null;
      return rotateObjectOperation({ objectId: operation.payload.objectId, rotation: object.transform.rotation });
    }
    case "resize_object": {
      const object = before.objects?.find((o) => o.id === operation.payload.objectId);
      if (!object || !object.dimensions) return null;
      return resizeObjectOperation({ objectId: operation.payload.objectId, dimensions: object.dimensions });
    }
    case "move_wall":
      // `-0` normalized to `0` (negating an axis that was already 0 would
      // otherwise produce `-0`, which is numerically equal but a
      // needlessly different wire value from the original forward delta).
      return moveWallOperation({
        wallId: operation.payload.wallId,
        delta: {
          x: -operation.payload.delta.x || 0,
          y: -operation.payload.delta.y || 0,
          z: -operation.payload.delta.z || 0,
        },
      });
    case "set_wall_thickness": {
      const wall = before.walls?.find((w) => w.id === operation.payload.wallId);
      if (!wall || wall.thickness === undefined) return null;
      return setWallThicknessOperation({ wallId: operation.payload.wallId, thickness: wall.thickness, status: wall.thicknessStatus });
    }
    case "resize_opening": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening || opening.width === undefined || opening.height === undefined) return null;
      return resizeOpeningOperation({ openingId: operation.payload.openingId, width: opening.width, height: opening.height });
    }
    case "reclassify_opening": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening) return null;
      return reclassifyOpeningOperation({ openingId: operation.payload.openingId, kind: opening.kind, profile: opening.profile });
    }
    case "move_fixture": {
      const fixture = before.fixtures?.find((f) => f.id === operation.payload.fixtureId);
      if (!fixture) return null;
      return moveFixtureOperation({ fixtureId: operation.payload.fixtureId, transform: fixture.transform });
    }
    case "resize_fixture": {
      const fixture = before.fixtures?.find((f) => f.id === operation.payload.fixtureId);
      if (!fixture || !fixture.dimensions) return null;
      return resizeFixtureOperation({ fixtureId: operation.payload.fixtureId, dimensions: fixture.dimensions });
    }
    case "move_service_point": {
      const sp = before.servicePoints?.find((s) => s.id === operation.payload.servicePointId);
      if (!sp) return null;
      return moveServicePointOperation({ servicePointId: operation.payload.servicePointId, position: sp.position });
    }
    case "move_constraint": {
      const constraint = before.constraints?.find((c) => c.id === operation.payload.constraintId);
      if (!constraint) return null;
      return moveConstraintOperation({ constraintId: operation.payload.constraintId, transform: constraint.transform });
    }
    case "remove_object": {
      const object = before.objects?.find((o) => o.id === operation.payload.objectId);
      if (!object) return null;
      return restoreElementOperation({ kind: "object", object });
    }
    case "remove_opening": {
      const opening = before.openings?.find((o) => o.id === operation.payload.openingId);
      if (!opening) return null;
      return restoreElementOperation({ kind: "opening", opening });
    }
    case "remove_fixture": {
      const fixture = before.fixtures?.find((f) => f.id === operation.payload.fixtureId);
      if (!fixture) return null;
      return {
        kind: "add_fixture",
        payload: {
          id: fixture.id,
          category: fixture.category,
          transform: fixture.transform,
          ...(fixture.dimensions ? { dimensions: fixture.dimensions } : {}),
          ...(fixture.parentWallId ? { parentWallId: fixture.parentWallId } : {}),
        },
      };
    }
    case "remove_service_point": {
      const sp = before.servicePoints?.find((s) => s.id === operation.payload.servicePointId);
      if (!sp) return null;
      return {
        kind: "add_service_point",
        payload: {
          id: sp.id,
          kind: sp.kind,
          position: sp.position,
          ...(sp.parentWallId ? { parentWallId: sp.parentWallId } : {}),
        },
      };
    }
    default:
      // Every other canonical operation (add_*, door property edits, visual
      // asset assign/clear, remove_constraint, etc.) is out of this patch's
      // tested scope — not undoable YET, not "impossible," reported as a
      // remaining limitation rather than guessed at.
      return null;
  }
}

// Finds the single element present in `after` but not `before`, by id.
// Returns null if zero or more than one were added — buildCommittedHistoryEntry
// treats that as "no safe committed-add mapping" rather than guessing which
// one the just-submitted add_* operation actually created.
function findAddedById<T extends { id: string }>(before: T[] | null | undefined, after: T[] | null | undefined): T | null {
  const previousIds = new Set((before ?? []).map((item) => item.id));
  const added = (after ?? []).filter((item) => !previousIds.has(item.id));
  return added.length === 1 ? (added[0] as T) : null;
}

// §3/closure-patch: builds a committed HistoryEntry from operation + the
// authoritative BEFORE and AFTER RoomDraft — the model add_* operations
// need, since the server (not the client) assigns their canonical id.
// Non-add operations still only need `before` (delegates to
// buildInverseEdit unchanged); `after` is read only for the three add_*
// cases below. Returns null when no reversible mapping exists — callers
// (SpatialEditor.submitUserEdit) must treat null as "this edit is not
// undoable," never silently commit a user action while failing to
// register Undo (see the assertNever/dev-invariant convention at the call
// site, not duplicated here — this function stays a pure query).
export function buildCommittedHistoryEntry(input: {
  operation: EditOperationPayload;
  before: RoomDraft;
  after: RoomDraft;
}): HistoryEntry | null {
  const { operation, before, after } = input;

  switch (operation.kind) {
    case "add_fixture": {
      const added = findAddedById<RoomDraftFixture>(before.fixtures, after.fixtures);
      if (!added) return null;
      return {
        id: crypto.randomUUID(),
        label: describeOperation(operation),
        undo: { kind: "edit", operation: removeFixtureOperation({ fixtureId: added.id }) },
        // Redo restores the EXACT server-created record (never re-runs
        // add_fixture, which would mint a new random id) — restore_element
        // covers fixture for precisely this reason.
        redo: { kind: "edit", operation: restoreElementOperation({ kind: "fixture", fixture: added }) },
      };
    }
    case "add_service_point": {
      const added = findAddedById<RoomDraftServicePoint>(before.servicePoints, after.servicePoints);
      if (!added) return null;
      return {
        id: crypto.randomUUID(),
        label: describeOperation(operation),
        undo: { kind: "edit", operation: removeServicePointOperation({ servicePointId: added.id }) },
        // No restore_element support for service_point — add_service_point
        // with the added record's own id already restores it exactly
        // (always contractor-created, no provenance/appearance/visual-asset
        // to lose), so reusing the add operation itself (with the real id,
        // never a freshly generated one) is the correct, smallest Redo.
        redo: {
          kind: "edit",
          operation: {
            kind: "add_service_point",
            payload: {
              id: added.id,
              kind: added.kind,
              position: added.position,
              ...(added.parentWallId ? { parentWallId: added.parentWallId } : {}),
            },
          },
        },
      };
    }
    case "add_constraint": {
      const added = findAddedById<RoomDraftConstraint>(before.constraints, after.constraints);
      if (!added) return null;
      return {
        id: crypto.randomUUID(),
        label: describeOperation(operation),
        undo: { kind: "edit", operation: removeConstraintOperation({ constraintId: added.id }) },
        redo: {
          kind: "edit",
          operation: {
            kind: "add_constraint",
            payload: {
              id: added.id,
              kind: added.kind,
              transform: added.transform,
              ...(added.dimensions ? { dimensions: added.dimensions } : {}),
            },
          },
        },
      };
    }
    default: {
      const inverse = buildInverseEdit(operation, before);
      if (inverse === null) return null;
      return {
        id: crypto.randomUUID(),
        label: describeOperation(operation),
        undo: { kind: "edit", operation: inverse },
        redo: { kind: "edit", operation },
      };
    }
  }
}

function describeOperation(operation: EditOperationPayload): string {
  const label = operation.kind.replace(/_/g, " ");
  return label.charAt(0).toUpperCase() + label.slice(1);
}

// --- session history reducer/hook ---

export type EditorHistoryState = {
  undoStack: HistoryEntry[];
  redoStack: HistoryEntry[];
};

export function useEditorHistory() {
  const [undoStack, setUndoStack] = useState<HistoryEntry[]>([]);
  const [redoStack, setRedoStack] = useState<HistoryEntry[]>([]);

  // A genuinely new forward edit clears redo (§7: "New forward edit after
  // Undo: undoStack = [...undoStack, newEntry], redoStack = [] strictly").
  const record = useCallback((entry: HistoryEntry) => {
    setUndoStack((current) => [...current, entry]);
    setRedoStack([]);
  }, []);

  const committedUndo = useCallback((entry: HistoryEntry) => {
    setUndoStack((current) => (current.length > 0 ? current.slice(0, -1) : current));
    setRedoStack((current) => [...current, entry]);
  }, []);

  const committedRedo = useCallback((entry: HistoryEntry) => {
    setRedoStack((current) => (current.length > 0 ? current.slice(0, -1) : current));
    setUndoStack((current) => [...current, entry]);
  }, []);

  // §19: a stale-revision reconciliation proves external/newer state
  // exists — clearing session history is the safe default rather than
  // trusting old inverse data was built from a state that's still real.
  const clear = useCallback(() => {
    setUndoStack([]);
    setRedoStack([]);
  }, []);

  return {
    undoStack,
    redoStack,
    canUndo: undoStack.length > 0,
    canRedo: redoStack.length > 0,
    record,
    committedUndo,
    committedRedo,
    clear,
  };
}
