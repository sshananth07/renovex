import { describe, expect, it } from "vitest";
import { validateRegistry, VISUAL_ASSET_REGISTRY } from "./registry";
import type { VisualAssetDescriptor } from "./types";

const NORMALIZATION = { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" } as const;

function descriptor(overrides: Partial<VisualAssetDescriptor>): VisualAssetDescriptor {
  return {
    id: "d1",
    version: 1,
    appliesTo: { elementKind: "fixture", category: "boiler" },
    format: "glb",
    source: { kind: "builtin", path: "/models/d1.glb" },
    normalization: NORMALIZATION,
    displayName: "Test descriptor",
    ...overrides,
  };
}

describe("VISUAL_ASSET_REGISTRY", () => {
  it("contains at least one fixture-category descriptor for boiler", () => {
    const boiler = VISUAL_ASSET_REGISTRY.find(
      (d) => d.appliesTo.elementKind === "fixture" && d.appliesTo.category === "boiler",
    );
    expect(boiler).toBeDefined();
  });

  it("has no two descriptors with the same id", () => {
    const ids = VISUAL_ASSET_REGISTRY.map((d) => d.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("has no two descriptors claiming the same default (elementKind, category) binding", () => {
    const keys = VISUAL_ASSET_REGISTRY.map((d) => `${d.appliesTo.elementKind}:${d.appliesTo.category}`);
    expect(new Set(keys).size).toBe(keys.length);
  });
});

describe("validateRegistry", () => {
  it("rejects two descriptors sharing the same id", () => {
    const bad = [descriptor({ id: "dup" }), descriptor({ id: "dup", appliesTo: { elementKind: "object", category: "sofa" } })];
    expect(() => validateRegistry(bad)).toThrow(/duplicate descriptor id/i);
  });

  it("rejects two descriptors claiming the same default (elementKind, category) binding", () => {
    const bad = [descriptor({ id: "a" }), descriptor({ id: "b" })];
    expect(() => validateRegistry(bad)).toThrow(/ambiguous default binding/i);
  });

  it("accepts a registry with distinct ids and distinct bindings", () => {
    const good = [descriptor({ id: "a" }), descriptor({ id: "b", appliesTo: { elementKind: "object", category: "sofa" } })];
    expect(() => validateRegistry(good)).not.toThrow();
  });
});
