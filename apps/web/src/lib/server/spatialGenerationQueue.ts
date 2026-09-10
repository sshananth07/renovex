import { send } from "@vercel/queue";

/**
 * RP4E2 Gate 4: the two Vercel Queue topics this project publishes to and
 * consumes from. A queue message is only a wake-up hint — Mongo (via the
 * Go API) remains the sole source of truth for what actually happened, so
 * at-least-once delivery on either topic is harmless by design.
 */
export const SPATIAL_GENERATION_TOPIC = "spatial-generation" as const;

export type SpatialGenerationWakeKind = "design_attempt" | "asset_job";

export interface SpatialGenerationWakeMessage {
  kind: SpatialGenerationWakeKind;
  id: string;
}

/**
 * Publishes one wake hint for a design generation attempt or asset
 * generation job.
 *
 * idempotencyKey is set ONLY for the initial (non-delayed) publish from the
 * enqueue bridge, keyed on kind+id+createdAt-less content so a retried
 * bridge request (Go's own HTTP client has no retry logic — see
 * spatialgenerationwakeclient.go — but the bridge route itself could still
 * be retried by its caller) doesn't double-publish. A queue-consumer
 * follow-up (options.delaySeconds set, from Go's nextWakeDelaySeconds)
 * intentionally omits idempotencyKey: each follow-up is a genuinely new,
 * distinct wake in a chain, and reusing a kind:id key across the whole
 * chain would risk a later legitimate wake being silently deduped against
 * an earlier one still inside the dedup window.
 */
export async function publishSpatialGenerationWake(
  message: SpatialGenerationWakeMessage,
  options?: { delaySeconds?: number },
): Promise<{ messageId: string | null }> {
  return send(SPATIAL_GENERATION_TOPIC, message, {
    idempotencyKey: options?.delaySeconds === undefined ? `${message.kind}:${message.id}` : undefined,
    delaySeconds: options?.delaySeconds,
  });
}
