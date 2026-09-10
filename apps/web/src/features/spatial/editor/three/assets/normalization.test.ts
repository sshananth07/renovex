import { describe, expect, it } from "vitest";
import { computeAssetNormalization } from "./normalization";
import type { VisualAssetNormalization } from "./types";

const METERS_Y_CENTER_BOTTOM: VisualAssetNormalization = {
  intrinsicUnit: "meters",
  upAxis: "y",
  pivot: "center-bottom",
};

const METERS_Y_CENTER: VisualAssetNormalization = {
  intrinsicUnit: "meters",
  upAxis: "y",
  pivot: "center",
};

describe("computeAssetNormalization", () => {
  it("computes a zero offset and unit scale for a unit box already centered at the origin, with a center-bottom pivot", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.5, y: 0, z: -0.5 }, max: { x: 0.5, y: 1, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: METERS_Y_CENTER_BOTTOM,
    });
    expect(result).toEqual({
      offset: { x: 0, y: 0, z: 0 },
      scale: { x: 1, y: 1, z: 1 },
    });
  });

  it("re-centers an off-center bounding box onto the pivot rather than assuming it is already centered", () => {
    // Intrinsic mesh bounds are NOT centered at the origin (min/max shifted +2 on X).
    const result = computeAssetNormalization({
      bounds: { min: { x: 1.5, y: 0, z: -0.5 }, max: { x: 2.5, y: 1, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: METERS_Y_CENTER_BOTTOM,
    });
    // The offset must pull the box's actual center (x=2) back to x=0.
    expect(result).toEqual({
      offset: { x: -2, y: 0, z: 0 },
      scale: { x: 1, y: 1, z: 1 },
    });
  });

  it("uses a center pivot (not center-bottom) when normalization.pivot is 'center'", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.5, y: -0.5, z: -0.5 }, max: { x: 0.5, y: 0.5, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: METERS_Y_CENTER,
    });
    expect(result).toEqual({
      offset: { x: 0, y: 0, z: 0 },
      scale: { x: 1, y: 1, z: 1 },
    });
  });

  it("computes uniform-looking per-axis scale to match authoritative dimensions when intrinsic bounds are smaller", () => {
    // Intrinsic bounds are a 0.5m cube; authoritative dimensions call for 1m x 2m x 0.5m.
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.25, y: 0, z: -0.25 }, max: { x: 0.25, y: 0.5, z: 0.25 } },
      dimensions: { x: 1, y: 2, z: 0.5 },
      normalization: METERS_Y_CENTER_BOTTOM,
    });
    expect(result).toEqual({
      offset: { x: 0, y: 0, z: 0 },
      scale: { x: 2, y: 4, z: 1 },
    });
  });

  it("returns a fallback result for zero/degenerate intrinsic bounds", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: 0, y: 0, z: 0 }, max: { x: 0, y: 0, z: 0 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: METERS_Y_CENTER_BOTTOM,
    });
    expect(result).toEqual({ fallback: true, reason: expect.any(String) });
  });

  it("returns a fallback result for a non-'meters' intrinsicUnit", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.5, y: 0, z: -0.5 }, max: { x: 0.5, y: 1, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: { ...METERS_Y_CENTER_BOTTOM, intrinsicUnit: "centimeters" as unknown as "meters" },
    });
    expect(result).toEqual({ fallback: true, reason: expect.any(String) });
  });

  it("returns a fallback result for a non-'y' upAxis", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.5, y: 0, z: -0.5 }, max: { x: 0.5, y: 1, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: { ...METERS_Y_CENTER_BOTTOM, upAxis: "z" as unknown as "y" },
    });
    expect(result).toEqual({ fallback: true, reason: expect.any(String) });
  });

  it("returns a fallback result for an unrecognized pivot value", () => {
    const result = computeAssetNormalization({
      bounds: { min: { x: -0.5, y: 0, z: -0.5 }, max: { x: 0.5, y: 1, z: 0.5 } },
      dimensions: { x: 1, y: 1, z: 1 },
      normalization: { ...METERS_Y_CENTER_BOTTOM, pivot: "top-left" as unknown as "center" },
    });
    expect(result).toEqual({ fallback: true, reason: expect.any(String) });
  });
});
