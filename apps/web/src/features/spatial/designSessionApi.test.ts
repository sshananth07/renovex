import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import {
  createSpatialDesignSession,
  createSpatialDesignTurn,
  getSpatialDesignSession,
  listSpatialDesignTurns,
} from "./designSessionApi";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const sessionDTO = {
  id: "session_1",
  roomDraftId: "roomdraft_1",
  basedOnRoomDraftRevision: 17,
  target: { kind: "object", id: "object_sofa_123" },
  status: "active",
  currentWorkingDesign: { resolvedSpatialOperations: [] },
  revision: 0,
  createdAt: "2026-09-09T00:00:00Z",
  updatedAt: "2026-09-09T00:00:00Z",
};

const turnDTO = {
  id: "turn_1",
  sessionId: "session_1",
  sequence: 1,
  status: "proposed",
  instruction: "Actually make it beige.",
  basedOnRoomDraftRevision: 17,
  createdAt: "2026-09-09T00:00:00Z",
};

describe("spatial design session api", () => {
  it("createSpatialDesignSession POSTs exact body and returns the session", async () => {
    let capturedBody: unknown;
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json({ session: sessionDTO });
      }),
    );

    const result = await createSpatialDesignSession({
      clientSessionId: "client-session-uuid",
      roomDraftId: "roomdraft_1",
      expectedRoomDraftRevision: 17,
      target: { kind: "object", id: "object_sofa_123" },
    });

    expect(capturedUrl).toContain("/spatial/design-sessions");
    expect(capturedBody).toEqual({
      clientSessionId: "client-session-uuid",
      roomDraftId: "roomdraft_1",
      expectedRoomDraftRevision: 17,
      target: { kind: "object", id: "object_sofa_123" },
    });
    expect(result.session.id).toBe("session_1");
  });

  it("createSpatialDesignTurn POSTs to the session's turns endpoint with only clientRequestId/instruction", async () => {
    let capturedUrl = "";
    let capturedBody: unknown;
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:id/turns`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json(turnDTO);
      }),
    );

    const result = await createSpatialDesignTurn("session_1", {
      clientRequestId: "client-request-uuid",
      instruction: "Actually make it beige.",
    });

    expect(capturedUrl).toContain("/spatial/design-sessions/session_1/turns");
    expect(capturedBody).toEqual({
      clientRequestId: "client-request-uuid",
      instruction: "Actually make it beige.",
    });
    // No provider/company/geometry snapshot fields on the request body.
    expect(capturedBody).not.toHaveProperty("companyId");
    expect(capturedBody).not.toHaveProperty("provider");
    expect(capturedBody).not.toHaveProperty("fingerprint");
    expect(result.id).toBe("turn_1");
  });

  it("getSpatialDesignSession GETs the session by id (never POST)", async () => {
    let method = "";
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:id`, ({ request }) => {
        method = request.method;
        return HttpResponse.json({ session: sessionDTO, stale: false, currentRoomDraftRevision: 17 });
      }),
      http.post(`${baseUrl}/spatial/design-sessions/:id`, () => {
        throw new Error("must never POST to the session GET route");
      }),
    );

    const result = await getSpatialDesignSession("session_1");

    expect(method).toBe("GET");
    expect(result.stale).toBe(false);
    expect(result.session.id).toBe("session_1");
  });

  it("listSpatialDesignTurns GETs turns with query params (never POST)", async () => {
    let method = "";
    let capturedUrl = "";
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:id/turns`, ({ request }) => {
        method = request.method;
        capturedUrl = request.url;
        return HttpResponse.json([turnDTO]);
      }),
    );

    const result = await listSpatialDesignTurns("session_1", { limit: 10 });

    expect(method).toBe("GET");
    expect(capturedUrl).toContain("limit=10");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("turn_1");
  });

  it("throws a normalized error on a 409 design_turn_in_progress conflict", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:id/turns`, () =>
        HttpResponse.json({ status: 409, detail: "in progress", type: "design_turn_in_progress" }, { status: 409 }),
      ),
    );

    await expect(
      createSpatialDesignTurn("session_1", { clientRequestId: "x", instruction: "y" }),
    ).rejects.toMatchObject({ status: 409 });
  });
});
