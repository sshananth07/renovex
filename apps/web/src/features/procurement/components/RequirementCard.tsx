import type { components } from "@/lib/api/generated/schema";
import { deriveRequirementUIState, type RequirementPrimaryAction as Action, type RequirementSecondaryAction as SecondaryAction, type RequirementUIContext } from "../requirementPresentation";
import { RequirementBadges } from "./RequirementBadges";
import { RequirementPrimaryAction, RequirementSecondaryAction } from "./RequirementPrimaryAction";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

type Props = {
  requirement: MaterialRequirement;
  context: RequirementUIContext;
  pending: boolean;
  onAction: (action: Action) => void;
  onSecondaryAction: (action: SecondaryAction) => void;
};

// Source-type label, checked in this order — provenance before raw type,
// since a create_separate child is stored with sourceType=manual but is
// system-derived, not contractor-entered.
function sourceLabel(requirement: MaterialRequirement): string {
  if (requirement.createdFromDiscrepancyRequirementId) return "Created from source change";
  switch (requirement.sourceType) {
    case "manual":
      return "Manual";
    case "cost_item":
      return "Generated";
    case "split":
      return "Split batch";
    default:
      return requirement.sourceType;
  }
}

// A pure renderer: it derives the presentation state and lays out identity
// fields, badges, message, and one primary action. It never fetches data
// and never decides business legality — that's deriveRequirementUIState's
// job, driven entirely by the already-loaded requirement and context.
export function RequirementCard({ requirement, context, pending, onAction, onSecondaryAction }: Props) {
  const state = deriveRequirementUIState(requirement, context);

  return (
    <article className="surface-card flex flex-col gap-2 p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="font-heading font-semibold">{requirement.materialName}</h3>
          <p className="font-mono text-sm text-muted-foreground">
            {requirement.requiredQuantity.value} {requirement.requiredQuantity.unit}
          </p>
        </div>
        <RequirementBadges badges={state.badges} />
      </div>

      {requirement.specification && <p className="text-sm">{requirement.specification}</p>}

      <p className="text-xs text-muted-foreground">
        <span>{context.workItemName ?? (requirement.workItemId ? "Work item" : "Project-level requirement")}</span>
        <span aria-hidden="true"> · </span>
        <span>{sourceLabel(requirement)}</span>
      </p>

      {state.message && <p className="text-sm text-muted-foreground">{state.message}</p>}

      <div className="flex justify-end gap-2">
        <RequirementSecondaryAction action={state.secondaryAction} pending={pending} onAction={onSecondaryAction} />
        <RequirementPrimaryAction action={state.primaryAction} pending={pending} onAction={onAction} />
      </div>
    </article>
  );
}
