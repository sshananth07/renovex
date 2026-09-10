"use client";

import { useLoader } from "@react-three/fiber";
import { USDLoader } from "three/addons/loaders/USDLoader.js";
import { resolveSourceUrl } from "../three/assets/sourceUrl";
import { useNormalizedAsset } from "./useNormalizedAsset";
import type { VisualAssetProps } from "./VisualAsset";

// Calls useLoader(USDLoader, ...) unconditionally on every render — never
// behind a condition — so this component alone carries the "usdz" loader
// hook, mirroring GLTFVisualAsset's contract exactly. No drei useUSDZ
// hook exists; useLoader is R3F's generic Suspense-compatible loader
// hook, confirmed to work with any Loader subclass including USDLoader.
//
// loadUrl is REQUIRED for an "authorized" descriptor and optional for
// "builtin" — see VisualAssetProps' doc comment in VisualAsset.tsx.
export default function USDVisualAsset({ descriptor, dimensions, loadUrl, appearance }: VisualAssetProps) {
  const url = loadUrl ?? resolveSourceUrl(descriptor.source as Extract<typeof descriptor.source, { kind: "builtin" }>);
  const scene = useLoader(USDLoader, url);
  const normalized = useNormalizedAsset(scene, dimensions, descriptor.normalization, appearance);

  if (!normalized) return null;

  return (
    <group position={[normalized.offset.x, normalized.offset.y, normalized.offset.z]} scale={[normalized.scale.x, normalized.scale.y, normalized.scale.z]}>
      <primitive object={normalized.scene} />
    </group>
  );
}
