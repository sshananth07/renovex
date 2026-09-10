"use client";

import { useVisualAssetAccess } from "../../queries";
import type { RoomLocalPoint } from "../types";
import type { VisualAssetDescriptor } from "../three/assets/types";
import type { AppearanceSpec } from "../three/sceneProjection";
import AssetFallback from "./AssetFallback";
import VisualAsset from "./VisualAsset";

export type AuthorizedVisualAssetProps = {
  assetRef: { assetId: string; version: number };
  builtinFallback: VisualAssetDescriptor | null;
  dimensions: RoomLocalPoint;
  color: string;
  appearance?: AppearanceSpec;
};

// Owns the ONE useVisualAssetAccess call for an explicit canonical
// binding. Ordinary useQuery errors surface via isError/isPending — they
// do NOT throw during render the way a Suspense-integrated loader does,
// so this component explicitly branches on both states itself, rather
// than assuming AssetErrorBoundary/Suspense (which wrap the subsequent
// real GLTF/USD *load*, not this access-grant fetch) will catch them.
//
// A VisualAssetDescriptor for an "authorized" source is constructed HERE,
// and only from a genuine successful response — never fabricated ahead of
// time (see resolveAsset.ts's VisualResolution). appliesTo is carried from
// the caller's builtinFallback (a Web-rendering concept the server has no
// reason to know) when available; when there is no category match at all,
// the resolved element kind/category are not available here either, so a
// neutral fixture/unknown-category placeholder is used — it is never read
// by anything except AssetErrorBoundary's keying and useNormalizedAsset,
// neither of which branches on it.
export default function AuthorizedVisualAsset({ assetRef, builtinFallback, dimensions, color, appearance }: AuthorizedVisualAssetProps) {
  const access = useVisualAssetAccess(assetRef.assetId, assetRef.version);

  if (access.isPending || access.isError) {
    return builtinFallback ? (
      <VisualAsset descriptor={builtinFallback} dimensions={dimensions} appearance={appearance} />
    ) : (
      <AssetFallback dimensions={dimensions} color={color} appearance={appearance} />
    );
  }

  const descriptor: VisualAssetDescriptor = {
    id: assetRef.assetId,
    version: assetRef.version,
    appliesTo: builtinFallback?.appliesTo ?? { elementKind: "fixture", category: "" },
    format: access.data.asset.format as VisualAssetDescriptor["format"],
    source: { kind: "authorized", assetId: assetRef.assetId, version: assetRef.version },
    // The server DTO carries only `pivot` — intrinsicUnit/upAxis are fixed
    // constants per this project's confirmed coordinate contract (see
    // backend/internal/spatial/visualasset.go's VisualAssetNormalization
    // doc comment), never per-asset configurable, so they are never sent
    // over the wire redundantly.
    normalization: {
      intrinsicUnit: "meters",
      upAxis: "y",
      pivot: access.data.asset.normalization.pivot as VisualAssetDescriptor["normalization"]["pivot"],
    },
    displayName: access.data.asset.displayName,
  };

  return <VisualAsset descriptor={descriptor} dimensions={dimensions} loadUrl={access.data.access.url} appearance={appearance} />;
}
