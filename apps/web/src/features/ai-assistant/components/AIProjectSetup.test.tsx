import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { AIProjectSetup } from "./AIProjectSetup";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("AIProjectSetup", () => {
  it("shows the current brief summary and setup progress from real state", async () => {
    server.use(
      http.get(`${baseUrl}/projects/:id`, () =>
        HttpResponse.json({ id: "p1", clientId: "c1", name: "Ahmad Residence", status: "lead", scopeBrief: "Full renovation of a condo.", createdAt: "2026-08-16T00:00:00Z" })
      ),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 20, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 20, total: 0 }))
    );
    renderWithProviders(<AIProjectSetup projectId="p1" />);

    expect(await screen.findByText(/full renovation of a condo/i)).toBeInTheDocument();
  });

  it("shows a locked Work Items stage when no Space is confirmed yet", async () => {
    server.use(
      http.get(`${baseUrl}/projects/:id`, () =>
        HttpResponse.json({ id: "p1", clientId: "c1", name: "x", status: "lead", scopeBrief: "brief", createdAt: "2026-08-16T00:00:00Z" })
      ),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 20, total: 0 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 20, total: 0 }))
    );
    renderWithProviders(<AIProjectSetup projectId="p1" />);

    expect(await screen.findByText(/review spaces/i)).toBeInTheDocument();
  });

  it("shows a compact completion summary once every stage has a completed batch and nothing pending", async () => {
    const completedBatch = (type: string, id: string) => ({
      id, projectId: "p1", type, status: "completed", promptVersion: "v1", schemaVersion: 1,
      inputFingerprint: "fp", sourceBrief: "Full renovation of a condo.",
      startedAt: "2026-09-01T00:00:00Z", completedAt: "2026-09-01T00:05:00Z",
    });
    server.use(
      http.get(`${baseUrl}/projects/:id`, () =>
        HttpResponse.json({ id: "p1", clientId: "c1", name: "x", status: "lead", scopeBrief: "Full renovation of a condo.", createdAt: "2026-08-16T00:00:00Z" })
      ),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [{ id: "s1" }], page: 1, pageSize: 20, total: 1 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [{ id: "w1" }], page: 1, pageSize: 20, total: 1 })),
      http.get(`${baseUrl}/work-resource-requirements`, () => HttpResponse.json({ requirements: [{ id: "r1" }] })),
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, ({ request }) => {
        const url = new URL(request.url);
        const type = url.searchParams.get("type");
        return HttpResponse.json({ batches: [completedBatch(type ?? "space_suggestions", `b-${type}`)] });
      }),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () => HttpResponse.json({ suggestions: [] }))
    );
    renderWithProviders(<AIProjectSetup projectId="p1" />);

    expect(await screen.findByText(/setup complete/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /re-run ai setup/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /view setup history/i })).toBeInTheDocument();
  });

  it("clicking Re-run AI setup with an unchanged brief shows the confirmation dialog", async () => {
    const completedBatch = (type: string, id: string) => ({
      id, projectId: "p1", type, status: "completed", promptVersion: "v1", schemaVersion: 1,
      inputFingerprint: "fp", sourceBrief: "Full renovation of a condo.",
      startedAt: "2026-09-01T00:00:00Z", completedAt: "2026-09-01T00:05:00Z",
    });
    server.use(
      http.get(`${baseUrl}/projects/:id`, () =>
        HttpResponse.json({ id: "p1", clientId: "c1", name: "x", status: "lead", scopeBrief: "Full renovation of a condo.", createdAt: "2026-08-16T00:00:00Z" })
      ),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [{ id: "s1" }], page: 1, pageSize: 20, total: 1 })),
      http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [{ id: "w1" }], page: 1, pageSize: 20, total: 1 })),
      http.get(`${baseUrl}/work-resource-requirements`, () => HttpResponse.json({ requirements: [{ id: "r1" }] })),
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, ({ request }) => {
        const url = new URL(request.url);
        const type = url.searchParams.get("type");
        return HttpResponse.json({ batches: [completedBatch(type ?? "space_suggestions", `b-${type}`)] });
      }),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () => HttpResponse.json({ suggestions: [] }))
    );
    const user = userEvent.setup();
    renderWithProviders(<AIProjectSetup projectId="p1" />);

    await screen.findByText(/setup complete/i);
    await user.click(screen.getByRole("button", { name: /re-run ai setup/i }));

    expect(await screen.findByText(/re-run ai setup\?/i)).toBeInTheDocument();
    expect(screen.getByText(/brief has not changed/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^cancel$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /re-run and compare/i })).toBeInTheDocument();
  });
});
