import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import {
  acknowledgeUnitMismatch,
  getSourceDiscrepancy,
  listIssuedVersionsOrEmpty,
  resolveSourceDiscrepancy,
  reviewMaterialRequirement,
} from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const requirement = {
  id: "req-1",
  projectId: "proj-1",
  materialId: "mat-1",
  materialName: "Cement",
  catalogUnit: "bag",
  requiredQuantity: { value: "35", unit: "bag" },
  status: "reviewed",
  sourceType: "manual",
  sourceSyncState: "clean",
  unitMismatch: false,
  unitMismatchAcknowledged: false,
  revision: 4,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("procurement api wrappers", () => {
  it("reviewMaterialRequirement posts the expectedRevision to /review", async () => {
    let capturedBody: unknown = null;
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/review`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json({ ...requirement, revision: 4 });
      })
    );
    await reviewMaterialRequirement("req-1", 3);
    expect(capturedUrl).toContain("/material-requirements/req-1/review");
    expect(capturedBody).toEqual({ expectedRevision: 3 });
  });

  it("acknowledgeUnitMismatch posts the expectedRevision to /acknowledge-unit", async () => {
    let capturedBody: unknown = null;
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/acknowledge-unit`, async ({ request }) => {
        capturedUrl = request.url;
        capturedBody = await request.json();
        return HttpResponse.json(requirement);
      })
    );
    await acknowledgeUnitMismatch("req-1", 3);
    expect(capturedUrl).toContain("/material-requirements/req-1/acknowledge-unit");
    expect(capturedBody).toEqual({ expectedRevision: 3 });
  });

  it("getSourceDiscrepancy GETs the discrepancy for the requirement", async () => {
    let capturedUrl = "";
    server.use(
      http.get(`${baseUrl}/material-requirements/:id/source-discrepancy`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({
          requirementRevision: 4,
          syncState: "change_detected",
          anchorStatus: "reviewed",
          accepted: { quantity: { value: "33", unit: "bag" }, costItemIds: ["c1", "c2"], fingerprint: "fp-old" },
          proposed: { quantity: { value: "41", unit: "bag" }, costItemIds: ["c1", "c2", "c3"], fingerprint: "fp-new" },
          availableActions: ["update", "merge", "keep_current", "create_separate"],
        });
      })
    );
    const result = await getSourceDiscrepancy("req-1");
    expect(capturedUrl).toContain("/material-requirements/req-1/source-discrepancy");
    expect(result.proposed.fingerprint).toBe("fp-new");
    expect(result.availableActions).toEqual(["update", "merge", "keep_current", "create_separate"]);
  });

  it("resolveSourceDiscrepancy posts the exact generated field names", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(requirement);
      })
    );
    await resolveSourceDiscrepancy("req-1", {
      action: "keep_current",
      expectedRevision: 4,
      expectedProposedFingerprint: "fp-new",
    });
    expect(capturedBody).toEqual({
      action: "keep_current",
      expectedRevision: 4,
      expectedProposedFingerprint: "fp-new",
    });
  });

  it("resolveSourceDiscrepancy includes resolutionOperationId only when provided", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json(requirement);
      })
    );
    await resolveSourceDiscrepancy("req-1", {
      action: "create_separate",
      expectedRevision: 4,
      expectedProposedFingerprint: "fp-new",
      resolutionOperationId: "op-1",
    });
    expect(capturedBody).toMatchObject({ resolutionOperationId: "op-1" });
  });

  it("listIssuedVersionsOrEmpty returns the versions array on 200", async () => {
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () => HttpResponse.json({ versions: [{ id: "v1" }] }))
    );
    const result = await listIssuedVersionsOrEmpty("chain-1");
    expect(result).toHaveLength(1);
  });

  it("listIssuedVersionsOrEmpty normalizes a 404 to an empty array", async () => {
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      )
    );
    const result = await listIssuedVersionsOrEmpty("chain-1");
    expect(result).toEqual([]);
  });

  it("listIssuedVersionsOrEmpty still throws on a non-404 failure", async () => {
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 })
      )
    );
    await expect(listIssuedVersionsOrEmpty("chain-1")).rejects.toMatchObject({ status: 500 });
  });
});
