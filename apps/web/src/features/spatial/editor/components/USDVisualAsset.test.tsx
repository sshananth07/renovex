import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as THREE from "three";
import USDVisualAsset from "./USDVisualAsset";
import type { VisualAssetDescriptor } from "../three/assets/types";

const mockUseLoader = vi.fn();
vi.mock("@react-three/fiber", () => ({
  useLoader: (Loader: unknown, path: string) => mockUseLoader(Loader, path),
  invalidate: vi.fn(),
}));

function unitCubeScene(): THREE.Group {
  const group = new THREE.Group();
  const mesh = new THREE.Mesh(new THREE.BoxGeometry(0.4, 0.4, 0.4), new THREE.MeshStandardMaterial());
  group.add(mesh);
  return group;
}

const DESCRIPTOR: VisualAssetDescriptor = {
  id: "format-proof",
  version: 1,
  appliesTo: { elementKind: "object", category: "loader-proof-only" },
  format: "usdz",
  source: { kind: "builtin", path: "/models/format-proof.usdz" },
  normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center" },
  displayName: "Format proof (USDZ)",
};

describe("USDVisualAsset", () => {
  it("calls useLoader with USDLoader and the descriptor's resolved source path", () => {
    mockUseLoader.mockReturnValue(unitCubeScene());
    render(<USDVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 0.4, y: 0.4, z: 0.4 }} />);
    expect(mockUseLoader).toHaveBeenCalledWith(expect.any(Function), "/models/format-proof.usdz");
  });

  it("renders the normalized scene inside a group when normalization succeeds", () => {
    mockUseLoader.mockReturnValue(unitCubeScene());
    const { container } = render(<USDVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 0.4, y: 0.4, z: 0.4 }} />);
    expect(container.querySelector("group")).not.toBeNull();
    expect(container.querySelector("primitive")).not.toBeNull();
  });

  it("renders nothing when the loaded scene has degenerate bounds (normalization fallback)", () => {
    mockUseLoader.mockReturnValue(new THREE.Group());
    const { container } = render(<USDVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 0.4, y: 0.4, z: 0.4 }} />);
    expect(container.querySelector("group")).toBeNull();
  });
});
