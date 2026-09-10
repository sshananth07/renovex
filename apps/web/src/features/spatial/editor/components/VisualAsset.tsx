"use client";

import type { RoomLocalPoint } from "../types";
import type { VisualAssetDescriptor } from "../three/assets/types";
import type { AppearanceSpec } from "../three/sceneProjection";
import GLTFVisualAsset from "./GLTFVisualAsset";
import USDVisualAsset from "./USDVisualAsset";

export type VisualAssetProps = {
  descriptor: VisualAssetDescriptor;
  dimensions: RoomLocalPoint;
  // For a "builtin" descriptor, omit this — the loader components resolve
  // their own URL internally via resolveSourceUrl(descriptor.source). For
  // an "authorized" descriptor (RP4D), this is REQUIRED and must come from
  // a genuine successful useVisualAssetAccess response — descriptor.source
  // for "authorized" carries only {assetId, version}, deliberately no URL
  // (identity/access separation is a hard invariant; see three/assets/types.ts).
  loadUrl?: string;
  // Persisted or candidate material override (RP4E1 appearance /
  // RP4E2 concept preview) — mapped to roughness/metalness inside
  // useNormalizedAsset (plan's exact matte/satin/glossy -> roughness table).
  // Omitted entirely means "render the asset's own baked materials
  // unchanged," never a fabricated default appearance.
  appearance?: AppearanceSpec;
};

// Pure format dispatcher — calls NO loader hook itself. useGLTF and
// useLoader(USDLoader, ...) must each be called unconditionally by their
// own component (React's Rules of Hooks forbid a hook called on one
// branch and not another within the same component across renders); this
// component only decides WHICH of those two always-hook-calling
// components to mount, never calls either loader hook directly.
export default function VisualAsset({ descriptor, dimensions, loadUrl, appearance }: VisualAssetProps) {
  if (descriptor.format === "usdz") {
    return <USDVisualAsset descriptor={descriptor} dimensions={dimensions} loadUrl={loadUrl} appearance={appearance} />;
  }
  return <GLTFVisualAsset descriptor={descriptor} dimensions={dimensions} loadUrl={loadUrl} appearance={appearance} />;
}
