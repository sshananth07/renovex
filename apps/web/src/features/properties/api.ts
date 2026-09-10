import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";

export type Property = components["schemas"]["PropertyDTO"];
export type CreatePropertyInput = components["schemas"]["CreatePropertyInputBody"];
export type UpdatePropertyInput = components["schemas"]["UpdatePropertyInputBody"];

// GET /properties returns {properties: PropertyDTO[] | null} — NOT the
// canonical {items,page,pageSize,total} envelope used by Clients/Spaces/
// Work Items. A Project has at most one Property (phase1.md §6), so this
// unwraps directly to a single value or null rather than a list.
export async function getProjectProperty(projectId: string): Promise<Property | null> {
  const { data, error } = await apiClient.GET("/properties", {
    params: { query: { projectId } },
  });
  if (error) throw error;
  const [property] = data.properties ?? [];
  return property ?? null;
}

export async function createProperty(input: CreatePropertyInput) {
  const { data, error } = await apiClient.POST("/properties", { body: input });
  if (error) throw error;
  return data;
}

// address is always required on update — NOT a genuine partial PATCH like
// Clients/Work Items (see UpdatePropertyInputBody).
export async function updateProperty(id: string, input: UpdatePropertyInput) {
  const { data, error } = await apiClient.PATCH("/properties/{id}", {
    params: { path: { id } },
    body: input,
  });
  if (error) throw error;
  return data;
}
