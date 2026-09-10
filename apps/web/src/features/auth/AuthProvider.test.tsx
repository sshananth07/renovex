import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { useAuth } from "./useAuth";
import { getAccessToken, setAccessToken } from "@/lib/api/client";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function Probe() {
  const auth = useAuth();
  return (
    <div>
      <div data-testid="status">{auth.status}</div>
      <div data-testid="email">{auth.user?.email ?? ""}</div>
    </div>
  );
}

describe("AuthProvider", () => {
  afterEach(() => {
    setAccessToken(null);
  });

  it("becomes authenticated after refresh and /auth/me both succeed", async () => {
    server.use(
      http.post(`${baseUrl}/auth/refresh`, () =>
        HttpResponse.json({ accessToken: "fresh-token", mustChangePassword: false })
      ),
      http.get(`${baseUrl}/auth/me`, () =>
        HttpResponse.json({
          userId: "u1",
          email: "owner@example.com",
          companyId: "c1",
          companyName: "Acme Renovations",
          role: "owner",
          mustChangePassword: false,
        })
      )
    );

    renderWithProviders(<Probe />);

    expect(screen.getByTestId("status").textContent).toBe("initializing");

    await waitFor(() => {
      expect(screen.getByTestId("status").textContent).toBe("authenticated");
    });
    expect(screen.getByTestId("email").textContent).toBe("owner@example.com");
    expect(getAccessToken()).toBe("fresh-token");
  });

  it("becomes unauthenticated without redirecting when refresh fails", async () => {
    server.use(
      http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 }))
    );

    renderWithProviders(<Probe />);

    await waitFor(() => {
      expect(screen.getByTestId("status").textContent).toBe("unauthenticated");
    });
  });

  it("never registers /auth/me as a TanStack Query cache key", async () => {
    server.use(
      http.post(`${baseUrl}/auth/refresh`, () =>
        HttpResponse.json({ accessToken: "fresh-token", mustChangePassword: false })
      ),
      http.get(`${baseUrl}/auth/me`, () =>
        HttpResponse.json({
          userId: "u1",
          email: "owner@example.com",
          companyId: "c1",
          companyName: "Acme Renovations",
          role: "owner",
          mustChangePassword: false,
        })
      )
    );

    const { queryClient } = renderWithProviders(<Probe />);

    await waitFor(() => {
      expect(screen.getByTestId("status").textContent).toBe("authenticated");
    });

    const cacheKeys = queryClient.getQueryCache().getAll().map((q) => q.queryKey);
    const hasMeKey = cacheKeys.some((key) =>
      key.some((segment) => typeof segment === "string" && segment.includes("me"))
    );
    expect(hasMeKey).toBe(false);
  });
});
