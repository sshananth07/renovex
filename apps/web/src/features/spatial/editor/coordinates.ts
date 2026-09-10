import type { RoomDraftWall, RoomLocalPoint, Viewport } from "./types";

// RoomDraft/world coordinates are the canonical Renovex room-local contract
// (backend/internal/spatial/roomdraft.go, mirroring
// ios/.../RoomLocalTransform.swift §8.8): metres, right-handed, Y-up. The 2D
// floor plan is a top-down view onto the X-Z plane (Y is height, out of
// plan). Looking down the -Y axis in a right-handed frame, +X is
// screen-right and +Z points toward the viewer — so moving "up"/"away" on
// the 2D plan is -Z. This file is the ONLY place that convention is encoded;
// no other component may reimplement it.
export type ScreenPoint = { x: number; y: number };
export type ContainerSize = { width: number; height: number };

export function worldToScreen(point: RoomLocalPoint, viewport: Viewport, container: ContainerSize): ScreenPoint {
  return {
    x: container.width / 2 + viewport.panX + point.x * viewport.zoom,
    y: container.height / 2 + viewport.panY - point.z * viewport.zoom,
  };
}

export function screenToWorld(point: ScreenPoint, viewport: Viewport, container: ContainerSize): RoomLocalPoint {
  return {
    x: (point.x - container.width / 2 - viewport.panX) / viewport.zoom,
    y: 0,
    z: -((point.y - container.height / 2 - viewport.panY) / viewport.zoom),
  };
}

const MIN_ZOOM = 4; // px/metre
const MAX_ZOOM = 400;
const FIT_PADDING_RATIO = 0.85; // leave headroom around the room bounds

// Frames the room so all wall endpoints are visible, centered, at a zoom
// that fills the container without clipping. Falls back to a fixed default
// viewport when there are no walls to frame (e.g. an empty/new draft).
export function fitToRoom(walls: RoomDraftWall[] | null | undefined, container: ContainerSize): Viewport {
  const points = (walls ?? []).flatMap((wall) => [wall.start, wall.end]);
  if (points.length === 0 || container.width <= 0 || container.height <= 0) {
    return { panX: 0, panY: 0, zoom: 40 };
  }

  let minX = Infinity;
  let maxX = -Infinity;
  let minZ = Infinity;
  let maxZ = -Infinity;
  for (const p of points) {
    minX = Math.min(minX, p.x);
    maxX = Math.max(maxX, p.x);
    minZ = Math.min(minZ, p.z);
    maxZ = Math.max(maxZ, p.z);
  }

  const spanX = Math.max(maxX - minX, 0.01);
  const spanZ = Math.max(maxZ - minZ, 0.01);
  const zoomX = (container.width * FIT_PADDING_RATIO) / spanX;
  const zoomY = (container.height * FIT_PADDING_RATIO) / spanZ;
  const zoom = Math.min(Math.max(Math.min(zoomX, zoomY), MIN_ZOOM), MAX_ZOOM);

  const centerX = (minX + maxX) / 2;
  const centerZ = (minZ + maxZ) / 2;

  // panX/panY are defined so that worldToScreen(center) lands at the
  // container's center: container.width/2 + panX + centerX*zoom = container.width/2
  // => panX = -centerX*zoom (and the mirrored -Z sign for panY).
  return {
    panX: -centerX * zoom,
    panY: centerZ * zoom,
    zoom,
  };
}

export function clampZoom(zoom: number): number {
  return Math.min(Math.max(zoom, MIN_ZOOM), MAX_ZOOM);
}

export type WorldPoint2D = { x: number; z: number };

// M8.5C closure patch §16: the four world-space corners of an object's
// top-down footprint — given its center, room-local width (X)/depth (Z),
// and yaw (radians, matching quaternionToYawDegrees' own Y-axis-only
// convention). Callers project each corner through worldToScreen()
// exactly like every other element's geometry; this never computes screen
// positions itself. Pure and jsdom-free, matching sceneProjection.ts's own
// convention for domain-geometry helpers.
export function objectFootprintCorners(center: WorldPoint2D, width: number, depth: number, yawRadians: number): WorldPoint2D[] {
  const halfWidth = width / 2;
  const halfDepth = depth / 2;
  const local: WorldPoint2D[] = [
    { x: -halfWidth, z: -halfDepth },
    { x: halfWidth, z: -halfDepth },
    { x: halfWidth, z: halfDepth },
    { x: -halfWidth, z: halfDepth },
  ];
  const cos = Math.cos(yawRadians);
  const sin = Math.sin(yawRadians);
  return local.map((p) => ({
    x: center.x + p.x * cos - p.z * sin,
    z: center.z + p.x * sin + p.z * cos,
  }));
}
