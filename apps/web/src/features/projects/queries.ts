import { useQuery } from "@tanstack/react-query";
import type { ListParams } from "@/lib/url/listParams";
import { getProject, listClientProjects, listProjects } from "./api";

export function useProjectList(params: ListParams) {
  return useQuery({
    queryKey: ["projects", params],
    queryFn: () => listProjects(params),
  });
}

export function useProject(id: string) {
  return useQuery({
    queryKey: ["projects", id],
    queryFn: () => getProject(id),
    enabled: Boolean(id),
  });
}

export function useClientProjects(clientId: string) {
  return useQuery({ queryKey: ["clients", clientId, "projects"], queryFn: () => listClientProjects(clientId), enabled: Boolean(clientId) });
}
