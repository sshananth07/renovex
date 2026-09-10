"use client";

import type { RoomLocalPoint } from "../types";
import type { AppearanceSpec } from "../three/sceneProjection";

export type AssetFallbackProps = {
  dimensions: RoomLocalPoint;
  color: string;
  appearance?: AppearanceSpec;
};

// Plan's exact roughness mapping, same table useNormalizedAsset.ts applies
// to real loaded assets — kept as a small local duplicate rather than a
// shared import so this file (the one procedural-rendering path with zero
// "three" module dependency beyond JSX intrinsics) stays trivially simple;
// both copies encode the same fixed plan constant, not independently
// derived values that could drift apart.
const ROUGHNESS_BY_LABEL: Record<string, number> = { matte: 0.82, satin: 0.48, glossy: 0.18 };

// The permanent procedural fallback for a fixture/object with no
// registered visual asset (or one that failed to load/normalize). This
// is the SAME box geometry/material RP4C3 always rendered — extracted
// here so both FixtureMesh/ObjectMesh's no-asset branch and the
// Suspense/error-boundary fallback for a real asset render identically,
// rather than two hand-synced copies. When an appearance override is
// present (RP4E1 persisted appearance or an RP4E2 concept preview), it
// takes over color/roughness/metalness entirely — the caller's plain
// selection/hover `color` is only used when there is no override.
export default function AssetFallback({ dimensions, color, appearance }: AssetFallbackProps) {
  const materialColor = appearance?.baseColor ?? color;
  const roughness = appearance ? (ROUGHNESS_BY_LABEL[appearance.roughness] ?? ROUGHNESS_BY_LABEL.matte!) : undefined;
  const metalness = appearance ? (appearance.metallic ? 1 : 0) : undefined;
  return (
    <mesh>
      <boxGeometry args={[dimensions.x, dimensions.y, dimensions.z]} />
      <meshStandardMaterial color={materialColor} roughness={roughness} metalness={metalness} transparent opacity={0.85} />
    </mesh>
  );
}
