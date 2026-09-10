import type { RoomLocalPoint } from "../../types";
import type { VisualAssetNormalization } from "./types";

// Pure bounds/pivot/scale math — no "three" import. Input bounds are a
// plain {min,max} pair (computed once from a loaded model's Box3 by the
// rendering layer, then passed in as data).
export type AssetBounds = { min: RoomLocalPoint; max: RoomLocalPoint };

export type AssetNormalizationInput = {
  bounds: AssetBounds;
  dimensions: RoomLocalPoint;
  normalization: VisualAssetNormalization;
};

export type AssetNormalizationResult =
  | { offset: RoomLocalPoint; scale: RoomLocalPoint }
  | { fallback: true; reason: string };

const EPSILON = 1e-6;

// A loaded mesh's intrinsic bounds are NEVER allowed to dictate Renovex
// dimensions — RoomDraft's authoritative `dimensions` is construction
// truth (hard invariant, RP4C4 kickoff §13-16). This function only
// computes how to fit the LOADED VISUAL into that truth; it never
// reports the visual's own size as a candidate dimension.
export function computeAssetNormalization(input: AssetNormalizationInput): AssetNormalizationResult {
  const { bounds, dimensions, normalization } = input;

  if (normalization.intrinsicUnit !== "meters") {
    return { fallback: true, reason: `unsupported intrinsicUnit "${normalization.intrinsicUnit}"` };
  }
  if (normalization.upAxis !== "y") {
    return { fallback: true, reason: `unsupported upAxis "${normalization.upAxis}"` };
  }
  if (normalization.pivot !== "center-bottom" && normalization.pivot !== "center") {
    return { fallback: true, reason: `unsupported pivot "${normalization.pivot}"` };
  }

  const sizeX = bounds.max.x - bounds.min.x;
  const sizeY = bounds.max.y - bounds.min.y;
  const sizeZ = bounds.max.z - bounds.min.z;

  if (sizeX <= EPSILON || sizeY <= EPSILON || sizeZ <= EPSILON) {
    return { fallback: true, reason: "degenerate or zero intrinsic bounds" };
  }

  const centerX = (bounds.min.x + bounds.max.x) / 2;
  const centerZ = (bounds.min.z + bounds.max.z) / 2;
  const pivotY = normalization.pivot === "center-bottom" ? bounds.min.y : (bounds.min.y + bounds.max.y) / 2;

  return {
    offset: { x: zeroSafe(-centerX), y: zeroSafe(-pivotY), z: zeroSafe(-centerZ) },
    scale: { x: dimensions.x / sizeX, y: dimensions.y / sizeY, z: dimensions.z / sizeZ },
  };
}

// Avoids emitting "-0" (a valid but surprising IEEE-754 value produced by
// negating an exact zero) — downstream position/offset consumers should
// only ever see a plain 0.
function zeroSafe(value: number): number {
  return value === 0 ? 0 : value;
}
