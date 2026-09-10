import { useQuery } from "@tanstack/react-query";
import type { ListParams } from "@/lib/url/listParams";
import { listWorkItems } from "./api";

// GET /work-items requires exactly one of projectId/spaceId server-side
// (422 otherwise) — this hook is Project-scoped only; a Space-scoped variant
// would be a separate hook, not an overload, to keep that constraint explicit
// rather than accidentally satisfiable with both or neither.
export function useWorkItemList(projectId: string, params: ListParams) {
  return useQuery({
    queryKey: ["projects", projectId, "work-items", params],
    queryFn: () => listWorkItems(projectId, params),
    enabled: Boolean(projectId),
  });
}

// Total-only usage for ProjectOverview until F1.8 builds the full Work Items list UI.
export function useProjectWorkItemsTotal(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "work-items", { page: 1 }],
    queryFn: () => listWorkItems(projectId, { page: 1, pageSize: 1 }),
    enabled: Boolean(projectId),
  });
}
