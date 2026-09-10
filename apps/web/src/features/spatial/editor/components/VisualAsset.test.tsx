import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import VisualAsset from "./VisualAsset";
import type { VisualAssetDescriptor } from "../three/assets/types";

vi.mock("./GLTFVisualAsset", () => ({
  default: vi.fn(() => <div data-testid="gltf-visual-asset" />),
}));
vi.mock("./USDVisualAsset", () => ({
  default: vi.fn(() => <div data-testid="usd-visual-asset" />),
}));

import GLTFVisualAsset from "./GLTFVisualAsset";
import USDVisualAsset from "./USDVisualAsset";

const DIMENSIONS = { x: 1, y: 1, z: 1 };

function glbDescriptor(overrides: Partial<VisualAssetDescriptor> = {}): VisualAssetDescriptor {
  return {
    id: "d1",
    version: 1,
    appliesTo: { elementKind: "fixture", category: "boiler" },
    format: "glb",
    source: { kind: "builtin", path: "/models/d1.glb" },
    normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" },
    displayName: "Test",
    ...overrides,
  };
}

describe("VisualAsset — format dispatch", () => {
  it("mounts GLTFVisualAsset and never USDVisualAsset for a glb descriptor", () => {
    render(<VisualAsset descriptor={glbDescriptor({ format: "glb" })} dimensions={DIMENSIONS} />);
    expect(screen.getByTestId("gltf-visual-asset")).toBeInTheDocument();
    expect(screen.queryByTestId("usd-visual-asset")).not.toBeInTheDocument();
  });

  it("mounts GLTFVisualAsset for a gltf descriptor", () => {
    render(<VisualAsset descriptor={glbDescriptor({ format: "gltf" })} dimensions={DIMENSIONS} />);
    expect(screen.getByTestId("gltf-visual-asset")).toBeInTheDocument();
  });

  it("mounts USDVisualAsset and never GLTFVisualAsset for a usdz descriptor", () => {
    render(<VisualAsset descriptor={glbDescriptor({ format: "usdz" })} dimensions={DIMENSIONS} />);
    expect(screen.getByTestId("usd-visual-asset")).toBeInTheDocument();
    expect(screen.queryByTestId("gltf-visual-asset")).not.toBeInTheDocument();
  });
});

describe("VisualAsset — loadUrl prop threading", () => {
  it("passes the explicit loadUrl prop through to GLTFVisualAsset when provided (authorized source)", () => {
    const descriptor = glbDescriptor({
      format: "glb",
      source: { kind: "authorized", assetId: "custom-boiler-asset", version: 3 },
    });
    render(
      <VisualAsset
        descriptor={descriptor}
        dimensions={DIMENSIONS}
        loadUrl="https://api.example.com/spatial/visual-assets/content?cap=xyz"
      />,
    );
    expect(GLTFVisualAsset).toHaveBeenCalledWith(
      expect.objectContaining({ loadUrl: "https://api.example.com/spatial/visual-assets/content?cap=xyz" }),
      undefined,
    );
  });

  it("passes the explicit loadUrl prop through to USDVisualAsset when provided (authorized source)", () => {
    const descriptor = glbDescriptor({
      format: "usdz",
      source: { kind: "authorized", assetId: "custom-boiler-asset", version: 3 },
    });
    render(
      <VisualAsset
        descriptor={descriptor}
        dimensions={DIMENSIONS}
        loadUrl="https://api.example.com/spatial/visual-assets/content?cap=xyz"
      />,
    );
    expect(USDVisualAsset).toHaveBeenCalledWith(
      expect.objectContaining({ loadUrl: "https://api.example.com/spatial/visual-assets/content?cap=xyz" }),
      undefined,
    );
  });

  it("does not require loadUrl for a builtin descriptor (resolved internally)", () => {
    render(<VisualAsset descriptor={glbDescriptor({ format: "glb" })} dimensions={DIMENSIONS} />);
    expect(GLTFVisualAsset).toHaveBeenCalledWith(expect.objectContaining({ loadUrl: undefined }), undefined);
  });
});
