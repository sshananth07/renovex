import { useQuery } from "@tanstack/react-query";
import type { ListParams } from "@/lib/url/listParams";
import { getSpace, listSpaces } from "./api";

export function useSpaceList(projectId: string, params: ListParams) {
  return useQuery({
    queryKey: ["projects", projectId, "spaces", params],
    queryFn: () => listSpaces(projectId, params),
    enabled: Boolean(projectId),
  });
}

export function useSpace(id: string) {
  return useQuery({
    queryKey: ["spaces", id],
    queryFn: () => getSpace(id),
    enabled: Boolean(id),
  });
}

// Total-only usage for ProjectOverview until F1.7 builds the full Spaces list UI.
export function useProjectSpacesTotal(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "spaces", { page: 1 }],
    queryFn: () => listSpaces(projectId, { page: 1, pageSize: 1 }),
    enabled: Boolean(projectId),
  });
}
