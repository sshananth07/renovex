import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SpaceSuggestionReview } from "./SpaceSuggestionReview";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const batch = {
  id: "batch1", projectId: "p1", type: "space_suggestions", status: "completed",
  promptVersion: "spaces-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z",
};

describe("SpaceSuggestionReview", () => {
  it("shows a Suggest Spaces button and a loading state while generating", async () => {
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 30));
        return HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
          ],
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await user.click(screen.getByRole("button", { name: /suggest spaces/i }));
    expect(await screen.findByText(/generating/i)).toBeInTheDocument();
    expect(await screen.findByText("Kitchen")).toBeInTheDocument();
  });

  it("shows a safe error and does not mutate project data when the provider fails", async () => {
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () =>
        HttpResponse.json({ status: 503, detail: "AI suggestions are temporarily unavailable. Your existing project data has not been changed." }, { status: 503 })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await user.click(screen.getByRole("button", { name: /suggest spaces/i }));
    expect(await screen.findByText(/temporarily unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/pydantic|fastapi|traceback/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /try again/i })).toBeVisible();
  });

  it("retries generation when Try Again is clicked after a provider failure", async () => {
    let attempt = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () => {
        attempt += 1;
        if (attempt === 1) {
          return HttpResponse.json({ status: 503, detail: "AI suggestions are temporarily unavailable. Your existing project data has not been changed." }, { status: 503 });
        }
        return HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
          ],
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await user.click(screen.getByRole("button", { name: /suggest spaces/i }));
    await screen.findByText(/temporarily unavailable/i);

    await user.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("Kitchen")).toBeInTheDocument();
    expect(screen.queryByText(/temporarily unavailable/i)).not.toBeInTheDocument();
  });

  it("accepting a suggestion invalidates and the card shows Added to project", async () => {
    let acceptCalled = false;
    let accepted = false;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
          ],
        })
      ),
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () => {
        acceptCalled = true;
        accepted = true;
        return HttpResponse.json({ domainObjectId: "space_1" });
      }),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            accepted
              ? { id: "s1", batchId: "batch1", type: "space", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "space_1", suggestedData: { name: "Kitchen", spaceType: "kitchen" } }
              : { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
          ],
        })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await user.click(screen.getByRole("button", { name: /suggest spaces/i }));
    await screen.findByText("Kitchen");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    await waitFor(() => expect(acceptCalled).toBe(true));
    expect(await screen.findByText(/added to project/i)).toBeInTheDocument();
  });

  it("shows a stale-suggestion explanation when accept returns 409 ai-stale-suggestion", async () => {
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () =>
        HttpResponse.json({
          batch,
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
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
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await user.click(screen.getByRole("button", { name: /suggest spaces/i }));
    await screen.findByText("Kitchen");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    expect(await screen.findByText(/older project state/i)).toBeInTheDocument();
  });

  it("shows the latest completed batch's suggestions on mount without requiring a click", async () => {
    server.use(
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, () =>
        HttpResponse.json({ batches: [batch] })
      ),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
          ],
        })
      )
    );
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    expect(await screen.findByText("Kitchen")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /generate fresh suggestions/i })).toBeInTheDocument();
  });

  it("collapses multiple resolved suggestions into one summary row instead of repeating cards", async () => {
    server.use(
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, () => HttpResponse.json({ batches: [batch] })),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () =>
        HttpResponse.json({
          suggestions: [
            { id: "s1", batchId: "batch1", type: "space", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "space_1", suggestedData: { name: "Kitchen", spaceType: "kitchen" } },
            { id: "s2", batchId: "batch1", type: "space", status: "accepted", revision: 2, createdAt: "2026-08-16T00:00:00Z", acceptedDomainObjectId: "space_2", suggestedData: { name: "Bathroom", spaceType: "bathroom" } },
            { id: "s3", batchId: "batch1", type: "space", status: "rejected", revision: 2, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Study", spaceType: "study" } },
          ],
        })
      )
    );
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    expect(await screen.findByText(/2 spaces added, 1 rejected/i)).toBeInTheDocument();
    expect(screen.queryByText("Kitchen")).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /show details/i }));
    expect(await screen.findByText("Kitchen")).toBeInTheDocument();
    expect(screen.getByText("Bathroom")).toBeInTheDocument();
  });

  it("Generate fresh suggestions creates a new batch without deleting the old one from view", async () => {
    server.use(
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, () =>
        HttpResponse.json({ batches: [batch] })
      ),
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, ({ params }) =>
        HttpResponse.json({
          suggestions:
            params.batchId === "batch1"
              ? [{ id: "s1", batchId: "batch1", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:00:00Z", suggestedData: { name: "Kitchen", spaceType: "kitchen" } }]
              : [{ id: "s2", batchId: "batch2", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:01:00Z", suggestedData: { name: "Bathroom", spaceType: "bathroom" } }],
        })
      ),
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () =>
        HttpResponse.json({
          batch: { ...batch, id: "batch2" },
          suggestions: [{ id: "s2", batchId: "batch2", type: "space", status: "pending", revision: 1, createdAt: "2026-08-16T00:01:00Z", suggestedData: { name: "Bathroom", spaceType: "bathroom" } }],
        })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<SpaceSuggestionReview projectId="p1" />);

    await screen.findByText("Kitchen");
    await user.click(screen.getByRole("button", { name: /generate fresh suggestions/i }));

    expect(await screen.findByText("Bathroom")).toBeInTheDocument();
  });
});
