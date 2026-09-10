import { describe, expect, it } from "vitest";
import {
  DEFAULT_FIXTURE_DIMENSIONS,
  DEFAULT_OPENING_HEIGHT_METERS,
  DEFAULT_OPENING_WIDTH_METERS,
  DEFAULT_WALL_HEIGHT_METERS,
  DEFAULT_WALL_THICKNESS_METERS,
  dimensionsFromScale,
  projectScene,
  quaternionToYawDegrees,
  transformToPose,
  withUpdatedPosition,
  withUpdatedRotation,
  yawDegreesToQuaternion,
} from "./sceneProjection";
import type { RoomDraft } from "../../api";
import type { RoomLocalTransform } from "../types";

const identityRotation = { x: 0, y: 0, z: 0, w: 1 };
// 90° rotation about Y, matching the exact reference value pinned in
// ios/.../RoomLocalCoordinateNormalizerTests.swift's
// test_quaternion_from90DegreeYRotation_matchesExpectedValue.
const ninetyDegreeYRotation = { x: 0, y: Math.sqrt(2) / 2, z: 0, w: Math.sqrt(2) / 2 };

function emptyDraft(overrides: Partial<RoomDraft> = {}): RoomDraft {
  return {
    id: "draft_1",
    captureId: "capture_1",
    revision: 1,
    canResetToScan: false,
    walls: [],
    openings: [],
    objects: [],
    fixtures: [],
    servicePoints: [],
    constraints: [],
    ...overrides,
  };
}

describe("transformToPose — RoomLocalTransform decomposition (explicit, tested projection concern)", () => {
  it("round-trips an identity transform unchanged", () => {
    const transform: RoomLocalTransform = { position: { x: 0, y: 0, z: 0 }, rotation: identityRotation };
    const pose = transformToPose(transform);
    expect(pose.position).toEqual({ x: 0, y: 0, z: 0 });
    expect(pose.rotation).toEqual(identityRotation);
  });

  it("round-trips a translation-only transform, preserving identity rotation", () => {
    const transform: RoomLocalTransform = { position: { x: 1.5, y: 0, z: -2.25 }, rotation: identityRotation };
    const pose = transformToPose(transform);
    expect(pose.position).toEqual({ x: 1.5, y: 0, z: -2.25 });
    expect(pose.rotation).toEqual(identityRotation);
  });

  it("round-trips a 90-degree Y-rotation transform with the rotation preserved exactly (no renormalization, no sign flip)", () => {
    const transform: RoomLocalTransform = { position: { x: 0, y: 0, z: 0 }, rotation: ninetyDegreeYRotation };
    const pose = transformToPose(transform);
    expect(pose.rotation.x).toBeCloseTo(ninetyDegreeYRotation.x, 12);
    expect(pose.rotation.y).toBeCloseTo(ninetyDegreeYRotation.y, 12);
    expect(pose.rotation.z).toBeCloseTo(ninetyDegreeYRotation.z, 12);
    expect(pose.rotation.w).toBeCloseTo(ninetyDegreeYRotation.w, 12);
  });

  it("round-trips a combined transform (non-zero position AND non-identity rotation), preserving both independently", () => {
    const transform: RoomLocalTransform = { position: { x: 3, y: 0.8, z: -1.2 }, rotation: ninetyDegreeYRotation };
    const pose = transformToPose(transform);
    expect(pose.position).toEqual({ x: 3, y: 0.8, z: -1.2 });
    expect(pose.rotation.x).toBeCloseTo(ninetyDegreeYRotation.x, 12);
    expect(pose.rotation.y).toBeCloseTo(ninetyDegreeYRotation.y, 12);
    expect(pose.rotation.z).toBeCloseTo(ninetyDegreeYRotation.z, 12);
    expect(pose.rotation.w).toBeCloseTo(ninetyDegreeYRotation.w, 12);
  });
});

describe("withUpdatedPosition — changes only translation, never rotation", () => {
  it("replaces position and copies rotation verbatim (byte-for-byte) for a non-identity rotation", () => {
    const original: RoomLocalTransform = { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation };
    const updated = withUpdatedPosition(original, { x: 5, y: 0, z: -3 });

    expect(updated.position).toEqual({ x: 5, y: 0, z: -3 });
    expect(updated.rotation.x).toBe(original.rotation.x);
    expect(updated.rotation.y).toBe(original.rotation.y);
    expect(updated.rotation.z).toBe(original.rotation.z);
    expect(updated.rotation.w).toBe(original.rotation.w);
  });

  it("does not mutate the original transform", () => {
    const original: RoomLocalTransform = { position: { x: 1, y: 0, z: 1 }, rotation: identityRotation };
    withUpdatedPosition(original, { x: 9, y: 0, z: 9 });
    expect(original.position).toEqual({ x: 1, y: 0, z: 1 });
  });
});

describe("withUpdatedRotation — changes only rotation, never translation", () => {
  it("replaces rotation and copies position verbatim (byte-for-byte)", () => {
    const original: RoomLocalTransform = { position: { x: 1, y: 0, z: 1 }, rotation: identityRotation };
    const updated = withUpdatedRotation(original, ninetyDegreeYRotation);

    expect(updated.rotation).toEqual(ninetyDegreeYRotation);
    expect(updated.position.x).toBe(original.position.x);
    expect(updated.position.y).toBe(original.position.y);
    expect(updated.position.z).toBe(original.position.z);
  });

  it("does not mutate the original transform", () => {
    const original: RoomLocalTransform = { position: { x: 1, y: 0, z: 1 }, rotation: identityRotation };
    withUpdatedRotation(original, ninetyDegreeYRotation);
    expect(original.rotation).toEqual(identityRotation);
  });
});

describe("yawDegreesToQuaternion / quaternionToYawDegrees — Y-axis-only rotation round trip", () => {
  it("0 degrees is the identity quaternion", () => {
    const q = yawDegreesToQuaternion(0);
    expect(q.x).toBeCloseTo(0);
    expect(q.y).toBeCloseTo(0);
    expect(q.z).toBeCloseTo(0);
    expect(q.w).toBeCloseTo(1);
  });

  it("90 degrees matches the pinned reference quaternion", () => {
    const q = yawDegreesToQuaternion(90);
    expect(q.x).toBeCloseTo(ninetyDegreeYRotation.x);
    expect(q.y).toBeCloseTo(ninetyDegreeYRotation.y);
    expect(q.z).toBeCloseTo(ninetyDegreeYRotation.z);
    expect(q.w).toBeCloseTo(ninetyDegreeYRotation.w);
  });

  it("round-trips yaw degrees through a quaternion and back, modulo 360", () => {
    for (const degrees of [0, 45, 90, 137, 200, 359]) {
      const yaw = quaternionToYawDegrees(yawDegreesToQuaternion(degrees));
      const normalizedYaw = ((yaw % 360) + 360) % 360;
      const normalizedDegrees = ((degrees % 360) + 360) % 360;
      expect(normalizedYaw).toBeCloseTo(normalizedDegrees, 5);
    }
  });
});

describe("projectScene — walls", () => {
  it("computes midpoint and length for an axis-aligned wall", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 4, y: 0, z: 0 },
          thicknessStatus: "estimated",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    const scene = projectScene(draft);
    expect(scene.walls).toHaveLength(1);
    const wall = scene.walls[0];
    expect(wall.center).toEqual({ x: 2, y: 0, z: 0 });
    expect(wall.length).toBeCloseTo(4, 9);
  });

  it("computes length correctly for a diagonal wall", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 3, y: 0, z: 4 },
          thicknessStatus: "estimated",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    const scene = projectScene(draft);
    expect(scene.walls[0].length).toBeCloseTo(5, 9);
    expect(scene.walls[0].center).toEqual({ x: 1.5, y: 0, z: 2 });
  });

  it("derives yaw 0 for a wall running along +X", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 5, y: 0, z: 0 },
          thicknessStatus: "estimated",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    expect(projectScene(draft).walls[0].yaw).toBeCloseTo(0, 9);
  });

  it("derives yaw of 90 degrees (PI/2) for a wall running along +Z", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 0, y: 0, z: 5 },
          thicknessStatus: "estimated",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    expect(projectScene(draft).walls[0].yaw).toBeCloseTo(Math.PI / 2, 9);
  });

  it("applies the documented visual-only height/thickness fallback when both are absent, and flags them as fallback", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 2, y: 0, z: 0 },
          thicknessStatus: "estimated",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    const wall = projectScene(draft).walls[0];
    expect(wall.height).toBe(DEFAULT_WALL_HEIGHT_METERS);
    expect(wall.heightIsFallback).toBe(true);
    expect(wall.thickness).toBe(DEFAULT_WALL_THICKNESS_METERS);
    expect(wall.thicknessIsFallback).toBe(true);
  });

  it("uses real height/thickness when present, and does not flag them as fallback", () => {
    const draft = emptyDraft({
      walls: [
        {
          id: "wall_1",
          start: { x: 0, y: 0, z: 0 },
          end: { x: 2, y: 0, z: 0 },
          height: 2.7,
          thickness: 0.15,
          thicknessStatus: "unconfirmed",
          provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
        },
      ],
    });

    const wall = projectScene(draft).walls[0];
    expect(wall.height).toBe(2.7);
    expect(wall.heightIsFallback).toBe(false);
    expect(wall.thickness).toBe(0.15);
    expect(wall.thicknessIsFallback).toBe(false);
  });
});

describe("projectScene — openings", () => {
  it("maps position/rotation straight from the opening's transform (zero-remap coordinate contract)", () => {
    const draft = emptyDraft({
      openings: [
        {
          id: "opening_1",
          parentWallId: "wall_1",
          kind: "door",
          profile: "rectangle",
          transform: { position: { x: 2, y: 0, z: 0 }, rotation: identityRotation },
          width: 0.9,
          height: 2.1,
          provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
        },
      ],
    });

    const opening = projectScene(draft).openings[0];
    expect(opening.position).toEqual({ x: 2, y: 0, z: 0 });
    expect(opening.rotation).toEqual(identityRotation);
    expect(opening.width).toBe(0.9);
    expect(opening.height).toBe(2.1);
    expect(opening.widthIsFallback).toBe(false);
    expect(opening.heightIsFallback).toBe(false);
  });

  it("applies documented visual-only width/height fallbacks when absent", () => {
    const draft = emptyDraft({
      openings: [
        {
          id: "opening_1",
          kind: "window",
          profile: "rectangle",
          transform: { position: { x: 1, y: 0, z: 0 }, rotation: identityRotation },
          provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
        },
      ],
    });

    const opening = projectScene(draft).openings[0];
    expect(opening.width).toBe(DEFAULT_OPENING_WIDTH_METERS);
    expect(opening.height).toBe(DEFAULT_OPENING_HEIGHT_METERS);
    expect(opening.widthIsFallback).toBe(true);
    expect(opening.heightIsFallback).toBe(true);
  });
});

describe("projectScene — objects, fixtures, constraints (transform-bearing elements)", () => {
  it("maps object position/rotation/dimensions straight from canonical fields", () => {
    const draft = emptyDraft({
      objects: [
        {
          id: "object_1",
          category: "sofa",
          transform: { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation },
          dimensions: { x: 2, y: 0.8, z: 1 },
          provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" },
        },
      ],
    });

    const object = projectScene(draft).objects[0];
    expect(object.position).toEqual({ x: 1, y: 0, z: 1 });
    expect(object.rotation.y).toBeCloseTo(ninetyDegreeYRotation.y, 12);
    expect(object.dimensions).toEqual({ x: 2, y: 0.8, z: 1 });
    expect(object.dimensionsAreFallback).toBe(false);
  });

  it("applies the fixture-dimension fallback (matching ElementInspector's existing 0.5 default) when dimensions are absent", () => {
    const draft = emptyDraft({
      fixtures: [
        {
          id: "fixture_1",
          category: "boiler",
          transform: { position: { x: 0, y: 0, z: 0 }, rotation: identityRotation },
          createdBy: "contractor",
        },
      ],
    });

    const fixture = projectScene(draft).fixtures[0];
    expect(fixture.dimensions).toEqual(DEFAULT_FIXTURE_DIMENSIONS);
    expect(fixture.dimensionsAreFallback).toBe(true);
  });

  it("carries a canonical visualAsset binding through fixture projection unchanged", () => {
    const draft = emptyDraft({
      fixtures: [
        {
          id: "fixture_1",
          category: "boiler",
          transform: { position: { x: 0, y: 0, z: 0 }, rotation: identityRotation },
          createdBy: "contractor",
          visualAsset: { assetId: "custom-boiler-asset", version: 3 },
        },
      ],
    });

    const fixture = projectScene(draft).fixtures[0];
    expect(fixture.visualAsset).toEqual({ assetId: "custom-boiler-asset", version: 3 });
  });

  it("leaves fixture visualAsset undefined when no canonical binding is present", () => {
    const draft = emptyDraft({
      fixtures: [
        {
          id: "fixture_1",
          category: "boiler",
          transform: { position: { x: 0, y: 0, z: 0 }, rotation: identityRotation },
          createdBy: "contractor",
        },
      ],
    });

    const fixture = projectScene(draft).fixtures[0];
    expect(fixture.visualAsset).toBeUndefined();
  });

  it("carries a canonical visualAsset binding through object projection unchanged", () => {
    const draft = emptyDraft({
      objects: [
        {
          id: "object_1",
          category: "sofa",
          transform: { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation },
          dimensions: { x: 2, y: 0.8, z: 1 },
          provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" },
          visualAsset: { assetId: "custom-sofa-asset", version: 1 },
        },
      ],
    });

    const object = projectScene(draft).objects[0];
    expect(object.visualAsset).toEqual({ assetId: "custom-sofa-asset", version: 1 });
  });

  it("leaves object visualAsset undefined when no canonical binding is present", () => {
    const draft = emptyDraft({
      objects: [
        {
          id: "object_1",
          category: "sofa",
          transform: { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation },
          dimensions: { x: 2, y: 0.8, z: 1 },
          provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" },
        },
      ],
    });

    const object = projectScene(draft).objects[0];
    expect(object.visualAsset).toBeUndefined();
  });

  it("carries a persisted appearance override through object and fixture projection unchanged", () => {
    const appearance = { baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "matte", metallic: false };
    const draft = emptyDraft({
      objects: [
        {
          id: "object_1",
          category: "sofa",
          transform: { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation },
          dimensions: { x: 2, y: 0.8, z: 1 },
          provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" },
          appearance,
        },
      ],
      fixtures: [
        {
          id: "fixture_1",
          category: "boiler",
          transform: { position: { x: 0, y: 0, z: 0 }, rotation: identityRotation },
          createdBy: "contractor",
          appearance,
        },
      ],
    });

    const scene = projectScene(draft);
    expect(scene.objects[0]!.appearance).toEqual(appearance);
    expect(scene.fixtures[0]!.appearance).toEqual(appearance);
  });

  it("leaves appearance undefined when no persisted override is present", () => {
    const draft = emptyDraft({
      objects: [
        {
          id: "object_1",
          category: "sofa",
          transform: { position: { x: 1, y: 0, z: 1 }, rotation: ninetyDegreeYRotation },
          dimensions: { x: 2, y: 0.8, z: 1 },
          provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" },
        },
      ],
    });

    const object = projectScene(draft).objects[0];
    expect(object.appearance).toBeUndefined();
  });

  it("maps constraint position/rotation/dimensions straight from canonical fields", () => {
    const draft = emptyDraft({
      constraints: [
        {
          id: "constraint_1",
          kind: "column",
          transform: { position: { x: 3, y: 0, z: 3 }, rotation: identityRotation },
          dimensions: { x: 0.4, y: 2.4, z: 0.4 },
          createdBy: "contractor",
        },
      ],
    });

    const constraint = projectScene(draft).constraints[0];
    expect(constraint.position).toEqual({ x: 3, y: 0, z: 3 });
    expect(constraint.dimensions).toEqual({ x: 0.4, y: 2.4, z: 0.4 });
    expect(constraint.dimensionsAreFallback).toBe(false);
  });
});

describe("projectScene — service points (unoriented markers, no transform)", () => {
  it("maps a service point's bare position straight through", () => {
    const draft = emptyDraft({
      servicePoints: [{ id: "sp_1", kind: "plumbing", position: { x: 1, y: 0, z: 2 }, createdBy: "contractor" }],
    });

    const sp = projectScene(draft).servicePoints[0];
    expect(sp.position).toEqual({ x: 1, y: 0, z: 2 });
    expect(sp.kind).toBe("plumbing");
  });
});

describe("projectScene — null array handling", () => {
  it("treats null walls/openings/objects/fixtures/servicePoints/constraints as empty, not a crash", () => {
    const draft = emptyDraft({
      walls: null,
      openings: null,
      objects: null,
      fixtures: null,
      servicePoints: null,
      constraints: null,
    });

    const scene = projectScene(draft);
    expect(scene.walls).toEqual([]);
    expect(scene.openings).toEqual([]);
    expect(scene.objects).toEqual([]);
    expect(scene.fixtures).toEqual([]);
    expect(scene.servicePoints).toEqual([]);
    expect(scene.constraints).toEqual([]);
  });
});

// M8.5C closure patch §14: TransformControls scale is transient Three.js UI
// state ONLY — this is the one pure function that converts it into
// canonical dimensions (startDimensions * |scale|), the only form that ever
// becomes RoomDraft data. Never called with the scaled dimensions
// themselves as `start` (that would compound on every resize gesture).
describe("dimensionsFromScale — canonical dimensions from transform-start dimensions + transient Three scale", () => {
  it("multiplies each axis by the absolute scale factor", () => {
    const result = dimensionsFromScale({ x: 1, y: 2, z: 0.5 }, { x: 2, y: 1.5, z: 4 });
    expect(result).toEqual({ x: 2, y: 3, z: 2 });
  });

  it("uses the ABSOLUTE value of a negative scale (never a negative or zero canonical dimension)", () => {
    const result = dimensionsFromScale({ x: 1, y: 1, z: 1 }, { x: -2, y: -0.5, z: 1 });
    expect(result.x).toBeGreaterThan(0);
    expect(result.y).toBeGreaterThan(0);
    expect(result).toEqual({ x: 2, y: 0.5, z: 1 });
  });

  it("clamps to a minimum dimension rather than allowing it to collapse to (near) zero", () => {
    const result = dimensionsFromScale({ x: 1, y: 1, z: 1 }, { x: 0.001, y: 1, z: 1 });
    expect(result.x).toBeGreaterThanOrEqual(0.05);
  });
});
