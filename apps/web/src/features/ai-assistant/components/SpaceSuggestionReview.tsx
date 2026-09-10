"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useGenerateSpaceSuggestions, useAcceptSuggestion, useRejectSuggestion } from "../mutations";
import { useAIBatches, useAISuggestions } from "../queries";
import { acceptRejectErrorMessage, type Suggestion } from "../presentation";
import type { SpaceAcceptanceOverride } from "./types";
import { SpaceSuggestionCard } from "./SpaceSuggestionCard";
import { ResolvedSuggestionsSummary } from "./ResolvedSuggestionsSummary";

interface BackendProblem {
  title?: string;
  detail?: string;
}

function generationErrorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  return problem?.detail ?? problem?.title ?? "We couldn't generate suggestions right now. Your existing project data has not been changed.";
}

export function SpaceSuggestionReview({ projectId }: { projectId: string }) {
  const [explicitBatchId, setExplicitBatchId] = useState<string | null>(null);
  const batchesQuery = useAIBatches(projectId, "space_suggestions");
  const generate = useGenerateSpaceSuggestions(projectId);
  // Existing batches are sorted newest-first by the backend (ai/repository_mongo.go)
  // — the latest completed batch is shown by default so the contractor sees
  // prior suggestions on return visits without re-clicking Generate
  // (design doc §28). Derived directly from the query result rather than
  // synced into state via an effect, since it's fully determined by
  // explicitBatchId/batchesQuery on every render.
  const batchId = explicitBatchId ?? batchesQuery.data?.batches?.[0]?.id ?? null;
  const suggestionsQuery = useAISuggestions(batchId ?? "");
  const accept = useAcceptSuggestion(batchId ?? "", projectId);
  const reject = useRejectSuggestion(batchId ?? "", projectId);
  const [pendingSuggestionId, setPendingSuggestionId] = useState<string | null>(null);

  const handleGenerate = () => {
    generate.mutate(undefined, {
      onSuccess: (data) => setExplicitBatchId(data.batch.id),
    });
  };

  const hasExistingBatch = batchId !== null;
  const suggestions = batchId ? suggestionsQuery.data?.suggestions ?? generate.data?.suggestions ?? [] : generate.data?.suggestions ?? [];
  const pendingSuggestions = suggestions.filter((s) => s.status === "pending");
  const resolvedSuggestions = suggestions.filter((s) => s.status !== "pending");

  const renderCard = (suggestion: Suggestion) => {
    const isPendingSuggestion = pendingSuggestionId === suggestion.id;
    const reviewError = isPendingSuggestion && (accept.isError || reject.isError)
      ? acceptRejectErrorMessage(accept.error ?? reject.error)
      : null;
    return (
      <div className="flex flex-col gap-1.5">
        <SpaceSuggestionCard
          suggestion={suggestion}
          accepting={accept.isPending && isPendingSuggestion}
          rejecting={reject.isPending && isPendingSuggestion}
          onAccept={(override: SpaceAcceptanceOverride | undefined) => {
            setPendingSuggestionId(suggestion.id);
            accept.mutate({
              suggestionId: suggestion.id,
              input: {
                expectedRevision: suggestion.revision,
                ...(override ? { space: { name: override.name, type: override.spaceType, description: override.description } } : {}),
              },
            });
          }}
          onReject={() => {
            setPendingSuggestionId(suggestion.id);
            reject.mutate({ suggestionId: suggestion.id, expectedRevision: suggestion.revision });
          }}
        />
        {reviewError && (
          <p role="alert" className="text-xs text-destructive">
            {reviewError}
          </p>
        )}
      </div>
    );
  };

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="font-heading text-base font-semibold">Suggested Spaces</h3>
          <p className="text-xs text-muted-foreground">AI suggestion · Review required</p>
        </div>
        <Button onClick={handleGenerate} disabled={generate.isPending}>
          {generate.isPending ? "Generating…" : hasExistingBatch ? "Generate fresh suggestions" : "Suggest Spaces"}
        </Button>
      </div>

      {generate.isError && (
        <div className="flex flex-col items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
          <p role="alert" className="text-sm text-destructive">
            {generationErrorMessage(generate.error)}
          </p>
          <Button size="sm" variant="outline" onClick={handleGenerate} disabled={generate.isPending}>
            Try Again
          </Button>
        </div>
      )}

      <div className="flex flex-col gap-3">
        {pendingSuggestions.map((suggestion) => (
          <div key={suggestion.id}>{renderCard(suggestion)}</div>
        ))}
        <ResolvedSuggestionsSummary resolved={resolvedSuggestions} renderCard={renderCard} itemNoun="Space" />
      </div>
    </section>
  );
}
