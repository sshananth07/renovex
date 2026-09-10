import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as THREE from "three";
import { useNormalizedAsset } from "./useNormalizedAsset";
import type { VisualAssetNormalization } from "../three/assets/types";

vi.mock("@react-three/fiber", () => ({ invalidate: vi.fn() }));

const CENTER_BOTTOM: VisualAssetNormalization = { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" };
const CENTER: VisualAssetNormalization = { intrinsicUnit: "meters", upAxis: "y", pivot: "center" };
const NORMALIZATION = CENTER_BOTTOM;

function unitCubeScene(): THREE.Group {
  const group = new THREE.Group();
  const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshStandardMaterial());
  group.add(mesh);
  return group;
}

describe("useNormalizedAsset", () => {
  it("returns null (fallback) when the loaded scene has degenerate bounds", () => {
    const { result } = renderHook(() => useNormalizedAsset(new THREE.Group(), { x: 1, y: 1, z: 1 }, NORMALIZATION));
    expect(result.current).toBeNull();
  });

  it("returns a cloned scene distinct from the loader's cached source object", () => {
    const cached = unitCubeScene();
    const { result } = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION));
    expect(result.current).not.toBeNull();
    expect(result.current!.scene).not.toBe(cached);
  });

  it("gives two mounted instances of the same cached source independent scene graphs but identical (shared) geometry/material references", () => {
    const cached = unitCubeScene();
    const cachedMesh = cached.children[0] as THREE.Mesh;

    const first = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION));
    const second = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION));

    const firstScene = first.result.current!.scene;
    const secondScene = second.result.current!.scene;

    // Independent hierarchies: two distinct clones, not the same instance.
    expect(firstScene).not.toBe(secondScene);

    // Shared, non-duplicated GPU resources: both clones' leaf mesh
    // references the SAME geometry/material as the cached source and as
    // each other — proving no duplicate GPU resource was created per
    // instance.
    const firstMesh = (firstScene as THREE.Group).children[0] as THREE.Mesh;
    const secondMesh = (secondScene as THREE.Group).children[0] as THREE.Mesh;
    expect(firstMesh.geometry).toBe(cachedMesh.geometry);
    expect(secondMesh.geometry).toBe(cachedMesh.geometry);
    expect(firstMesh.material).toBe(cachedMesh.material);
    expect(secondMesh.material).toBe(cachedMesh.material);
  });

  // FixtureMesh/ObjectMesh's CanonicalElementRoot <group> is
  // center-anchored: its own local origin already represents the
  // element's VERTICAL CENTER (the group's world position is pre-offset
  // by +dimensions.y/2 so the whole box's bottom lands at floor level —
  // see Spatial3DViewport.tsx's FixtureMesh comment). A "center" pivot
  // asset therefore needs NO further vertical adjustment (its own pivot
  // is already the mesh's center, matching the group's convention
  // exactly) — but a "center-bottom" pivot asset's raw offset would put
  // the mesh's BOTTOM at local origin, which is dimensions.y/2 too high
  // relative to the group's center-anchored convention, so it must be
  // shifted down by that amount here.
  it("shifts a center-bottom pivot asset so it ends up centered on the CanonicalElementRoot's local origin, matching the procedural fallback's own placement", () => {
    // unitCubeScene() is a 1x1x1 box centered at its own local origin:
    // intrinsic bounds are [-0.5,0.5] on every axis. A raw center-bottom
    // normalization would put this box's bottom (-0.5) at local origin
    // (raw offset.y = +0.5) — but the group's own convention is
    // center-anchored (its local origin is the box's vertical CENTER,
    // matching the procedural fallback box which is centered at [0,0,0]
    // with no offset). The group-convention adjustment must cancel the
    // raw offset back to 0 in this case, so the final box ends up
    // centered on the group's origin exactly like the procedural
    // fallback — not floating above it with its bottom at the origin.
    const cached = unitCubeScene();
    const { result } = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, CENTER_BOTTOM));
    expect(result.current!.offset.y).toBeCloseTo(0);
  });

  it("distinguishes center-bottom from center for an asymmetric (off-center) intrinsic mesh", () => {
    // A box whose intrinsic bounds are NOT symmetric around its own
    // local origin: y in [0, 2] (bottom sits at the mesh's own origin,
    // not below it) with an authoritative height of 2m. A center-bottom
    // pivot should still land the box centered on the group's origin
    // (spanning -1..+1) — the SAME final placement as a symmetric box
    // would get — proving the adjustment is driven by pivot policy and
    // authoritative dimensions, not by assuming any particular intrinsic
    // bounds shape.
    const asymmetric = new THREE.Group();
    const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshStandardMaterial());
    mesh.position.set(0, 1, 0); // bounds become y in [0, 2]
    asymmetric.add(mesh);

    const { result } = renderHook(() => useNormalizedAsset(asymmetric, { x: 1, y: 2, z: 1 }, CENTER_BOTTOM));
    // Raw center-bottom offset.y = -min.y = 0; group-convention shift =
    // -dimensions.y/2 = -1; final = -1.
    expect(result.current!.offset.y).toBeCloseTo(-1);
  });

  it("applies no extra vertical adjustment for a center pivot asset, since it already matches the group's center-anchored convention", () => {
    // A unit cube centered at the origin (min.y=-0.5, max.y=0.5) with a
    // center pivot: computeAssetNormalization's raw offset.y is 0, and
    // no group-convention adjustment applies for "center".
    const centered = new THREE.Group();
    const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshStandardMaterial());
    centered.add(mesh);
    const { result } = renderHook(() => useNormalizedAsset(centered, { x: 1, y: 1, z: 1 }, CENTER));
    expect(result.current!.offset.y).toBeCloseTo(0);
  });

  describe("appearance override (plan's exact matte/satin/glossy -> roughness table)", () => {
    it.each([
      ["matte", 0.82],
      ["satin", 0.48],
      ["glossy", 0.18],
    ] as const)("maps roughness label %s to %f", (label, expectedRoughness) => {
      const cached = unitCubeScene();
      const { result } = renderHook(() =>
        useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION, {
          baseColor: "#2f4f3a",
          materialFamily: "fabric",
          roughness: label,
          metallic: false,
        }),
      );
      const mesh = (result.current!.scene as THREE.Group).children[0] as THREE.Mesh;
      const material = mesh.material as THREE.MeshStandardMaterial;
      expect(material.roughness).toBeCloseTo(expectedRoughness);
      expect(material.color.getHexString()).toBe("2f4f3a");
    });

    it.each([
      [true, 1],
      [false, 0],
    ] as const)("maps metallic=%s to metalness %f", (metallic, expectedMetalness) => {
      const cached = unitCubeScene();
      const { result } = renderHook(() =>
        useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION, {
          baseColor: "#c8a464",
          materialFamily: "metal",
          roughness: "satin",
          metallic,
        }),
      );
      const mesh = (result.current!.scene as THREE.Group).children[0] as THREE.Mesh;
      const material = mesh.material as THREE.MeshStandardMaterial;
      expect(material.metalness).toBe(expectedMetalness);
    });

    it("clones the material rather than mutating the shared cached one, so a second instance's appearance never bleeds onto the first", () => {
      const cached = unitCubeScene();
      const cachedMaterial = (cached.children[0] as THREE.Mesh).material as THREE.MeshStandardMaterial;

      const first = renderHook(() =>
        useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION, {
          baseColor: "#ff0000",
          materialFamily: "fabric",
          roughness: "matte",
          metallic: false,
        }),
      );
      const second = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION));

      const firstMaterial = ((first.result.current!.scene as THREE.Group).children[0] as THREE.Mesh)
        .material as THREE.MeshStandardMaterial;
      const secondMaterial = ((second.result.current!.scene as THREE.Group).children[0] as THREE.Mesh)
        .material as THREE.MeshStandardMaterial;

      expect(firstMaterial).not.toBe(cachedMaterial);
      expect(firstMaterial.color.getHexString()).toBe("ff0000");
      // The second, appearance-less instance still has the cached material's
      // ORIGINAL color/finish — the first instance's override never mutated it.
      expect(secondMaterial).toBe(cachedMaterial);
      expect(secondMaterial.color.getHexString()).not.toBe("ff0000");
    });

    it("leaves materials untouched when no appearance is provided", () => {
      const cached = unitCubeScene();
      const cachedMaterial = (cached.children[0] as THREE.Mesh).material;
      const { result } = renderHook(() => useNormalizedAsset(cached, { x: 1, y: 1, z: 1 }, NORMALIZATION));
      const mesh = (result.current!.scene as THREE.Group).children[0] as THREE.Mesh;
      expect(mesh.material).toBe(cachedMaterial);
    });
  });
});
