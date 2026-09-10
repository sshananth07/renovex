import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { getVisualAssetAccess } from "./api";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("spatial api — getVisualAssetAccess", () => {
  it("POSTs to the versioned access endpoint and returns asset + access", async () => {
    let capturedUrl = "";
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({
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
          access: {
            url: "https://api.example.com/spatial/visual-assets/content?cap=xyz",
            expiresAt: "2026-09-06T12:10:00Z",
          },
        });
      }),
    );

    const result = await getVisualAssetAccess("custom-boiler-asset", 3);

    expect(capturedUrl).toContain("/spatial/visual-assets/custom-boiler-asset/versions/3/access");
    expect(result.asset.assetId).toBe("custom-boiler-asset");
    expect(result.asset.version).toBe(3);
    expect(result.access.url).toBe("https://api.example.com/spatial/visual-assets/content?cap=xyz");
  });

  it("throws a normalized error on a 404 (cross-company or nonexistent asset)", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/visual-assets/:assetId/versions/:version/access`, () =>
        HttpResponse.json({ status: 404, detail: "not found" }, { status: 404 }),
      ),
    );

    await expect(getVisualAssetAccess("other-company-asset", 1)).rejects.toMatchObject({ status: 404 });
  });
});
