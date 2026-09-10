import type { VisualAssetDescriptor } from "./types";

// Small, deterministic, project-original builtin catalogue (RP4C4 §10):
// self-generated via scripts/generate-demo-assets-temp (deleted after
// producing the checked-in files below) using Three.js's own
// GLTFExporter/USDZExporter addons — no third-party assets, no licensing
// ambiguity. See tasks/done.md's RP4C4 entry for the generation narrative.
const REGISTRY: VisualAssetDescriptor[] = [
  {
    id: "fixture-boiler-v1",
    version: 1,
    appliesTo: { elementKind: "fixture", category: "boiler" },
    format: "glb",
    source: { kind: "builtin", path: "/models/fixture-boiler-v1.glb" },
    normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" },
    displayName: "Boiler (demo)",
    license: { name: "Renovex project-original" },
  },
  {
    id: "fixture-electrical-panel-v1",
    version: 1,
    appliesTo: { elementKind: "fixture", category: "electrical_panel" },
    format: "glb",
    source: { kind: "builtin", path: "/models/fixture-electrical-panel-v1.glb" },
    normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center" },
    displayName: "Electrical panel (demo)",
    license: { name: "Renovex project-original" },
  },
  {
    id: "object-sofa-v1",
    version: 1,
    appliesTo: { elementKind: "object", category: "sofa" },
    format: "glb",
    source: { kind: "builtin", path: "/models/object-sofa-v1.glb" },
    normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" },
    displayName: "Sofa (demo)",
    license: { name: "Renovex project-original" },
  },
];

function assertNoDuplicateIds(registry: VisualAssetDescriptor[]): void {
  const seen = new Set<string>();
  for (const descriptor of registry) {
    if (seen.has(descriptor.id)) {
      throw new Error(`VisualAssetRegistry: duplicate descriptor id "${descriptor.id}"`);
    }
    seen.add(descriptor.id);
  }
}

function assertNoAmbiguousDefaultBindings(registry: VisualAssetDescriptor[]): void {
  const seen = new Set<string>();
  for (const descriptor of registry) {
    const key = `${descriptor.appliesTo.elementKind}:${descriptor.appliesTo.category}`;
    if (seen.has(key)) {
      throw new Error(
        `VisualAssetRegistry: ambiguous default binding for ${key} — two descriptors both claim it`,
      );
    }
    seen.add(key);
  }
}

export function validateRegistry(registry: VisualAssetDescriptor[]): void {
  assertNoDuplicateIds(registry);
  assertNoAmbiguousDefaultBindings(registry);
}

validateRegistry(REGISTRY);

export const VISUAL_ASSET_REGISTRY: readonly VisualAssetDescriptor[] = REGISTRY;
