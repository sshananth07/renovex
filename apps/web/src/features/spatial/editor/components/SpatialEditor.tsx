"use client";

import { useRef, useState } from "react";
import dynamic from "next/dynamic";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { normalizeApiError, type ApiError } from "@/lib/api/errors";
import { useRoomDraft } from "../../queries";
import { useResetRoomDraft, useSubmitEditOperation, useUndoResetRoomDraft } from "../../mutations";
import { useEditorState, elementStillExistsInDraft } from "../useEditorState";
import { buildCommittedHistoryEntry, useEditorHistory, type HistoryCommand, type HistoryEntry } from "../history";
import {
  addConstraintOperation,
  addFixtureOperation,
  addServicePointOperation,
  buildPendingEdit,
  buildPendingReset,
  buildPendingUndoReset,
  moveConstraintOperation,
  moveCornerOperation,
  moveFixtureOperation,
  moveObjectOperation,
  moveServicePointOperation,
  moveWallOperation,
  removeConstraintOperation,
  removeFixtureOperation,
  removeObjectOperation,
  removeOpeningOperation,
  removeServicePointOperation,
  resizeObjectOperation,
  restoreElementOperation,
  rotateObjectOperation,
} from "../operations";
import { withUpdatedPosition } from "../three/sceneProjection";
import { usePrefersReducedMotion } from "../three/usePrefersReducedMotion";
import { classifyEditError, EditStatusBanner } from "./EditStatusBanner";
import { FloorPlanViewport, type DragTarget } from "./FloorPlanViewport";
import { EditorViewSwitcher } from "./EditorViewSwitcher";
import { Spatial3DErrorBoundary } from "./Spatial3DErrorBoundary";
import { AIDesignStudio } from "../../design-studio/components/AIDesignStudio";
import type { ComparisonMode, ConceptRenderState } from "../../design-studio/types";
import type { Spatial3DDragEnd } from "./Spatial3DViewport";
import type { EditOperationPayload, ElementKind, RoomLocalPoint, Selection } from "../types";
import type { RoomDraft } from "../../api";

// Closure patch §2/§8: every EditOperationPayload.kind the Web editor can
// actually originate (SpatialEditor/FloorPlanViewport/Spatial3DViewport/
// ElementInspector/toolbar add actions) — used only to distinguish "this is
// a real coverage gap in buildCommittedHistoryEntry" (dev-time throw) from
// "this specific record legitimately has no previous value to restore"
// (production-safe warning, see submitPendingEdit's onSuccess). Kept as an
// explicit list rather than inferred from EditOperationPayload's own union
// so a backend-only operation kind (assign_visual_asset/clear_visual_asset
// — never submitted via submitUserEdit, only through direct submit() calls
// that don't build history) is never mistaken for a missing case.
const EDITOR_HISTORY_OPERATION_KINDS = new Set<EditOperationPayload["kind"]>([
  "move_corner",
  "move_wall",
  "set_wall_thickness",
  "reclassify_opening",
  "remove_opening",
  "resize_opening",
  "set_door_leaf_count",
  "set_door_hinge",
  "set_door_swing",
  "set_door_open_direction",
  "move_object",
  "rotate_object",
  "resize_object",
  "remove_object",
  "add_fixture",
  "move_fixture",
  "resize_fixture",
  "reclassify_fixture",
  "remove_fixture",
  "add_service_point",
  "move_service_point",
  "remove_service_point",
  "add_constraint",
  "move_constraint",
  "remove_constraint",
  "assign_visual_asset",
  "clear_visual_asset",
]);

function isKnownEditorHistoryOperationKind(kind: EditOperationPayload["kind"]): boolean {
  return EDITOR_HISTORY_OPERATION_KINDS.has(kind);
}

function labelForSelection(selection: NonNullable<Selection>): string {
  switch (selection.kind) {
    case "wall":
      return "wall";
    case "opening":
      return "opening";
    case "object":
      return "object";
    case "fixture":
      return "fixture";
    case "servicePoint":
      return "service point";
    case "constraint":
      return "constraint";
  }
}

// Spatial3DViewport touches "three"/@react-three/fiber's WebGL/window APIs
// at module scope in places — dynamically imported with ssr:false so
// SpatialEditor (server-rendered by default in the App Router) never tries
// to construct a WebGL context on the server. FloorPlanViewport needs no
// such guard (pure SVG/DOM).
const Spatial3DViewport = dynamic(() => import("./Spatial3DViewport"), { ssr: false });

// Wires authoritative loading (GET /room-drafts/{id}) → the 2D viewport →
// selection/inspector → RP4B submission, per plan §RP4C1/§RP4C2's
// mutation-flow contract:
//   user intent -> canonical EditOperation -> RP4B POST -> authoritative
//   RoomDraft/revision -> re-render. Cache replacement (never merge) happens
//   inside useSubmitEditOperation's onSuccess; this component's job is
//   turning viewport interactions into PendingEdits and reconciling
//   UI-only state (selection, pendingEdit) around the authoritative result.
// Reset-to-Scan follows a structurally parallel but distinct path
// (PendingReset/useResetRoomDraft) — see submitPendingReset below.
// Initial fit-to-room framing is owned by FloorPlanViewport itself (it's the
// only place that knows the SVG's real measured size — see its own
// auto-frame effect), not guessed here against a placeholder container size.
export function SpatialEditor({ roomDraftId }: { roomDraftId: string }) {
  const draftQuery = useRoomDraft(roomDraftId);
  const submitEdit = useSubmitEditOperation(roomDraftId);
  const resetDraft = useResetRoomDraft(roomDraftId);
  const undoReset = useUndoResetRoomDraft(roomDraftId);
  const editor = useEditorState();
  const history = useEditorHistory();
  // Set once per submit() call, read (and cleared) in the matching
  // onSuccess/onError — the ONLY thing that ever moves undoStack/redoStack
  // is a server-confirmed success (§5: "history must change only after
  // server success").
  const pendingHistoryRef = useRef<HistoryCommand>({ kind: "none" });
  // Undo Reset-to-Scan's own pending slot — kept local rather than widening
  // useEditorState's shared pendingReset (a distinct wire shape, only ever
  // used from history's executeSpecialHistoryCommand below).
  const [pendingUndoReset, setPendingUndoReset] = useState<ReturnType<typeof buildPendingUndoReset> | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<NonNullable<Selection> | null>(null);
  const [lastError, setLastError] = useState<ApiError | null>(null);
  const [resetDialogOpen, setResetDialogOpen] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  // Lifted from AIDesignStudio via onConceptPreviewChange — SpatialEditor
  // owns the canvas, so it owns rendering CurrentConceptToggle as a DOM
  // overlay over it and passing the projection into Spatial3DViewport (plan's
  // layout tree: the toggle floats over SpatialCanvasStage, not inside the
  // studio dock). AIDesignStudio remains the single source of truth for
  // comparisonMode itself — this only mirrors it for rendering.
  const [conceptPreview, setConceptPreview] = useState<ConceptRenderState>({ kind: "none" });
  const [comparisonMode, setComparisonModeState] = useState<ComparisonMode>("current");
  const [setComparisonMode, setSetComparisonMode] = useState<(mode: ComparisonMode) => void>(() => () => {});

  const draft: RoomDraft | undefined = draftQuery.data;

  // One shared submit path for both a brand-new logical edit and a retry of
  // an existing PendingEdit — the ONLY difference between them is whether a
  // fresh PendingEdit is built first. Success/error handling (cache
  // replacement happens in useSubmitEditOperation's onSuccess; this is the
  // UI-state reconciliation layer) must stay identical either way, so it
  // lives in one place rather than two handlers that could drift.
  function submitPendingEdit(pendingEdit: ReturnType<typeof buildPendingEdit>) {
    setLastError(null);
    submitEdit.mutate(pendingEdit, {
      onSuccess: (result) => {
        editor.clearPendingEdit();
        editor.reconcileSelection(elementStillExistsInDraft(editor.selection, result.roomDraft));

        // History changes ONLY here, after server success (§5) — never at
        // submit() call time, since validation/CAS/network can still fail.
        const historyAction = pendingHistoryRef.current;
        if (historyAction.kind === "recordPending") {
          const entry = buildCommittedHistoryEntry({
            operation: historyAction.operation,
            before: historyAction.before,
            after: result.roomDraft,
          });
          if (entry === null) {
            // Closure patch §7: every EDITOR-SUPPORTED operation kind must
            // register Undo — a missing case in buildCommittedHistoryEntry's
            // switch is a real coverage bug, so it fails loudly in
            // development rather than silently shipping an unreversed
            // mutation. A specific RECORD lacking the field its inverse
            // needs (e.g. a wall whose thickness was never measured — there
            // is no clear_wall_thickness operation to represent "restore to
            // unmeasured") is a genuine, narrow domain limitation rather
            // than a missing switch case: the edit still commits safely,
            // just without an Undo entry, and this is surfaced via a
            // console warning rather than crashing the whole editor over a
            // wall that happens to have no prior measurement.
            if (isKnownEditorHistoryOperationKind(historyAction.operation.kind)) {
              console.warn(`Editor mutation "${historyAction.operation.kind}" could not be reversed (source record missing a required field) — committed without an Undo entry.`);
            } else if (process.env.NODE_ENV !== "production") {
              throw new Error(`Editor mutation "${historyAction.operation.kind}" has no reversible history mapping — add a case to buildCommittedHistoryEntry/buildInverseEdit.`);
            }
          } else {
            history.record(entry);
          }
        }
        if (historyAction.kind === "record") history.record(historyAction.entry);
        if (historyAction.kind === "undo") history.committedUndo(historyAction.entry);
        if (historyAction.kind === "redo") history.committedRedo(historyAction.entry);
        pendingHistoryRef.current = { kind: "none" };
      },
      onError: (thrown) => {
        const error = toApiError(thrown);
        setLastError(error);
        const kind = classifyEditError(error);
        if (kind === "network") {
          // Genuinely retryable transport failure: keep the PendingEdit so
          // the caller can retry the SAME logical request (same
          // operationId/payload), never regenerating it. History intent
          // stays pending too — a retry of this same submission should
          // still commit its history entry once it succeeds.
          editor.markPendingEditRetryable();
          return;
        }
        if (kind === "stale_revision") {
          // Clear the pending edit FIRST so no reload can trigger a replay
          // against it, then reload the authoritative draft. §19: newer
          // external state is now proven to exist — session history may no
          // longer be built from a state that's still real, so it is
          // cleared rather than risking an inverse operation against
          // unknown state.
          editor.clearPendingEdit();
          pendingHistoryRef.current = { kind: "none" };
          history.clear();
          draftQuery.refetch();
          return;
        }
        // Every other definitive server response (validation, conflict,
        // not-found, forbidden) discards the PendingEdit AND the pending
        // history intent — it will not be retried automatically, and never
        // silently mutates undo/redo for an edit that never took effect.
        editor.clearPendingEdit();
        pendingHistoryRef.current = { kind: "none" };
      },
    });
  }

  function submit(operation: EditOperationPayload, historyCommand: HistoryCommand = { kind: "none" }) {
    if (!draft) return;
    const pendingEdit = buildPendingEdit(operation, draft.revision);
    pendingHistoryRef.current = historyCommand;
    editor.startPendingEdit(pendingEdit);
    submitPendingEdit(pendingEdit);
  }

  // Every ordinary user-initiated edit (gesture, inspector field, delete
  // confirmation) goes through here — the ONE place that captures the
  // BEFORE state, so 3D drag, 2D drag, and manual-field edits all
  // automatically receive Undo for free (§3/§4). The committed HistoryEntry
  // itself is only built in submitPendingEdit's onSuccess, once the
  // authoritative AFTER draft exists (closure patch §3/§6 — add_* needs the
  // server-assigned id, which does not exist yet at submit time).
  function submitUserEdit(operation: EditOperationPayload) {
    if (!draft) return;
    submit(operation, { kind: "recordPending", operation, before: draft });
  }

  // Closure patch §D: AI "Use Design" already persists server-side (a real
  // CAS-checked revision bump — see backend's ApplyAcceptance), so unlike
  // Reset it needs no special resetToScan-style HistoryAction: Undo/Redo are
  // plain restore_element{replace:true} edits reading the selected object/
  // fixture's exact pre-/post-acceptance record, submitted through the same
  // submit() pipeline as everything else. isSupportedDesignTarget scopes
  // every AI session to exactly one object OR fixture, so this is always a
  // single restore, never a multi-record batch.
  function handleDesignCommitted(commit: { selection: Selection; before: RoomDraft; after: RoomDraft }) {
    const { selection, before, after } = commit;
    if (!selection) return;
    if (selection.kind === "object") {
      const beforeObject = before.objects?.find((o) => o.id === selection.id);
      const afterObject = after.objects?.find((o) => o.id === selection.id);
      if (!beforeObject || !afterObject) return;
      const entry: HistoryEntry = {
        id: crypto.randomUUID(),
        label: "Use design",
        undo: { kind: "edit", operation: restoreElementOperation({ kind: "object", object: beforeObject, replace: true }) },
        redo: { kind: "edit", operation: restoreElementOperation({ kind: "object", object: afterObject, replace: true }) },
      };
      history.record(entry);
      return;
    }
    if (selection.kind === "fixture") {
      const beforeFixture = before.fixtures?.find((f) => f.id === selection.id);
      const afterFixture = after.fixtures?.find((f) => f.id === selection.id);
      if (!beforeFixture || !afterFixture) return;
      const entry: HistoryEntry = {
        id: crypto.randomUUID(),
        label: "Use design",
        undo: { kind: "edit", operation: restoreElementOperation({ kind: "fixture", fixture: beforeFixture, replace: true }) },
        redo: { kind: "edit", operation: restoreElementOperation({ kind: "fixture", fixture: afterFixture, replace: true }) },
      };
      history.record(entry);
    }
  }

  function retryPendingEdit() {
    if (!editor.pendingEdit) return;
    submitPendingEdit(editor.pendingEdit);
  }

  // §6: Undo/Redo always build a FRESH PendingEdit against the CURRENT
  // authoritative revision — never replay an old operationId/PendingEdit.
  function handleUndo() {
    const entry = history.undoStack.at(-1);
    if (!entry || isSubmitting || !draft) return;
    if (entry.undo.kind === "edit") {
      submit(entry.undo.operation, { kind: "undo", entry });
      return;
    }
    executeSpecialHistoryCommand(entry, "undo");
  }

  function handleRedo() {
    const entry = history.redoStack.at(-1);
    if (!entry || isSubmitting || !draft) return;
    if (entry.redo.kind === "edit") {
      submit(entry.redo.operation, { kind: "redo", entry });
      return;
    }
    executeSpecialHistoryCommand(entry, "redo");
  }

  // Reset-to-Scan is the one HistoryEntry whose undo/redo are NOT plain
  // EditOperationPayloads (they're resetToScan/restoreAfterReset — see
  // confirmReset below), so they need their own submission path alongside
  // submitPendingEdit's PendingEdit-shaped one — same server-success-only
  // history discipline, same fresh-operationId-per-attempt discipline.
  function executeSpecialHistoryCommand(entry: HistoryEntry, direction: "undo" | "redo") {
    if (!draft) return;
    const action = direction === "undo" ? entry.undo : entry.redo;
    if (action.kind === "resetToScan") {
      const pendingReset = buildPendingReset(draft.revision);
      pendingHistoryRef.current = { kind: direction, entry };
      editor.startPendingReset(pendingReset);
      submitPendingReset(pendingReset);
      return;
    }
    if (action.kind === "restoreAfterReset") {
      const pending = buildPendingUndoReset(action.resetOperationId, draft.revision);
      pendingHistoryRef.current = { kind: direction, entry };
      setPendingUndoReset(pending);
      submitPendingUndoReset(pending);
    }
  }

  // Structurally parallel to submitPendingReset (§5's same
  // server-success-only history discipline), operating on
  // useUndoResetRoomDraft/pendingUndoReset instead of useResetRoomDraft/
  // editor.pendingReset.
  function submitPendingUndoReset(pending: ReturnType<typeof buildPendingUndoReset>) {
    setLastError(null);
    undoReset.mutate(pending, {
      onSuccess: () => {
        setPendingUndoReset(null);
        editor.reconcileSelection(false);
        const historyAction = pendingHistoryRef.current;
        if (historyAction.kind === "undo") history.committedUndo(historyAction.entry);
        if (historyAction.kind === "redo") history.committedRedo(historyAction.entry);
        pendingHistoryRef.current = { kind: "none" };
      },
      onError: (thrown) => {
        const error = toApiError(thrown);
        setLastError(error);
        const kind = classifyEditError(error);
        if (kind === "network") {
          setPendingUndoReset((current) => (current ? { ...current, status: "retryable-error" } : current));
          return;
        }
        if (kind === "stale_revision") {
          setPendingUndoReset(null);
          pendingHistoryRef.current = { kind: "none" };
          history.clear();
          draftQuery.refetch();
          return;
        }
        setPendingUndoReset(null);
        pendingHistoryRef.current = { kind: "none" };
      },
    });
  }

  // Structurally parallel to submitPendingEdit, operating on the separate
  // pendingReset slot/useResetRoomDraft mutation — see PendingReset's doc
  // comment in types.ts for why this is not folded into submitPendingEdit.
  function submitPendingReset(pendingReset: ReturnType<typeof buildPendingReset>) {
    setLastError(null);
    resetDraft.mutate(pendingReset, {
      onSuccess: () => {
        editor.clearPendingReset();
        // Nothing selected reliably survives a reset (fixtures/service
        // points/constraints are cleared entirely; walls/openings/objects
        // revert to baseline IDs that may not match current selection) —
        // clear unconditionally rather than checking elementStillExistsInDraft.
        editor.reconcileSelection(false);

        const historyAction = pendingHistoryRef.current;
        if (historyAction.kind === "record") history.record(historyAction.entry);
        if (historyAction.kind === "undo") history.committedUndo(historyAction.entry);
        if (historyAction.kind === "redo") history.committedRedo(historyAction.entry);
        pendingHistoryRef.current = { kind: "none" };
      },
      onError: (thrown) => {
        const error = toApiError(thrown);
        setLastError(error);
        const kind = classifyEditError(error);
        if (kind === "network") {
          editor.markPendingResetRetryable();
          return;
        }
        if (kind === "stale_revision") {
          editor.clearPendingReset();
          pendingHistoryRef.current = { kind: "none" };
          history.clear();
          draftQuery.refetch();
          return;
        }
        editor.clearPendingReset();
        pendingHistoryRef.current = { kind: "none" };
      },
    });
  }

  // §11: Reset is ONE atomic HistoryEntry — undoing it once restores the
  // exact pre-reset state via restore_element/UndoResetToScan's own
  // operationId reference, never a replay of every individual operation
  // reset would otherwise have undone.
  function confirmReset() {
    if (!draft) return;
    const pendingReset = buildPendingReset(draft.revision);
    const entry: HistoryEntry = {
      id: crypto.randomUUID(),
      label: "Reset to scan",
      undo: { kind: "restoreAfterReset", resetOperationId: pendingReset.operationId },
      redo: { kind: "resetToScan" },
    };
    pendingHistoryRef.current = { kind: "record", entry };
    editor.startPendingReset(pendingReset);
    setResetDialogOpen(false);
    submitPendingReset(pendingReset);
  }

  function retryPendingReset() {
    if (!editor.pendingReset) return;
    submitPendingReset(editor.pendingReset);
  }

  function retryPendingUndoReset() {
    if (!pendingUndoReset) return;
    submitPendingUndoReset(pendingUndoReset);
  }

  function handleSelect(kind: ElementKind, id: string) {
    editor.select(kind, id);
  }

  // RP4C3/T1B: 3D drag completion for the supported direct 3D edits
  // (free-standing fixtures/service points/objects only — wall-attached
  // elements get no gizmo, see Spatial3DViewport's draggable computation).
  // Routes through the exact same `submit` function 2D drag handlers call —
  // no parallel mutation path.
  // §10: all three object transform modes route through the SAME
  // reversible submitUserEdit path fixture/service-point translate already
  // used — no parallel history logic lives in Spatial3DViewport.
  function handle3DDragEnd(drag: Spatial3DDragEnd) {
    if (!draft) return;
    switch (drag.kind) {
      case "fixture": {
        const fixture = draft.fixtures?.find((f) => f.id === drag.fixtureId);
        if (!fixture) return;
        submitUserEdit(moveFixtureOperation({ fixtureId: drag.fixtureId, transform: withUpdatedPosition(fixture.transform, drag.position) }));
        return;
      }
      case "object":
        submitUserEdit(moveObjectOperation({ objectId: drag.objectId, position: drag.position }));
        return;
      case "objectRotate":
        submitUserEdit(rotateObjectOperation({ objectId: drag.objectId, rotation: drag.rotation }));
        return;
      case "objectResize":
        submitUserEdit(resizeObjectOperation({ objectId: drag.objectId, dimensions: drag.dimensions }));
        return;
      case "servicePoint":
        submitUserEdit(moveServicePointOperation({ servicePointId: drag.servicePointId, position: drag.position }));
        return;
    }
  }

  function handleDragEnd(target: DragTarget, position: RoomLocalPoint) {
    if (!draft) return;
    switch (target.kind) {
      case "wallEndpoint":
        submitUserEdit(
          moveCornerOperation({
            endpoints: coincidentEndpoints(draft, target.wallId, target.endpoint),
            newPosition: position,
          }),
        );
        return;
      case "wallBody":
        // position here is the drag's delta (see FloorPlanViewport's
        // DragEndPosition doc comment), matching MoveWallOperation exactly.
        submitUserEdit(moveWallOperation({ wallId: target.wallId, delta: position }));
        return;
      case "servicePoint":
        submitUserEdit(moveServicePointOperation({ servicePointId: target.servicePointId, position }));
        return;
      case "fixture": {
        const fixture = draft.fixtures?.find((f) => f.id === target.fixtureId);
        if (!fixture) return;
        submitUserEdit(moveFixtureOperation({ fixtureId: target.fixtureId, transform: withUpdatedPosition(fixture.transform, position) }));
        return;
      }
      case "object": {
        submitUserEdit(moveObjectOperation({ objectId: target.objectId, position }));
        return;
      }
      case "constraint": {
        const constraint = draft.constraints?.find((c) => c.id === target.constraintId);
        if (!constraint) return;
        submitUserEdit(moveConstraintOperation({ constraintId: target.constraintId, transform: { ...constraint.transform, position } }));
        return;
      }
    }
  }

  // Deliberately minimal "add" affordances (matching RP4C1's
  // add_service_point precedent): drop a new element near the room center
  // with a default category; precise placement/classification is a
  // drag/inspector-edit away afterward. Not a creation wizard.
  function handleAddServicePoint() {
    if (!draft) return;
    submitUserEdit(addServicePointOperation({ kind: "plumbing", position: roomCenter(draft) }));
  }

  function handleAddFixture() {
    if (!draft) return;
    submitUserEdit(
      addFixtureOperation({
        category: "other",
        transform: { position: roomCenter(draft), rotation: { x: 0, y: 0, z: 0, w: 1 } },
      }),
    );
  }

  function handleAddConstraint() {
    if (!draft) return;
    submitUserEdit(
      addConstraintOperation({
        kind: "immovable_obstacle",
        transform: { position: roomCenter(draft), rotation: { x: 0, y: 0, z: 0, w: 1 } },
      }),
    );
  }

  // §8: one centralized Delete affordance for every canonical-remove-capable
  // selection, instead of each inspector branch inventing its own UX.
  // Walls have no remove_wall operation (not deletable — see the "no exact
  // inverse" matrix), so they are excluded here rather than wired to
  // something that doesn't exist.
  function deleteOperationForSelection(selection: NonNullable<Selection>): EditOperationPayload | null {
    switch (selection.kind) {
      case "opening":
        return removeOpeningOperation({ openingId: selection.id });
      case "object":
        return removeObjectOperation({ objectId: selection.id });
      case "fixture":
        return removeFixtureOperation({ fixtureId: selection.id });
      case "servicePoint":
        return removeServicePointOperation({ servicePointId: selection.id });
      case "constraint":
        return removeConstraintOperation({ constraintId: selection.id });
      case "wall":
        return null;
    }
  }

  function requestDelete() {
    if (!editor.selection) return;
    if (!deleteOperationForSelection(editor.selection)) return;
    setDeleteTarget(editor.selection);
  }

  function confirmDelete() {
    if (!deleteTarget) return;
    const operation = deleteOperationForSelection(deleteTarget);
    setDeleteTarget(null);
    if (!operation) return;
    submitUserEdit(operation);
  }

  if (draftQuery.isLoading) {
    return (
      <div className="surface-card min-h-96 p-5">
        <Skeleton className="h-full w-full" />
      </div>
    );
  }

  if (draftQuery.isError) {
    const error = toApiError(draftQuery.error);
    const kind = classifyEditError(error);
    return (
      <div className="surface-card flex min-h-96 flex-col items-center justify-center gap-2 p-8 text-center">
        <p className="font-heading text-base font-semibold">
          {kind === "not_found" ? "Floor plan not found" : kind === "forbidden" ? "You don't have access to this floor plan" : "Could not load the floor plan"}
        </p>
        <p className="max-w-md text-sm text-muted-foreground">
          {kind === "network" ? "Check your connection and try again." : "Please try again, or come back to this later."}
        </p>
        <Button className="mt-2" variant="outline" onClick={() => draftQuery.refetch()}>
          Retry
        </Button>
      </div>
    );
  }

  const isSubmitting = submitEdit.isPending || resetDraft.isPending || undoReset.isPending;
  const pendingEdit = editor.pendingEdit;
  const pendingReset = editor.pendingReset;
  const deletableSelected = Boolean(editor.selection && deleteOperationForSelection(editor.selection));

  return (
    // §1: revision is an internal CAS token, not contractor-facing history —
    // no longer displayed in the UI. data-revision stays for diagnostics/
    // tests (proving a mutation actually advanced the CAS token) without
    // exposing it visually.
    <div
      className="@container grid min-w-0 grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_clamp(390px,29cqw,440px)] xl:gap-6"
      data-revision={draft?.revision}
    >
      <div className="grid min-h-130 gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1">
              <Button size="sm" variant="outline" onClick={handleUndo} disabled={!history.canUndo || isSubmitting} aria-label="Undo">
                Undo
              </Button>
              <Button size="sm" variant="outline" onClick={handleRedo} disabled={!history.canRedo || isSubmitting} aria-label="Redo">
                Redo
              </Button>
            </div>
            <EditorViewSwitcher activeView={editor.activeView} onChange={editor.setActiveView} disabled={isSubmitting} />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={handleAddServicePoint} disabled={isSubmitting || !draft}>
              Add service point
            </Button>
            <Button size="sm" variant="outline" onClick={handleAddFixture} disabled={isSubmitting || !draft}>
              Add fixture
            </Button>
            <Button size="sm" variant="outline" onClick={handleAddConstraint} disabled={isSubmitting || !draft}>
              Add constraint
            </Button>
            {deletableSelected && (
              <Button size="sm" variant="outline" className="text-destructive" onClick={requestDelete} disabled={isSubmitting}>
                Delete {editor.selection && labelForSelection(editor.selection)}
              </Button>
            )}
            {draft?.canResetToScan && (
              <Button size="sm" variant="outline" className="text-destructive" onClick={() => setResetDialogOpen(true)} disabled={isSubmitting}>
                Reset to scan
              </Button>
            )}
          </div>
        </div>
        {editor.activeView === "2d" ? (
          <FloorPlanViewport
            draft={draft}
            viewport={editor.viewport}
            onViewportChange={editor.setViewport}
            selection={editor.selection}
            onSelect={handleSelect}
            hoverId={editor.hoverId}
            onHover={editor.setHover}
            onDragEnd={handleDragEnd}
            onObjectResizeEnd={(end) => submitUserEdit(resizeObjectOperation({ objectId: end.objectId, dimensions: end.dimensions }))}
            onObjectRotateEnd={(end) => submitUserEdit(rotateObjectOperation({ objectId: end.objectId, rotation: end.rotation }))}
            dragDisabled={isSubmitting}
          />
        ) : (
          draft && (
            <Spatial3DErrorBoundary>
              <Spatial3DViewport
                draft={draft}
                selection={editor.selection}
                onSelect={handleSelect}
                onClearSelection={editor.clearSelection}
                hoverId={editor.hoverId}
                onHover={editor.setHover}
                onDragEnd={handle3DDragEnd}
                dragDisabled={isSubmitting}
                cameraPose={editor.cameraPose}
                onCameraPoseSettled={editor.setCameraPose}
                reducedMotion={reducedMotion}
                conceptPreview={conceptPreview}
                comparisonMode={comparisonMode}
                onComparisonModeChange={setComparisonMode}
              />
            </Spatial3DErrorBoundary>
          )
        )}
        {(pendingEdit || pendingReset || pendingUndoReset || lastError) && (
          <EditStatusBanner
            pending={pendingEdit?.status === "submitting" || pendingReset?.status === "submitting" || pendingUndoReset?.status === "submitting"}
            error={lastError}
          />
        )}
        {pendingEdit?.status === "retryable-error" && (
          <Button size="sm" onClick={retryPendingEdit}>
            Retry edit
          </Button>
        )}
        {pendingReset?.status === "retryable-error" && (
          <Button size="sm" onClick={retryPendingReset}>
            Retry reset
          </Button>
        )}
        {pendingUndoReset?.status === "retryable-error" && (
          <Button size="sm" onClick={retryPendingUndoReset}>
            Retry undo
          </Button>
        )}
      </div>
      <AIDesignStudio
        roomDraftId={roomDraftId}
        draft={draft}
        selection={editor.selection}
        onSubmitEdit={submitUserEdit}
        editingDisabled={isSubmitting}
        onConceptPreviewChange={(preview, mode, setMode) => {
          setConceptPreview(preview);
          setComparisonModeState(mode);
          setSetComparisonMode(() => setMode);
        }}
        onDesignCommitted={handleDesignCommitted}
      />

      <Dialog open={resetDialogOpen} onOpenChange={setResetDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reset room to original scan?</DialogTitle>
            <DialogDescription>
              This restores the original RoomPlan capture. Manual additions and edits will disappear from the current room.
              You can undo this reset during this editing session.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setResetDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmReset}>
              Reset to Scan
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {deleteTarget && labelForSelection(deleteTarget)}?</DialogTitle>
            <DialogDescription>
              This removes the selected element from the current room. You can undo this during this editing session.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmDelete}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function toApiError(thrown: unknown): ApiError {
  if (thrown && typeof thrown === "object" && "kind" in thrown) return thrown as ApiError;
  return normalizeApiError({});
}

// move_corner selection assistance ONLY (plan §6/§RP4C1 amendment): finds
// every wall endpoint within a small tolerance of the dragged endpoint's
// ORIGINAL position, so dragging one visual corner moves every wall that
// shares it. This never expands automatically beyond what the user visually
// dragged — it identifies coincidence at the moment of the drag, then the
// resulting explicit endpoint list is what's submitted; the backend performs
// no further epsilon-based discovery of its own.
const CORNER_COINCIDENCE_EPSILON_METERS = 0.02;

function coincidentEndpoints(draft: RoomDraft, wallId: string, endpoint: "start" | "end") {
  const draggedWall = draft.walls?.find((w) => w.id === wallId);
  if (!draggedWall) return [{ wallId, endpoint }];
  const origin = endpoint === "start" ? draggedWall.start : draggedWall.end;

  const result: { wallId: string; endpoint: "start" | "end" }[] = [];
  for (const wall of draft.walls ?? []) {
    for (const candidateEndpoint of ["start", "end"] as const) {
      const point = candidateEndpoint === "start" ? wall.start : wall.end;
      if (distance(point, origin) <= CORNER_COINCIDENCE_EPSILON_METERS) {
        result.push({ wallId: wall.id, endpoint: candidateEndpoint });
      }
    }
  }
  return result.length > 0 ? result : [{ wallId, endpoint }];
}

function distance(a: RoomLocalPoint, b: RoomLocalPoint): number {
  return Math.sqrt((a.x - b.x) ** 2 + (a.y - b.y) ** 2 + (a.z - b.z) ** 2);
}

function roomCenter(draft: RoomDraft): RoomLocalPoint {
  const points = (draft.walls ?? []).flatMap((w) => [w.start, w.end]);
  if (points.length === 0) return { x: 0, y: 0, z: 0 };
  const sum = points.reduce((acc, p) => ({ x: acc.x + p.x, y: acc.y + p.y, z: acc.z + p.z }), { x: 0, y: 0, z: 0 });
  return { x: sum.x / points.length, y: 0, z: sum.z / points.length };
}
