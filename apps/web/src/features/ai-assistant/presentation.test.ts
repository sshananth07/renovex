import { describe, expect, it } from "vitest";
import {
  acceptRejectErrorMessage,
  confidenceLabel,
  groupResourceSuggestionsByWorkItem,
  groupWorkItemSuggestionsBySpace,
  scopeOriginLabel,
  type Suggestion,
} from "./presentation";

describe("confidenceLabel", () => {
  it("returns High for >= 0.80", () => {
    expect(confidenceLabel(0.8)).toBe("High");
    expect(confidenceLabel(0.95)).toBe("High");
  });
  it("returns Medium for >= 0.55 and < 0.80", () => {
    expect(confidenceLabel(0.55)).toBe("Medium");
    expect(confidenceLabel(0.79)).toBe("Medium");
  });
  it("returns Low for < 0.55", () => {
    expect(confidenceLabel(0.54)).toBe("Low");
    expect(confidenceLabel(0)).toBe("Low");
  });
  it("returns null for missing confidence, never a fabricated label", () => {
    expect(confidenceLabel(undefined)).toBeNull();
    expect(confidenceLabel(null)).toBeNull();
  });
});

describe("scopeOriginLabel", () => {
  it("maps explicit_scope to Requested scope", () => {
    expect(scopeOriginLabel("explicit_scope")).toBe("Requested scope");
  });
  it("maps supporting_scope to Supporting scope", () => {
    expect(scopeOriginLabel("supporting_scope")).toBe("Supporting scope");
  });
  it("maps possible_missing_scope to Possible missing scope, never a confirmed-omission phrase", () => {
    const label = scopeOriginLabel("possible_missing_scope");
    expect(label).toBe("Possible missing scope");
    expect(label.toLowerCase()).not.toContain("forgot");
    expect(label.toLowerCase()).not.toContain("confirmed");
  });
});

function makeWorkItemSuggestion(overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id: "s1",
    batchId: "b1",
    type: "work_item",
    status: "pending",
    revision: 1,
    createdAt: "2026-08-16T00:00:00Z",
    suggestedData: { description: "x", workType: "y", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" },
    ...overrides,
  };
}

describe("groupWorkItemSuggestionsBySpace", () => {
  it("groups space-scoped suggestions under their spaceId and project-level under null", () => {
    const suggestions = [
      makeWorkItemSuggestion({ id: "s1", suggestedData: { description: "a", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "explicit_scope" } }),
      makeWorkItemSuggestion({ id: "s2", suggestedData: { description: "b", scopeLevel: "project", spaceId: null, scopeOrigin: "supporting_scope" } }),
      makeWorkItemSuggestion({ id: "s3", suggestedData: { description: "c", scopeLevel: "space", spaceId: "space_1", scopeOrigin: "possible_missing_scope" } }),
    ];
    const grouped = groupWorkItemSuggestionsBySpace(suggestions);
    expect(grouped.bySpaceId["space_1"]).toHaveLength(2);
    expect(grouped.projectWide).toHaveLength(1);
    expect(grouped.projectWide[0].id).toBe("s2");
  });

  it("returns empty groups for no suggestions", () => {
    const grouped = groupWorkItemSuggestionsBySpace([]);
    expect(grouped.bySpaceId).toEqual({});
    expect(grouped.projectWide).toEqual([]);
  });
});

function makeResourceSuggestion(overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id: "r1",
    batchId: "b1",
    type: "material_resource",
    status: "pending",
    revision: 1,
    createdAt: "2026-08-16T00:00:00Z",
    suggestedData: { workItemId: "work_1", name: "Tile Adhesive", candidateMaterialId: null },
    ...overrides,
  };
}

describe("acceptRejectErrorMessage", () => {
  it("shows a stale-suggestion explanation for the ai-stale-suggestion problem type, not the raw backend detail", () => {
    const message = acceptRejectErrorMessage({
      type: "urn:renovex:problem:ai-stale-suggestion",
      detail: "suggestion was already reviewed or has changed",
      title: "Conflict",
    });
    expect(message.toLowerCase()).toContain("older");
    expect(message.toLowerCase()).toMatch(/regenerat|review/);
  });

  it("falls back to the backend detail for other problem types", () => {
    const message = acceptRejectErrorMessage({
      type: "urn:renovex:problem:ai-work-item-duplicate",
      detail: "a similar Work Item already exists",
      title: "Conflict",
    });
    expect(message).toBe("a similar Work Item already exists");
  });

  it("falls back to a generic message when nothing usable is present", () => {
    expect(acceptRejectErrorMessage(null)).toMatch(/could not|try again/i);
  });
});

describe("groupResourceSuggestionsByWorkItem", () => {
  it("groups by workItemId then by resource type", () => {
    const suggestions = [
      makeResourceSuggestion({ id: "r1", type: "material_resource", suggestedData: { workItemId: "work_1", name: "Tile Adhesive" } }),
      makeResourceSuggestion({ id: "r2", type: "trade_resource", suggestedData: { workItemId: "work_1", name: "Tiler" } }),
      makeResourceSuggestion({ id: "r3", type: "equipment_resource", suggestedData: { workItemId: "work_1", name: "Tile Cutter" } }),
      makeResourceSuggestion({ id: "r4", type: "material_resource", suggestedData: { workItemId: "work_2", name: "Grout" } }),
    ];
    const grouped = groupResourceSuggestionsByWorkItem(suggestions);
    expect(grouped["work_1"].materials).toHaveLength(1);
    expect(grouped["work_1"].trades).toHaveLength(1);
    expect(grouped["work_1"].equipment).toHaveLength(1);
    expect(grouped["work_2"].materials).toHaveLength(1);
  });
});
