import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";
import { ensureFreshToken } from "@/features/auth/singleFlightRefresh";
import { getAccessToken } from "./accessToken";

export { getAccessToken, setAccessToken } from "./accessToken";

// The single typed HTTP client every feature's api.ts calls through. Per
// the session contract: calls the Go API directly (NEXT_PUBLIC_API_BASE_URL),
// always sends credentials so the httpOnly refresh cookie travels with
// requests, and injects the bearer token from the in-memory holder below
// (never localStorage/sessionStorage). 401-driven refresh coordination is
// layered on top in features/auth (client.ts itself owns no refresh logic).
const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
const retryableRequests = new Map<string, Request>();

export const apiClient = createClient<paths>({
  baseUrl,
  credentials: "include",
  // Resolve globalThis.fetch per call, not at client-construction time.
  // openapi-fetch defaults to `fetch: globalThis.fetch` as a parameter
  // default, which captures a snapshot at module-eval time — if that
  // happens before a test's fetch-mocking library (e.g. MSW) patches
  // globalThis.fetch, every request silently bypasses the mock.
  fetch: (request) => globalThis.fetch(request),
});

apiClient.use({
  onRequest({ id, request, schemaPath }) {
    const token = getAccessToken();
    if (token) {
      request.headers.set("Authorization", `Bearer ${token}`);
    }
    if (!schemaPath.startsWith("/auth/")) {
      retryableRequests.set(id, request.clone());
    }
    return request;
  },
  async onResponse({ id, response, schemaPath }) {
    const retryableRequest = retryableRequests.get(id);
    retryableRequests.delete(id);
    if (response.status !== 401 || schemaPath.startsWith("/auth/") || !retryableRequest) {
      return response;
    }

    const freshToken = await ensureFreshToken();
    const headers = new Headers(retryableRequest.headers);
    headers.set("Authorization", `Bearer ${freshToken}`);
    return globalThis.fetch(new Request(retryableRequest, { headers }));
  },
  onError({ id }) {
    retryableRequests.delete(id);
  },
});
