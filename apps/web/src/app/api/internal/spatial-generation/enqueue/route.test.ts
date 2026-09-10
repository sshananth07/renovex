import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const publishSpatialGenerationWake = vi.fn();
vi.mock("@/lib/server/spatialGenerationQueue", () => ({
  publishSpatialGenerationWake: (...args: unknown[]) => publishSpatialGenerationWake(...args),
}));

function postRequest(body: unknown, token?: string): NextRequest {
  const headers = new Headers({ "Content-Type": "application/json" });
  if (token !== undefined) headers.set("X-Spatial-Queue-Enqueue-Token", token);
  return new NextRequest("http://localhost/api/internal/spatial-generation/enqueue", {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
}

describe("POST /api/internal/spatial-generation/enqueue", () => {
  const originalEnv = { ...process.env };

  beforeEach(() => {
    vi.resetModules();
    publishSpatialGenerationWake.mockReset();
    publishSpatialGenerationWake.mockResolvedValue({ messageId: "msg_1" });
    process.env.SPATIAL_QUEUE_ENQUEUE_TOKEN = "enqueue-secret";
  });

  afterEach(() => {
    process.env = { ...originalEnv };
  });

  it("rejects a request with no token when none is configured to compare against", async () => {
    delete process.env.SPATIAL_QUEUE_ENQUEUE_TOKEN;
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "design_attempt", id: "a1" }));
    expect(res.status).toBe(401);
    expect(publishSpatialGenerationWake).not.toHaveBeenCalled();
  });

  it("rejects a missing token", async () => {
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "design_attempt", id: "a1" }));
    expect(res.status).toBe(401);
  });

  it("rejects a wrong token", async () => {
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "design_attempt", id: "a1" }, "wrong"));
    expect(res.status).toBe(401);
  });

  it("rejects an invalid kind", async () => {
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "something_else", id: "a1" }, "enqueue-secret"));
    expect(res.status).toBe(400);
    expect(publishSpatialGenerationWake).not.toHaveBeenCalled();
  });

  it("rejects a missing id", async () => {
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "asset_job" }, "enqueue-secret"));
    expect(res.status).toBe(400);
  });

  it("publishes the wake and returns 202 with the message id for a valid request", async () => {
    const { POST } = await import("./route");
    const res = await POST(postRequest({ kind: "asset_job", id: "job_1" }, "enqueue-secret"));
    expect(res.status).toBe(202);
    expect(publishSpatialGenerationWake).toHaveBeenCalledWith({ kind: "asset_job", id: "job_1" });
    const body = await res.json();
    expect(body).toEqual({ messageId: "msg_1" });
  });
});
