"use client";

import { useState } from "react";
import { Check } from "lucide-react";
import type { Suggestion } from "../presentation";

interface ResolvedSuggestionsSummaryProps {
  resolved: Suggestion[];
  renderCard: (suggestion: Suggestion) => React.ReactNode;
  itemNoun: string;
}

// Collapses N ≥ 2 already-resolved (accepted/modified/rejected) suggestions
// into one compact summary row instead of repeating "Added to project" once
// per card (T1.5 PART B §10). A single resolved item stays inline — there's
// nothing to collapse yet.
export function ResolvedSuggestionsSummary({ resolved, renderCard, itemNoun }: ResolvedSuggestionsSummaryProps) {
  const [expanded, setExpanded] = useState(false);

  if (resolved.length === 0) return null;
  if (resolved.length === 1) return <>{renderCard(resolved[0])}</>;

  const acceptedCount = resolved.filter((s) => s.status === "accepted" || s.status === "modified").length;
  const rejectedCount = resolved.length - acceptedCount;

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex items-center justify-between rounded-lg border border-dashed border-border p-3 text-left text-sm text-muted-foreground hover:bg-muted/30"
      >
        <span className="flex items-center gap-1.5">
          <Check className="size-3.5 text-primary" />
          {acceptedCount} {itemNoun}
          {acceptedCount === 1 ? "" : "s"} added
          {rejectedCount > 0 ? `, ${rejectedCount} rejected` : ""}
        </span>
        <span className="text-xs font-medium">{expanded ? "Hide details" : "Show details"}</span>
      </button>
      {expanded && <div className="flex flex-col gap-2">{resolved.map((s) => <div key={s.id}>{renderCard(s)}</div>)}</div>}
    </div>
  );
}
