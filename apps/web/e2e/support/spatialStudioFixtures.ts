import type { Page, Route } from "@playwright/test";
import type { components } from "@/lib/api/generated/schema";

// RP4E3 visual-acceptance spec fixtures: deterministic network responses
// shaped exactly like the real generated OpenAPI DTOs (components["schemas"]),
// intercepted at the Playwright network layer via page.route against the
// real NEXT_PUBLIC_API_BASE_URL. This is the ONLY thing mocked — SpatialEditor,
// AIDesignStudio, Spatial3DViewport, and every design-studio component are
// the real production components, rendered in a real browser, with real
// Three.js selection/raycasting and real React Query/MSW-free network
// plumbing. No test-only seeding route, no fabricated RoomPlan capture
// chain, no alternate renderer.

type RoomDraftDTO = components["schemas"]["RoomDraftDTO"];
type DesignSessionDTO = components["schemas"]["DesignSessionDTO"];
type DesignTurnDTO = components["schemas"]["DesignTurnDTO"];
type DesignGenerationAttemptDTO = components["schemas"]["DesignGenerationAttemptDTO"];

export const API_BASE_URL = process.env.PLAYWRIGHT_API_BASE_URL ?? "http://localhost:8080";

export const PROJECT_ID = "project_studio_e2e";
export const SPACE_ID = "space_studio_e2e";
export const ROOM_DRAFT_ID = "draft_studio_e2e";
export const OBJECT_ID = "object_sofa_e2e";
export const SESSION_ID = "session_studio_e2e";

export function studioRoute(): string {
  return `/projects/${PROJECT_ID}/spaces/${SPACE_ID}/spatial/${ROOM_DRAFT_ID}`;
}

// A square room whose wall-corner centroid is exactly (2,0,2) — the same
// point the single selectable object sits at. Spatial3DViewport's initial
// camera (computeOverviewPreset) targets the wall-bounds centroid, so the
// object projects to the exact center pixel of the canvas: a real Playwright
// click at the canvas's visual center deterministically raycasts onto it,
// with no per-test camera-math guessing.
export function roomDraft(overrides: Partial<RoomDraftDTO> = {}): RoomDraftDTO {
  return {
    id: ROOM_DRAFT_ID,
    captureId: "capture_studio_e2e",
    revision: 1,
    canResetToScan: true,
    walls: [
      { id: "wall_n", start: { x: 0, y: 0, z: 0 }, end: { x: 4, y: 0, z: 0 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w1" }, thicknessStatus: "estimated" },
      { id: "wall_e", start: { x: 4, y: 0, z: 0 }, end: { x: 4, y: 0, z: 4 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w2" }, thicknessStatus: "estimated" },
      { id: "wall_s", start: { x: 4, y: 0, z: 4 }, end: { x: 0, y: 0, z: 4 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w3" }, thicknessStatus: "estimated" },
      { id: "wall_w", start: { x: 0, y: 0, z: 4 }, end: { x: 0, y: 0, z: 0 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w4" }, thicknessStatus: "estimated" },
    ],
    openings: [],
    objects: [
      {
        id: OBJECT_ID,
        // Deliberately NOT "sofa" (or any other category with a builtin
        // VISUAL_ASSET_REGISTRY entry, e.g. sofa/boiler/electrical_panel):
        // a registry hit loads a real GLB model whose actual raycastable
        // mesh geometry is opaque to this fixture and (confirmed
        // empirically) can occlude/be occluded by neighboring room
        // geometry in ways that don't match its apparent on-screen size,
        // making click-position math unreliable. An unregistered category
        // always renders AssetFallback's plain, exactly-`dimensions`-sized
        // procedural box (see ObjectMesh's hasAsset branch) — the only
        // shape this fixture's screen-position math can reason about
        // precisely.
        category: "armchair",
        transform: { position: { x: 2, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
        dimensions: { x: 2, y: 0.85, z: 1 },
        provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
      },
    ],
    fixtures: [],
    servicePoints: [],
    // No constraints in this fixture: ConstraintMesh renders as a
    // wireframe lineSegments-only mesh with no solid hit-test geometry —
    // Three.js's built-in Line raycasting uses a generous default
    // 1-world-unit threshold around every edge, which (confirmed
    // empirically) covers most of this small room's canvas regardless of
    // where the constraint sits, making it an unreliable click target for
    // a deterministic test. The "unsupported target" state instead
    // selects wall_n directly (see the "03 unsupported" test) — walls
    // render as ordinary solid `<mesh><boxGeometry>` with precise,
    // ordinary raycasting.
    constraints: [],
    ...overrides,
  };
}

export function designSession(overrides: Partial<DesignSessionDTO> = {}): DesignSessionDTO {
  return {
    id: SESSION_ID,
    roomDraftId: ROOM_DRAFT_ID,
    basedOnRoomDraftRevision: 1,
    revision: 1,
    status: "active",
    target: { kind: "object", id: OBJECT_ID },
    currentWorkingDesign: { resolvedSpatialOperations: [] },
    createdAt: "2026-09-10T00:00:00Z",
    updatedAt: "2026-09-10T00:00:00Z",
    ...overrides,
  };
}

export function geometryTurn(overrides: Partial<DesignTurnDTO> = {}): DesignTurnDTO {
  return {
    id: "turn_studio_e2e",
    sessionId: SESSION_ID,
    basedOnRoomDraftRevision: 1,
    sequence: 1,
    status: "proposed",
    instruction: "Make this sofa curved, dark green velvet, and move it slightly.",
    planFingerprint: "sha256:e2e-fixture",
    createdAt: "2026-09-10T00:00:00Z",
    changePlan: {
      summary: ["Curved silhouette, three-seat.", "Dark green velvet upholstery.", "Shifted 20cm from the wall."],
      turnDelta: { geometry: { mode: "replace" }, material: { mode: "replace" }, spatial: { mode: "replace" } },
      workingDesign: {
        geometry: { category: "sofa", shapeDescription: "Curved three-seat sofa with wooden legs", preserveCanonicalDimensions: true },
        material: { baseColor: "#2f5233", materialFamily: "fabric", roughness: "matte", metallic: false },
        resolvedSpatialOperations: [],
      },
    },
    execution: { executable: true, hunyuanRequired: true, requiresConfirmation: true, turnRequiresAssetGeneration: true },
    fitAnalysis: { status: "passed", blockers: [], warnings: [], observations: [] },
    ...overrides,
  };
}

export function materialOnlyTurn(overrides: Partial<DesignTurnDTO> = {}): DesignTurnDTO {
  return {
    id: "turn_material_e2e",
    sessionId: SESSION_ID,
    basedOnRoomDraftRevision: 1,
    sequence: 1,
    status: "proposed",
    instruction: "Change the upholstery to warm beige boucle.",
    planFingerprint: "sha256:e2e-material-fixture",
    createdAt: "2026-09-10T00:00:00Z",
    changePlan: {
      summary: ["Warm beige boucle upholstery."],
      turnDelta: { geometry: { mode: "preserve" }, material: { mode: "replace" }, spatial: { mode: "preserve" } },
      workingDesign: {
        material: { baseColor: "#e8dcc4", materialFamily: "fabric", roughness: "matte", metallic: false },
        resolvedSpatialOperations: [],
      },
    },
    execution: { executable: true, hunyuanRequired: false, requiresConfirmation: true, turnRequiresAssetGeneration: false },
    fitAnalysis: { status: "passed", blockers: [], warnings: [], observations: [] },
    ...overrides,
  };
}

export function blockedTurn(overrides: Partial<DesignTurnDTO> = {}): DesignTurnDTO {
  return {
    id: "turn_blocked_e2e",
    sessionId: SESSION_ID,
    basedOnRoomDraftRevision: 1,
    sequence: 1,
    status: "blocked",
    instruction: "Make it twice as wide and move it through the wall.",
    createdAt: "2026-09-10T00:00:00Z",
    review: { confidence: 0.4, notes: ["That size would not fit against this wall — try a narrower profile."], assumptions: [] },
    ...overrides,
  };
}

export function geometryAttempt(status: string, overrides: Partial<DesignGenerationAttemptDTO> = {}): DesignGenerationAttemptDTO {
  return {
    id: "attempt_studio_e2e",
    sessionId: SESSION_ID,
    turnId: "turn_studio_e2e",
    attemptNumber: 1,
    kind: "geometry",
    status,
    basedOnRoomDraftRevision: 1,
    createdAt: "2026-09-10T00:00:00Z",
    updatedAt: "2026-09-10T00:00:00Z",
    ...overrides,
  };
}

export function conceptReadyAttempt(overrides: Partial<DesignGenerationAttemptDTO> = {}): DesignGenerationAttemptDTO {
  return geometryAttempt("concept_ready", {
    candidate: {
      target: { kind: "object", id: OBJECT_ID },
      transform: { position: { x: 2, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      visualAction: "assign",
      appearanceAction: "set",
      appearance: { baseColor: "#2f5233", materialFamily: "fabric", roughness: "matte", metallic: false },
      visualAsset: { assetId: "asset_e2e", version: 1 },
    },
    ...overrides,
  });
}

export function materialOnlyConceptReadyAttempt(overrides: Partial<DesignGenerationAttemptDTO> = {}): DesignGenerationAttemptDTO {
  return {
    id: "attempt_material_e2e",
    sessionId: SESSION_ID,
    turnId: "turn_material_e2e",
    attemptNumber: 1,
    kind: "material_only",
    status: "concept_ready",
    basedOnRoomDraftRevision: 1,
    createdAt: "2026-09-10T00:00:00Z",
    updatedAt: "2026-09-10T00:00:00Z",
    candidate: {
      target: { kind: "object", id: OBJECT_ID },
      transform: { position: { x: 2, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      visualAction: "preserve",
      appearanceAction: "set",
      appearance: { baseColor: "#e8dcc4", materialFamily: "fabric", roughness: "matte", metallic: false },
    },
    ...overrides,
  };
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
}

// installBaseRoutes wires the fixed, always-present routes every state
// needs: a successful auth bootstrap (AuthProvider's mount-time refresh+me)
// and the RoomDraft GET. Call once per test before installStudioState.
export async function installBaseRoutes(page: Page, draft: RoomDraftDTO = roomDraft()) {
  await page.route(`${API_BASE_URL}/auth/refresh`, (route) =>
    json(route, { accessToken: "e2e-fixture-token" }),
  );
  await page.route(`${API_BASE_URL}/auth/me`, (route) =>
    json(route, {
      userId: "user_e2e", companyId: "company_e2e", companyName: "Fixture Renovations",
      email: "e2e@example.com", role: "owner", mustChangePassword: false,
    }),
  );
  await page.route(`${API_BASE_URL}/spatial/room-drafts/${ROOM_DRAFT_ID}`, (route) => json(route, draft));

  // The 3D viewport's one-time onboarding hint would otherwise float over
  // the canvas in every screenshot — pre-seed localStorage as dismissed,
  // matching a returning user, not the first-ever visit.
  await page.addInitScript(() => {
    window.localStorage.setItem("renovex.spatial3d.hintDismissed", "true");
  });
}

export type StudioStateFixture = {
  session?: DesignSessionDTO | null;
  turns?: DesignTurnDTO[];
  attempt?: DesignGenerationAttemptDTO | null;
};

// installStudioState wires the design-session/turns/attempt routes for one
// deterministic UX state. Passing session: null means "no session exists
// yet" (the empty/unsupported/first-welcome states, before
// useEnsureDesignSession's mutation would ever fire for a supported
// target) — the create-session route still responds so a supported-target
// selection can transition forward within the same test if needed.
export async function installStudioState(page: Page, fixture: StudioStateFixture) {
  const session = fixture.session === undefined ? designSession() : fixture.session;
  const turns = fixture.turns ?? [];
  const attempt = fixture.attempt ?? null;

  await page.route(`${API_BASE_URL}/spatial/design-sessions`, (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return json(route, { session: session ?? designSession() });
  });

  if (session) {
    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}`, (route) =>
      json(route, { session, stale: false, currentRoomDraftRevision: session.basedOnRoomDraftRevision }),
    );
    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/turns`, (route) => json(route, turns));

    if (attempt) {
      await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/generation-attempts/${attempt.id}`, (route) =>
        json(route, attempt),
      );
    }

    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/turns/*/confirm`, (route) =>
      json(route, attempt ?? conceptReadyAttempt()),
    );
    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/turns/*/regenerate`, (route) =>
      json(route, attempt ?? conceptReadyAttempt()),
    );
    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/generation-attempts/*/cancel`, (route) =>
      json(route, attempt ? { ...attempt, status: "abandoned" } : conceptReadyAttempt({ status: "abandoned" })),
    );
    await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/generation-attempts/*/use`, (route) =>
      json(route, { roomDraft: roomDraft({ revision: (session.basedOnRoomDraftRevision ?? 1) + 1 }) }),
    );
  }
}
