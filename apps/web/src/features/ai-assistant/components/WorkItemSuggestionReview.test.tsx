import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { WorkItemSuggestionReview } from "./WorkItemSuggestionReview";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("WorkItemSuggestionReview", () => {
  it("locks generation with an explanation when there are no confirmed Spaces", async () => {
    server.use(http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [], page: 1, pageSize: 20, total: 0 })));
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    expect(await screen.findByText(/at least one.*space/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /generate work items/i })).not.toBeInTheDocument();
  });

  it("enables generation once at least one real Space exists (manually created counts too)", async () => {
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 })
      )
    );
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    expect(await screen.findByRole("button", { name: /generate work items/i })).toBeEnabled();
  });

  it("collapses multiple resolved Work Item suggestions within a group into one summary row", async () => {
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 })
      ),
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, () =>
        HttpResponse.json({
          batches: [{ id: "b1", projectId: "p1", type: "work_item_suggestions", status: "completed", promptVersion: "work-items-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z" }],
        })
      ),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            { id: "s1", batchId: "b1", type: "work_item", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "wi_1", suggestedData: { description: "Replace flooring", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" } },
            { id: "s2", batchId: "b1", type: "work_item", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "wi_2", suggestedData: { description: "Patch walls", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "supporting_scope" } },
            { id: "s3", batchId: "b1", type: "work_item", status: "rejected", revision: 2, createdAt: "2026-08-16T00:00:00Z", suggestedData: { description: "Install spa", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "possible_missing_scope" } },
          ],
        })
      )
    );
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    expect(await screen.findByText(/2 work items added, 1 rejected/i)).toBeInTheDocument();
    expect(screen.queryByText("Replace flooring")).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /show details/i }));
    expect(await screen.findByText("Replace flooring")).toBeInTheDocument();
    expect(screen.getByText("Patch walls")).toBeInTheDocument();
  });

  it("groups suggestions by Space and shows a Project-wide section for null spaceId", async () => {
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 })
      ),
      http.post(`${baseUrl}/projects/:projectId/ai/work-item-suggestions`, () =>
        HttpResponse.json({
          batch: { id: "b1", projectId: "p1", type: "work_item_suggestions", status: "completed", promptVersion: "work-items-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z" },
          suggestions: [
            { id: "s1", batchId: "b1", type: "work_item", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { description: "Replace flooring", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" } },
            { id: "s2", batchId: "b1", type: "work_item", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { description: "Site protection", scopeLevel: "project", spaceId: null, scopeOrigin: "supporting_scope" } },
          ],
        })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /generate work items/i }));

    expect(await screen.findByText("Replace flooring")).toBeInTheDocument();
    expect(screen.getAllByText("Kitchen").length).toBeGreaterThan(0);
    expect(screen.getByText(/project-wide/i)).toBeInTheDocument();
    expect(screen.getByText("Site protection")).toBeInTheDocument();
  });

  it("shows a stale-suggestion explanation when accept returns 409 ai-stale-suggestion", async () => {
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 })
      ),
      http.post(`${baseUrl}/projects/:projectId/ai/work-item-suggestions`, () =>
        HttpResponse.json({
          batch: { id: "b1", projectId: "p1", type: "work_item_suggestions", status: "completed", promptVersion: "work-items-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z" },
          suggestions: [
            { id: "s1", batchId: "b1", type: "work_item", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { description: "Replace flooring", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" } },
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
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /generate work items/i }));
    await screen.findByText("Replace flooring");
    await user.type(screen.getByLabelText(/quantity/i), "30");
    await user.type(screen.getByLabelText(/unit/i), "m2");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    expect(await screen.findByText(/older project state/i)).toBeInTheDocument();
  });

  it("shows a safe error with a Try Again action when the provider fails, and retrying recovers", async () => {
    server.use(
      http.get(`${baseUrl}/spaces`, () =>
        HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 })
      )
    );
    let attempt = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/work-item-suggestions`, () => {
        attempt += 1;
        if (attempt === 1) {
          return HttpResponse.json({ status: 503, detail: "AI suggestions are temporarily unavailable. Your existing project data has not been changed." }, { status: 503 });
        }
        return HttpResponse.json({
          batch: { id: "b1", projectId: "p1", type: "work_item_suggestions", status: "completed", promptVersion: "work-items-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z" },
          suggestions: [
            { id: "s1", batchId: "b1", type: "work_item", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { description: "Replace flooring", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" } },
          ],
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<WorkItemSuggestionReview projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: /generate work items/i }));
    expect(await screen.findByText(/temporarily unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/pydantic|fastapi|traceback/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("Replace flooring")).toBeInTheDocument();
    expect(screen.queryByText(/temporarily unavailable/i)).not.toBeInTheDocument();
  });
});
