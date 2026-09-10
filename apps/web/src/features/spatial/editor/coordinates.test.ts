import { describe, expect, it } from "vitest";
import { clampZoom, fitToRoom, objectFootprintCorners, screenToWorld, worldToScreen } from "./coordinates";
import type { RoomDraftWall, Viewport } from "./types";

const container = { width: 800, height: 600 };

function wall(id: string, sx: number, sz: number, ex: number, ez: number): RoomDraftWall {
  return {
    id,
    start: { x: sx, y: 0, z: sz },
    end: { x: ex, y: 0, z: ez },
    provenance: { provider: "roomplan", sourceElementIdentifier: id },
    thicknessStatus: "estimated",
  };
}

describe("worldToScreen / screenToWorld", () => {
  const viewport: Viewport = { panX: 10, panY: -5, zoom: 50 };

  it("round-trips a world point through screen and back", () => {
    const original = { x: 1.5, y: 0, z: -2.25 };
    const screen = worldToScreen(original, viewport, container);
    const roundTripped = screenToWorld(screen, viewport, container);
    expect(roundTripped.x).toBeCloseTo(original.x, 6);
    expect(roundTripped.z).toBeCloseTo(original.z, 6);
  });

  it("maps the world origin to the container center offset by pan", () => {
    const screen = worldToScreen({ x: 0, y: 0, z: 0 }, viewport, container);
    expect(screen.x).toBeCloseTo(container.width / 2 + viewport.panX, 6);
    expect(screen.y).toBeCloseTo(container.height / 2 + viewport.panY, 6);
  });

  it("moves +X world to screen-right and +Z world to screen-up (away from viewer)", () => {
    const origin = worldToScreen({ x: 0, y: 0, z: 0 }, viewport, container);
    const plusX = worldToScreen({ x: 1, y: 0, z: 0 }, viewport, container);
    const plusZ = worldToScreen({ x: 0, y: 0, z: 1 }, viewport, container);
    expect(plusX.x).toBeGreaterThan(origin.x);
    expect(plusX.y).toBeCloseTo(origin.y, 6);
    expect(plusZ.y).toBeLessThan(origin.y);
    expect(plusZ.x).toBeCloseTo(origin.x, 6);
  });

  it("is stable under a composed pan+zoom change (round-trip still holds)", () => {
    const zoomedViewport: Viewport = { panX: -120, panY: 60, zoom: 133 };
    const original = { x: -3.4, y: 0, z: 5.1 };
    const roundTripped = screenToWorld(worldToScreen(original, zoomedViewport, container), zoomedViewport, container);
    expect(roundTripped.x).toBeCloseTo(original.x, 6);
    expect(roundTripped.z).toBeCloseTo(original.z, 6);
  });
});

describe("fitToRoom", () => {
  it("returns a default viewport when there are no walls", () => {
    const viewport = fitToRoom([], container);
    expect(viewport.zoom).toBeGreaterThan(0);
  });

  it("returns a default viewport when walls is null/undefined", () => {
    expect(fitToRoom(null, container).zoom).toBeGreaterThan(0);
    expect(fitToRoom(undefined, container).zoom).toBeGreaterThan(0);
  });

  it("frames all wall endpoints so each maps within the container bounds", () => {
    const walls = [wall("w1", 0, 0, 4, 0), wall("w2", 4, 0, 4, 3), wall("w3", 4, 3, 0, 3), wall("w4", 0, 3, 0, 0)];
    const viewport = fitToRoom(walls, container);

    for (const w of walls) {
      for (const endpoint of [w.start, w.end]) {
        const screen = worldToScreen(endpoint, viewport, container);
        expect(screen.x).toBeGreaterThanOrEqual(0);
        expect(screen.x).toBeLessThanOrEqual(container.width);
        expect(screen.y).toBeGreaterThanOrEqual(0);
        expect(screen.y).toBeLessThanOrEqual(container.height);
      }
    }
  });

  it("centers the room bounds in the container", () => {
    const walls = [wall("w1", 0, 0, 4, 0), wall("w2", 4, 3, 0, 3)];
    const viewport = fitToRoom(walls, container);
    const center = worldToScreen({ x: 2, y: 0, z: 1.5 }, viewport, container);
    expect(center.x).toBeCloseTo(container.width / 2, 1);
    expect(center.y).toBeCloseTo(container.height / 2, 1);
  });
});

describe("clampZoom", () => {
  it("clamps below the minimum", () => {
    expect(clampZoom(0)).toBeGreaterThan(0);
  });

  it("clamps above the maximum", () => {
    expect(clampZoom(1_000_000)).toBeLessThan(1_000_000);
  });

  it("leaves an in-range value unchanged", () => {
    expect(clampZoom(50)).toBe(50);
  });
});

// M8.5C closure patch §16: the 2D object footprint's four world-space
// corners, given the object's centre/width/depth/yaw — the only geometry
// this closure patch's FloorPlanViewport object rendering needs beyond the
// existing worldToScreen() projection it already uses for every other
// element.
describe("objectFootprintCorners", () => {
  it("returns the four corners of an axis-aligned (yaw=0) rectangle centered on the given point", () => {
    const corners = objectFootprintCorners({ x: 0, z: 0 }, 2, 1, 0);
    expect(corners).toHaveLength(4);
    expect(corners).toContainEqual({ x: -1, z: -0.5 });
    expect(corners).toContainEqual({ x: 1, z: -0.5 });
    expect(corners).toContainEqual({ x: 1, z: 0.5 });
    expect(corners).toContainEqual({ x: -1, z: 0.5 });
  });

  it("offsets the corners by the given center", () => {
    const corners = objectFootprintCorners({ x: 5, z: 5 }, 2, 2, 0);
    expect(corners).toContainEqual({ x: 4, z: 4 });
    expect(corners).toContainEqual({ x: 6, z: 6 });
  });

  it("rotates the corners by yaw (90 degrees swaps width/depth extents)", () => {
    const corners = objectFootprintCorners({ x: 0, z: 0 }, 2, 1, Math.PI / 2);
    // A 90° yaw rotation swaps which axis the 2 (width) vs 1 (depth) extent
    // lands on — a corner originally at local (1, -0.5) rotates to
    // approximately (0.5, 1) in world space.
    const found = corners.some((c) => Math.abs(c.x - 0.5) < 1e-9 && Math.abs(c.z - 1) < 1e-9);
    expect(found).toBe(true);
  });
});
