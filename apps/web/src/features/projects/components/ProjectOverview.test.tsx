import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectOverview } from "./ProjectOverview";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const project = {
  id: "proj1",
  clientId: "c1",
  name: "Ahmad Residence Renovation",
  status: "in_progress",
  createdAt: "2026-01-15T00:00:00Z",
};

const client = {
  id: "c1",
  name: "Ahmad Residence",
  createdAt: "2026-01-01T00:00:00Z",
};

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/projects/:id`, () => HttpResponse.json(project)),
    http.get(`${baseUrl}/clients/:id`, () => HttpResponse.json(client)),
    http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [] })),
    http.get(`${baseUrl}/estimates`, () => HttpResponse.json({ estimates: [] })),
    http.get(`${baseUrl}/quotations`, () => HttpResponse.json({ quotations: [] })),
    http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs: [] }))
  );
}

describe("ProjectOverview", () => {
  it("shows project identity, status, and client association", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence Renovation")).toBeInTheDocument();
    });
    expect(screen.getAllByText("In Progress")).toHaveLength(2);
    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    });
  });

  it("uses the authoritative project status label in both summary locations", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/projects/:id`, () => HttpResponse.json({ ...project, status: "lead" })),
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    await waitFor(() => expect(screen.getAllByText("Lead")).toHaveLength(2));
    expect(screen.queryByText("Archived")).not.toBeInTheDocument();
  });

  it("changes status only after explicit confirmation and refreshes status-bearing queries", async () => {
    const user = userEvent.setup();
    let persistedProject = { ...project, status: "lead" };
    let patchCount = 0;
    server.use(
      http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
      http.get(`${baseUrl}/projects/:id`, () => HttpResponse.json(persistedProject)),
      http.get(`${baseUrl}/clients/:id`, () => HttpResponse.json(client)),
      http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [] })),
      http.get(`${baseUrl}/estimates`, () => HttpResponse.json({ estimates: [] })),
      http.get(`${baseUrl}/quotations`, () => HttpResponse.json({ quotations: [] })),
      http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs: [] })),
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.patch(`${baseUrl}/projects/:id/status`, async ({ request }) => {
        patchCount += 1;
        expect(await request.json()).toEqual({ status: "completed" });
        persistedProject = { ...persistedProject, status: "completed" };
        return HttpResponse.json(persistedProject);
      })
    );
    const { queryClient } = renderWithProviders(<ProjectOverview projectId="proj1" />);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await user.click(await screen.findByRole("button", { name: /change project status, current status lead/i }));
    expect(screen.getByRole("heading", { name: "Change project status" })).toBeInTheDocument();
    expect(screen.getByText("Changing the lifecycle status does not alter project setup progress.")).toBeInTheDocument();

    const updateButton = screen.getByRole("button", { name: "Update status" });
    expect(updateButton).toBeDisabled();
    await user.click(screen.getByRole("combobox", { name: "New status" }));
    expect(await screen.findAllByRole("option")).toHaveLength(8);
    await user.click(screen.getByRole("option", { name: "Completed" }));
    expect(patchCount).toBe(0);
    expect(updateButton).toBeEnabled();

    await user.click(updateButton);
    await waitFor(() => expect(patchCount).toBe(1));
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Change project status" })).not.toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole("button", { name: /current status completed/i })).toBeInTheDocument());
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["projects"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["clients"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["dashboard"] });
    expect(screen.getByText("0 of 3 complete")).toBeInTheDocument();
  });

  it("keeps the dialog open and shows the backend rejection message", async () => {
    const user = userEvent.setup();
    mockBase();
    server.use(
      http.get(`${baseUrl}/projects/:id`, () => HttpResponse.json({ ...project, status: "lead" })),
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.patch(`${baseUrl}/projects/:id/status`, () => HttpResponse.json(
        { title: "Unprocessable Entity", status: 422, detail: "Status transition rejected by policy." },
        { status: 422 }
      ))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /change project status, current status lead/i }));
    await user.click(screen.getByRole("combobox", { name: "New status" }));
    await user.click(await screen.findByRole("option", { name: "Closed" }));
    await user.click(screen.getByRole("button", { name: "Update status" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Status transition rejected by policy.");
    expect(screen.getByRole("heading", { name: "Change project status" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "New status" })).toHaveTextContent("Closed");
  });

  it("shows all setup steps incomplete when no Property, Spaces, or Work Items exist", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByText("0 of 3 complete")).toBeInTheDocument();
    });
  });

  it("counts every RFQ on the project even when none have been issued yet", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({
        rfqs: [
          { id: "rfq1", projectId: "proj1", rfqNumber: "RFQ-000001", status: "ready", revision: 1, lines: [] },
          { id: "rfq2", projectId: "proj1", rfqNumber: "RFQ-000002", status: "draft", revision: 1, lines: [] },
        ],
      })),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    expect(await screen.findByText(/2 RFQs/i)).toBeInTheDocument();
  });

  it("derives setup progress from real fetched resources, not a stored flag", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/properties`, () =>
        HttpResponse.json({
          properties: [{ id: "prop1", projectId: "proj1", address: "1 Jalan Ampang", createdAt: "2026-01-16T00:00:00Z" }],
        })
      ),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 }))
    );
    renderWithProviders(<ProjectOverview projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByText("1 of 3 complete")).toBeInTheDocument();
    });
  });
});
