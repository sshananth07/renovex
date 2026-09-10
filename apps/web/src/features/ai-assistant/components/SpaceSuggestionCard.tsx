"use client";

import { useState } from "react";
import Link from "next/link";
import { Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { confidenceLabel, type Suggestion } from "../presentation";
import type { SpaceAcceptanceOverride } from "./types";

interface SpaceSuggestionCardProps {
  suggestion: Suggestion;
  onAccept: (override: SpaceAcceptanceOverride | undefined) => void;
  onReject: () => void;
  accepting: boolean;
  rejecting: boolean;
}

interface SpaceSuggestedData {
  name: string;
  spaceType: string;
}

export function SpaceSuggestionCard({ suggestion, onAccept, onReject, accepting, rejecting }: SpaceSuggestionCardProps) {
  const data = suggestion.suggestedData as unknown as SpaceSuggestedData;
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(data.name);
  const [spaceType, setSpaceType] = useState(data.spaceType);

  if (suggestion.status === "accepted" || suggestion.status === "modified") {
    return (
      <div className="surface-card flex items-center justify-between p-4">
        <div className="flex items-center gap-2">
          <span className="flex size-6 items-center justify-center rounded-full bg-primary/10 text-primary">
            <Check className="size-3.5" />
          </span>
          <div>
            <p className="text-sm font-medium">Added to project</p>
            <p className="text-xs text-muted-foreground">{name}</p>
          </div>
        </div>
        {suggestion.acceptedDomainObjectId && (
          <Link href={`/spaces/${suggestion.acceptedDomainObjectId}`} className="text-xs font-semibold text-primary hover:underline">
            View Space
          </Link>
        )}
      </div>
    );
  }

  if (suggestion.status === "rejected") {
    return (
      <div className="flex items-center justify-between rounded-lg border border-dashed border-border p-4 opacity-60">
        <div>
          <p className="text-sm font-medium">{data.name}</p>
          <p className="text-xs text-muted-foreground">Rejected</p>
        </div>
      </div>
    );
  }

  const label = confidenceLabel(suggestion.confidence);

  return (
    <div className="surface-card flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          {editing ? (
            <div className="flex flex-col gap-2">
              <div className="flex flex-col gap-1">
                <Label htmlFor={`name-${suggestion.id}`}>Name</Label>
                <Input id={`name-${suggestion.id}`} value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor={`type-${suggestion.id}`}>Type</Label>
                <Input id={`type-${suggestion.id}`} value={spaceType} onChange={(e) => setSpaceType(e.target.value)} />
              </div>
            </div>
          ) : (
            <>
              <p className="text-sm font-semibold">{data.name}</p>
              {suggestion.rationale && <p className="mt-0.5 text-xs text-muted-foreground">{suggestion.rationale}</p>}
            </>
          )}
          <p className="mt-1 text-xs text-muted-foreground">AI suggestion · Review required</p>
        </div>
        {label && !editing && (
          <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-xs font-medium text-accent-foreground">
            {`Confidence: ${label}`}
          </span>
        )}
      </div>
      <div className="flex gap-2">
        {editing ? (
          <>
            <Button
              size="sm"
              disabled={accepting}
              onClick={() => onAccept({ name, spaceType, description: "" })}
            >
              {accepting ? "Saving…" : "Accept edits"}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </>
        ) : (
          <>
            <Button size="sm" disabled={accepting} onClick={() => onAccept(undefined)}>
              {accepting ? "Adding…" : "Accept"}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
              Edit
            </Button>
            <Button size="sm" variant="outline" disabled={rejecting} onClick={onReject}>
              {rejecting ? "…" : "Reject"}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
