import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import AuthorizedVisualAsset from "./AuthorizedVisualAsset";
import type { VisualAssetDescriptor } from "../three/assets/types";

vi.mock("./VisualAsset", () => ({
  default: vi.fn(({ descriptor, loadUrl }: { descriptor: VisualAssetDescriptor; loadUrl?: string }) => (
    <div data-testid="visual-asset" data-format={descriptor.format} data-load-url={loadUrl} />
  )),
}));
vi.mock("./AssetFallback", () => ({
  default: () => <div data-testid="asset-fallback" />,
}));

const mockUseVisualAssetAccess = vi.fn();
vi.mock("../../queries", () => ({
  useVisualAssetAccess: (assetId: string, version: number) => mockUseVisualAssetAccess(assetId, version),
}));

function wrapper() {
  const queryClient = new QueryClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const DIMENSIONS = { x: 1, y: 1, z: 1 };
const REF = { assetId: "custom-boiler-asset", version: 3 };

const builtinFallback: VisualAssetDescriptor = {
  id: "fixture-boiler-v1",
  version: 1,
  appliesTo: { elementKind: "fixture", category: "boiler" },
  format: "glb",
  source: { kind: "builtin", path: "/models/fixture-boiler-v1.glb" },
  normalization: { intrinsicUnit: "meters", upAxis: "y", pivot: "center-bottom" },
  displayName: "Boiler",
};

describe("AuthorizedVisualAsset", () => {
  it("renders the builtin fallback while the access grant is pending", () => {
    mockUseVisualAssetAccess.mockReturnValue({ isPending: true, isError: false, data: undefined });
    render(
      <AuthorizedVisualAsset assetRef={REF} builtinFallback={builtinFallback} dimensions={DIMENSIONS} color="#fff" />,
      { wrapper: wrapper() },
    );
    expect(screen.getByTestId("visual-asset")).toHaveAttribute("data-format", "glb");
    expect(screen.queryByTestId("asset-fallback")).not.toBeInTheDocument();
  });

  it("renders the procedural AssetFallback while pending when there is no builtin fallback", () => {
    mockUseVisualAssetAccess.mockReturnValue({ isPending: true, isError: false, data: undefined });
    render(<AuthorizedVisualAsset assetRef={REF} builtinFallback={null} dimensions={DIMENSIONS} color="#fff" />, {
      wrapper: wrapper(),
    });
    expect(screen.getByTestId("asset-fallback")).toBeInTheDocument();
  });

  it("renders the builtin fallback on access error, not a thrown/uncaught render error", () => {
    mockUseVisualAssetAccess.mockReturnValue({ isPending: false, isError: true, data: undefined });
    render(
      <AuthorizedVisualAsset assetRef={REF} builtinFallback={builtinFallback} dimensions={DIMENSIONS} color="#fff" />,
      { wrapper: wrapper() },
    );
    expect(screen.getByTestId("visual-asset")).toBeInTheDocument();
  });

  it("renders the procedural fallback on access error when there is no builtin fallback", () => {
    mockUseVisualAssetAccess.mockReturnValue({ isPending: false, isError: true, data: undefined });
    render(<AuthorizedVisualAsset assetRef={REF} builtinFallback={null} dimensions={DIMENSIONS} color="#fff" />, {
      wrapper: wrapper(),
    });
    expect(screen.getByTestId("asset-fallback")).toBeInTheDocument();
  });

  it("constructs the complete descriptor from a successful response and passes the resolved loadUrl", () => {
    mockUseVisualAssetAccess.mockReturnValue({
      isPending: false,
      isError: false,
      data: {
        asset: {
          assetId: "custom-boiler-asset",
          version: 3,
          format: "usdz",
          displayName: "Custom Boiler",
          normalization: { pivot: "center" },
          contentType: "model/vnd.usdz+zip",
          byteCount: 2048,
          checksum: "deadbeef",
        },
        access: { url: "https://api.example.com/spatial/visual-assets/content?cap=xyz", expiresAt: "2026-09-06T12:10:00Z" },
      },
    });
    render(
      <AuthorizedVisualAsset assetRef={REF} builtinFallback={builtinFallback} dimensions={DIMENSIONS} color="#fff" />,
      { wrapper: wrapper() },
    );
    const el = screen.getByTestId("visual-asset");
    expect(el).toHaveAttribute("data-format", "usdz");
    expect(el).toHaveAttribute("data-load-url", "https://api.example.com/spatial/visual-assets/content?cap=xyz");
  });
});
