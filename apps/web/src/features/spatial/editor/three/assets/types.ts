// Metadata-only visual-asset contract for RP4C4/RP4D. No React/Three
// imports — this describes WHAT asset to render, never HOW to render it.
// The rendering layer (components/VisualAsset.tsx and friends) is the only
// place that turns this data into actual GPU resources.
//
// RP4D added a canonical, PERSISTED per-element visual-asset reference
// (RoomDraftFixture.visualAsset / RoomDraftObject.visualAsset —
// backend/internal/spatial/roomdraft.go's VisualAssetRef, mirrored in
// Swift) — but it is deliberately narrow: `{assetId, version}` identity
// only, never a URL, never a storage key, never format/normalization
// metadata. The REGISTRY below remains the Web-only category-default
// rendering projection RP4C4 established; RP4D's binding is a HIGHER-
// priority tier resolved ahead of it (see resolveAsset.ts's
// VisualResolution), not a replacement for it.

export type VisualAssetFormat = "glb" | "gltf" | "usdz";

export type VisualAssetSource =
  | { kind: "builtin"; path: string }
  | { kind: "authorized"; assetId: string; version: number };
// "authorized" NEVER carries a url — the identity/access separation is a
// hard invariant (RP4D): a VisualAssetDescriptor identifies WHAT asset,
// never WHERE to fetch its bytes from. The actual short-lived access URL
// is a separate runtime value (VisualAssetAccess, below), resolved only
// inside AuthorizedVisualAsset.tsx and passed down as an explicit loadUrl
// prop — never merged into this descriptor, never cached alongside it.

export type VisualAssetNormalization = {
  intrinsicUnit: "meters";
  upAxis: "y";
  pivot: "center-bottom" | "center";
};

export type VisualAssetDescriptor = {
  id: string;
  version: number;
  appliesTo:
    | { elementKind: "fixture"; category: string }
    | { elementKind: "object"; category: string };
  format: VisualAssetFormat;
  source: VisualAssetSource;
  normalization: VisualAssetNormalization;
  displayName: string;
  license?: { name: string; attribution?: string; source?: string };
};

// VisualAssetAccess is the shape POST /spatial/visual-assets/{assetId}/versions/{version}/access
// returns (backend/internal/spatial/handler.go's createVisualAssetAccessOutput) —
// real, complete, server-verified metadata plus a short-lived loadUrl.
// A VisualAssetDescriptor for an "authorized" source is only ever
// constructed FROM a successful VisualAssetAccess response (inside
// AuthorizedVisualAsset.tsx) — never fabricated ahead of time.
export type VisualAssetAccess = {
  format: VisualAssetFormat;
  displayName: string;
  normalization: VisualAssetNormalization;
  contentType: string;
  byteCount: number;
  checksum: string;
  loadUrl: string;
  expiresAt: string;
};

// VisualResolution is resolveVisualAsset's actual return type — a
// discriminated union, never a fabricated partial VisualAssetDescriptor.
// The "authorized" tier carries only the canonical identity
// (ref: {assetId, version}) plus the ALREADY-COMPUTED category-default
// descriptor as its own documented fallback target (builtinFallback) —
// the real format/normalization for an authorized asset is filled in
// later, only from a genuine successful VisualAssetAccess response.
export type VisualResolution =
  | { tier: "authorized"; ref: { assetId: string; version: number }; builtinFallback: VisualAssetDescriptor | null }
  | { tier: "builtin"; descriptor: VisualAssetDescriptor }
  | { tier: "procedural" };
