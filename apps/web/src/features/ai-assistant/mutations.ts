import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  acceptSuggestion,
  generateResourceSuggestions,
  generateSpaceSuggestions,
  generateWorkItemSuggestions,
  rejectSuggestion,
  updateProjectScopeBrief,
  applySpaceDeltaSuggestion,
  type AcceptInput,
} from "./api";
import { aiKeys } from "./queryKeys";

// A fresh operationId per logical click/attempt. The same UI state (a
// pending mutation retried by the browser after a network hiccup) reuses
// the id already captured in the mutationFn closure; "Generate fresh
// suggestions" instead calls the hook's mutate again, which — because this
// helper is invoked once per call — produces a new id (design doc plan
// Task 12 Step 4).
export function newOperationId(): string {
  return crypto.randomUUID();
}

function invalidateGenerationQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  projectId: string
) {
  void queryClient.invalidateQueries({ queryKey: aiKeys.batches(projectId) });
  void queryClient.invalidateQueries({ queryKey: ["projects", projectId] });
}

export function useGenerateSpaceSuggestions(projectId: string) {
  const queryClient = useQueryClient();
  const operationId = newOperationId();
  return useMutation({
    mutationFn: () => generateSpaceSuggestions(projectId, operationId),
    retry: false,
    onSuccess: () => {
      invalidateGenerationQueries(queryClient, projectId);
      void queryClient.invalidateQueries({ queryKey: ["projects", projectId, "spaces"] });
    },
  });
}

export function useGenerateWorkItemSuggestions(projectId: string) {
  const queryClient = useQueryClient();
  const operationId = newOperationId();
  return useMutation({
    mutationFn: () => generateWorkItemSuggestions(projectId, operationId),
    retry: false,
    onSuccess: () => {
      invalidateGenerationQueries(queryClient, projectId);
      void queryClient.invalidateQueries({ queryKey: ["projects", projectId, "work-items"] });
    },
  });
}

export function useGenerateResourceSuggestions(projectId: string) {
  const queryClient = useQueryClient();
  const operationId = newOperationId();
  return useMutation({
    mutationFn: () => generateResourceSuggestions(projectId, operationId),
    retry: false,
    onSuccess: () => {
      invalidateGenerationQueries(queryClient, projectId);
      void queryClient.invalidateQueries({ queryKey: aiKeys.resources(projectId) });
    },
  });
}

// Only the domains an accepted suggestion can actually change are
// invalidated (design doc plan Task 12 Step 5) — Space acceptance touches
// Spaces, WorkItem acceptance touches Work Items, Resource acceptance
// touches Materials/WorkResourceRequirements. Since the suggestion's type
// determines which domain changed and the mutation is generic across all
// suggestion types, this invalidates the full set every accept call rather
// than branching on response shape; the cost is one extra cheap
// query-invalidation call, not an extra network request.
export function useAcceptSuggestion(batchId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ suggestionId, input }: { suggestionId: string; input: AcceptInput }) =>
      acceptSuggestion(suggestionId, input),
    retry: false,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: aiKeys.suggestions(batchId) });
      await queryClient.invalidateQueries({ queryKey: aiKeys.batches(projectId) });
      // "projects", projectId is a shared prefix for the Project itself plus
      // its nested Spaces/Work Items query keys (["projects", projectId,
      // "spaces", ...] / ["projects", projectId, "work-items", ...]), so one
      // invalidation call covers all three real domains an accepted
      // suggestion can touch.
      await queryClient.invalidateQueries({ queryKey: ["projects", projectId] });
      await queryClient.invalidateQueries({ queryKey: aiKeys.resources(projectId) });
    },
  });
}

export function useRejectSuggestion(batchId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ suggestionId, expectedRevision }: { suggestionId: string; expectedRevision: number }) =>
      rejectSuggestion(suggestionId, expectedRevision),
    retry: false,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: aiKeys.suggestions(batchId) });
      await queryClient.invalidateQueries({ queryKey: aiKeys.batches(projectId) });
    },
  });
}

export function useUseSpaceDelta(batchId: string, projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      suggestionId,
      expectedRevision,
      currentAuthoritativeId,
    }: {
      suggestionId: string;
      expectedRevision: number;
      currentAuthoritativeId: string;
    }) => applySpaceDeltaSuggestion(suggestionId, expectedRevision, currentAuthoritativeId),
    retry: false,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: aiKeys.suggestions(batchId) });
      await queryClient.invalidateQueries({ queryKey: aiKeys.batches(projectId) });
      await queryClient.invalidateQueries({ queryKey: ["projects", projectId] });
    },
  });
}

export function useUpdateProjectScopeBrief(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (scopeBrief: string) => updateProjectScopeBrief(projectId, scopeBrief),
    onSuccess: (data) => {
      queryClient.setQueryData(["projects", projectId], data);
      void queryClient.invalidateQueries({ queryKey: ["projects", projectId] });
    },
  });
}
