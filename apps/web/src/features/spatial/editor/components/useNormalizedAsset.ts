import { useMemo } from "react";
import { invalidate } from "@react-three/fiber";
import * as THREE from "three";
import { computeAssetNormalization } from "../three/assets/normalization";
import type { VisualAssetNormalization } from "../three/assets/types";
import type { AppearanceSpec } from "../three/sceneProjection";
import type { RoomLocalPoint } from "../types";

// Plan's exact roughness mapping (M8.5C §"Material-only preview passes
// appearance into the current visual path immediately"): matte/satin/glossy
// map to fixed PBR roughness values, materialFamily is retained for
// copy/future use only and never fabricates a texture.
const ROUGHNESS_BY_LABEL: Record<string, number> = { matte: 0.82, satin: 0.48, glossy: 0.18 };

// Applies a persisted/candidate appearance override to every
// MeshStandardMaterial (or subclass, e.g. MeshPhysicalMaterial) found in the
// cloned scene graph — cloning each material first so this never mutates
// the shared, cached material every OTHER instance of this asset still
// references (the same "never bleed a color change onto every instance"
// invariant ObjectMesh/FixtureMesh's own selection overlay comment
// documents). baseColor tints via .color, never replaces geometry/texture.
function applyAppearance(root: THREE.Object3D, appearance: AppearanceSpec): void {
  const roughness = ROUGHNESS_BY_LABEL[appearance.roughness] ?? ROUGHNESS_BY_LABEL.matte!;
  const metalness = appearance.metallic ? 1 : 0;
  const color = new THREE.Color(appearance.baseColor);

  root.traverse((child) => {
    if (!(child instanceof THREE.Mesh)) return;
    const materials = Array.isArray(child.material) ? child.material : [child.material];
    child.material = Array.isArray(child.material)
      ? materials.map((m) => cloneWithAppearance(m, color, roughness, metalness))
      : cloneWithAppearance(materials[0]!, color, roughness, metalness);
  });
}

function cloneWithAppearance(material: THREE.Material, color: THREE.Color, roughness: number, metalness: number): THREE.Material {
  const clone = material.clone();
  if (clone instanceof THREE.MeshStandardMaterial || clone instanceof THREE.MeshPhysicalMaterial) {
    clone.color = color;
    clone.roughness = roughness;
    clone.metalness = metalness;
  }
  return clone;
}

export type NormalizedAsset = {
  // A per-instance clone of the loaded scene graph — see cloning note
  // below. Geometries/materials inside it are NOT cloned; they remain
  // the same shared, cached objects every instance of this asset uses.
  scene: THREE.Object3D;
  // Offset/scale to apply to the NormalizationRoot wrapping `scene`.
  offset: RoomLocalPoint;
  scale: RoomLocalPoint;
} | null; // null means "fall back to the procedural renderer" — either
// the descriptor's normalization metadata is unsupported, or the loaded
// mesh's intrinsic bounds are degenerate.

// Shared post-load contract for both GLTFVisualAsset and USDVisualAsset:
// clone the loaded scene per-instance (kickoff §25 — duplicate fixtures
// sharing one asset must not share one mutable Object3D graph, but DO
// share geometry/material/texture, which this function never touches),
// compute intrinsic bounds once, and run the pure normalization math.
// Called unconditionally by each loader-specific component — never
// itself calls a loader hook, so it carries no Rules-of-Hooks risk when
// reused by two components with different loaders.
export function useNormalizedAsset(
  loadedScene: THREE.Object3D,
  dimensions: RoomLocalPoint,
  normalization: VisualAssetNormalization,
  appearance?: AppearanceSpec,
): NormalizedAsset {
  return useMemo(() => {
    const clone = loadedScene.clone(true);
    if (appearance) applyAppearance(clone, appearance);
    const box = new THREE.Box3().setFromObject(clone);
    const result = computeAssetNormalization({
      bounds: { min: { x: box.min.x, y: box.min.y, z: box.min.z }, max: { x: box.max.x, y: box.max.y, z: box.max.z } },
      dimensions,
      normalization,
    });
    if ("fallback" in result) {
      return null;
    }

    // FixtureMesh/ObjectMesh's CanonicalElementRoot <group> is
    // center-anchored: its own local origin already represents the
    // element's vertical CENTER (the group's world position is
    // pre-offset by +dimensions.y/2 so the whole element's bottom lands
    // at floor level — see Spatial3DViewport.tsx). computeAssetNormalization's
    // raw offset places the descriptor's OWN declared pivot at local
    // origin: for "center" that already matches this group's convention
    // (no adjustment needed), but for "center-bottom" it would put the
    // mesh's bottom at local origin — half the authoritative height too
    // high relative to the group's center-anchored convention — so shift
    // it down by that amount here. This is a rendering-layer convention
    // fix, not a pivot-policy concern, which is why it lives here and not
    // in the pure normalization.ts.
    const groupConventionShiftY = normalization.pivot === "center-bottom" ? -dimensions.y / 2 : 0;

    // A demand-frameloop Canvas won't automatically re-render when this
    // Suspense boundary resolves off the R3F render cycle.
    invalidate();
    return {
      scene: clone,
      offset: { x: result.offset.x, y: result.offset.y + groupConventionShiftY, z: result.offset.z },
      scale: result.scale,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- loadedScene identity from the shared loader cache is the correct re-run trigger; dimensions/normalization come from a stable resolved descriptor. appearance IS a real re-clone trigger (a material-only concept preview changing color/finish on an already-loaded asset).
  }, [loadedScene, appearance?.baseColor, appearance?.roughness, appearance?.metallic]);
}
