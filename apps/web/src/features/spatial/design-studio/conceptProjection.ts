import type { RoomDraft } from "../api";
import type { Selection } from "../editor/types";
import type { ConceptRenderState, DesignGenerationAttempt } from "./types";

// Derives the exact target render override (ConceptRenderState) from the
// authoritative RoomDraft plus the session's latest public
// DesignGenerationAttempt — a pure projection, matching sceneProjection.ts's
// own "jsdom-free-testable, never imports three/@react-three, never
// persisted" convention exactly. Spatial3DViewport (Gate 3) consumes the
// result read-only; this file never mutates RoomDraft or attempt data.
//
// Candidate transform/dimensions come from the attempt's own candidate
// (server-computed, authoritative placement) — never recomputed here.
// Target identity comes from the SELECTED RoomDraft element's canonical
// {kind,id}, confirmed to still exist in the current draft before ever
// projecting a concept onto it (M8.5C plan: "Candidate geometry replaces
// only the exact selected RoomDraft identity").
export function projectConceptRenderState(input: {
  draft: RoomDraft | undefined;
  selection: Selection;
  attempt: DesignGenerationAttempt | undefined;
  /** The RoomDraft revision the attempt's session was pinned to. */
  attemptBasedOnRoomDraftRevision: number | undefined;
  isAttemptStale: boolean;
}): ConceptRenderState {
  const { draft, selection, attempt, isAttemptStale } = input;

  if (!draft || !selection || !attempt) return { kind: "none" };

  // The concept must target the SAME element currently selected — an
  // attempt from a prior selection must never be projected onto whatever
  // happens to be selected now.
  if (attempt.candidate?.target?.kind !== selection.kind || attempt.candidate?.target?.id !== selection.id) {
    return { kind: "none" };
  }

  if (isAttemptStale) {
    return { kind: "stale", attemptId: attempt.id };
  }

  if (!elementExists(draft, selection)) {
    return { kind: "none" };
  }

  const candidate = attempt.candidate;
  const transform = candidate.transform;
  const dimensions = candidate.dimensions;
  const appearance = candidate.appearance;

  switch (attempt.status) {
    case "concept_ready":
    case "accepted":
      return {
        kind: "concept_ready",
        attemptId: attempt.id,
        target: selection,
        assetRef: candidate.visualAsset ? { assetId: candidate.visualAsset.assetId, version: candidate.visualAsset.version } : undefined,
        appearance,
        transform,
        dimensions,
      };
    case "reserved":
    case "generating_reference":
    case "reference_ready":
    case "asset_generation_pending":
    case "asset_generation_processing":
      // A material-only attempt reaches concept_ready directly (no
      // intermediate generation phases are ever observed for it), so any
      // attempt seen in one of these phases is by construction
      // geometry/mixed — appearance previews immediately without waiting
      // for Hunyuan (plan: "Material-only concepts preview without
      // Hunyuan"), geometry has no asset yet.
      return {
        kind: appearance ? "material_preview" : "geometry_pending",
        attemptId: attempt.id,
        target: selection,
        appearance: appearance as NonNullable<typeof appearance>,
        transform,
        dimensions,
      };
    default:
      // failed / needs_attention / abandoned — nothing to render; the
      // studio's own failure UI communicates state, the canvas stays on
      // the authoritative Current view.
      return { kind: "none" };
  }
}

function elementExists(draft: RoomDraft, selection: Selection): boolean {
  if (!selection) return false;
  if (selection.kind === "object") return (draft.objects ?? []).some((o) => o.id === selection.id);
  if (selection.kind === "fixture") return (draft.fixtures ?? []).some((f) => f.id === selection.id);
  return false;
}

// isSupportedDesignTarget mirrors the backend's DesignTargetKind = object |
// fixture restriction exactly (backend/internal/spatial/designsession.go) —
// the studio must show its "unsupported" state, never a silently broken
// empty composer, for any other selection kind.
export function isSupportedDesignTarget(selection: Selection): boolean {
  return selection !== null && (selection.kind === "object" || selection.kind === "fixture");
}
