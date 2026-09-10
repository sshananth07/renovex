import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as THREE from "three";
import GLTFVisualAsset from "./GLTFVisualAsset";
import type { VisualAssetDescriptor } from "../three/assets/types";

const mockUseGLTF = vi.fn();
vi.mock("@react-three/drei", () => ({
  useGLTF: (path: string) => mockUseGLTF(path),
}));

const mockInvalidate = vi.fn();
vi.mock("@react-three/fiber", () => ({
  invalidate: () => mockInvalidate(),
}));

function unitCubeScene(): THREE.Group {
  const group = new THREE.Group();
  const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshStandardMaterial());
  group.add(mesh);
  return group;
}

const DESCRIPTOR: VisualAssetDescriptor = {
  id: "fixture-boiler-v1",
  version: 1,
  appliesTo: { elementKind: "fixture", category: "boiler" },
  format: "glb",
  source: { kind: "builtin", path: "/models/fixture-boiler-v1.glb" },
  normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" },
  displayName: "Boiler (demo)",
};

describe("GLTFVisualAsset", () => {
  it("calls useGLTF with the descriptor's resolved source path", () => {
    mockUseGLTF.mockReturnValue({ scene: unitCubeScene() });
    render(<GLTFVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 1, y: 1, z: 1 }} />);
    expect(mockUseGLTF).toHaveBeenCalledWith("/models/fixture-boiler-v1.glb");
  });

  it("renders the normalized scene inside a group when normalization succeeds", () => {
    mockUseGLTF.mockReturnValue({ scene: unitCubeScene() });
    const { container } = render(<GLTFVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 1, y: 1, z: 1 }} />);
    expect(container.querySelector("group")).not.toBeNull();
    expect(container.querySelector("primitive")).not.toBeNull();
  });

  it("renders nothing when the loaded scene has degenerate bounds (normalization fallback)", () => {
    mockUseGLTF.mockReturnValue({ scene: new THREE.Group() }); // empty group -> zero bounds
    const { container } = render(<GLTFVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 1, y: 1, z: 1 }} />);
    expect(container.querySelector("group")).toBeNull();
  });

  it("propagates a load failure to the caller instead of swallowing it (caller's Suspense/error boundary is responsible for the fallback)", () => {
    mockUseGLTF.mockImplementation(() => {
      throw new Error("network error");
    });
    expect(() => render(<GLTFVisualAsset descriptor={DESCRIPTOR} dimensions={{ x: 1, y: 1, z: 1 }} />)).toThrow(
      "network error",
    );
  });

});
