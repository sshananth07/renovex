import { useMutation, useQueryClient } from "@tanstack/react-query";
import { resetRoomDraftToScan, submitRoomDraftEdit, startSpatialCapture, undoResetRoomDraftToScan, type RoomDraftEditResult } from "./api";
import { roomDraftQueryKey } from "./queries";
import { toSubmitEditBody, toSubmitResetBody, toSubmitUndoResetBody } from "./editor/operations";
import type { PendingEdit, PendingReset, PendingUndoReset } from "./editor/types";

export function useStartSpatialCapture(projectId: string, spaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => startSpatialCapture({ projectId, spaceId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["spaces", spaceId, "spatial-captures"] });
      queryClient.invalidateQueries({ queryKey: ["spaces", spaceId, "spatial-state"] });
    },
  });
}

// Submits one canonical EditOperation through RP4B. Intentionally does NOT
// retry automatically (retry: false) — RP4C1 keeps retry a caller-driven
// decision (resubmit the same stored PendingEdit) rather than React Query's
// generic retry, since only the caller knows whether a given failure is the
// "genuinely retryable transport failure" case PendingEdit is meant for.
//
// On success, the authoritative RoomDraftDTO REPLACES the cached value
// outright (setQueryData with the server response) — never a client-merged
// draft. UI-only reconciliation (selection survives/clears) is the caller's
// separate, explicit next step using the returned result.
export function useSubmitEditOperation(roomDraftId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (pendingEdit: PendingEdit) => submitRoomDraftEdit(roomDraftId, toSubmitEditBody(pendingEdit)),
    retry: false,
    onSuccess: (result: RoomDraftEditResult) => {
      queryClient.setQueryData(roomDraftQueryKey(roomDraftId), result.roomDraft);
    },
  });
}

// Submits Reset-to-Scan through RP4B's dedicated route (POST
// /reset-to-scan, not /edits). Structurally parallel to
// useSubmitEditOperation — same retry:false + cache-replace-on-success
// contract — but takes a PendingReset, not a PendingEdit, since reset is
// intentionally kept separate (see PendingReset's doc comment in
// editor/types.ts).
export function useResetRoomDraft(roomDraftId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (pendingReset: PendingReset) => resetRoomDraftToScan(roomDraftId, toSubmitResetBody(pendingReset)),
    retry: false,
    onSuccess: (result: RoomDraftEditResult) => {
      queryClient.setQueryData(roomDraftQueryKey(roomDraftId), result.roomDraft);
    },
  });
}

// M8.5C reversible-editing patch §11: undoes a prior Reset-to-Scan.
// Structurally parallel to useResetRoomDraft — same retry:false +
// cache-replace-on-success contract, takes a PendingUndoReset.
export function useUndoResetRoomDraft(roomDraftId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (pendingUndoReset: PendingUndoReset) =>
      undoResetRoomDraftToScan(roomDraftId, toSubmitUndoResetBody(pendingUndoReset)),
    retry: false,
    onSuccess: (result: RoomDraftEditResult) => {
      queryClient.setQueryData(roomDraftQueryKey(roomDraftId), result.roomDraft);
    },
  });
}
