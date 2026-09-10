import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import type { ListParams } from "@/lib/url/listParams";

export type WorkItem = components["schemas"]["WorkItemDTO"];
export type CreateWorkItemInput = components["schemas"]["CreateWorkItemInputBody"];
export type UpdateWorkItemInput = components["schemas"]["UpdateWorkItemInputBody"];
export type WorkItemStatus = "planned" | "cancelled";

export async function listWorkItems(projectId: string, params: ListParams) {
  const { data, error } = await apiClient.GET("/work-items", {
    params: { query: { projectId, ...params } },
  });
  if (error) throw error;
  return data;
}

export async function listSpaceWorkItems(spaceId: string, params: ListParams) {
  const { data, error } = await apiClient.GET("/work-items", {
    params: { query: { spaceId, ...params } },
  });
  if (error) throw error;
  return data;
}

export async function createWorkItem(input: CreateWorkItemInput) {
  const { data, error } = await apiClient.POST("/work-items", { body: input });
  if (error) throw error;
  return data;
}

// Genuine partial PATCH: every field on UpdateWorkItemInputBody is optional
// and omit-means-unchanged, EXCEPT spaceId which is tri-state — omit the key
// to leave the Space assignment unchanged, pass `null` to explicitly clear
// it, or pass an id to reassign. Callers must not include `spaceId: undefined`
// as a way to "omit" — JSON.stringify already drops undefined-valued keys,
// so building the input object with the key genuinely absent is what matters.
export async function updateWorkItem(workItemId: string, input: UpdateWorkItemInput) {
  const { data, error } = await apiClient.PATCH("/work-items/{workItemId}", {
    params: { path: { workItemId } },
    body: input,
  });
  if (error) throw error;
  return data;
}

// Separate endpoint from updateWorkItem — status transitions (planned ->
// cancelled only) are not part of the general partial-PATCH body.
export async function updateWorkItemStatus(id: string, status: WorkItemStatus) {
  const { data, error } = await apiClient.PATCH("/work-items/{id}/status", {
    params: { path: { id } },
    body: { status },
  });
  if (error) throw error;
  return data;
}
