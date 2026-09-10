import { describe, expect, it } from "vitest";
import { resolveVisualAsset } from "./resolveAsset";

describe("resolveVisualAsset", () => {
  it("resolves a known fixture category with no binding to a builtin tier", () => {
    const result = resolveVisualAsset({ elementKind: "fixture", category: "boiler" });
    expect(result).toEqual({ tier: "builtin", descriptor: expect.objectContaining({ id: "fixture-boiler-v1" }) });
  });

  it("resolves a known object category with no binding to a builtin tier", () => {
    const result = resolveVisualAsset({ elementKind: "object", category: "sofa" });
    expect(result).toEqual({ tier: "builtin", descriptor: expect.objectContaining({ id: "object-sofa-v1" }) });
  });

  it("resolves an unknown category with no binding to a procedural tier", () => {
    const result = resolveVisualAsset({ elementKind: "fixture", category: "unheard_of_category" });
    expect(result).toEqual({ tier: "procedural" });
  });

  it("resolves an empty category with no binding to a procedural tier without crashing", () => {
    const result = resolveVisualAsset({ elementKind: "object", category: "" });
    expect(result).toEqual({ tier: "procedural" });
  });

  it("does not cross-resolve a fixture category against an object binding", () => {
    const result = resolveVisualAsset({ elementKind: "fixture", category: "sofa" });
    expect(result).toEqual({ tier: "procedural" });
  });

  it("a canonical binding takes precedence over the category default, carrying the category default as its own fallback", () => {
    const result = resolveVisualAsset({
      elementKind: "fixture",
      category: "boiler",
      visualAsset: { assetId: "custom-boiler-asset", version: 3 },
    });
    expect(result).toEqual({
      tier: "authorized",
      ref: { assetId: "custom-boiler-asset", version: 3 },
      builtinFallback: expect.objectContaining({ id: "fixture-boiler-v1" }),
    });
  });

  it("a canonical binding with no category default carries a null builtinFallback", () => {
    const result = resolveVisualAsset({
      elementKind: "fixture",
      category: "unheard_of_category",
      visualAsset: { assetId: "custom-asset", version: 1 },
    });
    expect(result).toEqual({
      tier: "authorized",
      ref: { assetId: "custom-asset", version: 1 },
      builtinFallback: null,
    });
  });

  it("an absent binding (undefined) falls through to category/procedural resolution unchanged", () => {
    const result = resolveVisualAsset({ elementKind: "fixture", category: "boiler", visualAsset: undefined });
    expect(result).toEqual({ tier: "builtin", descriptor: expect.objectContaining({ id: "fixture-boiler-v1" }) });
  });
});
