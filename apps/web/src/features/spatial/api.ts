import { apiClient } from "@/lib/api/client";
import { unwrapOrThrow } from "@/lib/api/errors";
import type { components } from "@/lib/api/generated/schema";

export type SpatialCapture = components["schemas"]["SpatialCaptureDTO"];
export type SpatialSpaceState = components["schemas"]["SpatialSpaceStateDTO"];
export type StartCaptureInput = components["schemas"]["StartCaptureInputBody"];
export type AdvanceCaptureInput = components["schemas"]["AdvanceCaptureInputBody"];
export type RoomDraft = components["schemas"]["RoomDraftDTO"];
export type SubmitRoomDraftEditInput = components["schemas"]["SubmitRoomDraftEditInputBody"];
export type RoomDraftEditResult = components["schemas"]["RoomDraftEditResultDTO"];
export type ResetRoomDraftInput = components["schemas"]["ResetRoomDraftInputBody"];
export type UndoResetRoomDraftInput = components["schemas"]["UndoResetRoomDraftInputBody"];
export type VisualAssetAccessResult = components["schemas"]["CreateVisualAssetAccessOutputBody"];

export async function listSpatialCaptures(spaceId: string) {
  const { data, error } = await apiClient.GET("/spatial/captures", {
    params: { query: { spaceId } },
  });
  if (error) throw error;
  return data;
}

export async function getSpatialSpaceState(projectId: string, spaceId: string) {
  const { data, error } = await apiClient.GET("/spatial/space-state", {
    params: { query: { projectId, spaceId } },
  });
  if (error) throw error;
  return data;
}

export async function startSpatialCapture(input: StartCaptureInput) {
  const { data, error } = await apiClient.POST("/spatial/captures", { body: input });
  if (error) throw error;
  return data;
}

export async function advanceSpatialCapture(id: string, status: string) {
  const { data, error } = await apiClient.POST("/spatial/captures/{id}/advance", {
    params: { path: { id } },
    body: { status },
  });
  if (error) throw error;
  return data;
}

export async function confirmSpatialCapture(id: string) {
  const { data, error } = await apiClient.POST("/spatial/captures/{id}/confirm", {
    params: { path: { id } },
  });
  if (error) throw error;
  return data;
}

// RP4C1 consumed the GET/edit routes; RP4C2 adds reset-to-scan. The
// edit-history list route is still deferred — do not add unused client
// surface just because the route exists on the backend.

export async function getRoomDraft(roomDraftId: string) {
  return unwrapOrThrow(
    await apiClient.GET("/spatial/room-drafts/{id}", { params: { path: { id: roomDraftId } } }),
  );
}

export async function submitRoomDraftEdit(roomDraftId: string, body: SubmitRoomDraftEditInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/room-drafts/{id}/edits", { params: { path: { id: roomDraftId } }, body }),
  );
}

export async function resetRoomDraftToScan(roomDraftId: string, body: ResetRoomDraftInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/room-drafts/{id}/reset-to-scan", { params: { path: { id: roomDraftId } }, body }),
  );
}

// M8.5C reversible-editing patch §11: undoes a prior Reset-to-Scan through
// the canonical CAS transport, structurally parallel to resetRoomDraftToScan
// above.
export async function undoResetRoomDraftToScan(roomDraftId: string, body: UndoResetRoomDraftInput) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/room-drafts/{id}/undo-reset-to-scan", { params: { path: { id: roomDraftId } }, body }),
  );
}

// RP4D: mints a short-lived, single-use-per-request bearer capability for
// fetching one authorized visual asset version's bytes. The returned
// access.url is an ephemeral secret — callers must never persist it
// (RoomDraft, localStorage, logs) and must treat React Query's in-memory
// cache as the only acceptable place it's held.
export async function getVisualAssetAccess(assetId: string, version: number) {
  return unwrapOrThrow(
    await apiClient.POST("/spatial/visual-assets/{assetId}/versions/{version}/access", {
      params: { path: { assetId, version } },
    }),
  );
}
