import { handleCallback } from "@vercel/queue";
import { publishSpatialGenerationWake, type SpatialGenerationWakeMessage } from "@/lib/server/spatialGenerationQueue";

export const maxDuration = 120;

/**
 * RP4E2 Gate 4's private Queue consumer. Vercel invokes this route when a
 * message is available on the spatial-generation topic (see vercel.json's
 * experimentalTriggers wiring). It calls exactly ONE bounded Go worker
 * step — never loops locally, since Vercel functions have a hard execution
 * ceiling (RP4E0/E2 plan: "a bounded one-step worker that runs for at most
 * 45 seconds") — then republishes a delayed follow-up wake only if Go
 * reports more work remains. At-least-once delivery is harmless: Go's own
 * Mongo claim-safety fencing (lease ownership / activeSlot presence)
 * decides every transition, never queue delivery count.
 */
export const POST = handleCallback<SpatialGenerationWakeMessage>(async (message) => {
  const baseUrl = process.env.SPATIAL_API_INTERNAL_URL;
  const workerToken = process.env.SPATIAL_WORKER_TOKEN;
  if (!baseUrl || !workerToken) {
    throw new Error("spatial-generation consumer: SPATIAL_API_INTERNAL_URL/SPATIAL_WORKER_TOKEN not configured");
  }

  const path =
    message.kind === "design_attempt"
      ? "/internal/spatial/design-generation/process-one"
      : "/internal/spatial/asset-generation/process-one";

  const response = await fetch(`${baseUrl}${path}`, {
    method: "POST",
    headers: { "X-Spatial-Worker-Token": workerToken },
  });
  if (!response.ok) {
    throw new Error(`spatial-generation consumer: Go worker step returned ${response.status}`);
  }

  const body = (await response.json()) as {
    processed: boolean;
    terminal: boolean;
    nextWakeDelaySeconds?: number;
  };

  if (!body.terminal && body.nextWakeDelaySeconds) {
    await publishSpatialGenerationWake(message, { delaySeconds: body.nextWakeDelaySeconds });
  }
});
