import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectList } from "./ProjectList";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

const setParamsMock = vi.fn();
vi.mock("@/lib/url/listParams", () => ({
  useListSearchParams: () => ({
    params: { page: 1, pageSize: 25 },
    setParams: setParamsMock,
  }),
}));

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/clients`, () =>
      HttpResponse.json({
        items: [{ id: "c1", name: "Ahmad Residence", createdAt: "2026-01-01T00:00:00Z" }],
        page: 1,
        pageSize: 25,
        total: 1,
      })
    ),
    http.get(`${baseUrl}/projects`, () =>
      HttpResponse.json({
        items: [
          {
            id: "proj1",
            clientId: "c1",
            name: "Ahmad Residence Renovation",
            status: "lead",
            createdAt: "2026-01-15T00:00:00Z",
          },
        ],
        page: 1,
        pageSize: 25,
        total: 1,
      })
    )
  );
}

describe("ProjectList", () => {
  it("renders project rows including a status badge", async () => {
    mockBase();
    renderWithProviders(<ProjectList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence Renovation")).toBeInTheDocument();
    });
    expect(screen.getByText("Lead")).toBeInTheDocument();
  });

  it("opens a create dialog listing available clients", async () => {
    mockBase();
    const user = userEvent.setup();
    renderWithProviders(<ProjectList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence Renovation")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /add project/i }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("combobox", { name: /client/i })).toBeInTheDocument();
  });
});
