import { afterEach, describe, expect, it, vi } from "vitest";
import { apiClient, setAccessToken } from "./client";
import { resetSingleFlightRefreshForTests } from "@/features/auth/singleFlightRefresh";

describe("apiClient", () => {
  afterEach(() => {
    setAccessToken(null);
    resetSingleFlightRefreshForTests();
    vi.unstubAllGlobals();
  });

  it("omits Authorization when no token is set", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));

    await apiClient.GET("/auth/me" as never, { fetch: fetchMock } as never);

    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.headers.has("authorization")).toBe(false);
  });

  it("includes Authorization: Bearer <token> once a token is set", async () => {
    setAccessToken("test-token-123");
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));

    await apiClient.GET("/auth/me" as never, { fetch: fetchMock } as never);

    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.headers.get("authorization")).toBe("Bearer test-token-123");
  });

  it("always sets credentials: include", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));

    await apiClient.GET("/auth/me" as never, { fetch: fetchMock } as never);

    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.credentials).toBe("include");
  });

  it("coalesces concurrent business-request 401s into one refresh and retries both with the fresh token", async () => {
    setAccessToken("expired-access-token");
    const attempts = new Map<string, number>();
    let refreshes = 0;
    const retryAuthorizations: string[] = [];

    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = input instanceof Request ? input : new Request(new URL(String(input), "http://localhost"));
      const url = new URL(request.url);
      if (url.pathname === "/auth/refresh") {
        refreshes += 1;
        await Promise.resolve();
        return Response.json({ accessToken: "fresh-access-token" });
      }

      const count = (attempts.get(url.pathname) ?? 0) + 1;
      attempts.set(url.pathname, count);
      if (count === 1) {
        return Response.json({ title: "Unauthorized", status: 401 }, { status: 401 });
      }

      retryAuthorizations.push(request.headers.get("authorization") ?? "");
      return Response.json({ items: [], page: 1, pageSize: 25, total: 0 });
    }));

    const [clients, projects] = await Promise.all([
      apiClient.GET("/clients", { params: { query: { page: 1, pageSize: 25 } } }),
      apiClient.GET("/projects", { params: { query: { page: 1, pageSize: 25 } } }),
    ]);

    expect(clients.data).toEqual({ items: [], page: 1, pageSize: 25, total: 0 });
    expect(projects.data).toEqual({ items: [], page: 1, pageSize: 25, total: 0 });
    expect(refreshes).toBe(1);
    expect(attempts).toEqual(new Map([["/clients", 2], ["/projects", 2]]));
    expect(retryAuthorizations).toEqual(["Bearer fresh-access-token", "Bearer fresh-access-token"]);
  });
});
