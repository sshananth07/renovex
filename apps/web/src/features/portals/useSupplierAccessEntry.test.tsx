import { renderHook, waitFor } from "@testing-library/react";
import { act } from "react";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { useSupplierAccessEntry } from "./useSupplierAccessEntry";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("useSupplierAccessEntry", () => {
  it("goes straight to session-established when there is no token and a session already exists", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () => HttpResponse.json({ invitationId: "inv-1" })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry(undefined));

    await waitFor(() => expect(result.current.state.status).toBe("session-established"));
  });

  it("reports an error when there is no token and no session exists", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ type: "about:blank", status: 401, title: "Unauthorized" }, { status: 401 })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry(undefined));

    await waitFor(() => expect(result.current.state.status).toBe("error"));
  });

  it("exchanges a token and requests a challenge, landing in verification-required", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
      http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry("test-token"));

    await waitFor(() => expect(result.current.state.status).toBe("verification-required"));
    expect(result.current.state).toMatchObject({ status: "verification-required", challengeId: "chal-1" });
  });

  it("reports an error when the token exchange fails", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/open`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry("bad-token"));

    await waitFor(() => expect(result.current.state.status).toBe("error"));
  });

  it("verifyCode moves state to session-established on success", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
      http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" })),
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => HttpResponse.json({ ok: true })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry("test-token"));
    await waitFor(() => expect(result.current.state.status).toBe("verification-required"));

    await act(async () => {
      await result.current.verifyCode("123456");
    });

    expect(result.current.state.status).toBe("session-established");
  });

  it("verifyCode sets verifyError and stays in verification-required on failure", async () => {
    server.use(
      http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
      http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" })),
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () =>
        HttpResponse.json({ detail: "invalid code" }, { status: 422 })),
    );

    const { result } = renderHook(() => useSupplierAccessEntry("test-token"));
    await waitFor(() => expect(result.current.state.status).toBe("verification-required"));

    await act(async () => {
      await result.current.verifyCode("000000");
    });

    expect(result.current.state.status).toBe("verification-required");
    expect(result.current.verifyError).toMatch(/invalid, expired, or no longer current/i);
  });
});
