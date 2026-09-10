import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ResourceSuggestionReview } from "./ResourceSuggestionReview";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const batch = {
  id: "batch1", projectId: "p1", type: "resource_suggestions", status: "completed",
  promptVersion: "resources-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z",
};

function mockWorkItems(count: number) {
  server.use(
    http.get(`${baseUrl}/work-items`, () =>
      HttpResponse.json({
        items: Array.from({ length: count }, (_, i) => ({
          id: `wi${i + 1}`,
          projectId: "p1",
          description: `Work Item ${i + 1}`,
          workType: "tile_installation",
          quantityValue: "10",
          quantityUnit: "m2",
        })),
        total: count,
        page: 1,
        pageSize: 100,
      })
    ),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [] }))
  );
}

describe("ResourceSuggestionReview", () => {
  it("gates generation until at least one confirmed Work Item exists", async () => {
    mockWorkItems(0);
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);
    expect(await screen.findByText(/at least one confirmed work item/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /suggest resources/i })).not.toBeInTheDocument();
  });

  it("generates and groups suggestions by Work Item", async () => {
    mockWorkItems(1);
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "material_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tile Adhesive", candidateMaterialId: null } },
            { id: "s2", batchId: "batch1", type: "trade_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tiler" } },
          ],
        })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    expect(await screen.findByText("Work Item 1")).toBeInTheDocument();
    expect(await screen.findByText("Tile Adhesive")).toBeInTheDocument();
    expect(await screen.findByText("Tiler")).toBeInTheDocument();
  });

  it("collapses multiple resolved Resource suggestions within a work item group into one summary row", async () => {
    mockWorkItems(1);
    server.use(
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, () => HttpResponse.json({ batches: [batch] })),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            { id: "s1", batchId: "batch1", type: "trade_resource", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "wrr_1", suggestedData: { workItemId: "wi1", name: "Tiler" } },
            { id: "s2", batchId: "batch1", type: "equipment_resource", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "wrr_2", suggestedData: { workItemId: "wi1", name: "Tile Cutter" } },
            { id: "s3", batchId: "batch1", type: "material_resource", status: "rejected", revision: 2, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Porcelain Tile" } },
          ],
        })
      )
    );
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    expect(await screen.findByText(/2 resources added, 1 rejected/i)).toBeInTheDocument();
    expect(screen.queryByText("Tiler")).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /show details/i }));
    expect(await screen.findByText("Tiler")).toBeInTheDocument();
    expect(screen.getByText("Tile Cutter")).toBeInTheDocument();
  });

  it("accepting a trade suggestion invalidates and shows Added to project", async () => {
    mockWorkItems(1);
    let accepted = false;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s2", batchId: "batch1", type: "trade_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tiler" } },
          ],
        })
      ),
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () => {
        accepted = true;
        return HttpResponse.json({ domainObjectId: "wrr_1" });
      }),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            accepted
              ? { id: "s2", batchId: "batch1", type: "trade_resource", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "wrr_1", suggestedData: { workItemId: "wi1", name: "Tiler" } }
              : { id: "s2", batchId: "batch1", type: "trade_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tiler" } },
          ],
        })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    await screen.findByText("Tiler");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    await waitFor(() => expect(accepted).toBe(true));
    expect(await screen.findByText(/added to project/i)).toBeInTheDocument();
  });

  it("shows a stale-suggestion explanation when accept returns 409 ai-stale-suggestion", async () => {
    mockWorkItems(1);
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s2", batchId: "batch1", type: "trade_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tiler" } },
          ],
        })
      ),
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () =>
        HttpResponse.json(
          { type: "urn:renovex:problem:ai-stale-suggestion", status: 409, title: "Conflict", detail: "suggestion was already reviewed or has changed" },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    await screen.findByText("Tiler");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    expect(await screen.findByText(/older project state/i)).toBeInTheDocument();
  });

  it("shows a safe error with a Try Again action when the provider fails, and retrying recovers", async () => {
    mockWorkItems(1);
    let attempt = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () => {
        attempt += 1;
        if (attempt === 1) {
          return HttpResponse.json({ status: 503, detail: "AI suggestions are temporarily unavailable. Your existing project data has not been changed." }, { status: 503 });
        }
        return HttpResponse.json({
          batch,
          suggestions: [
            { id: "s2", batchId: "batch1", type: "trade_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tiler" } },
          ],
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    expect(await screen.findByText(/temporarily unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/pydantic|fastapi|traceback/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("Tiler")).toBeInTheDocument();
    expect(screen.queryByText(/temporarily unavailable/i)).not.toBeInTheDocument();
  });

  it("sends material.mode=create (matching the backend's MaterialAcceptanceMode) when using Create & Add", async () => {
    mockWorkItems(1);
    let acceptedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "material_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tile Spacers", candidateMaterialId: null } },
          ],
        })
      ),
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, async ({ request }) => {
        acceptedBody = await request.json();
        return HttpResponse.json({ domainObjectId: "wrr_1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    await screen.findByText("Tile Spacers");
    await user.click(screen.getByRole("button", { name: /create & add/i }));
    await user.type(screen.getByLabelText(/unit/i), "bag");
    await user.type(screen.getByLabelText(/reference price/i), "8.50");
    await user.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(acceptedBody).not.toBeNull());
    expect(acceptedBody).toMatchObject({ material: { mode: "create" } });
  });

  it("sends material.mode=existing (matching the backend's MaterialAcceptanceMode) when using an existing Material", async () => {
    mockWorkItems(1);
    let acceptedBody: unknown = null;
    server.use(
      http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [{ id: "material_1", name: "Cement" }] })),
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "material_resource", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { workItemId: "wi1", name: "Tile Adhesive", candidateMaterialId: "material_1" } },
          ],
        })
      ),
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, async ({ request }) => {
        acceptedBody = await request.json();
        return HttpResponse.json({ domainObjectId: "wrr_1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourceSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /suggest resources/i }));
    await screen.findByText("Tile Adhesive");
    await user.click(screen.getByRole("button", { name: /use existing/i }));

    await waitFor(() => expect(acceptedBody).not.toBeNull());
    expect(acceptedBody).toMatchObject({ material: { mode: "existing", materialId: "material_1" } });
  });
});
