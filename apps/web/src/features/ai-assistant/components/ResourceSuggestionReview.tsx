"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useWorkItemList } from "@/features/work-items/queries";
import { useGenerateResourceSuggestions, useAcceptSuggestion, useRejectSuggestion } from "../mutations";
import { useAIBatches, useAISuggestions, useMaterialsCatalog } from "../queries";
import { acceptRejectErrorMessage, groupResourceSuggestionsByWorkItem, type Suggestion } from "../presentation";
import { ResourceSuggestionCard } from "./ResourceSuggestionCard";
import { ResolvedSuggestionsSummary } from "./ResolvedSuggestionsSummary";
import type { NewMaterialFormInput } from "./types";

interface BackendProblem {
  detail?: string;
  title?: string;
}

function errorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  return problem?.detail ?? problem?.title ?? "We couldn't generate suggestions right now. Your existing project data has not been changed.";
}

export function ResourceSuggestionReview({ projectId }: { projectId: string }) {
  const workItemsQuery = useWorkItemList(projectId, { page: 1, pageSize: 100 });
  const workItems = workItemsQuery.data?.items ?? [];
  const materialsQuery = useMaterialsCatalog();
  const otherMaterials = (materialsQuery.data?.materials ?? []).map((m) => ({ id: m.id, name: m.name }));
  const [explicitBatchId, setExplicitBatchId] = useState<string | null>(null);
  const batchesQuery = useAIBatches(projectId, "resource_suggestions");
  const generate = useGenerateResourceSuggestions(projectId);
  const batchId = explicitBatchId ?? batchesQuery.data?.batches?.[0]?.id ?? null;
  const suggestionsQuery = useAISuggestions(batchId ?? "");
  const accept = useAcceptSuggestion(batchId ?? "", projectId);
  const reject = useRejectSuggestion(batchId ?? "", projectId);
  const [pendingId, setPendingId] = useState<string | null>(null);

  if (workItemsQuery.isLoading) return null;

  if (workItems.length === 0) {
    return (
      <section className="surface-card p-4">
        <p className="text-sm text-muted-foreground">
          Generate Resources is available once at least one confirmed Work Item exists for this project.
        </p>
      </section>
    );
  }

  const suggestions = batchId ? suggestionsQuery.data?.suggestions ?? generate.data?.suggestions ?? [] : generate.data?.suggestions ?? [];
  const groupedByWorkItem = groupResourceSuggestionsByWorkItem(suggestions);

  const acceptExistingMaterial = (suggestionId: string, revision: number, materialId: string) => {
    setPendingId(suggestionId);
    accept.mutate({
      suggestionId,
      input: { expectedRevision: revision, material: { mode: "existing", materialId } },
    });
  };

  const createAndAddMaterial = (suggestionId: string, revision: number, input: NewMaterialFormInput) => {
    setPendingId(suggestionId);
    accept.mutate({
      suggestionId,
      input: {
        expectedRevision: revision,
        material: {
          mode: "create",
          newMaterial: {
            name: input.name,
            category: input.category,
            specification: input.specification,
            unit: input.unit,
            referencePriceAmount: input.referencePriceAmount,
            referencePriceCurrency: input.referencePriceCurrency,
          },
        },
      },
    });
  };

  const acceptResource = (suggestionId: string, revision: number) => {
    setPendingId(suggestionId);
    accept.mutate({ suggestionId, input: { expectedRevision: revision, resource: {} } });
  };

  const rejectSuggestion = (suggestionId: string, revision: number) => {
    setPendingId(suggestionId);
    reject.mutate({ suggestionId, expectedRevision: revision });
  };

  const handleGenerate = () => {
    generate.mutate(undefined, { onSuccess: (data) => setExplicitBatchId(data.batch.id) });
  };

  const renderCard = (suggestion: Suggestion) => {
    const isPendingSuggestion = pendingId === suggestion.id;
    const reviewError = isPendingSuggestion && (accept.isError || reject.isError)
      ? acceptRejectErrorMessage(accept.error ?? reject.error)
      : null;
    return (
      <div className="flex flex-col gap-1.5">
        <ResourceSuggestionCard
          suggestion={suggestion}
          otherMaterials={otherMaterials}
          submitting={(accept.isPending || reject.isPending) && isPendingSuggestion}
          onAcceptExistingMaterial={(materialId) => acceptExistingMaterial(suggestion.id, suggestion.revision, materialId)}
          onCreateAndAddMaterial={(input) => createAndAddMaterial(suggestion.id, suggestion.revision, input)}
          onAcceptResource={() => acceptResource(suggestion.id, suggestion.revision)}
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

  // Active review shows only pending/actionable suggestions per work item
  // group; accepted/modified/rejected ones collapse into one compact
  // summary row (T1.5B closure — same pattern as SpaceSuggestionReview /
  // WorkItemSuggestionReview).
  const renderGroup = (cards: Suggestion[]) => {
    const pending = cards.filter((s) => s.status === "pending");
    const resolved = cards.filter((s) => s.status !== "pending");
    return (
      <div className="flex flex-col gap-3">
        {pending.map((s) => (
          <div key={s.id}>{renderCard(s)}</div>
        ))}
        <ResolvedSuggestionsSummary resolved={resolved} renderCard={renderCard} itemNoun="Resource" />
      </div>
    );
  };

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h3 className="font-heading text-base font-semibold">Suggested Resources</h3>
        <Button onClick={handleGenerate} disabled={generate.isPending}>
          {generate.isPending ? "Generating…" : batchId ? "Generate fresh suggestions" : "Suggest Resources"}
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

      {Object.entries(groupedByWorkItem).map(([workItemId, group]) => {
        const workItem = workItems.find((wi) => wi.id === workItemId);
        const cards = [...group.materials, ...group.trades, ...group.equipment];
        return (
          <div key={workItemId} className="flex flex-col gap-2">
            <h4 className="text-sm font-semibold">{workItem?.description ?? "Unknown work item"}</h4>
            {renderGroup(cards)}
          </div>
        );
      })}
    </section>
  );
}
