"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { confidenceLabel, scopeOriginLabel, type Suggestion } from "../presentation";
import type { Space } from "@/features/spaces/api";
import type { WorkItemAcceptanceInput } from "./types";

interface WorkItemSuggestedData {
  description: string;
  workType: string;
  scopeLevel: "space" | "project";
  spaceId: string | null;
  scopeOrigin: string;
}

interface WorkItemSuggestionCardProps {
  suggestion: Suggestion;
  spaces: Space[];
  onAccept: (input: WorkItemAcceptanceInput) => void;
  onReject: () => void;
  accepting: boolean;
  rejecting: boolean;
}

// quantityValue validation mirrors work-items/schemas.ts's positive-decimal
// rule exactly — the AI suggestion itself never supplies quantity/unit, so
// this contractor-only input reuses the same "positive number" client-side
// check rather than inventing a divergent parser (design doc plan Task 14
// Step 3).
function isPositiveDecimal(value: string): boolean {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0;
}

export function WorkItemSuggestionCard({
  suggestion,
  spaces,
  onAccept,
  onReject,
  accepting,
  rejecting,
}: WorkItemSuggestionCardProps) {
  const data = suggestion.suggestedData as unknown as WorkItemSuggestedData;
  const [quantityValue, setQuantityValue] = useState("");
  const [quantityUnit, setQuantityUnit] = useState("");
  const [errors, setErrors] = useState<{ quantityValue?: string; quantityUnit?: string }>({});

  if (suggestion.status === "accepted" || suggestion.status === "modified") {
    return (
      <div className="surface-card flex items-center justify-between p-4">
        <div>
          <p className="text-sm font-medium">Added to project</p>
          <p className="text-xs text-muted-foreground">{data.description}</p>
        </div>
      </div>
    );
  }

  if (suggestion.status === "rejected") {
    return (
      <div className="rounded-lg border border-dashed border-border p-4 opacity-60">
        <p className="text-sm font-medium">{data.description}</p>
        <p className="text-xs text-muted-foreground">Rejected</p>
      </div>
    );
  }

  const label = confidenceLabel(suggestion.confidence);
  const spaceName = data.spaceId ? spaces.find((s) => s.id === data.spaceId)?.name ?? "Unknown space" : null;

  const handleAccept = () => {
    const newErrors: typeof errors = {};
    if (!quantityValue.trim()) newErrors.quantityValue = "Quantity is required";
    else if (!isPositiveDecimal(quantityValue)) newErrors.quantityValue = "Enter a positive number";
    if (!quantityUnit.trim()) newErrors.quantityUnit = "Unit is required";
    setErrors(newErrors);
    if (Object.keys(newErrors).length > 0) return;

    onAccept({
      description: data.description,
      workType: data.workType,
      scopeLevel: data.scopeLevel,
      spaceId: data.spaceId,
      quantityValue,
      quantityUnit,
    });
  };

  return (
    <div className="surface-card flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold">{data.description}</p>
          {spaceName && <p className="text-xs text-muted-foreground">{spaceName}</p>}
          {suggestion.rationale && <p className="mt-0.5 text-xs text-muted-foreground">{suggestion.rationale}</p>}
          <p className="mt-1 text-xs font-medium text-primary">{scopeOriginLabel(data.scopeOrigin)}</p>
        </div>
        {label && (
          <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-xs font-medium text-accent-foreground">
            {`Confidence: ${label}`}
          </span>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor={`qty-${suggestion.id}`}>Quantity</Label>
          <Input
            id={`qty-${suggestion.id}`}
            inputMode="decimal"
            value={quantityValue}
            onChange={(e) => setQuantityValue(e.target.value)}
          />
          {errors.quantityValue && <p role="alert" className="text-xs text-destructive">{errors.quantityValue}</p>}
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`unit-${suggestion.id}`}>Unit</Label>
          <Input
            id={`unit-${suggestion.id}`}
            value={quantityUnit}
            onChange={(e) => setQuantityUnit(e.target.value)}
          />
          {errors.quantityUnit && <p role="alert" className="text-xs text-destructive">{errors.quantityUnit}</p>}
        </div>
      </div>

      <div className="flex gap-2">
        <Button size="sm" disabled={accepting} onClick={handleAccept}>
          {accepting ? "Adding…" : "Accept"}
        </Button>
        <Button size="sm" variant="outline" disabled={rejecting} onClick={onReject}>
          {rejecting ? "…" : "Reject"}
        </Button>
      </div>
    </div>
  );
}
