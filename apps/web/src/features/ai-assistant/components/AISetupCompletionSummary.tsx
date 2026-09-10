"use client";

import { Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { Batch } from "../api";
import { latestCompletedBatch } from "../aiSetupSummary";

interface AISetupCompletionSummaryProps {
  spaceBatches: Batch[];
  spacesTotal: number;
  workItemsTotal: number;
  resourcesTotal: number;
  onViewHistory: () => void;
  onRerun: () => void;
}

// Compact "setup complete" summary (T1.5 PART B §10) shown once every stage
// has a completed batch and nothing is left pending review — replaces the
// long "Added to project / Added to project…" active-review lists a
// completed setup would otherwise keep showing.
export function AISetupCompletionSummary({
  spaceBatches,
  spacesTotal,
  workItemsTotal,
  resourcesTotal,
  onViewHistory,
  onRerun,
}: AISetupCompletionSummaryProps) {
  const latest = latestCompletedBatch(spaceBatches);
  const lastUpdated = latest?.completedAt
    ? new Date(latest.completedAt).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
    : null;

  return (
    <div className="surface-card flex flex-col gap-4 p-5">
      <div className="flex items-center justify-between">
        <h3 className="font-heading text-base font-semibold">Setup complete</h3>
        <Check className="size-5 text-primary" />
      </div>

      <p className="text-sm text-muted-foreground">Your project has been structured into:</p>
      <dl className="grid grid-cols-3 gap-3 text-center">
        <div className="rounded-lg bg-muted/50 p-3">
          <dt className="text-xs text-muted-foreground">Spaces</dt>
          <dd className="mt-1 text-lg font-semibold">{spacesTotal}</dd>
        </div>
        <div className="rounded-lg bg-muted/50 p-3">
          <dt className="text-xs text-muted-foreground">Work Items</dt>
          <dd className="mt-1 text-lg font-semibold">{workItemsTotal}</dd>
        </div>
        <div className="rounded-lg bg-muted/50 p-3">
          <dt className="text-xs text-muted-foreground">Resources</dt>
          <dd className="mt-1 text-lg font-semibold">{resourcesTotal}</dd>
        </div>
      </dl>

      {lastUpdated && <p className="text-xs text-muted-foreground">Last updated {lastUpdated}</p>}

      <ul className="flex flex-col gap-1 text-sm text-muted-foreground">
        <li className="flex items-center gap-1.5">
          <Check className="size-3.5 text-primary" /> Spaces reviewed
        </li>
        <li className="flex items-center gap-1.5">
          <Check className="size-3.5 text-primary" /> Work Items reviewed
        </li>
        <li className="flex items-center gap-1.5">
          <Check className="size-3.5 text-primary" /> Resources reviewed
        </li>
      </ul>

      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={onViewHistory}>
          View setup history
        </Button>
        <Button size="sm" onClick={onRerun}>
          Re-run AI setup
        </Button>
      </div>
    </div>
  );
}
