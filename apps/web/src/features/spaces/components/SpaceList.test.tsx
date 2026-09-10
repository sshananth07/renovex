import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SpaceList } from "./SpaceList";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const currentParams = { page: 1, pageSize: 25 };

vi.mock("@/lib/url/listParams", () => ({
  useListSearchParams: () => ({ params: currentParams, setParams: vi.fn() }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

const spaces = [
  {
    id: "s1",
    projectId: "p1",
    name: "Kitchen",
    type: "Kitchen",
    description: "Main kitchen",
    createdAt: "2026-01-15T00:00:00Z",
  },
  {
    id: "s2",
    projectId: "p1",
    name: "Master Bedroom",
    type: "Bedroom",
    createdAt: "2026-01-16T00:00:00Z",
  },
];

function mockAuth() {
  server.use(http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })));
}

describe("SpaceList", () => {
  it("shows an empty state with a create CTA when there are no spaces", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<SpaceList projectId="p1" />);

    expect(await screen.findByText(/no spaces yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add space/i })).toBeInTheDocument();
  });

  it("renders each space's name, type, and description", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: spaces, page: 1, pageSize: 25, total: 2 })
      )
    );
    renderWithProviders(<SpaceList projectId="p1" />);

    expect(await screen.findAllByText("Kitchen")).toHaveLength(2); // card title + type badge
    expect(screen.getByText("Main kitchen")).toBeInTheDocument();
    expect(screen.getByText("Master Bedroom")).toBeInTheDocument();
  });

  it("creates a space through the dialog form, scoped to the current project", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })
      )
    );
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/spaces`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(spaces[0]);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceList projectId="p1" />);

    // fireEvent, not userEvent, for this click: base-ui's Button primitive
    // pairs with userEvent's pointer simulation unreliably in jsdom when the
    // button is the empty state's sole CTA (plain DOM .click() works too).
    fireEvent.click(await screen.findByRole("button", { name: /add space/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/name/i), "Kitchen");
    await user.click(within(dialog).getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ projectId: "p1", name: "Kitchen" });
    });
  });
});
