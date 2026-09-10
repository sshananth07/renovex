import { describe, expect, it } from "vitest";
import { isSupportedDesignTarget, projectConceptRenderState } from "./conceptProjection";
import type { RoomDraft } from "../api";
import type { DesignGenerationAttempt } from "./types";

const baseDraft: RoomDraft = {
  id: "roomdraft_1",
  revision: 17,
  walls: [],
  openings: [],
  objects: [{ id: "object_sofa_123", category: "sofa", transform: { position: { x: 1, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } }],
  fixtures: [],
  servicePoints: [],
  constraints: [],
} as unknown as RoomDraft;

function makeAttempt(overrides: Partial<DesignGenerationAttempt> = {}): DesignGenerationAttempt {
  return {
    id: "attempt_1",
    sessionId: "session_1",
    turnId: "turn_1",
    attemptNumber: 1,
    kind: "material_only",
    status: "concept_ready",
    basedOnRoomDraftRevision: 17,
    createdAt: "2026-09-09T00:00:00Z",
    updatedAt: "2026-09-09T00:00:00Z",
    candidate: {
      target: { kind: "object", id: "object_sofa_123" },
      transform: { position: { x: 1, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      visualAction: "preserve",
      appearanceAction: "set",
      appearance: { baseColor: "#c8a464", materialFamily: "fabric", roughness: "matte", metallic: false },
    },
    ...overrides,
  } as DesignGenerationAttempt;
}

describe("projectConceptRenderState", () => {
  it("returns none with no draft/selection/attempt", () => {
    expect(projectConceptRenderState({ draft: undefined, selection: null, attempt: undefined, attemptBasedOnRoomDraftRevision: undefined, isAttemptStale: false })).toEqual({ kind: "none" });
  });

  it("returns none when the attempt targets a DIFFERENT element than the current selection", () => {
    const attempt = makeAttempt();
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_other" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result).toEqual({ kind: "none" });
  });

  it("returns stale when isAttemptStale is true, even for a concept_ready attempt", () => {
    const attempt = makeAttempt({ status: "concept_ready" });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_sofa_123" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: true,
    });
    expect(result).toEqual({ kind: "stale", attemptId: "attempt_1" });
  });

  it("returns none when the selected element no longer exists in the draft", () => {
    const attempt = makeAttempt({
      candidate: {
        target: { kind: "object", id: "object_removed" },
        transform: { position: { x: 1, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
        visualAction: "preserve",
        appearanceAction: "set",
        appearance: { baseColor: "#c8a464", materialFamily: "fabric", roughness: "matte", metallic: false },
      },
    });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_removed" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result).toEqual({ kind: "none" });
  });

  it("returns concept_ready for a concept_ready material-only attempt with appearance and no asset ref", () => {
    const attempt = makeAttempt({ status: "concept_ready" });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_sofa_123" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result.kind).toBe("concept_ready");
    if (result.kind === "concept_ready") {
      expect(result.assetRef).toBeUndefined();
      expect(result.appearance?.baseColor).toBe("#c8a464");
    }
  });

  it("returns concept_ready with an asset ref once geometry publishes", () => {
    const attempt = makeAttempt({
      status: "concept_ready",
      kind: "geometry",
      candidate: {
        target: { kind: "object", id: "object_sofa_123" },
        transform: { position: { x: 1, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
        visualAction: "assign",
        appearanceAction: "preserve",
        visualAsset: { assetId: "asset_1", version: 2 },
      },
    });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_sofa_123" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result.kind).toBe("concept_ready");
    if (result.kind === "concept_ready") {
      expect(result.assetRef).toEqual({ assetId: "asset_1", version: 2 });
    }
  });

  it("returns material_preview for a non-terminal attempt that already has appearance (near-instant preview, no Hunyuan wait)", () => {
    const attempt = makeAttempt({ status: "reserved" });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_sofa_123" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result.kind).toBe("material_preview");
  });

  it("returns geometry_pending for a non-terminal geometry attempt with no appearance yet", () => {
    const attempt = makeAttempt({
      status: "asset_generation_processing",
      kind: "geometry",
      candidate: {
        target: { kind: "object", id: "object_sofa_123" },
        transform: { position: { x: 1, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
        visualAction: "assign",
        appearanceAction: "preserve",
      },
    });
    const result = projectConceptRenderState({
      draft: baseDraft,
      selection: { kind: "object", id: "object_sofa_123" },
      attempt,
      attemptBasedOnRoomDraftRevision: 17,
      isAttemptStale: false,
    });
    expect(result.kind).toBe("geometry_pending");
  });

  it("returns none for a failed/needs_attention/abandoned attempt", () => {
    for (const status of ["failed", "needs_attention", "abandoned"] as const) {
      const attempt = makeAttempt({ status });
      const result = projectConceptRenderState({
        draft: baseDraft,
        selection: { kind: "object", id: "object_sofa_123" },
        attempt,
        attemptBasedOnRoomDraftRevision: 17,
        isAttemptStale: false,
      });
      expect(result).toEqual({ kind: "none" });
    }
  });
});

describe("isSupportedDesignTarget", () => {
  it("accepts object and fixture selections", () => {
    expect(isSupportedDesignTarget({ kind: "object", id: "o1" })).toBe(true);
    expect(isSupportedDesignTarget({ kind: "fixture", id: "f1" })).toBe(true);
  });

  it("rejects wall/opening/servicePoint/constraint selections and null", () => {
    expect(isSupportedDesignTarget({ kind: "wall", id: "w1" })).toBe(false);
    expect(isSupportedDesignTarget({ kind: "opening", id: "op1" })).toBe(false);
    expect(isSupportedDesignTarget({ kind: "servicePoint", id: "sp1" })).toBe(false);
    expect(isSupportedDesignTarget({ kind: "constraint", id: "c1" })).toBe(false);
    expect(isSupportedDesignTarget(null)).toBe(false);
  });
});
