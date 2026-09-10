import { useQuery } from "@tanstack/react-query";
import { getProjectProperty } from "./api";

// Returns Property | null (create-prompt state) rather than a paginated
// list — deliberately NOT reusing the Clients/Spaces list-hook shape, since
// GET /properties has its own non-canonical envelope (see api.ts).
export function useProjectProperty(projectId: string) {
  return useQuery({
    queryKey: ["projects", projectId, "property"],
    queryFn: () => getProjectProperty(projectId),
    enabled: Boolean(projectId),
  });
}
