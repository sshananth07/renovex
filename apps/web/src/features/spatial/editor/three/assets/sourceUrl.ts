import type { VisualAssetSource } from "./types";

// Resolves a "builtin" VisualAssetSource to its fetchable static path. No
// branch accepts an arbitrary string — asset locations only ever come from
// the static registry, never user input, a query string, or RoomDraft data
// (RP4C4 §27 security requirement). Deliberately scoped to "builtin" only
// (RP4D): an "authorized" source carries no URL at all — its loadUrl comes
// exclusively from a genuine successful useVisualAssetAccess response,
// resolved inside AuthorizedVisualAsset.tsx, never fabricated here.
export function resolveSourceUrl(source: Extract<VisualAssetSource, { kind: "builtin" }>): string {
  return source.path;
}
