import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { WorkItemList } from "./WorkItemList";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const currentParams = { page: 1, pageSize: 25 };

vi.mock("@/lib/url/listParams", () => ({
  useListSearchParams: () => ({ params: currentParams, setParams: vi.fn() }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

const spaces = [{ id: "s1", projectId: "p1", name: "Kitchen", createdAt: "2026-01-15T00:00:00Z" }];

const workItems = [
  {
    id: "w1",
    projectId: "p1",
    spaceId: "s1",
    description: "Install cabinets",
    workType: "Carpentry",
    quantityValue: "12.5",
    quantityUnit: "sqft",
    status: "planned",
    source: "manual",
    verificationStatus: "confirmed",
    createdAt: "2026-01-15T00:00:00Z",
  },
  {
    id: "w2",
    projectId: "p1",
    description: "Demolish old flooring",
    quantityValue: "1",
    quantityUnit: "lot",
    status: "cancelled",
    source: "manual",
    verificationStatus: "confirmed",
    createdAt: "2026-01-16T00:00:00Z",
  },
];

function mockAuth() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: spaces, page: 1, pageSize: 25, total: 1 }))
  );
}

describe("WorkItemList", () => {
  it("shows an empty state with a create CTA when there are no work items", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/work-items`, () =>
        HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })
      )
    );
    renderWithProviders(<WorkItemList projectId="p1" />);

    expect(await screen.findByText(/no work items yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add work item/i })).toBeInTheDocument();
  });

  it("renders description, space name, quantity+unit, and status for each row", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/work-items`, () =>
        HttpResponse.json({ items: workItems, page: 1, pageSize: 25, total: 2 })
      )
    );
    renderWithProviders(<WorkItemList projectId="p1" />);

    expect(await screen.findByText("Install cabinets")).toBeInTheDocument();
    expect(screen.getByText("Kitchen")).toBeInTheDocument();
    expect(screen.getByText("12.5 sqft")).toBeInTheDocument();
    expect(screen.getByText("Demolish old flooring")).toBeInTheDocument();
    expect(screen.getByText("Cancelled")).toBeInTheDocument();
  });

  it("creates a work item through the drawer form, scoped to the current project", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/work-items`, () =>
        HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })
      )
    );
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/work-items`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(workItems[0]);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<WorkItemList projectId="p1" />);

    const addButton = await screen.findByRole("button", { name: /add work item/i });
    addButton.click();
    const drawer = await screen.findByRole("dialog");
    await user.type(within(drawer).getByLabelText(/description/i), "Install cabinets");
    await user.type(within(drawer).getByLabelText(/quantity/i), "12.5");
    await user.type(within(drawer).getByLabelText(/unit/i), "sqft");
    await user.click(within(drawer).getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ projectId: "p1", description: "Install cabinets" });
    });
  });

  it("cancels a planned work item and disables further edits once cancelled", async () => {
    mockAuth();
    let cancelled = false;
    server.use(
      http.get(`${baseUrl}/work-items`, () =>
        HttpResponse.json({
          items: [{ ...workItems[0], status: cancelled ? "cancelled" : "planned" }],
          page: 1,
          pageSize: 25,
          total: 1,
        })
      )
    );
    server.use(
      http.patch(`${baseUrl}/work-items/:id/status`, () => {
        cancelled = true;
        return HttpResponse.json({ ...workItems[0], status: "cancelled" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<WorkItemList projectId="p1" />);

    await screen.findByText("Install cabinets");
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    await user.click(screen.getByRole("button", { name: /confirm/i }));

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: /cancel/i })).not.toBeInTheDocument();
    });
  });

  it("shows a conflict message when editing an already-cancelled work item fails with 409", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/work-items`, () =>
        HttpResponse.json({ items: [workItems[1]], page: 1, pageSize: 25, total: 1 })
      )
    );
    server.use(
      http.patch(`${baseUrl}/work-items/:workItemId`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            title: "Conflict",
            status: 409,
            detail: "cancelled work items cannot change status",
          },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<WorkItemList projectId="p1" />);

    await screen.findByText("Demolish old flooring");
    await user.click(screen.getByRole("row", { name: /demolish old flooring/i }));
    const drawer = await screen.findByRole("dialog");
    await user.click(within(drawer).getByRole("button", { name: /save/i }));

    expect(await within(drawer).findByText(/cancelled work items cannot change status/i)).toBeInTheDocument();
  });
});
