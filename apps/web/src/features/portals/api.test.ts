import { describe, expect, it, beforeEach } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { createSupplierDraft, verifySupplierChallenge } from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

// Renovex's Web and API run on two different Vercel origins, so a cookie the
// API sets (supplier_csrf) is invisible to document.cookie on Web's page —
// a hard same-origin rule. The verify-challenge response therefore also
// returns the CSRF token in its JSON body; the frontend must hold it in
// memory and echo it as X-CSRF-Token on every supplier mutation instead of
// reading document.cookie.
describe("supplier access CSRF token handling", () => {
  beforeEach(() => {
    server.resetHandlers();
  });

  it("captures the csrfToken returned by a successful OTP verification", async () => {
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "server-issued-token-abc" })),
    );

    const result = await verifySupplierChallenge("chal-1", "123456");

    expect(result).toMatchObject({ status: "verified", csrfToken: "server-issued-token-abc" });
  });

  it("sends the token captured at verification as X-CSRF-Token on a subsequent supplier mutation", async () => {
    let capturedHeader: string | null = null;
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "in-memory-token-xyz" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        capturedHeader = request.headers.get("x-csrf-token");
        return HttpResponse.json({ id: "draft-1" });
      }),
    );

    await verifySupplierChallenge("chal-1", "123456");
    await createSupplierDraft("inv-1");

    expect(capturedHeader).toBe("in-memory-token-xyz");
  });

  it("createSupplierDraft sends the request with credentials so supplier_session and supplier_csrf cookies are attached", async () => {
    let capturedCredentials: RequestCredentials | undefined;
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "token-1" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        capturedCredentials = request.credentials;
        return HttpResponse.json({ id: "draft-1" });
      }),
    );

    await verifySupplierChallenge("chal-1", "123456");
    await createSupplierDraft("inv-1");

    expect(capturedCredentials).toBe("include");
  });

  it("uses the most recently captured token if verification runs again with a new value", async () => {
    let capturedHeader: string | null = null;
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "first-token" })),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, ({ request }) => {
        capturedHeader = request.headers.get("x-csrf-token");
        return HttpResponse.json({ id: "draft-1" });
      }),
    );
    await verifySupplierChallenge("chal-1", "123456");

    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ status: "verified", csrfToken: "second-token" })),
    );
    await verifySupplierChallenge("chal-2", "654321");

    await createSupplierDraft("inv-1");

    expect(capturedHeader).toBe("second-token");
  });
});
