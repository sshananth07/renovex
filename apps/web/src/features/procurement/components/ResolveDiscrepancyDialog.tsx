"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { normalizeApiError, type ApiError } from "@/lib/api/errors";
import { addQuantity, subtractQuantity } from "@/lib/formatting/quantityMath";
import type { components } from "@/lib/api/generated/schema";
import { translateProcurementError } from "../procurementErrors";
import { useSourceDiscrepancy } from "../queries";
import { useResolveSourceDiscrepancy } from "../mutations";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];
type ResolveDiscrepancyBody = components["schemas"]["ResolveDiscrepancyInputBody"];

type Props = {
  open: boolean;
  requirement: MaterialRequirement;
  onClose: () => void;
  onResolved: () => void;
};

type ResolutionActionName = "update" | "merge" | "keep_current" | "create_separate";

function isRawError(error: unknown): error is { status?: number; body?: unknown } {
  return typeof error === "object" && error !== null;
}

// The generated openapi-fetch error shape thrown by unwrapOrThrow doesn't
// carry field errors in a way applyFieldErrors' ApiError type expects
// directly when it comes from React Query's mutation.error (which is
// already an ApiError, per unwrapOrThrow). This narrows an unknown thrown
// value into ApiError for the field-error/workflow-translation pipeline.
function toApiError(error: unknown): ApiError {
  if (isRawError(error) && "kind" in error) return error as ApiError;
  return normalizeApiError({});
}

export function ResolveDiscrepancyDialog({ open, requirement, onClose, onResolved }: Props) {
  const discrepancyQuery = useSourceDiscrepancy(requirement.id, open);
  const resolveMutation = useResolveSourceDiscrepancy(requirement.projectId);

  const [selectedAction, setSelectedAction] = useState<ResolutionActionName | null>(null);
  const [operationId, setOperationId] = useState<string | null>(null);
  const [staleWarning, setStaleWarning] = useState(false);
  const [formError, setFormError] = useState<string | undefined>();

  function handleClose() {
    setSelectedAction(null);
    setOperationId(null);
    setStaleWarning(false);
    setFormError(undefined);
    onClose();
  }

  const discrepancy = discrepancyQuery.data;

  function selectAction(action: ResolutionActionName) {
    setSelectedAction(action);
    setFormError(undefined);
    if (action === "create_separate") {
      // One stable id per logical attempt: generated once when
      // create_separate is first selected, reused across retries of the
      // SAME attempt, only regenerated when the underlying fingerprint/delta
      // actually changes (handled in the stale-discrepancy branch below).
      setOperationId((current) => current ?? crypto.randomUUID());
    }
  }

  async function handleApply() {
    if (!discrepancy || !selectedAction) return;
    setFormError(undefined);

    const body: ResolveDiscrepancyBody = {
      action: selectedAction,
      expectedRevision: discrepancy.requirementRevision,
      expectedProposedFingerprint: discrepancy.proposed.fingerprint,
      ...(selectedAction === "create_separate" && operationId ? { resolutionOperationId: operationId } : {}),
    };

    try {
      await resolveMutation.mutateAsync({ requirementId: requirement.id, body });
      toast.success("Source change resolved.");
      onResolved();
      handleClose();
    } catch (thrown) {
      const error = toApiError(thrown);
      if (error.kind === "api") {
        const detail = error.detail ?? "";

        if (detail.includes("the source changed since the proposal was computed")) {
          // Stale proposal: stay open, refetch, clear the selection and the
          // create_separate operation id (the underlying fingerprint/delta
          // is about to change), and require a fresh choice.
          setSelectedAction(null);
          setOperationId(null);
          setStaleWarning(true);
          await discrepancyQuery.refetch();
          return;
        }
        if (detail.includes("claimed by an active RFQ chain") || detail.includes("already claimed by another rfq chain")) {
          handleClose();
          return;
        }

        // This dialog has no RHF form to map field-scoped errors onto (it's
        // a radio selection, not a text-input form), so a field error is
        // shown directly rather than via applyFieldErrors/setError.
        if (error.fieldErrors.length > 0) {
          setFormError(error.fieldErrors[0]?.message ?? translateProcurementError(error).message);
          return;
        }
      }
      setFormError(translateProcurementError(error).message);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && handleClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Review source change</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4">
          <p className="text-sm text-muted-foreground">
            {requirement.materialName}
          </p>

          {discrepancyQuery.isLoading && (
            <div className="grid gap-3">
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
          )}

          {discrepancy && (
            <DiscrepancyBody
              requirement={requirement}
              discrepancy={discrepancy}
              selectedAction={selectedAction}
              onSelectAction={selectAction}
              staleWarning={staleWarning}
              formError={formError}
            />
          )}

          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={handleClose}>
              Cancel
            </Button>
            <Button disabled={!selectedAction || resolveMutation.isPending} onClick={handleApply}>
              {resolveMutation.isPending ? "Applying…" : "Apply resolution"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

type Discrepancy = components["schemas"]["SourceDiscrepancyOutputBody"];

function DiscrepancyBody({
  requirement,
  discrepancy,
  selectedAction,
  onSelectAction,
  staleWarning,
  formError,
}: {
  requirement: MaterialRequirement;
  discrepancy: Discrepancy;
  selectedAction: ResolutionActionName | null;
  onSelectAction: (action: ResolutionActionName) => void;
  staleWarning: boolean;
  formError?: string;
}) {
  const isSourceRemoved = discrepancy.syncState === "source_removed";
  const current = requirement.requiredQuantity.value;
  const delta = subtractQuantity(discrepancy.proposed.quantity.value, discrepancy.accepted.quantity.value);
  const unit = discrepancy.proposed.quantity.unit;
  const availableActions = (discrepancy.availableActions ?? []) as ResolutionActionName[];
  const mergeAvailable = availableActions.includes("merge");
  const merged = mergeAvailable ? addQuantity(current, delta) : null;

  return (
    <div className="grid gap-4">
      <p className="text-sm">
        {isSourceRemoved
          ? "The costing data previously backing this requirement is no longer present."
          : "The costing data behind this requirement has changed. Review the difference before deciding how procurement should be updated."}
      </p>

      {staleWarning && (
        <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">
          Source data changed while you were reviewing it. Please review the latest values before applying a resolution.
        </p>
      )}

      <div className="grid grid-cols-2 gap-3">
        <div className="rounded-lg border p-3">
          <p className="eyebrow text-xs">Accepted source</p>
          <p className="mt-1 font-mono text-lg font-semibold">
            {discrepancy.accepted.quantity.value} {discrepancy.accepted.quantity.unit}
          </p>
          <p className="text-xs text-muted-foreground">{discrepancy.accepted.costItemIds?.length ?? 0} cost items</p>
        </div>
        <div className="rounded-lg border p-3">
          <p className="eyebrow text-xs">Current source</p>
          {isSourceRemoved ? (
            <p className="mt-1 text-sm font-medium">No source cost items</p>
          ) : (
            <p className="mt-1 font-mono text-lg font-semibold">
              {discrepancy.proposed.quantity.value} {discrepancy.proposed.quantity.unit}
            </p>
          )}
          <p className="text-xs text-muted-foreground">{discrepancy.proposed.costItemIds?.length ?? 0} cost items</p>
        </div>
      </div>

      {!isSourceRemoved && (
        <p className="text-sm">
          Change: {delta} {unit}
        </p>
      )}
      <p className="text-sm text-muted-foreground">
        Your current procurement quantity: {current} {requirement.requiredQuantity.unit}
      </p>

      <fieldset className="grid gap-2">
        <legend className="text-sm font-medium">What should Renovex do?</legend>
        {availableActions.includes("update") && (
          <ResolutionOption
            name="update"
            selected={selectedAction === "update"}
            onSelect={onSelectAction}
            label="Update requirement"
            description={`Replace ${current} ${requirement.requiredQuantity.unit} with the latest source quantity: ${discrepancy.proposed.quantity.value} ${unit}. The requirement returns to Draft for review.`}
          />
        )}
        {availableActions.includes("merge") && (
          <ResolutionOption
            name="merge"
            selected={selectedAction === "merge"}
            onSelect={onSelectAction}
            label="Merge source change"
            description={`Keep your adjustment and apply the source change. New quantity: ${merged} ${unit}. The requirement returns to Draft.`}
          />
        )}
        {availableActions.includes("keep_current") && (
          <ResolutionOption
            name="keep_current"
            selected={selectedAction === "keep_current"}
            onSelect={onSelectAction}
            label="Keep current requirement"
            description={`Keep ${current} ${requirement.requiredQuantity.unit}; accept the latest costing data as the new source baseline.`}
          />
        )}
        {availableActions.includes("create_separate") && (
          <ResolutionOption
            name="create_separate"
            selected={selectedAction === "create_separate"}
            onSelect={onSelectAction}
            label="Create separate requirement"
            description={`Keep this requirement at ${current} ${requirement.requiredQuantity.unit}; create another requirement for the additional ${delta} ${unit}.`}
          />
        )}
      </fieldset>

      {formError && <p className="text-sm text-destructive">{formError}</p>}
    </div>
  );
}

function ResolutionOption({
  name,
  selected,
  onSelect,
  label,
  description,
}: {
  name: ResolutionActionName;
  selected: boolean;
  onSelect: (action: ResolutionActionName) => void;
  label: string;
  description: string;
}) {
  return (
    <label className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 ${selected ? "border-primary bg-accent/20" : ""}`}>
      <input
        type="radio"
        role="radio"
        name="resolution-action"
        aria-checked={selected}
        checked={selected}
        onChange={() => onSelect(name)}
        className="mt-1"
      />
      <span>
        <span className="block text-sm font-medium">{label}</span>
        <span className="block text-xs text-muted-foreground">{description}</span>
      </span>
    </label>
  );
}
