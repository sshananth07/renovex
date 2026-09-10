"use client";

import { Button } from "@/components/ui/button";
import { confidenceLabel, type Suggestion } from "../presentation";
import { MaterialResourceAcceptDialog } from "./MaterialResourceAcceptDialog";
import type { Material, NewMaterialFormInput } from "./types";

interface ResourceSuggestedData {
  workItemId: string;
  name: string;
  candidateMaterialId?: string | null;
}

interface ResourceSuggestionCardProps {
  suggestion: Suggestion;
  otherMaterials: Material[];
  onAcceptExistingMaterial: (materialId: string) => void;
  onCreateAndAddMaterial: (input: NewMaterialFormInput) => void;
  onAcceptResource: () => void;
  onReject: () => void;
  submitting: boolean;
}

const resourceTypeLabels: Record<string, string> = {
  material_resource: "Material",
  trade_resource: "Trade",
  equipment_resource: "Equipment",
};

export function ResourceSuggestionCard({
  suggestion,
  otherMaterials,
  onAcceptExistingMaterial,
  onCreateAndAddMaterial,
  onAcceptResource,
  onReject,
  submitting,
}: ResourceSuggestionCardProps) {
  const data = suggestion.suggestedData as unknown as ResourceSuggestedData;
  const label = confidenceLabel(suggestion.confidence);
  const typeLabel = resourceTypeLabels[suggestion.type] ?? suggestion.type;

  if (suggestion.status === "accepted" || suggestion.status === "modified") {
    return (
      <div className="surface-card flex items-center justify-between p-4">
        <div>
          <p className="text-sm font-medium">Added to project</p>
          <p className="text-xs text-muted-foreground">{data.name}</p>
        </div>
      </div>
    );
  }

  if (suggestion.status === "rejected") {
    return (
      <div className="rounded-lg border border-dashed border-border p-4 opacity-60">
        <p className="text-sm font-medium">{data.name}</p>
        <p className="text-xs text-muted-foreground">Rejected</p>
      </div>
    );
  }

  if (suggestion.type === "material_resource") {
    const candidate: Material | null = data.candidateMaterialId
      ? otherMaterials.find((m) => m.id === data.candidateMaterialId) ?? { id: data.candidateMaterialId, name: data.name }
      : null;
    const remainingOthers = candidate
      ? otherMaterials.filter((m) => m.id !== candidate.id)
      : otherMaterials;

    return (
      <MaterialResourceAcceptDialog
        suggestedName={data.name}
        candidate={candidate}
        otherMaterials={remainingOthers}
        onAcceptExisting={onAcceptExistingMaterial}
        onCreateAndAdd={onCreateAndAddMaterial}
        onReject={onReject}
        submitting={submitting}
      />
    );
  }

  return (
    <div className="surface-card flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold">{data.name}</p>
          <p className="text-xs text-muted-foreground">{typeLabel}</p>
          {suggestion.rationale && <p className="mt-0.5 text-xs text-muted-foreground">{suggestion.rationale}</p>}
        </div>
        {label && (
          <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-xs font-medium text-accent-foreground">
            {`Confidence: ${label}`}
          </span>
        )}
      </div>
      <div className="flex gap-2">
        <Button size="sm" disabled={submitting} onClick={onAcceptResource}>
          Accept
        </Button>
        <Button size="sm" variant="outline" disabled={submitting} onClick={onReject}>
          Reject
        </Button>
      </div>
    </div>
  );
}
