import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { AuthGate } from "./AuthGate";
import { setAccessToken } from "@/lib/api/client";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const replaceMock = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock, push: vi.fn() }),
}));

describe("AuthGate", () => {
  afterEach(() => {
    setAccessToken(null);
    replaceMock.mockClear();
  });

  it("shows a loading state while initializing", () => {
    server.use(http.post(`${baseUrl}/auth/refresh`, () => new Promise(() => {})));

    renderWithProviders(
      <AuthGate>
        <div>protected content</div>
      </AuthGate>
    );

    expect(screen.queryByText("protected content")).toBeNull();
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it("renders protected children once authenticated", async () => {
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

    renderWithProviders(
      <AuthGate>
        <div>protected content</div>
      </AuthGate>
    );

    await waitFor(() => {
      expect(screen.getByText("protected content")).toBeInTheDocument();
    });
    expect(replaceMock).not.toHaveBeenCalled();
  });

  it("redirects via router.replace (not push) when unauthenticated", async () => {
    server.use(
      http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 }))
    );

    renderWithProviders(
      <AuthGate>
        <div>protected content</div>
      </AuthGate>
    );

    await waitFor(() => {
      expect(replaceMock).toHaveBeenCalledWith(expect.stringContaining("/login"));
    });
    expect(screen.queryByText("protected content")).toBeNull();
  });
});
