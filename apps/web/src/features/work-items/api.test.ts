import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { createWorkItem, updateWorkItem, updateWorkItemStatus } from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const workItem = {
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
};

describe("work-items api", () => {
  it("createWorkItem posts the full create body", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/work-items`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(workItem);
      })
    );
    await createWorkItem({
      projectId: "p1",
      description: "Install cabinets",
      quantityValue: "12.5",
      quantityUnit: "sqft",
      spaceId: "s1",
      workType: "Carpentry",
    });
    expect(capturedBody).toEqual({
      projectId: "p1",
      description: "Install cabinets",
      quantityValue: "12.5",
      quantityUnit: "sqft",
      spaceId: "s1",
      workType: "Carpentry",
    });
  });

  it("updateWorkItem sends only the fields provided, omitting the rest (genuine partial PATCH)", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/work-items/:workItemId`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(workItem);
      })
    );
    await updateWorkItem("w1", { description: "Install upper cabinets" });
    expect(capturedBody).toEqual({ description: "Install upper cabinets" });
  });

  it("updateWorkItem sends an explicit null spaceId to clear the assignment", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/work-items/:workItemId`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(workItem);
      })
    );
    await updateWorkItem("w1", { spaceId: null });
    expect(capturedBody).toEqual({ spaceId: null });
  });

  it("updateWorkItem omits spaceId entirely when not provided, leaving assignment unchanged", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/work-items/:workItemId`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(workItem);
      })
    );
    await updateWorkItem("w1", { description: "Same space" });
    expect(capturedBody).toEqual({ description: "Same space" });
    expect(capturedBody).not.toHaveProperty("spaceId");
  });

  it("updateWorkItemStatus hits the separate /status endpoint", async () => {
    let capturedBody: unknown = null;
    let capturedUrl = "";
    server.use(
      http.patch(`${baseUrl}/work-items/:id/status`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json({ ...workItem, status: "cancelled" });
      })
    );
    await updateWorkItemStatus("w1", "cancelled");
    expect(capturedBody).toEqual({ status: "cancelled" });
    expect(capturedUrl).toContain("/work-items/w1/status");
  });

  it("updateWorkItem surfaces a 409 error when the work item is cancelled", async () => {
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
    await expect(updateWorkItem("w1", { description: "x" })).rejects.toMatchObject({ status: 409 });
  });
});
