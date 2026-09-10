import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { AppShell } from "./AppShell";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => "/dashboard",
}));

function authenticate() {
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
}

describe("AppShell", () => {
  it("exposes the global nav items via the primary navigation landmark", async () => {
    authenticate();
    renderWithProviders(
      <AppShell>
        <div>page body</div>
      </AppShell>
    );

    await waitFor(() => {
      expect(screen.getByText("page body")).toBeInTheDocument();
    });

    const nav = screen.getByRole("navigation");
    expect(nav).toHaveTextContent("Dashboard");
    expect(nav).toHaveTextContent("Clients");
    expect(nav).toHaveTextContent("Projects");
  });

  it("opens and closes the mobile navigation drawer via its trigger", async () => {
    authenticate();
    const user = userEvent.setup();
    renderWithProviders(
      <AppShell>
        <div>page body</div>
      </AppShell>
    );

    await waitFor(() => {
      expect(screen.getByText("page body")).toBeInTheDocument();
    });

    const trigger = screen.getByRole("button", { name: /open menu/i });
    await user.click(trigger);

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Clients");

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("displays the current company name from the authenticated user", async () => {
    authenticate();
    renderWithProviders(
      <AppShell>
        <div>page body</div>
      </AppShell>
    );

    await waitFor(() => {
      expect(screen.getByText("Acme Renovations")).toBeInTheDocument();
    });
  });
});
