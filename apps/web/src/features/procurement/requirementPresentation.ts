import type { components } from "@/lib/api/generated/schema";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

export type RequirementPresentationKind =
  | "split-incomplete"
  | "claimed"
  | "source-discrepancy"
  | "archived"
  | "split-source"
  | "unit-mismatch"
  | "draft"
  | "rfq-eligible"
  | "unavailable";

export type RequirementBadgeType =
  | "archived" | "split" | "split-incomplete" | "superseded"
  | "claimed" | "draft" | "reviewed" | "rfq-ready"
  | "unit-mismatch" | "source-changed" | "source-removed" | "unavailable";

export type RequirementBadge = { type: RequirementBadgeType; label: string };

export type RequirementBlocker =
  | "terminal" | "claimed" | "split-incomplete"
  | "source-change-unresolved" | "unit-mismatch-unresolved" | "not-reviewed";

export type RequirementPrimaryAction =
  | { type: "review"; requirementId: string; expectedRevision: number }
  | { type: "acknowledge-unit"; requirementId: string; expectedRevision: number }
  | { type: "resolve-discrepancy"; requirementId: string }
  | { type: "add-to-rfq"; requirementId: string; expectedRequirementRevision: number }
  | { type: "view-rfq"; rfqChainId: string; rfqNumber: string; isSelectedRfq: boolean }
  | { type: "view-split-children"; requirementId: string };

export type RequirementSecondaryAction = { type: "reject"; requirementId: string; expectedRevision: number };

export type RequirementActionState = {
  kind: RequirementPresentationKind;
  primaryAction: RequirementPrimaryAction | null;
  secondaryAction: RequirementSecondaryAction | null;
  badges: RequirementBadge[];
  blockers: RequirementBlocker[];
  message?: string;
};

export type RequirementUIContext = {
  workItemName?: string;
  // The RFQ currently open in the page, if any. Only used to decide whether
  // "Add to RFQ" is offered on a reviewed+eligible requirement — the claimed
  // "In {activeRfqNumber}" badge always comes from the requirement's own
  // authoritative activeRfqNumber, never from this field, since a claimed
  // requirement may belong to a different chain than the one on screen.
  selectedRfq?: { id: string; number: string; status: string; revision: number };
};

function statusBadge(status: string): RequirementBadge | undefined {
  switch (status) {
    case "draft":
      return { type: "draft", label: "Draft" };
    case "reviewed":
      return { type: "reviewed", label: "Reviewed" };
    case "archived":
      return { type: "archived", label: "Archived" };
    case "split":
      return { type: "split", label: "Split" };
    default:
      return undefined;
  }
}

function unitMismatchBadge(requirement: MaterialRequirement): RequirementBadge | undefined {
  return requirement.unitMismatch && !requirement.unitMismatchAcknowledged
    ? { type: "unit-mismatch", label: "Unit mismatch" }
    : undefined;
}

function sourceStateBadge(requirement: MaterialRequirement): RequirementBadge | undefined {
  if (requirement.sourceSyncState === "change_detected") return { type: "source-changed", label: "Source changed" };
  if (requirement.sourceSyncState === "source_removed") return { type: "source-removed", label: "Source removed" };
  return undefined;
}

// A pure function: authoritative MaterialRequirement + UI context -> exactly
// one primary next action, with composable badges/blockers describing every
// applicable fact. Each branch returns immediately — evaluated in the exact
// order documented in docs/superpowers/specs/2026-08-13-procurement-workflow-redesign-design.md §5.
export function deriveRequirementUIState(
  requirement: MaterialRequirement,
  context: RequirementUIContext
): RequirementActionState {
  // 1. splitState === "creating" — checked first. The backend sets
  // status=split and splitState=creating atomically before creating
  // children, so an interrupted split always has both true; checking
  // splitState first is what makes this state reachable at all.
  if (requirement.splitState === "creating") {
    return {
      kind: "split-incomplete",
      primaryAction: null,
      secondaryAction: null,
      badges: [{ type: "split-incomplete", label: "Split incomplete" }],
      blockers: ["terminal", "split-incomplete"],
      message: "The requirement split did not complete and requires reconciliation.",
    };
  }

  // 2. Claimed — checked before the discrepancy branch because a claim
  // freezes every contractor field; the backend's availableActions returns
  // nothing at all for a claimed anchor.
  if (requirement.activeRfqChainId != null) {
    const badges: RequirementBadge[] = [
      { type: "claimed", label: `In ${requirement.activeRfqNumber ?? "RFQ"}` },
    ];
    const status = statusBadge(requirement.status);
    if (status) badges.push(status);
    const sourceBadge = sourceStateBadge(requirement);
    if (sourceBadge) badges.push(sourceBadge);
    const unitBadge = unitMismatchBadge(requirement);
    if (unitBadge) badges.push(unitBadge);

    return {
      kind: "claimed",
      primaryAction: {
        type: "view-rfq",
        rfqChainId: requirement.activeRfqChainId,
        rfqNumber: requirement.activeRfqNumber ?? "RFQ",
        isSelectedRfq: context.selectedRfq?.id === requirement.activeRfqChainId,
      },
      secondaryAction: null,
      badges,
      blockers: ["claimed"],
    };
  }

  // 3. Unresolved source discrepancy — checked before the terminal-status
  // branches below. A terminal anchor can still have a genuinely resolvable
  // discrepancy: the backend's availableActions always offers keep_current
  // on a terminal anchor, and offers create_separate too when the delta is
  // positive; only update/merge are withheld. Hiding this branch behind
  // archived/split would make a legitimately resolvable discrepancy
  // permanently unreachable in the UI.
  if (requirement.sourceSyncState !== "clean") {
    const isTerminal = requirement.status === "archived" || requirement.status === "split";
    const badges: RequirementBadge[] = [];
    const sourceBadge = sourceStateBadge(requirement);
    if (sourceBadge) badges.push(sourceBadge);
    const status = statusBadge(requirement.status);
    if (status) badges.push(status);

    const message =
      requirement.sourceSyncState === "source_removed"
        ? "The costing data previously backing this requirement is no longer present."
        : "The costing data behind this requirement has changed.";

    return {
      kind: "source-discrepancy",
      primaryAction: { type: "resolve-discrepancy", requirementId: requirement.id },
      secondaryAction: null,
      badges,
      blockers: isTerminal ? ["source-change-unresolved", "terminal"] : ["source-change-unresolved"],
      message,
    };
  }

  // 4. Archived (discrepancy already resolved/clean)
  if (requirement.status === "archived") {
    return {
      kind: "archived",
      primaryAction: null,
      secondaryAction: null,
      badges: [{ type: "archived", label: "Archived" }],
      blockers: ["terminal"],
    };
  }

  // 5. Completed split source (discrepancy already resolved/clean)
  if (requirement.status === "split") {
    return {
      kind: "split-source",
      primaryAction: { type: "view-split-children", requirementId: requirement.id },
      secondaryAction: null,
      badges: [
        { type: "split", label: "Split" },
        { type: "superseded", label: "Superseded" },
      ],
      blockers: ["terminal"],
    };
  }

  // 6. Unresolved unit mismatch
  if (requirement.unitMismatch && !requirement.unitMismatchAcknowledged) {
    const badges: RequirementBadge[] = [{ type: "unit-mismatch", label: "Unit mismatch" }];
    const status = statusBadge(requirement.status);
    if (status) badges.push(status);

    return {
      kind: "unit-mismatch",
      primaryAction: { type: "acknowledge-unit", requirementId: requirement.id, expectedRevision: requirement.revision },
      secondaryAction: null,
      badges,
      blockers: ["unit-mismatch-unresolved"],
    };
  }

  // 7. Draft
  if (requirement.status === "draft") {
    return {
      kind: "draft",
      primaryAction: { type: "review", requirementId: requirement.id, expectedRevision: requirement.revision },
      secondaryAction: { type: "reject", requirementId: requirement.id, expectedRevision: requirement.revision },
      badges: [{ type: "draft", label: "Draft" }],
      blockers: ["not-reviewed"],
      message: "This requirement must be confirmed before procurement.",
    };
  }

  // 8. Reviewed — every condition above cleared
  if (requirement.status === "reviewed") {
    const badges: RequirementBadge[] = [
      { type: "reviewed", label: "Reviewed" },
      { type: "rfq-ready", label: "Ready for RFQ" },
    ];
    if (context.selectedRfq?.status === "draft") {
      return {
        kind: "rfq-eligible",
        primaryAction: {
          type: "add-to-rfq",
          requirementId: requirement.id,
          expectedRequirementRevision: requirement.revision,
        },
        secondaryAction: null,
        badges,
        blockers: [],
      };
    }
    return {
      kind: "rfq-eligible",
      primaryAction: null,
      secondaryAction: null,
      badges,
      blockers: [],
      message: "Select a draft RFQ to add this requirement.",
    };
  }

  // 9. Defensive fallback
  return {
    kind: "unavailable",
    primaryAction: null,
    secondaryAction: null,
    badges: [{ type: "unavailable", label: "Unavailable" }],
    blockers: [],
  };
}
