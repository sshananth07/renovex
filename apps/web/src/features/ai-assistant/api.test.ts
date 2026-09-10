import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import {
  acceptSuggestion,
  generateResourceSuggestions,
  generateSpaceSuggestions,
  generateWorkItemSuggestions,
  listBatches,
  listSuggestionsByBatch,
  listWorkResourceRequirements,
  rejectSuggestion,
  updateProjectScopeBrief,
} from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const batch = {
  id: "batch1",
  projectId: "p1",
  type: "space_suggestions",
  status: "completed",
  promptVersion: "spaces-v1",
  schemaVersion: 1,
  inputFingerprint: "fp",
  startedAt: "2026-08-16T00:00:00Z",
};

const suggestion = {
  id: "sug1",
  batchId: "batch1",
  type: "space",
  status: "pending",
  suggestedData: { name: "Kitchen", spaceType: "kitchen" },
  revision: 1,
  createdAt: "2026-08-16T00:00:00Z",
};

describe("ai-assistant api", () => {
  it("updateProjectScopeBrief PATCHes the scope-brief endpoint", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/projects/:projectId/scope-brief`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ id: "p1", clientId: "c1", name: "x", status: "lead", scopeBrief: "brief", createdAt: "2026-08-16T00:00:00Z" });
      })
    );
    await updateProjectScopeBrief("p1", "brief");
    expect(capturedBody).toEqual({ scopeBrief: "brief" });
  });

  it("generateSpaceSuggestions posts operationId to the space-suggestions endpoint", async () => {
    let capturedUrl = "";
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json({ batch, suggestions: [suggestion] });
      })
    );
    const result = await generateSpaceSuggestions("p1", "op1");
    expect(capturedUrl).toContain("/projects/p1/ai/space-suggestions");
    expect(capturedBody).toEqual({ operationId: "op1" });
    expect(result.suggestions).toHaveLength(1);
  });

  it("generateWorkItemSuggestions posts to the work-item-suggestions endpoint", async () => {
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/work-item-suggestions`, async ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({ batch: { ...batch, type: "work_item_suggestions" }, suggestions: [] });
      })
    );
    await generateWorkItemSuggestions("p1", "op2");
    expect(capturedUrl).toContain("/projects/p1/ai/work-item-suggestions");
  });

  it("generateResourceSuggestions posts to the resource-suggestions endpoint", async () => {
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/resource-suggestions`, async ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({ batch: { ...batch, type: "resource_suggestions" }, suggestions: [] });
      })
    );
    await generateResourceSuggestions("p1", "op3");
    expect(capturedUrl).toContain("/projects/p1/ai/resource-suggestions");
  });

  it("listBatches includes the type query param", async () => {
    let capturedUrl = "";
    server.use(
      http.get(`${baseUrl}/projects/:projectId/ai/batches`, async ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({ batches: [batch] });
      })
    );
    const result = await listBatches("p1", "space_suggestions");
    expect(capturedUrl).toContain("type=space_suggestions");
    expect(result.batches).toHaveLength(1);
  });

  it("listSuggestionsByBatch hits the ai-batches suggestions endpoint", async () => {
    server.use(
      http.get(`${baseUrl}/ai-batches/:batchId/suggestions`, () => HttpResponse.json({ suggestions: [suggestion] }))
    );
    const result = await listSuggestionsByBatch("batch1");
    expect(result.suggestions).toHaveLength(1);
  });

  it("acceptSuggestion posts expectedRevision and the space decision branch", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ domainObjectId: "space_1" });
      })
    );
    const result = await acceptSuggestion("sug1", {
      expectedRevision: 1,
      space: { name: "Kitchen", type: "kitchen", description: "" },
    });
    expect(capturedBody).toEqual({
      expectedRevision: 1,
      space: { name: "Kitchen", type: "kitchen", description: "" },
    });
    expect(result.domainObjectId).toBe("space_1");
  });

  it("rejectSuggestion posts expectedRevision only", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/reject`, async ({ request }) => {
        capturedBody = await request.json();
        return new HttpResponse(null, { status: 204 });
      })
    );
    await rejectSuggestion("sug1", 1);
    expect(capturedBody).toEqual({ expectedRevision: 1 });
  });

  it("acceptSuggestion surfaces a 409 idempotency/stale conflict", async () => {
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () =>
        HttpResponse.json(
          { type: "urn:renovex:problem:ai-stale-suggestion", title: "Conflict", status: 409, detail: "stale" },
          { status: 409 }
        )
      )
    );
    await expect(acceptSuggestion("sug1", { expectedRevision: 1 })).rejects.toMatchObject({ status: 409 });
  });

  it("listWorkResourceRequirements includes projectId and optional workItemId", async () => {
    let capturedUrl = "";
    server.use(
      http.get(`${baseUrl}/work-resource-requirements`, async ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({ requirements: [] });
      })
    );
    await listWorkResourceRequirements("p1", "w1");
    expect(capturedUrl).toContain("projectId=p1");
    expect(capturedUrl).toContain("workItemId=w1");
  });
});
