import { NextRequest, NextResponse } from "next/server";
import { publishSpatialGenerationWake, type SpatialGenerationWakeKind } from "@/lib/server/spatialGenerationQueue";

/**
 * RP4E2 Gate 4: the shared-secret-protected Go-to-Queue bridge. Go calls
 * this immediately after durably writing a fresh design generation attempt
 * or asset generation job to Mongo (see the backend's notifyGenerationWake
 * / spatialgenerationwakeclient.go), passing only `{kind, id}` — never
 * authoritative state, matching the plan's "no authoritative state" trust
 * model for this internal boundary. Authenticated by
 * SPATIAL_QUEUE_ENQUEUE_TOKEN, a single static shared secret known only to
 * the Go backend and this route (same pattern as the Go side's own
 * X-Spatial-Worker-Token check) — never a contractor session/JWT.
 */
export async function POST(request: NextRequest) {
  const expectedToken = process.env.SPATIAL_QUEUE_ENQUEUE_TOKEN;
  const providedToken = request.headers.get("X-Spatial-Queue-Enqueue-Token");
  if (!expectedToken || providedToken !== expectedToken) {
    return NextResponse.json({ error: "invalid or missing enqueue token" }, { status: 401 });
  }

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }

  const { kind, id } = (body ?? {}) as { kind?: unknown; id?: unknown };
  if ((kind !== "design_attempt" && kind !== "asset_job") || typeof id !== "string" || id.length === 0) {
    return NextResponse.json({ error: "expected { kind: 'design_attempt' | 'asset_job', id: string }" }, { status: 400 });
  }

  const { messageId } = await publishSpatialGenerationWake({ kind: kind as SpatialGenerationWakeKind, id });
  return NextResponse.json({ messageId }, { status: 202 });
}
