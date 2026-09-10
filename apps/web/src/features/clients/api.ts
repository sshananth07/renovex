import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import type { ListParams } from "@/lib/url/listParams";

export type Client = components["schemas"]["ClientDTO"];
export type CreateClientInput = components["schemas"]["CreateClientInputBody"];
export type UpdateClientInput = components["schemas"]["UpdateClientInputBody"];

export async function listClients(params: ListParams) {
  const { data, error } = await apiClient.GET("/clients", {
    params: { query: params },
  });
  if (error) throw error;
  return data;
}

export async function getClient(id: string) {
  const { data, error } = await apiClient.GET("/clients/{id}", {
    params: { path: { id } },
  });
  if (error) throw error;
  return data;
}

export async function createClient(input: CreateClientInput) {
  const { data, error } = await apiClient.POST("/clients", { body: input });
  if (error) throw error;
  return data;
}

export async function updateClient(id: string, input: UpdateClientInput) {
  const { data, error } = await apiClient.PATCH("/clients/{id}", {
    params: { path: { id } },
    body: input,
  });
  if (error) throw error;
  return data;
}
