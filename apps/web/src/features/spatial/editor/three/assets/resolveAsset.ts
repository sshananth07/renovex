import { VISUAL_ASSET_REGISTRY } from "./registry";
import type { VisualResolution } from "./types";

export type ResolvableElement = {
  elementKind: "fixture" | "object";
  category: string;
  // RP4D's canonical, persisted per-element binding — {assetId, version}
  // identity only, never format/normalization/URL. Absent (or undefined)
  // means "no explicit binding," falling through to category/procedural
  // resolution exactly as RP4C4 always worked.
  visualAsset?: { assetId: string; version: number };
};

// Pure, deterministic resolution — no React, no Three. Computes the
// category-default ONCE and carries it along as the authorized tier's own
// documented fallback target, rather than recomputing it ad hoc inside a
// render branch. Returns a VisualResolution discriminated union — NEVER a
// fabricated/partial VisualAssetDescriptor for the authorized tier, since
// a descriptor must only ever represent complete, real metadata (the
// actual format/normalization for an authorized asset comes only from a
// genuine successful server response, resolved later inside
// AuthorizedVisualAsset.tsx).
export function resolveVisualAsset(element: ResolvableElement): VisualResolution {
  const builtinFallback =
    VISUAL_ASSET_REGISTRY.find(
      (d) => d.appliesTo.elementKind === element.elementKind && d.appliesTo.category === element.category,
    ) ?? null;

  if (element.visualAsset) {
    return { tier: "authorized", ref: element.visualAsset, builtinFallback };
  }
  return builtinFallback ? { tier: "builtin", descriptor: builtinFallback } : { tier: "procedural" };
}
