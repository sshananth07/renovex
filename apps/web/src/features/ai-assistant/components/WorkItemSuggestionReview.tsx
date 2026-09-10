"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useSpaceList } from "@/features/spaces/queries";
import { useGenerateWorkItemSuggestions, useAcceptSuggestion, useRejectSuggestion } from "../mutations";
import { useAIBatches, useAISuggestions } from "../queries";
import { acceptRejectErrorMessage, groupWorkItemSuggestionsBySpace, type Suggestion } from "../presentation";
import { WorkItemSuggestionCard } from "./WorkItemSuggestionCard";
import { ResolvedSuggestionsSummary } from "./ResolvedSuggestionsSummary";
import type { WorkItemAcceptanceInput } from "./types";

interface BackendProblem {
  detail?: string;
  title?: string;
}

function errorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  return problem?.detail ?? problem?.title ?? "We couldn't generate suggestions right now. Your existing project data has not been changed.";
}

export function WorkItemSuggestionReview({ projectId }: { projectId: string }) {
  const spacesQuery = useSpaceList(projectId, { page: 1, pageSize: 100 });
  const spaces = spacesQuery.data?.items ?? [];
  const [explicitBatchId, setExplicitBatchId] = useState<string | null>(null);
  const batchesQuery = useAIBatches(projectId, "work_item_suggestions");
  const generate = useGenerateWorkItemSuggestions(projectId);
  const batchId = explicitBatchId ?? batchesQuery.data?.batches?.[0]?.id ?? null;
  const suggestionsQuery = useAISuggestions(batchId ?? "");
  const accept = useAcceptSuggestion(batchId ?? "", projectId);
  const reject = useRejectSuggestion(batchId ?? "", projectId);
  const [pendingId, setPendingId] = useState<string | null>(null);

  if (spacesQuery.isLoading) return null;

  if (spaces.length === 0) {
    return (
      <section className="surface-card p-4">
        <p className="text-sm text-muted-foreground">
          Generate Work Items is available once at least one confirmed Space exists for this project.
        </p>
      </section>
    );
  }

  const suggestions = batchId ? suggestionsQuery.data?.suggestions ?? generate.data?.suggestions ?? [] : generate.data?.suggestions ?? [];
  const { bySpaceId, projectWide } = groupWorkItemSuggestionsBySpace(suggestions);

  const acceptSuggestion = (suggestionId: string, revision: number, input: WorkItemAcceptanceInput) => {
    setPendingId(suggestionId);
    accept.mutate({
      suggestionId,
      input: {
        expectedRevision: revision,
        workItem: {
          description: input.description,
          workType: input.workType,
          scopeLevel: input.scopeLevel,
          spaceId: input.spaceId ?? "",
          quantityValue: input.quantityValue,
          quantityUnit: input.quantityUnit,
        },
      },
    });
  };

  const rejectSuggestion = (suggestionId: string, revision: number) => {
    setPendingId(suggestionId);
    reject.mutate({ suggestionId, expectedRevision: revision });
  };

  const renderCard = (suggestion: Suggestion) => {
    const isPendingSuggestion = pendingId === suggestion.id;
    const reviewError = isPendingSuggestion && (accept.isError || reject.isError)
      ? acceptRejectErrorMessage(accept.error ?? reject.error)
      : null;
    return (
      <div className="flex flex-col gap-1.5">
        <WorkItemSuggestionCard
          suggestion={suggestion}
          spaces={spaces}
          accepting={accept.isPending && isPendingSuggestion}
          rejecting={reject.isPending && isPendingSuggestion}
          onAccept={(input) => acceptSuggestion(suggestion.id, suggestion.revision, input)}
          onReject={() => rejectSuggestion(suggestion.id, suggestion.revision)}
        />
        {reviewError && (
          <p role="alert" className="text-xs text-destructive">
            {reviewError}
          </p>
        )}
      </div>
    );
  };

  // Active review shows only pending/actionable suggestions per group;
  // accepted/modified/rejected ones collapse into one compact summary row
  // via ResolvedSuggestionsSummary (T1.5B closure — same pattern already
  // used by SpaceSuggestionReview).
  const renderGroup = (groupSuggestions: Suggestion[]) => {
    const pending = groupSuggestions.filter((s) => s.status === "pending");
    const resolved = groupSuggestions.filter((s) => s.status !== "pending");
    return (
      <div className="flex flex-col gap-3">
        {pending.map((s) => (
          <div key={s.id}>{renderCard(s)}</div>
        ))}
        <ResolvedSuggestionsSummary resolved={resolved} renderCard={renderCard} itemNoun="Work Item" />
      </div>
    );
  };

  const handleGenerate = () => {
    generate.mutate(undefined, { onSuccess: (data) => setExplicitBatchId(data.batch.id) });
  };

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h3 className="font-heading text-base font-semibold">Suggested Work Items</h3>
        <Button onClick={handleGenerate} disabled={generate.isPending}>
          {generate.isPending ? "Generating…" : batchId ? "Generate fresh suggestions" : "Generate Work Items"}
        </Button>
      </div>

      {generate.isError && (
        <div className="flex flex-col items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(generate.error)}
          </p>
          <Button size="sm" variant="outline" onClick={handleGenerate} disabled={generate.isPending}>
            Try Again
          </Button>
        </div>
      )}

      {Object.entries(bySpaceId).map(([spaceId, spaceSuggestions]) => {
        const space = spaces.find((s) => s.id === spaceId);
        return (
          <div key={spaceId} className="flex flex-col gap-2">
            <h4 className="text-sm font-semibold">{space?.name ?? "Unknown space"}</h4>
            {renderGroup(spaceSuggestions)}
          </div>
        );
      })}

      {projectWide.length > 0 && (
        <div className="flex flex-col gap-2">
          <h4 className="text-sm font-semibold">Project-wide</h4>
          {renderGroup(projectWide)}
        </div>
      )}
    </section>
  );
}
