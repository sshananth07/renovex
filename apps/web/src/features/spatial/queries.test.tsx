import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { visualAssetAccessQueryKey, useVisualAssetAccess } from "./queries";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

let mockCompanyId: string | undefined = "company-a";
vi.mock("@/features/auth/useAuth", () => ({
  useAuth: () => ({
    status: mockCompanyId ? "authenticated" : "unauthenticated",
    user: mockCompanyId ? { companyId: mockCompanyId } : null,
  }),
}));

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const accessResponse = {
  asset: {
    assetId: "custom-boiler-asset",
    version: 3,
    format: "glb",
    displayName: "Custom Boiler",
    normalization: { pivot: "center-bottom" },
    contentType: "model/gltf-binary",
    byteCount: 1024,
    checksum: "abc123",
  },
  access: { url: "https://api.example.com/spatial/visual-assets/content?cap=xyz", expiresAt: "2026-09-06T12:10:00Z" },
};

describe("visualAssetAccessQueryKey", () => {
  it("includes companyId, assetId, and version so entries never alias across companies", () => {
    expect(visualAssetAccessQueryKey("company-a", "asset-1", 1)).toEqual([
      "spatial",
      "visual-asset-access",
      "company-a",
      "asset-1",
      1,
    ]);
  });
});

describe("useVisualAssetAccess", () => {
  it("fetches access for the current company and returns the server response", async () => {
    mockCompanyId = "company-a";
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, () =>
        HttpResponse.json(accessResponse),
      ),
    );
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useVisualAssetAccess("custom-boiler-asset", 3), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.access.url).toBe(accessResponse.access.url);
  });

  it("is disabled (does not fetch) when companyId is not yet available", async () => {
    mockCompanyId = undefined;
    let callCount = 0;
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, () => {
        callCount++;
        return HttpResponse.json(accessResponse);
      }),
    );
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useVisualAssetAccess("custom-boiler-asset", 3), {
      wrapper: wrapper(queryClient),
    });

    expect(result.current.isPending).toBe(true);
    expect(result.current.fetchStatus).toBe("idle");
    expect(callCount).toBe(0);
  });

  it("does not retry on a 404 (cross-company or nonexistent asset)", async () => {
    mockCompanyId = "company-a";
    let callCount = 0;
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, () => {
        callCount++;
        return HttpResponse.json({ status: 404, detail: "not found" }, { status: 404 });
      }),
    );
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useVisualAssetAccess("custom-boiler-asset", 3), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(callCount).toBe(1);
  });

  it("stores two companies' access grants for the same assetId/version as distinct cache entries (no aliasing)", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, () =>
        HttpResponse.json(accessResponse),
      ),
    );
    const queryClient = new QueryClient();

    mockCompanyId = "company-a";
    const { result: resultA } = renderHook(() => useVisualAssetAccess("shared-asset-id", 1), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() => expect(resultA.current.isSuccess).toBe(true));

    mockCompanyId = "company-b";
    const { result: resultB } = renderHook(() => useVisualAssetAccess("shared-asset-id", 1), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() => expect(resultB.current.isSuccess).toBe(true));

    const dataA = queryClient.getQueryData(visualAssetAccessQueryKey("company-a", "shared-asset-id", 1));
    const dataB = queryClient.getQueryData(visualAssetAccessQueryKey("company-b", "shared-asset-id", 1));
    expect(dataA).toBeDefined();
    expect(dataB).toBeDefined();
    expect(queryClient.getQueryData(["spatial", "visual-asset-access", "company-a", "shared-asset-id", 1])).not.toBe(
      undefined,
    );
    // The two entries are keyed distinctly — proven by both resolving independently
    // under their own company-scoped key rather than one overwriting the other.
    expect(dataA).toEqual(dataB); // same mocked response, but two SEPARATE cache slots
    expect(queryClient.getQueryCache().findAll({ queryKey: ["spatial", "visual-asset-access"] })).toHaveLength(2);
  });
});
