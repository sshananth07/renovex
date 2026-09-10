import { describe, expect, it } from "vitest";
import type { components } from "@/lib/api/generated/schema";
import { deriveRequirementUIState, type RequirementUIContext } from "./requirementPresentation";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

const draftSelectedRfq: RequirementUIContext["selectedRfq"] = {
  id: "rfq-1",
  number: "RFQ-000001",
  status: "draft",
  revision: 2,
};

const readySelectedRfq: RequirementUIContext["selectedRfq"] = {
  id: "rfq-2",
  number: "RFQ-000002",
  status: "ready",
  revision: 5,
};

function makeRequirement(overrides: Partial<MaterialRequirement> = {}): MaterialRequirement {
  return {
    id: "req-1",
    projectId: "proj-1",
    materialId: "mat-1",
    materialName: "Cement",
    catalogUnit: "bag",
    requiredQuantity: { value: "35", unit: "bag" },
    status: "draft",
    sourceType: "manual",
    sourceSyncState: "clean",
    unitMismatch: false,
    unitMismatchAcknowledged: false,
    revision: 3,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("deriveRequirementUIState", () => {
  it("splitState=creating resolves to split-incomplete, even though status=split", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "split", splitState: "creating" }),
      {}
    );
    expect(state.kind).toBe("split-incomplete");
    expect(state.primaryAction).toBeNull();
    expect(state.blockers).toEqual(expect.arrayContaining(["terminal", "split-incomplete"]));
  });

  it("claimed + change_detected shows badge, not a resolve action", () => {
    const state = deriveRequirementUIState(
      makeRequirement({
        status: "reviewed",
        activeRfqChainId: "chain-1",
        activeRfqNumber: "RFQ-000003",
        sourceSyncState: "change_detected",
      }),
      {}
    );
    expect(state.kind).toBe("claimed");
    expect(state.primaryAction).toEqual({ type: "view-rfq", rfqChainId: "chain-1", rfqNumber: "RFQ-000003", isSelectedRfq: false });
    expect(state.badges.some((b) => b.type === "source-changed")).toBe(true);
    expect(state.blockers).toEqual(["claimed"]);
  });

  it("claimed badge label comes from activeRfqNumber, never context.selectedRfq.number", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "reviewed", activeRfqChainId: "chain-1", activeRfqNumber: "RFQ-000003" }),
      { selectedRfq: draftSelectedRfq }
    );
    const claimedBadge = state.badges.find((b) => b.type === "claimed");
    expect(claimedBadge?.label).toContain("RFQ-000003");
    expect(claimedBadge?.label).not.toContain(draftSelectedRfq!.number);
  });

  it("archived + clean has no action", () => {
    const state = deriveRequirementUIState(makeRequirement({ status: "archived" }), {});
    expect(state.kind).toBe("archived");
    expect(state.primaryAction).toBeNull();
    expect(state.blockers).toEqual(["terminal"]);
  });

  it("split + clean offers view-split-children", () => {
    const state = deriveRequirementUIState(makeRequirement({ status: "split" }), {});
    expect(state.kind).toBe("split-source");
    expect(state.primaryAction).toEqual({ type: "view-split-children", requirementId: "req-1" });
    expect(state.blockers).toEqual(["terminal"]);
  });

  it("archived + change_detected resolves to source-discrepancy with terminal blocker, not archived", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "archived", sourceSyncState: "change_detected" }),
      {}
    );
    expect(state.kind).toBe("source-discrepancy");
    expect(state.primaryAction).toEqual({ type: "resolve-discrepancy", requirementId: "req-1" });
    expect(state.blockers).toEqual(expect.arrayContaining(["source-change-unresolved", "terminal"]));
  });

  it("split + change_detected resolves to source-discrepancy with terminal blocker, not split-source", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "split", sourceSyncState: "change_detected" }),
      {}
    );
    expect(state.kind).toBe("source-discrepancy");
    expect(state.primaryAction).toEqual({ type: "resolve-discrepancy", requirementId: "req-1" });
    expect(state.blockers).toEqual(expect.arrayContaining(["source-change-unresolved", "terminal"]));
  });

  it("draft + change_detected resolves to source-discrepancy", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "draft", sourceSyncState: "change_detected" }),
      {}
    );
    expect(state.kind).toBe("source-discrepancy");
    expect(state.badges.some((b) => b.type === "source-changed")).toBe(true);
  });

  it("reviewed + source_removed shows the source-removed badge", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "reviewed", sourceSyncState: "source_removed" }),
      {}
    );
    expect(state.kind).toBe("source-discrepancy");
    expect(state.badges.some((b) => b.type === "source-removed")).toBe(true);
    expect(state.badges.some((b) => b.type === "source-changed")).toBe(false);
  });

  it("unresolved unit mismatch resolves to unit-mismatch", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ unitMismatch: true, unitMismatchAcknowledged: false }),
      {}
    );
    expect(state.kind).toBe("unit-mismatch");
    expect(state.primaryAction).toEqual({ type: "acknowledge-unit", requirementId: "req-1", expectedRevision: 3 });
    expect(state.blockers).toEqual(["unit-mismatch-unresolved"]);
  });

  it("draft + clean resolves to review", () => {
    const state = deriveRequirementUIState(makeRequirement({ status: "draft" }), {});
    expect(state.kind).toBe("draft");
    expect(state.primaryAction).toEqual({ type: "review", requirementId: "req-1", expectedRevision: 3 });
    expect(state.blockers).toEqual(["not-reviewed"]);
  });

  it("reviewed + clean + selected draft RFQ resolves to add-to-rfq", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "reviewed" }),
      { selectedRfq: draftSelectedRfq }
    );
    expect(state.kind).toBe("rfq-eligible");
    expect(state.primaryAction).toEqual({ type: "add-to-rfq", requirementId: "req-1", expectedRequirementRevision: 3 });
  });

  it("reviewed + clean + no selected RFQ has no action and a selection message", () => {
    const state = deriveRequirementUIState(makeRequirement({ status: "reviewed" }), {});
    expect(state.kind).toBe("rfq-eligible");
    expect(state.primaryAction).toBeNull();
    expect(state.message).toMatch(/select a draft rfq/i);
  });

  it("reviewed + clean + selected ready (non-draft) RFQ has no action and a selection message", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "reviewed" }),
      { selectedRfq: readySelectedRfq }
    );
    expect(state.kind).toBe("rfq-eligible");
    expect(state.primaryAction).toBeNull();
    expect(state.message).toMatch(/select a draft rfq/i);
  });

  it("falls back to unavailable for an unrecognized status", () => {
    const state = deriveRequirementUIState(
      makeRequirement({ status: "something-unexpected" }),
      {}
    );
    expect(state.kind).toBe("unavailable");
    expect(state.primaryAction).toBeNull();
  });
});
