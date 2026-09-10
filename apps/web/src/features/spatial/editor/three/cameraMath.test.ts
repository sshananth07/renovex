import { describe, expect, it } from "vitest";
import {
  computeEyeLevelPreset,
  computeFitBounds,
  computeFocusTarget,
  computeOverviewPreset,
  computeTopPreset,
} from "./cameraMath";
import type { WallMeshSpec } from "./sceneProjection";

function wall(id: string, center: { x: number; y: number; z: number }, length: number, yaw = 0): WallMeshSpec {
  return { id, center, length, yaw, height: 2.4, thickness: 0.1, heightIsFallback: false, thicknessIsFallback: false };
}

describe("computeFitBounds", () => {
  it("returns a zero-radius bound at the origin for an empty wall list", () => {
    const bounds = computeFitBounds([]);
    expect(bounds.center).toEqual({ x: 0, y: 0, z: 0 });
    expect(bounds.radius).toBe(0);
  });

  it("computes the center and radius for a single wall centered at the origin", () => {
    // A 4m-long wall centered at the origin: its endpoints are 2m from
    // center along its own axis, so half-extent = 2.
    const bounds = computeFitBounds([wall("w1", { x: 0, y: 0, z: 0 }, 4)]);
    expect(bounds.center).toEqual({ x: 0, y: 0, z: 0 });
    expect(bounds.radius).toBeGreaterThan(0);
  });

  it("computes a center that is the average of multiple wall centers", () => {
    const bounds = computeFitBounds([wall("w1", { x: -2, y: 0, z: 0 }, 2), wall("w2", { x: 2, y: 0, z: 0 }, 2)]);
    expect(bounds.center.x).toBeCloseTo(0, 9);
    expect(bounds.center.z).toBeCloseTo(0, 9);
  });

  it("grows the radius to cover walls further from the center", () => {
    const near = computeFitBounds([wall("w1", { x: 0, y: 0, z: 0 }, 2)]);
    const far = computeFitBounds([wall("w1", { x: 10, y: 0, z: 0 }, 2), wall("w2", { x: -10, y: 0, z: 0 }, 2)]);
    expect(far.radius).toBeGreaterThan(near.radius);
  });
});

describe("computeFocusTarget", () => {
  it("returns the element's own position for a point-like spec", () => {
    const target = computeFocusTarget({ x: 1, y: 0.5, z: -2 });
    expect(target).toEqual({ x: 1, y: 0.5, z: -2 });
  });
});

describe("camera presets — pure functions of fit bounds", () => {
  const bounds = { center: { x: 2, y: 0, z: 3 }, radius: 5 };

  it("Overview: elevated 3/4 position, looking at the bounds center", () => {
    const preset = computeOverviewPreset(bounds);
    expect(preset.target).toEqual(bounds.center);
    // Elevated: camera Y is above the room (Y-up frame).
    expect(preset.position.y).toBeGreaterThan(bounds.center.y);
    // Offset horizontally from center (a 3/4 angled view, not directly overhead).
    const horizontalOffset = Math.hypot(preset.position.x - bounds.center.x, preset.position.z - bounds.center.z);
    expect(horizontalOffset).toBeGreaterThan(0);
  });

  it("Top: camera directly above the center, looking straight down", () => {
    const preset = computeTopPreset(bounds);
    expect(preset.target).toEqual(bounds.center);
    expect(preset.position.x).toBeCloseTo(bounds.center.x, 9);
    expect(preset.position.z).toBeCloseTo(bounds.center.z, 9);
    expect(preset.position.y).toBeGreaterThan(bounds.center.y);
  });

  it("Eye Level: camera at human height, angled toward the room center", () => {
    const preset = computeEyeLevelPreset(bounds);
    expect(preset.target).toEqual(bounds.center);
    expect(preset.position.y).toBeCloseTo(1.6, 9);
    const horizontalOffset = Math.hypot(preset.position.x - bounds.center.x, preset.position.z - bounds.center.z);
    expect(horizontalOffset).toBeGreaterThan(0);
  });

  it("presets scale their camera distance with the fit-bounds radius", () => {
    const small = computeOverviewPreset({ center: { x: 0, y: 0, z: 0 }, radius: 2 });
    const large = computeOverviewPreset({ center: { x: 0, y: 0, z: 0 }, radius: 20 });
    const smallDistance = Math.hypot(small.position.x, small.position.y, small.position.z);
    const largeDistance = Math.hypot(large.position.x, large.position.y, large.position.z);
    expect(largeDistance).toBeGreaterThan(smallDistance);
  });
});
