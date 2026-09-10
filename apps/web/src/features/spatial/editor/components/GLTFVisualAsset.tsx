"use client";

import { useGLTF } from "@react-three/drei";
import { resolveSourceUrl } from "../three/assets/sourceUrl";
import { useNormalizedAsset } from "./useNormalizedAsset";
import type { VisualAssetProps } from "./VisualAsset";

// Calls useGLTF unconditionally on every render — never behind a
// condition — so this component alone carries the "glb"/"gltf" loader
// hook. VisualAsset.tsx's dispatcher decides WHETHER to mount this
// component; once mounted, the hook call itself is never conditional.
//
// loadUrl is REQUIRED for an "authorized" descriptor (there is no static
// path to resolve) and optional for "builtin" (resolved internally) — see
// VisualAssetProps' doc comment.
export default function GLTFVisualAsset({ descriptor, dimensions, loadUrl, appearance }: VisualAssetProps) {
  const url = loadUrl ?? resolveSourceUrl(descriptor.source as Extract<typeof descriptor.source, { kind: "builtin" }>);
  const gltf = useGLTF(url);
  const normalized = useNormalizedAsset(gltf.scene, dimensions, descriptor.normalization, appearance);

  if (!normalized) return null;

  return (
    <group position={[normalized.offset.x, normalized.offset.y, normalized.offset.z]} scale={[normalized.scale.x, normalized.scale.y, normalized.scale.z]}>
      <primitive object={normalized.scene} />
    </group>
  );
}
