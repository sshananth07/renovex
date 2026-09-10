import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";

export type Batch = components["schemas"]["BatchDTO"];
export type Suggestion = components["schemas"]["SuggestionDTO"];
export type AcceptInput = components["schemas"]["AcceptInputBody"];
export type BatchType = "space_suggestions" | "work_item_suggestions" | "resource_suggestions";
export type RequirementDTO = components["schemas"]["RequirementDTO"];
export type MaterialDTO = components["schemas"]["MaterialDTO"];

export async function updateProjectScopeBrief(projectId: string, scopeBrief: string) {
  const { data, error } = await apiClient.PATCH("/projects/{projectId}/scope-brief", {
    params: { path: { projectId } },
    body: { scopeBrief },
  });
  if (error) throw error;
  return data;
}

export async function generateSpaceSuggestions(projectId: string, operationId: string) {
  const { data, error } = await apiClient.POST("/projects/{projectId}/ai/space-suggestions", {
    params: { path: { projectId } },
    body: { operationId },
  });
  if (error) throw error;
  return data;
}

export async function generateWorkItemSuggestions(projectId: string, operationId: string) {
  const { data, error } = await apiClient.POST("/projects/{projectId}/ai/work-item-suggestions", {
    params: { path: { projectId } },
    body: { operationId },
  });
  if (error) throw error;
  return data;
}

export async function generateResourceSuggestions(projectId: string, operationId: string) {
  const { data, error } = await apiClient.POST("/projects/{projectId}/ai/resource-suggestions", {
    params: { path: { projectId } },
    body: { operationId },
  });
  if (error) throw error;
  return data;
}

export async function listBatches(projectId: string, type?: BatchType) {
  const { data, error } = await apiClient.GET("/projects/{projectId}/ai/batches", {
    params: { path: { projectId }, query: type ? { type } : undefined },
  });
  if (error) throw error;
  return data;
}

export async function listSuggestionsByBatch(batchId: string) {
  const { data, error } = await apiClient.GET("/ai-batches/{batchId}/suggestions", {
    params: { path: { batchId } },
  });
  if (error) throw error;
  return data;
}

export async function acceptSuggestion(suggestionId: string, input: AcceptInput) {
  const { data, error } = await apiClient.POST("/ai-suggestions/{id}/accept", {
    params: { path: { id: suggestionId } },
    body: input,
  });
  if (error) throw error;
  return data;
}

export async function rejectSuggestion(suggestionId: string, expectedRevision: number) {
  const { error } = await apiClient.POST("/ai-suggestions/{id}/reject", {
    params: { path: { id: suggestionId } },
    body: { expectedRevision },
  });
  if (error) throw error;
}

// "Use suggestion" for a CHANGED rerun delta item — mutates the linked
// authoritative Space in place through the existing domain update path.
// "Keep current" needs no API call at all (a pure no-op on the authoritative
// side); UI only needs to dismiss/reject the suggestion for that decision.
export async function applySpaceDeltaSuggestion(
  suggestionId: string,
  expectedRevision: number,
  currentAuthoritativeId: string
) {
  const { data, error } = await apiClient.POST("/ai-suggestions/{id}/use-delta", {
    params: { path: { id: suggestionId } },
    body: { expectedRevision, currentAuthoritativeId },
  });
  if (error) throw error;
  return data;
}

export async function listWorkResourceRequirements(projectId: string, workItemId?: string) {
  const { data, error } = await apiClient.GET("/work-resource-requirements", {
    params: { query: workItemId ? { projectId, workItemId } : { projectId } },
  });
  if (error) throw error;
  return data;
}

export async function listMaterials() {
  const { data, error } = await apiClient.GET("/materials", {});
  if (error) throw error;
  return data;
}
