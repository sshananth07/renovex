import type { components } from "@/lib/api/generated/schema";
import type { RoomLocalPoint, RoomLocalTransform, Selection } from "../editor/types";

// Public RP4E2 attempt/acceptance types — generated OpenAPI aliases only,
// never a hand-maintained parallel shape (matches designSessionApi.ts's
// existing convention exactly).
export type DesignGenerationAttempt = components["schemas"]["DesignGenerationAttemptDTO"];
export type DesignConcept = components["schemas"]["DesignConceptDTO"];
export type DesignAcceptance = components["schemas"]["DesignAcceptanceDTO"];
export type VisualAppearance = components["schemas"]["VisualAppearanceDTO"];
export type DesignTarget = components["schemas"]["DesignTargetDTO"];

export type ConfirmDesignPlanInput = components["schemas"]["ConfirmDesignPlanInputBody"];
export type RegenerateDesignPlanInput = components["schemas"]["RegenerateDesignPlanInputBody"];
export type CancelDesignGenerationAttemptInput = components["schemas"]["CancelDesignGenerationAttemptInputBody"];
export type UseDesignPlanInput = components["schemas"]["UseDesignPlanInputBody"];
export type UseDesignPlanOutput = components["schemas"]["UseDesignPlanOutputBody"];

// The four appearance/binding action strings the backend's DesignConceptDTO
// carries as plain `string` (Huma doesn't narrow these in the generated
// schema) — declared here as the exact closed set
// backend/internal/spatial/designgeneration.go defines, so studio code can
// switch on them without re-validating at every call site.
export type ConceptVisualAction = "preserve" | "assign" | "clear";
export type ConceptAppearanceAction = "preserve" | "set" | "clear";

// DesignGenerationAttemptDTO.status's closed set (backend
// DesignGenerationStatus) — declared here for the same reason.
export type DesignGenerationStatus =
  | "reserved"
  | "generating_reference"
  | "reference_ready"
  | "asset_generation_pending"
  | "asset_generation_processing"
  | "concept_ready"
  | "failed"
  | "needs_attention"
  | "abandoned"
  | "accepted";

// ConceptRenderState is a PROJECTION of server state for the renderer —
// never an independent source of truth (M8.5C plan: "This is a projection
// of server state, never an independent source of truth. Candidate
// transform and dimensions come from the validated plan/attempt, target
// identity comes from the authoritative RoomDraft selection, and an asset
// reference comes only from a completed RP4E0 job."). conceptProjection.ts
// derives this from a RoomDraft + the latest public DesignGenerationAttempt;
// Spatial3DViewport (Gate 3) consumes it read-only.
export type ConceptRenderState =
  | { kind: "none" }
  | {
      kind: "material_preview";
      attemptId: string;
      target: Selection;
      appearance: VisualAppearance;
      transform: RoomLocalTransform;
      dimensions?: RoomLocalPoint;
    }
  | {
      kind: "geometry_pending";
      attemptId: string;
      target: Selection;
      appearance?: VisualAppearance;
      transform: RoomLocalTransform;
      dimensions?: RoomLocalPoint;
    }
  | {
      kind: "concept_ready";
      attemptId: string;
      target: Selection;
      assetRef?: { assetId: string; version: number };
      appearance?: VisualAppearance;
      transform: RoomLocalTransform;
      dimensions?: RoomLocalPoint;
    }
  | { kind: "stale"; attemptId: string };

// Current/Concept comparison mode for the canvas — "current" always shows
// the authoritative RoomDraft; "concept" additionally substitutes the exact
// selected target with ConceptRenderState's candidate when one exists.
export type ComparisonMode = "current" | "concept";

// The AI Design Studio's own UI state machine — distinct from
// DesignGenerationStatus (server attempt lifecycle) and ActiveEditorView
// (2D/3D switch, unrelated). Drives which state-specific component renders
// in the dock/overlay/bottom-sheet (see the plan's "Explicit UX state
// machine" table).
// "concept_ready" covers both the material-only and geometry/mixed
// concept-ready cases (plan's "Confirmed/material-only" row is visually a
// near-instant instance of the same ready-to-review state, distinguished
// at render time by DesignGenerationAttemptDTO.kind, not by a separate
// StudioState value).
export type StudioState =
  | "empty"
  | "unsupported"
  | "restoring"
  | "welcome"
  | "planning"
  | "plan_ready"
  | "plan_blocked"
  | "geometry_generating"
  | "concept_ready"
  | "regenerating"
  | "failed"
	| "needs_attention"
  | "stale"
  | "accepted";

// Whether the studio dock is expanded, collapsed to a floating reopen
// control, or (below 1024px) shown as an overlay/bottom sheet — layout
// concern only, orthogonal to StudioState.
export type StudioLayoutMode = "dock" | "overlay" | "sheet";
