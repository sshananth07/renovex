import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { TopBar } from "./TopBar";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

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
    ),
    http.post(`${baseUrl}/auth/logout`, () => new HttpResponse(null, { status: 200 }))
  );
}

describe("TopBar", () => {
  it("offers a logout action in the account menu that ends the session", async () => {
    authenticate();
    const user = userEvent.setup();
    renderWithProviders(<TopBar onOpenMenu={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText("Acme Renovations")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: "OW" }));
    const logoutItem = await screen.findByText(/log out/i);
    await user.click(logoutItem);

    await waitFor(() => {
      expect(screen.queryByText("Acme Renovations")).toBeNull();
    });
  });

  it("keeps the profile email readable in the account menu", async () => {
    authenticate();
    const user = userEvent.setup();
    renderWithProviders(<TopBar onOpenMenu={vi.fn()} />);

    await screen.findByText("Acme Renovations");
    await user.click(screen.getByRole("button", { name: "OW" }));

    const email = await screen.findByText("owner@example.com");
    expect(email).toHaveClass("break-all");

    const menu = email.closest('[data-slot="dropdown-menu-content"]');
    expect(menu).not.toBeNull();
    expect(menu).toHaveClass("w-64", "max-w-[calc(100vw-2rem)]");
  });
});
