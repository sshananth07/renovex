import { describe, expect, it } from "vitest";
import { isSetupComplete, latestCompletedBatch } from "./aiSetupSummary";
import type { Batch } from "./api";

function batch(overrides: Partial<Batch>): Batch {
  return {
    id: "b1",
    projectId: "p1",
    type: "space_suggestions",
    status: "completed",
    promptVersion: "v1",
    schemaVersion: 1,
    inputFingerprint: "fp",
    startedAt: "2026-09-01T00:00:00Z",
    ...overrides,
  } as Batch;
}

describe("latestCompletedBatch", () => {
  it("returns the first completed batch (backend returns newest-first)", () => {
    const batches = [batch({ id: "b2", status: "completed" }), batch({ id: "b1", status: "failed" })];
    expect(latestCompletedBatch(batches)?.id).toBe("b2");
  });

  it("skips a newer failed/processing batch to find the latest completed one", () => {
    const batches = [
      batch({ id: "b3", status: "processing" }),
      batch({ id: "b2", status: "failed" }),
      batch({ id: "b1", status: "completed" }),
    ];
    expect(latestCompletedBatch(batches)?.id).toBe("b1");
  });

  it("returns undefined when there is no completed batch", () => {
    expect(latestCompletedBatch([batch({ status: "processing" })])).toBeUndefined();
  });

  it("returns undefined for an empty list", () => {
    expect(latestCompletedBatch([])).toBeUndefined();
  });
});

describe("isSetupComplete", () => {
  it("is false when any stage has no completed batch yet", () => {
    expect(
      isSetupComplete({
        spaceBatches: [],
        workItemBatches: [batch({ status: "completed" })],
        resourceBatches: [batch({ status: "completed" })],
        pendingCounts: { spaces: 0, workItems: 0, resources: 0 },
      })
    ).toBe(false);
  });

  it("is false when any stage still has pending suggestions", () => {
    expect(
      isSetupComplete({
        spaceBatches: [batch({ status: "completed" })],
        workItemBatches: [batch({ status: "completed" })],
        resourceBatches: [batch({ status: "completed" })],
        pendingCounts: { spaces: 2, workItems: 0, resources: 0 },
      })
    ).toBe(false);
  });

  it("is true when all three stages have a completed batch and zero pending suggestions", () => {
    expect(
      isSetupComplete({
        spaceBatches: [batch({ status: "completed" })],
        workItemBatches: [batch({ status: "completed" })],
        resourceBatches: [batch({ status: "completed" })],
        pendingCounts: { spaces: 0, workItems: 0, resources: 0 },
      })
    ).toBe(true);
  });
});
