import type { Batch } from "./api";

// The backend already returns batches newest-first (ai/repository_mongo.go)
// — this just walks that order looking for the first completed one, rather
// than re-sorting.
export function latestCompletedBatch(batches: Batch[]): Batch | undefined {
  return batches.find((b) => b.status === "completed");
}

interface PendingCounts {
  spaces: number;
  workItems: number;
  resources: number;
}

interface SetupCompletionInput {
  spaceBatches: Batch[];
  workItemBatches: Batch[];
  resourceBatches: Batch[];
  pendingCounts: PendingCounts;
}

// Setup is "complete" once every stage has produced at least one completed
// batch and nothing from any stage is still pending review (T1.5 PART B
// §10) — accepting/rejecting every suggestion isn't required, only that
// none are left in limbo.
export function isSetupComplete(input: SetupCompletionInput): boolean {
  if (!latestCompletedBatch(input.spaceBatches)) return false;
  if (!latestCompletedBatch(input.workItemBatches)) return false;
  if (!latestCompletedBatch(input.resourceBatches)) return false;
  return input.pendingCounts.spaces === 0 && input.pendingCounts.workItems === 0 && input.pendingCounts.resources === 0;
}
