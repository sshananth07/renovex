import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SpatialGenerationWakeMessage } from "@/lib/server/spatialGenerationQueue";

// handleCallback normally returns a Vercel-invoked Request handler; for a
// unit test we only care about the raw (message, metadata) => Promise<void>
// callback it was given, so the mock captures it directly instead of
// simulating a Web Request round-trip.
let capturedHandler: ((message: SpatialGenerationWakeMessage, metadata: unknown) => Promise<void>) | null = null;
vi.mock("@vercel/queue", () => ({
  handleCallback: (handler: (message: SpatialGenerationWakeMessage, metadata: unknown) => Promise<void>) => {
    capturedHandler = handler;
    return handler;
  },
}));

const publishSpatialGenerationWake = vi.fn();
vi.mock("@/lib/server/spatialGenerationQueue", () => ({
  publishSpatialGenerationWake: (...args: unknown[]) => publishSpatialGenerationWake(...args),
}));

describe("spatial-generation queue consumer", () => {
  const originalFetch = global.fetch;
  const originalEnv = { ...process.env };

  beforeEach(() => {
    vi.resetModules();
    publishSpatialGenerationWake.mockReset();
    process.env.SPATIAL_API_INTERNAL_URL = "http://go-internal:8080";
    process.env.SPATIAL_WORKER_TOKEN = "worker-secret";
  });

  afterEach(() => {
    global.fetch = originalFetch;
    process.env = { ...originalEnv };
  });

  async function loadHandler() {
    await import("./route");
    if (!capturedHandler) throw new Error("handleCallback was not invoked by route.ts");
    return capturedHandler;
  }

  it("calls the design-generation worker route for a design_attempt message", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ processed: true, terminal: false, nextWakeDelaySeconds: 5 }), { status: 200 }),
    );
    global.fetch = fetchMock as unknown as typeof fetch;

    const handler = await loadHandler();
    await handler({ kind: "design_attempt", id: "attempt_1" }, {});

    expect(fetchMock).toHaveBeenCalledWith(
      "http://go-internal:8080/internal/spatial/design-generation/process-one",
      expect.objectContaining({ method: "POST", headers: { "X-Spatial-Worker-Token": "worker-secret" } }),
    );
  });

  it("calls the asset-generation worker route for an asset_job message", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ processed: false, terminal: true }), { status: 200 }),
    );
    global.fetch = fetchMock as unknown as typeof fetch;

    const handler = await loadHandler();
    await handler({ kind: "asset_job", id: "job_1" }, {});

    expect(fetchMock).toHaveBeenCalledWith(
      "http://go-internal:8080/internal/spatial/asset-generation/process-one",
      expect.anything(),
    );
  });

  it("schedules a delayed follow-up wake when the Go step reports more work remains", async () => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ processed: true, terminal: false, nextWakeDelaySeconds: 5 }), { status: 200 }),
    ) as unknown as typeof fetch;

    const handler = await loadHandler();
    const message: SpatialGenerationWakeMessage = { kind: "design_attempt", id: "attempt_1" };
    await handler(message, {});

    expect(publishSpatialGenerationWake).toHaveBeenCalledWith(message, { delaySeconds: 5 });
  });

  it("does not republish when the Go step reports terminal", async () => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ processed: false, terminal: true }), { status: 200 }),
    ) as unknown as typeof fetch;

    const handler = await loadHandler();
    await handler({ kind: "asset_job", id: "job_1" }, {});

    expect(publishSpatialGenerationWake).not.toHaveBeenCalled();
  });

  it("throws (so @vercel/queue retries) when the Go worker step returns a non-2xx status", async () => {
    global.fetch = vi.fn().mockResolvedValue(new Response("", { status: 503 })) as unknown as typeof fetch;

    const handler = await loadHandler();
    await expect(handler({ kind: "design_attempt", id: "attempt_1" }, {})).rejects.toThrow();
    expect(publishSpatialGenerationWake).not.toHaveBeenCalled();
  });

  it("throws when SPATIAL_API_INTERNAL_URL/SPATIAL_WORKER_TOKEN are not configured", async () => {
    delete process.env.SPATIAL_API_INTERNAL_URL;
    delete process.env.SPATIAL_WORKER_TOKEN;
    global.fetch = vi.fn() as unknown as typeof fetch;

    const handler = await loadHandler();
    await expect(handler({ kind: "design_attempt", id: "attempt_1" }, {})).rejects.toThrow();
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
