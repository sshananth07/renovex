"use client";

import type { Batch } from "../api";

interface AISetupHistoryProps {
  spaceBatches: Batch[];
  workItemBatches: Batch[];
  resourceBatches: Batch[];
}

function formatDate(iso: string | undefined | null): string {
  if (!iso) return "";
  return new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

// Compact setup-run history (T1.5 PART C §18) — audit/context, not the
// primary workspace. Groups batches by their startedAt timestamp across the
// three stage types rather than a heavy version-management UI; a run with
// no completed batch of a given type simply shows 0 for that count.
export function AISetupHistory({ spaceBatches, workItemBatches, resourceBatches }: AISetupHistoryProps) {
  const runs = [...spaceBatches, ...workItemBatches, ...resourceBatches]
    .filter((b) => b.status === "completed")
    .sort((a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime());

  if (runs.length === 0) {
    return <p className="text-xs text-muted-foreground">No setup history yet.</p>;
  }

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
      <h4 className="text-sm font-semibold">Setup history</h4>
      <ul className="flex flex-col gap-1.5 text-xs text-muted-foreground">
        {runs.map((run) => (
          <li key={run.id}>
            {run.type === "space_suggestions" ? "Space suggestions" : run.type === "work_item_suggestions" ? "Work Item suggestions" : "Resource suggestions"}
            {" · "}
            {formatDate(run.completedAt ?? run.startedAt)}
          </li>
        ))}
      </ul>
    </div>
  );
}
