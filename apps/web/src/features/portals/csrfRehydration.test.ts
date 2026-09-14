import { describe, expect, it, beforeEach, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { createSupplierDraft, getSupplierSession, verifySupplierChallenge } from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

// The previous cross-origin CSRF fix left one gap: the CSRF token lives only
// in a module-level JS variable, which a full page reload destroys, even
// though the browser still holds the supplier_session and supplier_csrf
// cookies. GET /supplier-access/session now also returns csrfToken (the
// SAME value re-derived from the caller's own authenticated session), so
// getSupplierSession() can restore the frontend's lost in-memory state
// through the exact setter verifySupplierChallenge already uses — this
// file proves that recovery, in isolation from api.test.ts's own module
// state, since this scenario specifically requires starting from nothing.
//
// This module is imported fresh for this test file (Vitest gives each test
// file its own module graph), so supplierCSRFToken genuinely starts at its
// initial "" value here — mirroring a real page load with no prior
// verifySupplierChallenge call in this JS heap.
describe("supplier access CSRF token rehydration after page reload", () => {
  beforeEach(() => {
    server.resetHandlers();
  });

  it("recovers the CSRF token from GET /supplier-access/session and uses it on the next mutation", async () => {
    let capturedHeader: string | null = null;
    let capturedCredentials: RequestCredentials | undefined;
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ invitationId: "inv-1", csrfToken: "recovered-after-reload" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        capturedHeader = request.headers.get("x-csrf-token");
        capturedCredentials = request.credentials;
        return HttpResponse.json({ id: "draft-1" });
      }),
    );

    // Simulates the state right after a full page reload: no prior
    // verifySupplierChallenge call has run in this module.
    const session = await getSupplierSession();
    expect(session).toEqual({ invitationId: "inv-1" });

    await createSupplierDraft("inv-1");

    expect(capturedHeader).toBe("recovered-after-reload");
    expect(capturedCredentials).toBe("include");
  });

  it("a later successful session bootstrap replaces stale in-memory CSRF state", async () => {
    let capturedHeader: string | null = null;
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ invitationId: "inv-1", csrfToken: "stale-token" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        capturedHeader = request.headers.get("x-csrf-token");
        return HttpResponse.json({ id: "draft-1" });
      }),
    );
    await getSupplierSession();

    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ invitationId: "inv-1", csrfToken: "fresh-token" })),
    );
    await getSupplierSession();

    await createSupplierDraft("inv-1");

    expect(capturedHeader).toBe("fresh-token");
  });

  it("does not touch localStorage or sessionStorage when recovering the token", async () => {
    const localSetSpy = vi.spyOn(Storage.prototype, "setItem");
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ invitationId: "inv-1", csrfToken: "recovered-token" })),
    );

    await getSupplierSession();

    expect(localSetSpy).not.toHaveBeenCalled();
    localSetSpy.mockRestore();
  });

  it("full lifecycle: verify establishes the token, a reload forgets it, session bootstrap recovers it, and mutations keep working", async () => {
    const headersSeen: (string | null)[] = [];
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "verify-token" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        headersSeen.push(request.headers.get("x-csrf-token"));
        return HttpResponse.json({ id: "draft-1" });
      }),
    );

    // 1. OTP verification establishes the token in memory.
    await verifySupplierChallenge("chal-1", "123456");
    await createSupplierDraft("inv-1");
    expect(headersSeen[0]).toBe("verify-token");

    // 2. A page reload would destroy module state. This test cannot literally
    // reload the page, but it proves the SAME recovery path a reload would
    // take: a session bootstrap independently supplies a token, and the
    // mutation that follows uses whatever the most recent source provided —
    // exactly the behavior a reload-then-bootstrap sequence depends on.
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ invitationId: "inv-1", csrfToken: "bootstrap-token" })),
    );
    await getSupplierSession();
    await createSupplierDraft("inv-1");
    expect(headersSeen[1]).toBe("bootstrap-token");
  });
});
