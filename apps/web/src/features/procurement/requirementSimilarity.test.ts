import { describe, expect, it } from "vitest";
import type { components } from "@/lib/api/generated/schema";
import { buildDuplicateCandidateKey, findSimilarRequirement } from "./requirementSimilarity";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

function makeRequirement(overrides: Partial<MaterialRequirement> = {}): MaterialRequirement {
  return {
    id: "req-1",
    projectId: "proj-1",
    materialId: "mat-cement",
    materialName: "Cement",
    catalogUnit: "bag",
    requiredQuantity: { value: "1", unit: "bag" },
    status: "draft",
    sourceType: "manual",
    sourceSyncState: "clean",
    unitMismatch: false,
    unitMismatchAcknowledged: false,
    revision: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("buildDuplicateCandidateKey", () => {
  it("joins materialId, workItemId, and trimmed unit", () => {
    expect(buildDuplicateCandidateKey({ materialId: "mat-1", workItemId: "wi-1", quantityUnit: " bag " })).toBe("mat-1|wi-1|bag");
  });

  it("normalizes undefined and null workItemId to the same project-level key", () => {
    const withUndefined = buildDuplicateCandidateKey({ materialId: "mat-1", quantityUnit: "bag" });
    const withNull = buildDuplicateCandidateKey({ materialId: "mat-1", workItemId: null, quantityUnit: "bag" });
    expect(withUndefined).toBe(withNull);
  });
});

describe("findSimilarRequirement", () => {
  it("matches on same material + same work item + same trimmed unit for a draft requirement", () => {
    const existing = [makeRequirement({ id: "existing-1", workItemId: "wi-1" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", workItemId: "wi-1", quantityUnit: "bag" });
    expect(match?.id).toBe("existing-1");
  });

  it("matches on same material + same work item + same trimmed unit for a reviewed requirement", () => {
    const existing = [makeRequirement({ id: "existing-1", status: "reviewed" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "bag" });
    expect(match?.id).toBe("existing-1");
  });

  it("treats undefined workItemId on the candidate and null on the existing record as the same project-level match", () => {
    const existing = [makeRequirement({ id: "existing-1", workItemId: undefined })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", workItemId: null, quantityUnit: "bag" });
    expect(match?.id).toBe("existing-1");
  });

  it("a generated (cost_item) existing requirement can trigger a warning against a manual candidate", () => {
    const existing = [makeRequirement({ id: "existing-1", sourceType: "cost_item" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "bag" });
    expect(match?.id).toBe("existing-1");
  });

  it("ignores archived requirements", () => {
    const existing = [makeRequirement({ id: "existing-1", status: "archived" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "bag" });
    expect(match).toBeUndefined();
  });

  it("ignores split (terminal source) requirements", () => {
    const existing = [makeRequirement({ id: "existing-1", status: "split" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "bag" });
    expect(match).toBeUndefined();
  });

  it("does not match when the unit differs", () => {
    const existing = [makeRequirement({ id: "existing-1" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "kg" });
    expect(match).toBeUndefined();
  });

  it("does not match a different material", () => {
    const existing = [makeRequirement({ id: "existing-1", materialId: "mat-sand" })];
    const match = findSimilarRequirement(existing, { materialId: "mat-cement", quantityUnit: "bag" });
    expect(match).toBeUndefined();
  });
});
