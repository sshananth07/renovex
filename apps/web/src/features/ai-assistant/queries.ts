import { useQuery } from "@tanstack/react-query";
import { listBatches, listMaterials, listSuggestionsByBatch, listWorkResourceRequirements, type BatchType } from "./api";
import { aiKeys } from "./queryKeys";

export function useAIBatches(projectId: string, type?: BatchType) {
  return useQuery({
    queryKey: aiKeys.batches(projectId, type),
    queryFn: () => listBatches(projectId, type),
    enabled: Boolean(projectId),
  });
}

export function useAISuggestions(batchId: string) {
  return useQuery({
    queryKey: aiKeys.suggestions(batchId),
    queryFn: () => listSuggestionsByBatch(batchId),
    enabled: Boolean(batchId),
  });
}

export function useWorkResourceRequirements(projectId: string, workItemId?: string) {
  return useQuery({
    queryKey: aiKeys.resources(projectId, workItemId),
    queryFn: () => listWorkResourceRequirements(projectId, workItemId),
    enabled: Boolean(projectId),
  });
}

export function useMaterialsCatalog() {
  return useQuery({
    queryKey: aiKeys.materialsCatalog(),
    queryFn: () => listMaterials(),
  });
}
