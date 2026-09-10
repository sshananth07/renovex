import type { Suggestion as GeneratedSuggestion } from "./api";

export type Suggestion = GeneratedSuggestion;

interface BackendProblem {
  type?: string;
  detail?: string;
  title?: string;
}

// The backend collapses "wrong revision", "already reviewed", and
// "conflicting current project state" into one RFC 7807 problem with the
// stable urn:renovex:problem:ai-stale-suggestion type (see ai/handler.go's
// mapHandlerError) rather than a distinguishable English detail string —
// this is the one place the frontend branches on that URN so every
// accept/reject caller shows the same design-doc §19 wording instead of the
// generic backend detail ("suggestion was already reviewed or has changed").
export function acceptRejectErrorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  if (problem?.type === "urn:renovex:problem:ai-stale-suggestion") {
    return "This suggestion was generated from an older project state. Please review the current project and regenerate suggestions if needed.";
  }
  return problem?.detail ?? problem?.title ?? "Could not save this. Please try again.";
}

// Coarse confidence label only — never render a raw model confidence
// percentage as though it were a calibrated probability (design doc §25.1).
export function confidenceLabel(confidence: number | null | undefined): "High" | "Medium" | "Low" | null {
  if (confidence === null || confidence === undefined) return null;
  if (confidence >= 0.8) return "High";
  if (confidence >= 0.55) return "Medium";
  return "Low";
}

const scopeOriginLabels: Record<string, string> = {
  explicit_scope: "Requested scope",
  supporting_scope: "Supporting scope",
  possible_missing_scope: "Possible missing scope",
};

// Advisory wording only — "possible_missing_scope" must never render as a
// confirmed contractor omission (design doc §10.5, §26).
export function scopeOriginLabel(scopeOrigin: string): string {
  return scopeOriginLabels[scopeOrigin] ?? scopeOrigin;
}

interface WorkItemSuggestionData {
  spaceId?: string | null;
}

// Groups Work Item suggestions by their real Space id; scopeLevel=project
// suggestions (spaceId null) land in `projectWide`, never a fabricated
// "Whole Property" bucket (design doc §10.4, §26).
export function groupWorkItemSuggestionsBySpace(suggestions: Suggestion[]): {
  bySpaceId: Record<string, Suggestion[]>;
  projectWide: Suggestion[];
} {
  const bySpaceId: Record<string, Suggestion[]> = {};
  const projectWide: Suggestion[] = [];
  for (const suggestion of suggestions) {
    const data = suggestion.suggestedData as WorkItemSuggestionData;
    if (data?.spaceId) {
      bySpaceId[data.spaceId] = [...(bySpaceId[data.spaceId] ?? []), suggestion];
    } else {
      projectWide.push(suggestion);
    }
  }
  return { bySpaceId, projectWide };
}

interface ResourceSuggestionData {
  workItemId: string;
}

export interface WorkItemResourceGroup {
  materials: Suggestion[];
  trades: Suggestion[];
  equipment: Suggestion[];
}

// Groups Resource suggestions by their WorkItem, then by resource type
// (design doc §29).
export function groupResourceSuggestionsByWorkItem(
  suggestions: Suggestion[]
): Record<string, WorkItemResourceGroup> {
  const grouped: Record<string, WorkItemResourceGroup> = {};
  for (const suggestion of suggestions) {
    const data = suggestion.suggestedData as ResourceSuggestionData;
    const workItemId = data?.workItemId;
    if (!workItemId) continue;
    if (!grouped[workItemId]) {
      grouped[workItemId] = { materials: [], trades: [], equipment: [] };
    }
    if (suggestion.type === "material_resource") {
      grouped[workItemId].materials.push(suggestion);
    } else if (suggestion.type === "trade_resource") {
      grouped[workItemId].trades.push(suggestion);
    } else if (suggestion.type === "equipment_resource") {
      grouped[workItemId].equipment.push(suggestion);
    }
  }
  return grouped;
}
