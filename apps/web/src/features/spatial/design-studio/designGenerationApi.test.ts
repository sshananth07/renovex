import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import {
  cancelDesignGenerationAttempt,
  confirmDesignPlan,
  getDesignGenerationAttempt,
  listDesignGenerationAttempts,
  regenerateDesignPlan,
  useDesignPlan,
} from "./designGenerationApi";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const attemptDTO = {
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
};

const acceptanceDTO = {
  id: "acceptance_1",
  sessionId: "session_1",
  turnId: "turn_1",
  attemptId: "attempt_1",
  roomDraftId: "roomdraft_1",
  baseRoomDraftRevision: 17,
  resultingRoomDraftRevision: 18,
  appliedOperationIds: ["use_req_1:0"],
  createdAt: "2026-09-09T00:00:00Z",
};

describe("design generation api", () => {
  it("confirmDesignPlan POSTs to the turn's confirm endpoint with only the documented fields", async () => {
    let capturedUrl = "";
    let capturedBody: unknown;
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json(attemptDTO);
      }),
    );

    const result = await confirmDesignPlan("session_1", "turn_1", {
      clientRequestId: "confirm-uuid",
      planFingerprint: "sha256:abc",
      expectedRoomDraftRevision: 17,
    });

    expect(capturedUrl).toContain("/spatial/design-sessions/session_1/turns/turn_1/confirm");
    expect(capturedBody).toEqual({
      clientRequestId: "confirm-uuid",
      planFingerprint: "sha256:abc",
      expectedRoomDraftRevision: 17,
    });
    expect(capturedBody).not.toHaveProperty("companyId");
    expect(capturedBody).not.toHaveProperty("provider");
    expect(result.id).toBe("attempt_1");
    expect(result.status).toBe("concept_ready");
  });

  it("regenerateDesignPlan POSTs to the turn's regenerate endpoint", async () => {
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/regenerate`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(attemptDTO);
      }),
    );

    const result = await regenerateDesignPlan("session_1", "turn_1", {
      clientRequestId: "regen-uuid",
      planFingerprint: "sha256:abc",
      expectedRoomDraftRevision: 17,
    });

    expect(capturedUrl).toContain("/regenerate");
    expect(result.id).toBe("attempt_1");
  });

  it("listDesignGenerationAttempts GETs attempts with query params (never POST)", async () => {
    let method = "";
    let capturedUrl = "";
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts`, ({ request }) => {
        method = request.method;
        capturedUrl = request.url;
        return HttpResponse.json([attemptDTO]);
      }),
    );

    const result = await listDesignGenerationAttempts("session_1", { turnId: "turn_1", limit: 10 });

    expect(method).toBe("GET");
    expect(capturedUrl).toContain("turnId=turn_1");
    expect(capturedUrl).toContain("limit=10");
    expect(result).toHaveLength(1);
  });

  it("listDesignGenerationAttempts returns an empty array for a null response body", async () => {
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts`, () => HttpResponse.json(null)),
    );

    const result = await listDesignGenerationAttempts("session_1");
    expect(result).toEqual([]);
  });

  it("getDesignGenerationAttempt GETs one attempt (never POST)", async () => {
    let method = "";
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId`, ({ request }) => {
        method = request.method;
        return HttpResponse.json(attemptDTO);
      }),
    );

    const result = await getDesignGenerationAttempt("session_1", "attempt_1");
    expect(method).toBe("GET");
    expect(result.id).toBe("attempt_1");
  });

  it("cancelDesignGenerationAttempt POSTs only clientRequestId", async () => {
    let capturedBody: unknown;
    server.use(
      http.post(
        `${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/cancel`,
        async ({ request }) => {
          capturedBody = await request.json();
          return HttpResponse.json({ ...attemptDTO, status: "abandoned" });
        },
      ),
    );

    const result = await cancelDesignGenerationAttempt("session_1", "attempt_1", { clientRequestId: "cancel-uuid" });

    expect(capturedBody).toEqual({ clientRequestId: "cancel-uuid" });
    expect(result.status).toBe("abandoned");
  });

  it("useDesignPlan POSTs to the attempt's use endpoint and returns acceptance + roomDraft", async () => {
    let capturedUrl = "";
    let capturedBody: unknown;
    server.use(
      http.post(
        `${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/use`,
        async ({ request }) => {
          capturedUrl = request.url;
          capturedBody = await request.json();
          return HttpResponse.json({ acceptance: acceptanceDTO, roomDraft: { id: "roomdraft_1", revision: 18 } });
        },
      ),
    );

    const result = await useDesignPlan("session_1", "attempt_1", {
      clientRequestId: "use-uuid",
      planFingerprint: "sha256:abc",
      expectedRoomDraftRevision: 17,
    });

    expect(capturedUrl).toContain("/use");
    expect(capturedBody).toEqual({
      clientRequestId: "use-uuid",
      planFingerprint: "sha256:abc",
      expectedRoomDraftRevision: 17,
    });
    expect(result.acceptance.id).toBe("acceptance_1");
    expect(result.roomDraft.revision).toBe(18);
  });

  it("throws a normalized error on a 409 design_generation_in_progress conflict", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, () =>
        HttpResponse.json({ status: 409, detail: "in progress", type: "design_generation_in_progress" }, { status: 409 }),
      ),
    );

    await expect(
      confirmDesignPlan("session_1", "turn_1", {
        clientRequestId: "x",
        planFingerprint: "sha256:abc",
        expectedRoomDraftRevision: 17,
      }),
    ).rejects.toMatchObject({ status: 409 });
  });

  it("throws a normalized error on a 422 abandoned-attempt rejection from use", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/use`, () =>
        HttpResponse.json({ status: 422, detail: "attempt abandoned" }, { status: 422 }),
      ),
    );

    await expect(
      useDesignPlan("session_1", "attempt_1", {
        clientRequestId: "x",
        planFingerprint: "sha256:abc",
        expectedRoomDraftRevision: 17,
      }),
    ).rejects.toMatchObject({ status: 422 });
  });
});
