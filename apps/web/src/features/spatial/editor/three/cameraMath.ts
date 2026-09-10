import type { RoomLocalPoint } from "../types";
import type { WallMeshSpec } from "./sceneProjection";

// Pure camera math: fit-room bounds, focus targets, and preset
// position/target pairs. No "three" or "@react-three/*" imports — plain
// {x,y,z} objects in and out, so this is testable without mounting a
// Canvas. Spatial3DViewport is the only place these results get wired into
// CameraControls' imperative APIs.

export type FitBounds = { center: RoomLocalPoint; radius: number };

// A wall's own two endpoints are its center +/- (length/2) along its own
// axis (yaw) — reconstructing them here keeps WallMeshSpec itself minimal
// (it doesn't carry raw endpoints) while still letting the camera fit to
// the room's real extent.
function wallEndpoints(wall: WallMeshSpec): [RoomLocalPoint, RoomLocalPoint] {
  const halfDx = Math.cos(wall.yaw) * (wall.length / 2);
  const halfDz = Math.sin(wall.yaw) * (wall.length / 2);
  return [
    { x: wall.center.x - halfDx, y: wall.center.y, z: wall.center.z - halfDz },
    { x: wall.center.x + halfDx, y: wall.center.y, z: wall.center.z + halfDz },
  ];
}

export function computeFitBounds(walls: WallMeshSpec[]): FitBounds {
  const points = walls.flatMap(wallEndpoints);
  if (points.length === 0) {
    return { center: { x: 0, y: 0, z: 0 }, radius: 0 };
  }

  const sum = points.reduce((acc, p) => ({ x: acc.x + p.x, y: acc.y + p.y, z: acc.z + p.z }), { x: 0, y: 0, z: 0 });
  const center = { x: sum.x / points.length, y: sum.y / points.length, z: sum.z / points.length };
  const radius = points.reduce((max, p) => Math.max(max, Math.hypot(p.x - center.x, p.y - center.y, p.z - center.z)), 0);

  return { center, radius };
}

export function computeFocusTarget(position: RoomLocalPoint): RoomLocalPoint {
  return position;
}

export type CameraPose = { position: RoomLocalPoint; target: RoomLocalPoint };

const MIN_FIT_RADIUS_METERS = 1;
const EYE_LEVEL_HEIGHT_METERS = 1.6;

// Angled 3/4 flyover: elevated above the room, offset diagonally so the
// room reads as a volume rather than a flat plan (RoomSketcher's Live 3D
// "Overview" framing, per the RP4C3 reference research).
export function computeOverviewPreset(bounds: FitBounds): CameraPose {
  const radius = Math.max(bounds.radius, MIN_FIT_RADIUS_METERS);
  const distance = radius * 1.8;
  return {
    position: {
      x: bounds.center.x + distance * 0.6,
      y: bounds.center.y + distance * 0.75,
      z: bounds.center.z + distance * 0.6,
    },
    target: bounds.center,
  };
}

// Near-plan straight-down view (SketchUp/Planner 5D "Top" framing).
export function computeTopPreset(bounds: FitBounds): CameraPose {
  const radius = Math.max(bounds.radius, MIN_FIT_RADIUS_METERS);
  return {
    position: { x: bounds.center.x, y: bounds.center.y + radius * 2.2, z: bounds.center.z },
    target: bounds.center,
  };
}

// Human-scale perspective toward the room center, at a fixed eye height
// rather than one scaled by room size (a person's eye level doesn't change
// with room size).
export function computeEyeLevelPreset(bounds: FitBounds): CameraPose {
  const radius = Math.max(bounds.radius, MIN_FIT_RADIUS_METERS);
  const distance = radius * 0.9;
  return {
    position: {
      x: bounds.center.x + distance,
      y: bounds.center.y + EYE_LEVEL_HEIGHT_METERS,
      z: bounds.center.z + distance * 0.3,
    },
    target: bounds.center,
  };
}
