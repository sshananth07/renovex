import { apiClient } from "@/lib/api/client";
import { unwrapOrThrow } from "@/lib/api/errors";
import type {
  CancelDesignGenerationAttemptInput,
  ConfirmDesignPlanInput,
  RegenerateDesignPlanInput,
  UseDesignPlanInput,
} from "./types";

// RP4E2 client wrappers — same "generated API/client surface only" shape
// as designSessionApi.ts. React Query hooks live in mutations.ts/queries.ts
// once a UI slice actually consumes them (this file's own convention,
// carried forward from RP4E1).

export async function confirmDesignPlan(sessionId: string, turnId: string, body: ConfirmDesignPlanInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/design-sessions/{sessionId}/turns/{turnId}/confirm", {
      params: { path: { sessionId, turnId } },
      body,
    }),
  );
}

export async function regenerateDesignPlan(sessionId: string, turnId: string, body: RegenerateDesignPlanInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/design-sessions/{sessionId}/turns/{turnId}/regenerate", {
      params: { path: { sessionId, turnId } },
      body,
    }),
  );
}

export async function listDesignGenerationAttempts(
  sessionId: string,
  params?: { turnId?: string; limit?: number },
) {
  const result = await unwrapOrThrow(
    await apiClient.GET("/spatial/design-sessions/{sessionId}/generation-attempts", {
      params: { path: { sessionId }, query: params },
    }),
  );
  return result ?? [];
}

export async function getDesignGenerationAttempt(sessionId: string, attemptId: string) {
  return unwrapOrThrow(
    await apiClient.GET("/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}", {
      params: { path: { sessionId, attemptId } },
    }),
  );
}

export async function cancelDesignGenerationAttempt(
  sessionId: string,
  attemptId: string,
  body: CancelDesignGenerationAttemptInput,
) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/cancel", {
      params: { path: { sessionId, attemptId } },
      body,
    }),
  );
}

export async function useDesignPlan(sessionId: string, attemptId: string, body: UseDesignPlanInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/design-sessions/{sessionId}/generation-attempts/{attemptId}/use", {
      params: { path: { sessionId, attemptId } },
      body,
    }),
  );
}
