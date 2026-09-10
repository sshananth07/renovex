import { useQuery } from "@tanstack/react-query";
import type { ListParams } from "@/lib/url/listParams";
import { getClient, listClients } from "./api";

// Query keys include every server-visible filter so each distinct
// page/search/sort/order combination caches independently (architecture
// doc §7.1/§19).
export function useClientList(params: ListParams) {
  return useQuery({
    queryKey: ["clients", params],
    queryFn: () => listClients(params),
  });
}

export function useClient(id: string) {
  return useQuery({
    queryKey: ["clients", id],
    queryFn: () => getClient(id),
    enabled: Boolean(id),
  });
}
