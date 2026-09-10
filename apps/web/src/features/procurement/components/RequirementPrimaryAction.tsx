import { Button } from "@/components/ui/button";
import type { RequirementPrimaryAction as Action, RequirementSecondaryAction as SecondaryAction } from "../requirementPresentation";

type Props = {
  action: Action | null;
  pending: boolean;
  onAction: (action: Action) => void;
};

type SecondaryProps = {
  action: SecondaryAction | null;
  pending: boolean;
  onAction: (action: SecondaryAction) => void;
};

// Same "looks, not legality" contract as RequirementPrimaryAction — the only
// secondary action today is removing a draft requirement, offered alongside
// "Confirm requirement" so a generated draft can be dismissed without
// leaving it stuck unreviewed. Unlike every other action here, clicking
// this does not mutate directly — onAction opens the Archive-vs-Delete
// choice dialog, since "reject" is ambiguous between the two.
export function RequirementSecondaryAction({ action, pending, onAction }: SecondaryProps) {
  if (!action) return null;

  switch (action.type) {
    case "reject":
      return (
        <Button size="sm" variant="outline" disabled={pending} onClick={() => onAction(action)}>
          Remove requirement
        </Button>
      );
    default:
      return null;
  }
}

// A dumb switch over action.type: it decides how an action LOOKS (label,
// disabled state), never whether it's legal — that decision was already
// made by deriveRequirementUIState. Navigation actions (view-rfq,
// view-split-children) never show a mutation-pending label.
export function RequirementPrimaryAction({ action, pending, onAction }: Props) {
  if (!action) return null;

  switch (action.type) {
    case "review":
      return (
        <Button size="sm" disabled={pending} onClick={() => onAction(action)}>
          {pending ? "Confirming…" : "Confirm requirement"}
        </Button>
      );
    case "acknowledge-unit":
      return (
        <Button size="sm" disabled={pending} onClick={() => onAction(action)}>
          {pending ? "Acknowledging…" : "Acknowledge unit"}
        </Button>
      );
    case "resolve-discrepancy":
      return (
        <Button size="sm" variant="outline" disabled={pending} onClick={() => onAction(action)}>
          Review source change
        </Button>
      );
    case "add-to-rfq":
      return (
        <Button size="sm" disabled={pending} onClick={() => onAction(action)}>
          {pending ? "Adding…" : "Add to RFQ"}
        </Button>
      );
    case "view-rfq":
      return (
        <Button size="sm" variant="outline" onClick={() => onAction(action)}>
          {action.isSelectedRfq ? "View RFQ scope" : `View ${action.rfqNumber}`}
        </Button>
      );
    case "view-split-children":
      return (
        <Button size="sm" variant="outline" onClick={() => onAction(action)}>
          View split batches
        </Button>
      );
    default:
      return null;
  }
}
