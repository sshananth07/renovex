import type { RoomDraft } from "../../api";
import type { DoorMetadata, RoomLocalPoint, RoomLocalQuaternion, RoomLocalTransform } from "../types";

// RoomDraft/world coordinates are the canonical Renovex room-local contract
// (backend/internal/spatial/roomdraft.go, mirroring
// ios/.../RoomLocalTransform.swift): metres, right-handed, Y-up. Three.js
// and @react-three/fiber use the SAME convention natively (confirmed by the
// iOS source's own doc comment: RoomLocalTransform's column-major matrix
// layout was chosen because it "match[es]... Three.js/R3F's convention on
// Web") — so, unlike the 2D floor-plan projection in ../coordinates.ts,
// there is NO axis remapping, sign flip, or basis change here. Every
// position/rotation field below is a direct, verified pass-through.
//
// This file is a pure, jsdom-free-testable projection layer: it turns a
// RoomDraft into plain-data rendering specs. It must never import from
// "three" or "@react-three/*", never be persisted, and never enter React
// Query state — Spatial3DViewport consumes these specs to build actual
// Three.js objects.

// Visual-only fallback constants. These exist because RP4C1/RP4C2 never
// required height/thickness/width/etc. to be present (the 2D floor plan
// never rendered them), so 3D introduces the first real need for a
// default. They are NEVER written back to RoomDraft and NEVER sent in any
// EditOperation payload — see the *IsFallback flags below, which let
// Spatial3DViewport avoid presenting a fallback as measured truth.
export const DEFAULT_WALL_HEIGHT_METERS = 2.4;
export const DEFAULT_WALL_THICKNESS_METERS = 0.1;
// Matches ElementInspector.tsx's existing `?? 0.9`/`?? 2` door-resize fallbacks.
export const DEFAULT_OPENING_WIDTH_METERS = 0.9;
export const DEFAULT_OPENING_HEIGHT_METERS = 2.1;
// Matches ElementInspector.tsx's existing `?? 0.5` fixture-resize fallback.
export const DEFAULT_FIXTURE_DIMENSIONS: RoomLocalPoint = { x: 0.5, y: 0.5, z: 0.5 };

export type WallMeshSpec = {
  id: string;
  center: RoomLocalPoint;
  length: number;
  yaw: number;
  height: number;
  thickness: number;
  heightIsFallback: boolean;
  thicknessIsFallback: boolean;
};

export type OpeningVisualSpec = {
  id: string;
  parentWallId?: string;
  kind: string;
  profile: string;
  position: RoomLocalPoint;
  rotation: RoomLocalQuaternion;
  width: number;
  height: number;
  widthIsFallback: boolean;
  heightIsFallback: boolean;
  door?: DoorMetadata;
};

// RP4D's canonical, persisted per-element visual-asset reference
// (RoomDraftFixture.visualAsset / RoomDraftObject.visualAsset) — identity
// only ({assetId, version}), never a URL/storage key/format. Undefined
// means "no explicit binding," matching resolveVisualAsset's contract.
export type VisualAssetRefSpec = { assetId: string; version: number };

// Persisted per-element appearance override (RP4E1/Gate 1's
// RoomDraftObject.appearance/RoomDraftFixture.appearance) — mirrors the
// generated VisualAppearance shape's own field set exactly rather than
// re-declaring it, since this file must stay import-free of the generated
// schema (kept jsdom-free-testable / no API-client dependency).
export type AppearanceSpec = { baseColor: string; materialFamily: string; roughness: string; metallic: boolean };

export type ObjectVisualSpec = {
  id: string;
  category: string;
  position: RoomLocalPoint;
  rotation: RoomLocalQuaternion;
  dimensions: RoomLocalPoint;
  dimensionsAreFallback: boolean;
  visualAsset?: VisualAssetRefSpec;
  appearance?: AppearanceSpec;
};

export type FixtureVisualSpec = {
  id: string;
  category: string;
  position: RoomLocalPoint;
  rotation: RoomLocalQuaternion;
  dimensions: RoomLocalPoint;
  dimensionsAreFallback: boolean;
  createdBy: string;
  parentWallId?: string;
  visualAsset?: VisualAssetRefSpec;
  appearance?: AppearanceSpec;
};

export type ServicePointVisualSpec = {
  id: string;
  kind: string;
  position: RoomLocalPoint;
  createdBy: string;
  parentWallId?: string;
};

export type ConstraintVisualSpec = {
  id: string;
  kind: string;
  position: RoomLocalPoint;
  rotation: RoomLocalQuaternion;
  dimensions: RoomLocalPoint;
  dimensionsAreFallback: boolean;
};

export type ProjectedScene = {
  walls: WallMeshSpec[];
  openings: OpeningVisualSpec[];
  objects: ObjectVisualSpec[];
  fixtures: FixtureVisualSpec[];
  servicePoints: ServicePointVisualSpec[];
  constraints: ConstraintVisualSpec[];
};

// Decomposes a canonical RoomLocalTransform into the plain position/
// rotation pair Spatial3DViewport consumes. Given the confirmed zero-remap
// coordinate contract this is a pass-through today, but it is named and
// tested as its own concern (not inlined) so a future real Three.js-side
// consumer is unambiguous about what convention it receives.
export function transformToPose(transform: RoomLocalTransform): { position: RoomLocalPoint; rotation: RoomLocalQuaternion } {
  return { position: transform.position, rotation: transform.rotation };
}

// Returns a NEW transform with only `position` replaced; `rotation` is
// copied verbatim (never renormalized, never perturbed). This is the one
// place a 3D edit may build an updated RoomLocalTransform — it must never
// be hand-constructed inline, so a move_fixture edit can never silently
// change rotation.
export function withUpdatedPosition(transform: RoomLocalTransform, position: RoomLocalPoint): RoomLocalTransform {
  return { position, rotation: transform.rotation };
}

// Returns a NEW transform with only `rotation` replaced; `position` is
// copied verbatim — the rotation-side mirror of withUpdatedPosition above,
// same "one place a 3D/manual edit may build an updated RoomLocalTransform"
// discipline.
export function withUpdatedRotation(transform: RoomLocalTransform, rotation: RoomLocalQuaternion): RoomLocalTransform {
  return { position: transform.position, rotation };
}

// T1B: objects' manual rotation control is yaw-only (rotation about the
// room's Y/up axis) — the one degree of freedom that is actually meaningful
// for placing room furniture, matching the convention walls/openings
// already use (wallYaw below, opening rotation display). Converts to/from
// the canonical RoomLocalQuaternion wire shape RotateObjectOperation
// expects; never invents a new rotation representation.
export function yawDegreesToQuaternion(degrees: number): RoomLocalQuaternion {
  const halfRadians = (degrees * Math.PI) / 360;
  return { x: 0, y: Math.sin(halfRadians), z: 0, w: Math.cos(halfRadians) };
}

export function quaternionToYawDegrees(rotation: RoomLocalQuaternion): number {
  const radians = 2 * Math.atan2(rotation.y, rotation.w);
  return (radians * 180) / Math.PI;
}

// M8.5C closure patch §14: TransformControls scale is transient Three.js
// UI state ONLY — this is the sole place a transform-start canonical
// dimensions record is combined with a completed scale gesture to produce
// the next canonical dimensions. Callers must pass `start` captured at
// gesture START (never the already-scaled current dimensions, which would
// compound on every resize). Math.abs guards against Three.js's negative
// scale (dragging a handle past the object's center) ever producing a
// negative/inverted canonical dimension; the minimum clamp prevents a
// degenerate near-zero object.
const MIN_OBJECT_DIMENSION_METERS = 0.05;

export function dimensionsFromScale(start: RoomLocalPoint, scale: RoomLocalPoint): RoomLocalPoint {
  return {
    x: Math.max(start.x * Math.abs(scale.x), MIN_OBJECT_DIMENSION_METERS),
    y: Math.max(start.y * Math.abs(scale.y), MIN_OBJECT_DIMENSION_METERS),
    z: Math.max(start.z * Math.abs(scale.z), MIN_OBJECT_DIMENSION_METERS),
  };
}

function midpoint(a: RoomLocalPoint, b: RoomLocalPoint): RoomLocalPoint {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, z: (a.z + b.z) / 2 };
}

function distance2D(a: RoomLocalPoint, b: RoomLocalPoint): number {
  return Math.sqrt((a.x - b.x) ** 2 + (a.z - b.z) ** 2);
}

// Rotation about the Y (up) axis so the wall's local +X axis aligns with
// start->end, matching the right-handed Y-up frame: yaw 0 = along +X,
// yaw PI/2 = along +Z (verified against the confirmed coordinate contract
// via the paired axis-aligned test cases in sceneProjection.test.ts, not
// assumed).
function wallYaw(start: RoomLocalPoint, end: RoomLocalPoint): number {
  return Math.atan2(end.z - start.z, end.x - start.x);
}

function projectWall(wall: RoomDraft["walls"] extends (infer T)[] | null ? T : never): WallMeshSpec {
  const heightIsFallback = wall.height === undefined;
  const thicknessIsFallback = wall.thickness === undefined;
  return {
    id: wall.id,
    center: midpoint(wall.start, wall.end),
    length: distance2D(wall.start, wall.end),
    yaw: wallYaw(wall.start, wall.end),
    height: wall.height ?? DEFAULT_WALL_HEIGHT_METERS,
    thickness: wall.thickness ?? DEFAULT_WALL_THICKNESS_METERS,
    heightIsFallback,
    thicknessIsFallback,
  };
}

function projectOpening(opening: RoomDraft["openings"] extends (infer T)[] | null ? T : never): OpeningVisualSpec {
  const pose = transformToPose(opening.transform);
  const widthIsFallback = opening.width === undefined;
  const heightIsFallback = opening.height === undefined;
  return {
    id: opening.id,
    parentWallId: opening.parentWallId,
    kind: opening.kind,
    profile: opening.profile,
    position: pose.position,
    rotation: pose.rotation,
    width: opening.width ?? DEFAULT_OPENING_WIDTH_METERS,
    height: opening.height ?? DEFAULT_OPENING_HEIGHT_METERS,
    widthIsFallback,
    heightIsFallback,
    door: opening.door,
  };
}

function projectObject(object: RoomDraft["objects"] extends (infer T)[] | null ? T : never): ObjectVisualSpec {
  const pose = transformToPose(object.transform);
  const dimensionsAreFallback = object.dimensions === undefined;
  return {
    id: object.id,
    category: object.category,
    position: pose.position,
    rotation: pose.rotation,
    dimensions: object.dimensions ?? DEFAULT_FIXTURE_DIMENSIONS,
    dimensionsAreFallback,
    visualAsset: object.visualAsset,
    appearance: object.appearance,
  };
}

function projectFixture(fixture: RoomDraft["fixtures"] extends (infer T)[] | null ? T : never): FixtureVisualSpec {
  const pose = transformToPose(fixture.transform);
  const dimensionsAreFallback = fixture.dimensions === undefined;
  return {
    id: fixture.id,
    category: fixture.category,
    position: pose.position,
    rotation: pose.rotation,
    dimensions: fixture.dimensions ?? DEFAULT_FIXTURE_DIMENSIONS,
    dimensionsAreFallback,
    createdBy: fixture.createdBy,
    parentWallId: fixture.parentWallId,
    visualAsset: fixture.visualAsset,
    appearance: fixture.appearance,
  };
}

function projectServicePoint(sp: RoomDraft["servicePoints"] extends (infer T)[] | null ? T : never): ServicePointVisualSpec {
  return {
    id: sp.id,
    kind: sp.kind,
    position: sp.position,
    createdBy: sp.createdBy,
    parentWallId: sp.parentWallId,
  };
}

function projectConstraint(constraint: RoomDraft["constraints"] extends (infer T)[] | null ? T : never): ConstraintVisualSpec {
  const pose = transformToPose(constraint.transform);
  const dimensionsAreFallback = constraint.dimensions === undefined;
  return {
    id: constraint.id,
    kind: constraint.kind,
    position: pose.position,
    rotation: pose.rotation,
    dimensions: constraint.dimensions ?? DEFAULT_FIXTURE_DIMENSIONS,
    dimensionsAreFallback,
  };
}

export function projectScene(draft: RoomDraft): ProjectedScene {
  return {
    walls: (draft.walls ?? []).map(projectWall),
    openings: (draft.openings ?? []).map(projectOpening),
    objects: (draft.objects ?? []).map(projectObject),
    fixtures: (draft.fixtures ?? []).map(projectFixture),
    servicePoints: (draft.servicePoints ?? []).map(projectServicePoint),
    constraints: (draft.constraints ?? []).map(projectConstraint),
  };
}
