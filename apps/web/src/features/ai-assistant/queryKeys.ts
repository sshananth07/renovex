import type { BatchType } from "./api";

export const aiKeys = {
  batches: (projectId: string, type?: BatchType) => ["ai", "batches", projectId, type] as const,
  suggestions: (batchId: string) => ["ai", "suggestions", batchId] as const,
  resources: (projectId: string, workItemId?: string) => ["ai", "resources", projectId, workItemId] as const,
  materialsCatalog: () => ["ai", "materials-catalog"] as const,
};
