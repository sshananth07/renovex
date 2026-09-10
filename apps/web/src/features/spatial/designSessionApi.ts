import { apiClient } from "@/lib/api/client";
import { unwrapOrThrow } from "@/lib/api/errors";
import type { components } from "@/lib/api/generated/schema";

// RP4E1: minimal generated API/client surface only — no React mutation
// hooks are added here since no UI slice yet consumes them (plan Task 9
// Step 3: "Do not add React mutation hooks unless a UI slice actually
// consumes them; the wrappers are enough for RP4E1's client contract").

export type DesignSession = components["schemas"]["DesignSessionDTO"];
export type DesignTurn = components["schemas"]["DesignTurnDTO"];
export type CreateDesignSessionInput = components["schemas"]["CreateDesignSessionInputBody"];
export type CreateDesignTurnInput = components["schemas"]["CreateDesignTurnInputBody"];
export type GetDesignSessionResult = components["schemas"]["GetDesignSessionOutputBody"];

export async function createSpatialDesignSession(body: CreateDesignSessionInput) {
  return unwrapOrThrow(await apiClient.POST("/spatial/design-sessions", { body }));
}

export async function createSpatialDesignTurn(sessionId: string, body: CreateDesignTurnInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/design-sessions/{id}/turns", {
      params: { path: { id: sessionId } },
      body,
    }),
  );
}

export async function getSpatialDesignSession(sessionId: string) {
  return unwrapOrThrow(
    await apiClient.GET("/spatial/design-sessions/{id}", { params: { path: { id: sessionId } } }),
  );
}

export async function listSpatialDesignTurns(
  sessionId: string,
  params?: { beforeSequence?: number; limit?: number },
) {
  const result = await unwrapOrThrow(
    await apiClient.GET("/spatial/design-sessions/{id}/turns", {
      params: { path: { id: sessionId }, query: params },
    }),
  );
  return result ?? [];
}
