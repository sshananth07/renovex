import { describe, expect, it } from "vitest";
import type { components } from "@/lib/api/generated/schema";
import { deriveRFQReadinessState } from "./rfqPresentation";

type RFQ = components["schemas"]["RfqDTO"];
type RFQLine = components["schemas"]["RfqLineDTO"];

function makeLine(overrides: Partial<RFQLine> = {}): RFQLine {
  return {
    id: "line-1",
    materialRequirementId: "req-1",
    materialName: "Cement",
    quantity: { value: "35", unit: "bag" },
    ...overrides,
  } as RFQLine;
}

function makeRFQ(overrides: Partial<RFQ> = {}): RFQ {
  return {
    id: "rfq-1",
    projectId: "proj-1",
    rfqNumber: "RFQ-000001",
    status: "draft",
    revision: 1,
    lines: [],
    deliveryAddress: "123 Main St",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("deriveRFQReadinessState", () => {
  it("blocks a non-draft RFQ", () => {
    const state = deriveRFQReadinessState(makeRFQ({ status: "ready" }));
    expect(state.canAttemptMarkReady).toBe(false);
    expect(state.blockers).toEqual(["not-draft"]);
  });

  it("blocks an empty draft RFQ with a scope message", () => {
    const state = deriveRFQReadinessState(makeRFQ({ lines: [] }));
    expect(state.canAttemptMarkReady).toBe(false);
    expect(state.blockers).toEqual(["no-scope"]);
    expect(state.message).toMatch(/add at least one reviewed material requirement/i);
  });

  it("blocks a draft RFQ with a line but a blank delivery address", () => {
    const state = deriveRFQReadinessState(makeRFQ({ lines: [makeLine()], deliveryAddress: "   " }));
    expect(state.canAttemptMarkReady).toBe(false);
    expect(state.blockers).toEqual(["missing-delivery-address"]);
    expect(state.message).toMatch(/delivery address/i);
  });

  it("allows attempting Mark ready when draft, has a line, and has an address", () => {
    const state = deriveRFQReadinessState(makeRFQ({ lines: [makeLine()] }));
    expect(state.canAttemptMarkReady).toBe(true);
    expect(state.blockers).toEqual([]);
  });
});
