import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createSpatialDesignSession, createSpatialDesignTurn, getSpatialDesignSession, listSpatialDesignTurns } from "../designSessionApi";
import {
  cancelDesignGenerationAttempt,
  confirmDesignPlan,
  getDesignGenerationAttempt,
  listDesignGenerationAttempts,
  regenerateDesignPlan,
  useDesignPlan as callUseDesignPlan,
} from "./designGenerationApi";
import { roomDraftQueryKey } from "../queries";
import type {
  CancelDesignGenerationAttemptInput,
  ConfirmDesignPlanInput,
  RegenerateDesignPlanInput,
  UseDesignPlanInput,
} from "./types";
import type { CreateDesignSessionInput, CreateDesignTurnInput } from "../designSessionApi";

// Query keys — session/turns/attempts are all scoped under one sessionId
// root so a single invalidate(["design-studio", "session", sessionId])
// clears everything for that session in one call.
export function designSessionQueryKey(sessionId: string) {
  return ["design-studio", "session", sessionId] as const;
}
export function designTurnsQueryKey(sessionId: string) {
  return ["design-studio", "session", sessionId, "turns"] as const;
}
export function designAttemptsQueryKey(sessionId: string) {
  return ["design-studio", "session", sessionId, "attempts"] as const;
}
export function designAttemptQueryKey(sessionId: string, attemptId: string) {
  return ["design-studio", "session", sessionId, "attempts", attemptId] as const;
}

const NONTERMINAL_TURN_STATUSES = new Set(["reserved", "reasoning"]);
const NONTERMINAL_ATTEMPT_STATUSES = new Set([
  "reserved",
  "generating_reference",
  "reference_ready",
  "asset_generation_pending",
  "asset_generation_processing",
]);

// Finds (or, via CreateDesignSession's fingerprint-based idempotent replay,
// creates) the one session for this exact {roomDraftId,target} — the
// backend's CreateDesignSession is itself the find-or-restore primitive
// (sessionRequestFingerprint is derived from company+clientSessionId+
// roomDraftId+target, so a deterministic clientSessionId makes repeat calls
// idempotent replays, never duplicate sessions). Callers pass a
// deterministic clientSessionId built from the target identity.
export function useEnsureDesignSession() {
  return useMutation({
    mutationFn: (input: CreateDesignSessionInput) => createSpatialDesignSession(input),
  });
}

export function useDesignSession(sessionId: string | null) {
  return useQuery({
    queryKey: designSessionQueryKey(sessionId ?? ""),
    queryFn: () => getSpatialDesignSession(sessionId as string),
    enabled: Boolean(sessionId),
  });
}

// Polls only while the latest known turn is nonterminal (reserved/
// reasoning) — matches the plan's "poll only nonterminal attempts"
// instruction, applied symmetrically to turns since GLM reasoning is itself
// an async step with the same shape of wait.
export function useDesignTurns(sessionId: string | null) {
  return useQuery({
    queryKey: designTurnsQueryKey(sessionId ?? ""),
    queryFn: () => listSpatialDesignTurns(sessionId as string),
    enabled: Boolean(sessionId),
    refetchInterval: (query) => {
      const turns = query.state.data;
      const latest = turns?.[0];
      return latest && NONTERMINAL_TURN_STATUSES.has(latest.status) ? 1500 : false;
    },
  });
}

export function useDesignGenerationAttempts(sessionId: string | null, turnId?: string) {
  return useQuery({
    queryKey: turnId ? [...designAttemptsQueryKey(sessionId ?? ""), turnId] : designAttemptsQueryKey(sessionId ?? ""),
    queryFn: () => listDesignGenerationAttempts(sessionId as string, turnId ? { turnId } : undefined),
    enabled: Boolean(sessionId),
  });
}

export function useDesignGenerationAttempt(sessionId: string | null, attemptId: string | null) {
  return useQuery({
    queryKey: designAttemptQueryKey(sessionId ?? "", attemptId ?? ""),
    queryFn: () => getDesignGenerationAttempt(sessionId as string, attemptId as string),
    enabled: Boolean(sessionId) && Boolean(attemptId),
    refetchInterval: (query) => (query.state.data && NONTERMINAL_ATTEMPT_STATUSES.has(query.state.data.status) ? 2000 : false),
  });
}

export function useCreateDesignTurn(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateDesignTurnInput) => createSpatialDesignTurn(sessionId, body),
    retry: false,
    onMutate: async () => {
      await queryClient.invalidateQueries({ queryKey: designTurnsQueryKey(sessionId) });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: designTurnsQueryKey(sessionId) });
    },
  });
}

export function useConfirmDesignPlan(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ turnId, body }: { turnId: string; body: ConfirmDesignPlanInput }) => confirmDesignPlan(sessionId, turnId, body),
    retry: false,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: designAttemptsQueryKey(sessionId) });
    },
  });
}

export function useRegenerateDesignPlan(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ turnId, body }: { turnId: string; body: RegenerateDesignPlanInput }) => regenerateDesignPlan(sessionId, turnId, body),
    retry: false,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: designAttemptsQueryKey(sessionId) });
    },
  });
}

export function useCancelDesignGenerationAttempt(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ attemptId, body }: { attemptId: string; body: CancelDesignGenerationAttemptInput }) =>
      cancelDesignGenerationAttempt(sessionId, attemptId, body),
    retry: false,
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: designAttemptQueryKey(sessionId, variables.attemptId) });
      queryClient.invalidateQueries({ queryKey: designAttemptsQueryKey(sessionId) });
    },
  });
}

// On success, replaces the authoritative RoomDraft cache outright — same
// "server response wins, never a client merge" contract as
// useSubmitEditOperation in ../mutations.ts.
export function useUseDesignPlan(sessionId: string, roomDraftId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ attemptId, body }: { attemptId: string; body: UseDesignPlanInput }) => callUseDesignPlan(sessionId, attemptId, body),
    retry: false,
    onSuccess: (result) => {
      if (result?.roomDraft) {
        queryClient.setQueryData(roomDraftQueryKey(roomDraftId), result.roomDraft);
      }
      queryClient.invalidateQueries({ queryKey: designSessionQueryKey(sessionId) });
      queryClient.invalidateQueries({ queryKey: designAttemptsQueryKey(sessionId) });
    },
  });
}
