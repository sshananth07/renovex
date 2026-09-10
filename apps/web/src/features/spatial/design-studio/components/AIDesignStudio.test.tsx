import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { resetSingleFlightRefreshForTests } from "@/features/auth/singleFlightRefresh";
import { AIDesignStudio } from "./AIDesignStudio";
import type { RoomDraft } from "../../api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

beforeEach(() => {
  resetSingleFlightRefreshForTests();
});

function mockAuth() {
  server.use(http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })));
}

const draft: RoomDraft = {
  id: "draft_1",
  captureId: "capture_1",
  revision: 5,
  canResetToScan: true,
  walls: [],
  openings: [],
  objects: [
    { id: "object_sofa_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } },
  ],
  fixtures: [],
  servicePoints: [],
  constraints: [],
} as unknown as RoomDraft;

const session = {
  id: "session_1",
  roomDraftId: "draft_1",
  basedOnRoomDraftRevision: 5,
  revision: 1,
  createdAt: "2026-09-09T00:00:00Z",
  currentWorkingDesign: { geometry: null, material: null, resolvedSpatialOperations: [] },
};

function mockSessionRoutes() {
  server.use(
    http.post(`${baseUrl}/spatial/design-sessions`, () => HttpResponse.json({ session })),
    http.get(`${baseUrl}/spatial/design-sessions/:id`, () =>
      HttpResponse.json({ session, stale: false, currentRoomDraftRevision: 5 }),
    ),
  );
}

describe("AIDesignStudio", () => {
  it("shows the empty state with no selection", async () => {
    mockAuth();
    renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={null} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );
    expect(await screen.findByText("Choose a piece in the room to imagine a change.")).toBeInTheDocument();
  });

  it("shows the unsupported state for a wall selection", async () => {
    mockAuth();
    renderWithProviders(
      <AIDesignStudio
        roomDraftId="draft_1"
        draft={draft}
        selection={{ kind: "wall", id: "wall_1" }}
        onSubmitEdit={vi.fn()}
        editingDisabled={false}
      />,
    );
    expect(await screen.findByText(/currently works with movable objects and fixtures/)).toBeInTheDocument();
  });

  it("restores/creates a session and shows the welcome state with category-specific chips for a supported selection", async () => {
    mockAuth();
    mockSessionRoutes();
    server.use(http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, () => HttpResponse.json([])));

    renderWithProviders(
      <AIDesignStudio
        roomDraftId="draft_1"
        draft={draft}
        selection={{ kind: "object", id: "object_sofa_1" }}
        onSubmitEdit={vi.fn()}
        editingDisabled={false}
      />,
    );

    expect(await screen.findByText("What should we try with this sofa?")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Make it softer" })).toBeInTheDocument();
  });

  it("collapses to a reopen control and expands again", async () => {
    mockAuth();
    renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={null} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Collapse AI Design" }));
    expect(screen.getByRole("button", { name: "Reopen AI Design" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Reopen AI Design" }));
    expect(screen.getByRole("button", { name: "Collapse AI Design" })).toBeInTheDocument();
  });

  it("shows a plan-ready AIChangePlanCard once the latest turn has a change plan, then progresses to concept_ready after Confirm", async () => {
    mockAuth();
    mockSessionRoutes();
    const turn = {
      id: "turn_1",
      sessionId: "session_1",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      instruction: "curve the back",
      sequence: 1,
      status: "proposed",
      planFingerprint: "sha256:abc",
      changePlan: {
        summary: ["Curve the backrest"],
        turnDelta: { geometry: { mode: "replace" }, material: { mode: "preserve" }, spatial: { mode: "preserve" } },
        workingDesign: {
          geometry: { category: "sofa", preserveCanonicalDimensions: true, shapeDescription: "Curved-back sofa" },
          resolvedSpatialOperations: [],
        },
      },
      execution: { executable: true, hunyuanRequired: true, requiresConfirmation: true, turnRequiresAssetGeneration: true },
    };
    server.use(http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, () => HttpResponse.json([turn])));

    const attempt = {
      id: "attempt_1",
      sessionId: "session_1",
      turnId: "turn_1",
      attemptNumber: 1,
      kind: "geometry",
      status: "reserved",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      updatedAt: "2026-09-09T00:00:00Z",
    };
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, () => HttpResponse.json(attempt)),
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId`, () => HttpResponse.json(attempt)),
    );

    renderWithProviders(
      <AIDesignStudio
        roomDraftId="draft_1"
        draft={draft}
        selection={{ kind: "object", id: "object_sofa_1" }}
        onSubmitEdit={vi.fn()}
        editingDisabled={false}
      />,
    );

    expect(await screen.findByText("Curved-back sofa")).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Create concept" }));

    expect(await screen.findByText("Preparing the visual direction")).toBeInTheDocument();
  });

  it("shows ConceptActions with Use Design/Regenerate/Cancel once an attempt is concept_ready", async () => {
    mockAuth();
    mockSessionRoutes();
    const turn = {
      id: "turn_1",
      sessionId: "session_1",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      instruction: "warmer fabric",
      sequence: 1,
      status: "proposed",
      planFingerprint: "sha256:abc",
      changePlan: {
        summary: ["Switch to warm fabric"],
        turnDelta: { geometry: { mode: "preserve" }, material: { mode: "replace" }, spatial: { mode: "preserve" } },
        workingDesign: { resolvedSpatialOperations: [] },
      },
      execution: { executable: true, hunyuanRequired: false, requiresConfirmation: true, turnRequiresAssetGeneration: false },
    };
    server.use(http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, () => HttpResponse.json([turn])));

    const attempt = {
      id: "attempt_1",
      sessionId: "session_1",
      turnId: "turn_1",
      attemptNumber: 1,
      kind: "material_only",
      status: "reserved",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      updatedAt: "2026-09-09T00:00:00Z",
    };
    let getCallCount = 0;
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, () => HttpResponse.json(attempt)),
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId`, () => {
        getCallCount += 1;
        return HttpResponse.json(getCallCount === 1 ? attempt : { ...attempt, status: "concept_ready" });
      }),
    );

    renderWithProviders(
      <AIDesignStudio
        roomDraftId="draft_1"
        draft={draft}
        selection={{ kind: "object", id: "object_sofa_1" }}
        onSubmitEdit={vi.fn()}
        editingDisabled={false}
      />,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Preview change" }));

    expect(await screen.findByRole("button", { name: "Use Design" }, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Regenerate" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });

  // M8.5C closure patch §D: Use Design must participate in the same session
  // Undo/Redo history as every other editor mutation — onDesignCommitted is
  // the boundary that reports the before/after RoomDraft + target selection
  // up to SpatialEditor so it can build ONE HistoryEntry, exactly like
  // submitUserEdit does for ordinary edits.
  it("calls onDesignCommitted with the selection and before/after RoomDraft once Use Design succeeds", async () => {
    mockAuth();
    mockSessionRoutes();
    const turn = {
      id: "turn_1",
      sessionId: "session_1",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      instruction: "warmer fabric",
      sequence: 1,
      status: "proposed",
      planFingerprint: "sha256:abc",
      changePlan: {
        summary: ["Switch to warm fabric"],
        turnDelta: { geometry: { mode: "preserve" }, material: { mode: "replace" }, spatial: { mode: "preserve" } },
        workingDesign: { resolvedSpatialOperations: [] },
      },
      execution: { executable: true, hunyuanRequired: false, requiresConfirmation: true, turnRequiresAssetGeneration: false },
    };
    server.use(http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, () => HttpResponse.json([turn])));

    const attempt = {
      id: "attempt_1",
      sessionId: "session_1",
      turnId: "turn_1",
      attemptNumber: 1,
      kind: "material_only",
      status: "reserved",
      basedOnRoomDraftRevision: 5,
      createdAt: "2026-09-09T00:00:00Z",
      updatedAt: "2026-09-09T00:00:00Z",
    };
    let getCallCount = 0;
    const afterRoomDraft = { ...draft, revision: 6 };
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, () => HttpResponse.json(attempt)),
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId`, () => {
        getCallCount += 1;
        return HttpResponse.json(getCallCount === 1 ? attempt : { ...attempt, status: "concept_ready" });
      }),
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/use`, () =>
        HttpResponse.json({ acceptance: { id: "acceptance_1" }, roomDraft: afterRoomDraft }),
      ),
    );

    const onDesignCommitted = vi.fn();
    const selection = { kind: "object" as const, id: "object_sofa_1" };
    renderWithProviders(
      <AIDesignStudio
        roomDraftId="draft_1"
        draft={draft}
        selection={selection}
        onSubmitEdit={vi.fn()}
        editingDisabled={false}
        onDesignCommitted={onDesignCommitted}
      />,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Preview change" }));
    await user.click(await screen.findByRole("button", { name: "Use Design" }, { timeout: 5000 }));

    await vi.waitFor(() => expect(onDesignCommitted).toHaveBeenCalledTimes(1));
    expect(onDesignCommitted).toHaveBeenCalledWith({
      selection,
      before: draft,
      after: afterRoomDraft,
    });
  });

  it("always renders ObjectControlsDisclosure hosting manual controls, regardless of studio state", async () => {
    mockAuth();
    renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={null} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );
    expect(await screen.findByText("Manual controls")).toBeInTheDocument();
  });
});

// M8.5C reversible-editing patch §13: a manual canonical edit landing WHILE
// a design-session ensure request is in flight makes that request's
// expectedRoomDraftRevision stale — the backend rejects it with
// design_session_request_conflict (409). Exactly one retry against the NEW
// revision is correct; a second failure or a same-revision 409 must not
// retry further.
describe("AIDesignStudio — design-session request-conflict retry (§13)", () => {
  function conflictError() {
    return HttpResponse.json(
      { status: 409, title: "Conflict", type: "design_session_request_conflict", detail: "design session request conflict" },
      { status: 409 },
    );
  }

  it("retries exactly once against the NEW revision after a request-conflict 409, then succeeds", async () => {
    mockAuth();
    let sessionCreateCalls = 0;
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions`, async ({ request }) => {
        sessionCreateCalls++;
        const body = (await request.json()) as { expectedRoomDraftRevision: number };
        if (body.expectedRoomDraftRevision === 5) {
          // Simulated network latency: the manual edit's response (and this
          // test's rerender) lands BEFORE this request's own 409 response
          // arrives — matching the real race the fix must handle.
          await new Promise((resolve) => setTimeout(resolve, 200));
          return conflictError();
        }
        return HttpResponse.json({ session: { ...session, basedOnRoomDraftRevision: body.expectedRoomDraftRevision } });
      }),
      http.get(`${baseUrl}/spatial/design-sessions/:id`, () => HttpResponse.json({ session, stale: false, currentRoomDraftRevision: 6 })),
      http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, () => HttpResponse.json([])),
    );

    const { rerender } = renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={{ kind: "object", id: "object_sofa_1" }} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );

    // The manual edit lands mid-flight: the same component instance now
    // receives a draft at revision 6, before the in-flight request's 409
    // response arrives (see the artificial delay above). rerender + an
    // explicit tick lets React actually commit and flush the
    // latestDraftRef-updating effect before that delayed response resolves.
    await vi.waitFor(() => expect(sessionCreateCalls).toBe(1));
    rerender(
      <AIDesignStudio roomDraftId="draft_1" draft={{ ...draft, revision: 6 }} selection={{ kind: "object", id: "object_sofa_1" }} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );
    await new Promise((resolve) => setTimeout(resolve, 0));

    await vi.waitFor(() => expect(sessionCreateCalls).toBe(2), { timeout: 3000 });
    // No third request — one retry only, and no visible error since the
    // retry succeeded.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(sessionCreateCalls).toBe(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("does not retry when the current revision still matches the attempted (failed) revision", async () => {
    mockAuth();
    let sessionCreateCalls = 0;
    server.use(http.post(`${baseUrl}/spatial/design-sessions`, () => {
      sessionCreateCalls++;
      return conflictError();
    }));

    renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={{ kind: "object", id: "object_sofa_1" }} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );

    await vi.waitFor(() => expect(sessionCreateCalls).toBe(1));
    await new Promise((resolve) => setTimeout(resolve, 100));
    // draft never changed — nothing proves the failed attempt was stale,
    // so no retry.
    expect(sessionCreateCalls).toBe(1);
  });

  it("retries once, and a second request-conflict on the retry itself does not trigger a third request", async () => {
    mockAuth();
    let sessionCreateCalls = 0;
    server.use(http.post(`${baseUrl}/spatial/design-sessions`, async ({ request }) => {
      sessionCreateCalls++;
      const body = (await request.json()) as { expectedRoomDraftRevision: number };
      if (body.expectedRoomDraftRevision === 5) {
        await new Promise((resolve) => setTimeout(resolve, 200));
      }
      return conflictError();
    }));

    const { rerender } = renderWithProviders(
      <AIDesignStudio roomDraftId="draft_1" draft={draft} selection={{ kind: "object", id: "object_sofa_1" }} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );

    await vi.waitFor(() => expect(sessionCreateCalls).toBe(1));
    rerender(
      <AIDesignStudio roomDraftId="draft_1" draft={{ ...draft, revision: 6 }} selection={{ kind: "object", id: "object_sofa_1" }} onSubmitEdit={vi.fn()} editingDisabled={false} />,
    );
    await new Promise((resolve) => setTimeout(resolve, 0));
    await vi.waitFor(() => expect(sessionCreateCalls).toBe(2), { timeout: 3000 });

    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(sessionCreateCalls).toBe(2);
  });
});
