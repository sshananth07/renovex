import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import type { ListParams } from "@/lib/url/listParams";

export type Space = components["schemas"]["SpaceDTO"];
export type CreateSpaceInput = components["schemas"]["CreateSpaceInputBody"];
export type UpdateSpaceInput = components["schemas"]["UpdateSpaceInputBody"];

export async function listSpaces(projectId: string, params: ListParams) {
  const { data, error } = await apiClient.GET("/spaces", {
    params: { query: { projectId, ...params } },
  });
  if (error) throw error;
  return data;
}

export async function getSpace(id: string) {
  const { data, error } = await apiClient.GET("/spaces/{id}", {
    params: { path: { id } },
  });
  if (error) throw error;
  return data;
}

export async function createSpace(input: CreateSpaceInput) {
  const { data, error } = await apiClient.POST("/spaces", { body: input });
  if (error) throw error;
  return data;
}

// name/type/description are always resubmitted in full — UpdateSpaceInputBody
// requires `name` and unconditionally overwrites `type`/`description`
// (plain strings, no optional wrapper) on the Go side, so omitting either
// silently blanks it. Not a genuine partial PATCH, unlike Clients/WorkItems.
export async function updateSpace(id: string, input: UpdateSpaceInput) {
  const { data, error } = await apiClient.PATCH("/spaces/{id}", {
    params: { path: { id } },
    body: input,
  });
  if (error) throw error;
  return data;
}
