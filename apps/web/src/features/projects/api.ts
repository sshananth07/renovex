import { apiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import type { ListParams } from "@/lib/url/listParams";
import type { ProjectStatus } from "./statusPresentation";

export type Project = components["schemas"]["ProjectDTO"];
export type CreateProjectInput = components["schemas"]["CreateProjectInputBody"];

export async function listProjects(params: ListParams) {
  const { data, error } = await apiClient.GET("/projects", {
    params: { query: params },
  });
  if (error) throw error;
  return data;
}

export async function getProject(id: string) {
  const { data, error } = await apiClient.GET("/projects/{id}", {
    params: { path: { id } },
  });
  if (error) throw error;
  return data;
}

export async function listClientProjects(clientId: string) {
  const { data, error } = await apiClient.GET("/projects", { params: { query: { clientId, page: 1, pageSize: 100 } } });
  if (error) throw error;
  return data;
}

export async function createProject(input: CreateProjectInput) {
  const { data, error } = await apiClient.POST("/projects", { body: input });
  if (error) throw error;
  return data;
}

export async function renameProject(projectId: string, name: string) {
  const { data, error } = await apiClient.PATCH("/projects/{projectId}", {
    params: { path: { projectId } },
    body: { name },
  });
  if (error) throw error;
  return data;
}

export async function updateProjectStatus(projectId: string, status: ProjectStatus) {
  const { data, error } = await apiClient.PATCH("/projects/{id}/status", {
    params: { path: { id: projectId } },
    body: { status },
  });
  if (error) throw error;
  return data;
}
