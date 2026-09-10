"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, ChevronRight, Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import type { RoomDraft } from "../../api";
import type { EditOperationPayload, Selection } from "../../editor/types";
import { useDesignStudio, deriveStudioState } from "../useDesignStudio";
import type { DesignGenerationStatus } from "../types";
import {
  useCancelDesignGenerationAttempt,
  useConfirmDesignPlan,
  useCreateDesignTurn,
  useDesignGenerationAttempt,
  useDesignSession,
  useDesignTurns,
  useEnsureDesignSession,
  useRegenerateDesignPlan,
  useUseDesignPlan,
} from "../useDesignStudioQueries";
import { classifyDesignStudioError, designStudioErrorMessage } from "../designStudioErrors";
import { isSupportedDesignTarget, projectConceptRenderState } from "../conceptProjection";
import type { ComparisonMode, ConceptRenderState } from "../types";
import { SelectedObjectLabel } from "./SelectedObjectLabel";
import { DesignStudioWelcome } from "./DesignStudioWelcome";
import { DesignPromptComposer } from "./DesignPromptComposer";
import { DesignProgress } from "./DesignProgress";
import { AIChangePlanCard } from "./AIChangePlanCard";
import { ConceptActions } from "./ConceptActions";
import { ObjectControlsDisclosure } from "./ObjectControlsDisclosure";

// The AIDesignStudio shell — assembles the full explicit UX state machine
// from the M8.5C plan (§"Explicit UX state machine"). Owns real session/
// turn/attempt lifecycle via useDesignStudioQueries (React Query) and pure
// UI orchestration via useDesignStudio (reducer). Renders inside
// SpatialEditor's 390-440px dock column (or overlay/sheet at narrower
// widths, caller's concern) — this component itself only fills its
// container, it does not know about layout breakpoints.
export function AIDesignStudio({
  roomDraftId,
  draft,
  selection,
  onSubmitEdit,
  editingDisabled,
  onConceptPreviewChange,
  onDesignCommitted,
}: {
  roomDraftId: string;
  draft: RoomDraft | undefined;
  selection: Selection;
  onSubmitEdit: (operation: EditOperationPayload) => void;
  editingDisabled: boolean;
  // Reports the derived canvas-facing preview state up to SpatialEditor,
  // which owns the actual canvas/CurrentConceptToggle overlay (plan's
  // layout tree: the toggle floats over SpatialCanvasStage, not inside this
  // dock). Called on every render where either value changes — never holds
  // its own copy, matching ConceptRenderState's "projection, never a source
  // of truth" contract.
  onConceptPreviewChange?: (preview: ConceptRenderState, comparisonMode: ComparisonMode, setComparisonMode: (mode: ComparisonMode) => void) => void;
  // M8.5C closure patch §D: fired once Use Design's server-side accept
  // succeeds, carrying exactly what SpatialEditor needs to build ONE session
  // HistoryEntry (RestoreElementOperation{replace:true} for `selection`'s
  // target, same "before/after" model as submitUserEdit) — never re-invokes
  // AI generation on Undo/Redo, only restores the object/fixture record.
  onDesignCommitted?: (commit: { selection: Selection; before: RoomDraft; after: RoomDraft }) => void;
}) {
  const studio = useDesignStudio();
  const ensureSession = useEnsureDesignSession();

  useEffect(() => {
    studio.selectTarget(selection);
  }, [selection?.kind, selection?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const isSupportedKind = isSupportedDesignTarget(selection);

  // Deterministic per-{roomDraftId,kind,id} clientSessionId — the backend's
  // CreateDesignSession fingerprints on exactly this tuple, so repeat calls
  // for the same target are idempotent replays (find-or-restore), never new
  // sessions (see designSessionQueries.ts's doc comment).
  const clientSessionId = selection ? `studio:${roomDraftId}:${selection.kind}:${selection.id}` : null;
  const [actionError, setActionError] = useState<string | null>(null);

  // §13: a manual canonical edit landing WHILE this ensure-session request
  // is in flight makes its expectedRoomDraftRevision stale (the backend
  // fingerprints CreateDesignSession on that revision too), returning
  // design_session_request_conflict. latestDraftRef/latestSelectionRef let
  // ensureForRevision's onError read the CURRENT draft/selection without
  // adding draft.revision to the effect's own dependency array — doing
  // that instead would re-run ensureSession after every single room edit,
  // not just recover from a genuinely stale request.
  const latestDraftRef = useRef(draft);
  useEffect(() => {
    latestDraftRef.current = draft;
  }, [draft]);
  const latestSelectionRef = useRef(selection);
  useEffect(() => {
    latestSelectionRef.current = selection;
  }, [selection]);

  function ensureForRevision(attemptedRevision: number, allowStaleRetry: boolean) {
    if (!selection || !clientSessionId) return;
    const attemptedTarget = { kind: selection.kind, id: selection.id };

    ensureSession.mutate(
      {
        clientSessionId,
        roomDraftId,
        expectedRoomDraftRevision: attemptedRevision,
        target: attemptedTarget,
      },
      {
        onSuccess: (result) => {
          if (!result?.session) return;
          studio.restoreSession(result.session.id, result.session.latestTurnId ?? null, null);
        },
        onError: (thrown) => {
          const kind = classifyDesignStudioError(thrown as never);

          const latestDraft = latestDraftRef.current;
          const latestSelection = latestSelectionRef.current;
          const sameTarget = latestSelection?.kind === attemptedTarget.kind && latestSelection?.id === attemptedTarget.id;
          // Only a revision the client can see is GREATER than the one this
          // attempt used proves the attempt was genuinely stale — a 409 at
          // the same revision proves nothing retryable (no evidence a newer
          // state exists), so it must not spin.
          const provenLocallyStale = kind === "request_conflict" && sameTarget && latestDraft !== undefined && latestDraft.revision > attemptedRevision;

          if (allowStaleRetry && provenLocallyStale) {
            ensureForRevision(latestDraft.revision, false); // ONE retry only
            return;
          }
          setActionError(designStudioErrorMessage(kind));
        },
      },
    );
  }

  useEffect(() => {
    if (!selection || !isSupportedKind || !draft || !clientSessionId) return;
    if (studio.sessionId) return;
    ensureForRevision(draft.revision, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection?.kind, selection?.id, isSupportedKind, Boolean(draft), clientSessionId, studio.sessionId]);

  const sessionQuery = useDesignSession(studio.sessionId);
  const turnsQuery = useDesignTurns(studio.sessionId);
  const latestTurn = turnsQuery.data?.[0];
  const activeTurnId = studio.activeTurnId ?? latestTurn?.id ?? null;

  useEffect(() => {
    if (latestTurn && latestTurn.id !== studio.activeTurnId) {
      studio.turnCreated(latestTurn.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latestTurn?.id]);

  const attemptQuery = useDesignGenerationAttempt(studio.sessionId, studio.activeAttemptId);

  const createTurn = useCreateDesignTurn(studio.sessionId ?? "");
  const confirmPlan = useConfirmDesignPlan(studio.sessionId ?? "");
  const regeneratePlan = useRegenerateDesignPlan(studio.sessionId ?? "");
  const cancelAttempt = useCancelDesignGenerationAttempt(studio.sessionId ?? "");
  const useDesignPlanMutation = useUseDesignPlan(studio.sessionId ?? "", roomDraftId);

  const isStalePlan = Boolean(draft && latestTurn && latestTurn.basedOnRoomDraftRevision !== draft.revision);

  const studioState = useMemo(
    () =>
      deriveStudioState({
        target: selection,
        isSupportedKind,
        // Restoring covers both "session exists, its data is loading" AND
        // "no session yet, but one is actively being created/restored" —
        // otherwise the studio flashes "welcome" for one render between
        // selecting a supported target and the session-creation mutation
        // settling, then immediately snaps to "restoring" once sessionId
        // lands (a real, user-visible flicker caught by an integration test).
        isRestoring: ensureSession.isPending || (Boolean(studio.sessionId) && (sessionQuery.isLoading || turnsQuery.isLoading)),
        turnStatus: latestTurn?.status as never,
        attemptStatus: attemptQuery.data?.status as never,
        isStalePlan,
        isRegenerating: regeneratePlan.isPending,
      }),
    [
      selection,
      isSupportedKind,
      ensureSession.isPending,
      studio.sessionId,
      sessionQuery.isLoading,
      turnsQuery.isLoading,
      latestTurn?.status,
      attemptQuery.data?.status,
      isStalePlan,
      regeneratePlan.isPending,
    ],
  );

  const conceptPreview = useMemo(
    () =>
      projectConceptRenderState({
        draft,
        selection,
        attempt: attemptQuery.data,
        attemptBasedOnRoomDraftRevision: latestTurn?.basedOnRoomDraftRevision,
        isAttemptStale: isStalePlan,
      }),
    [draft, selection, attemptQuery.data, latestTurn?.basedOnRoomDraftRevision, isStalePlan],
  );

  useEffect(() => {
    onConceptPreviewChange?.(conceptPreview, studio.comparisonMode, studio.setComparisonMode);
  }, [conceptPreview, studio.comparisonMode, studio.setComparisonMode, onConceptPreviewChange]);

  function handleSubmitPrompt() {
    if (!studio.sessionId || studio.promptDraft.trim().length === 0) return;
    setActionError(null);
    createTurn.mutate(
      { clientRequestId: crypto.randomUUID(), instruction: studio.promptDraft.trim() },
      {
        onError: (error) => setActionError(designStudioErrorMessage(classifyDesignStudioError(error as never))),
      },
    );
  }

  function handleConfirm() {
    if (!studio.sessionId || !activeTurnId || !latestTurn?.planFingerprint || !draft) return;
    setActionError(null);
    confirmPlan.mutate(
      {
        turnId: activeTurnId,
        body: {
          clientRequestId: crypto.randomUUID(),
          planFingerprint: latestTurn.planFingerprint,
          expectedRoomDraftRevision: draft.revision,
        },
      },
      {
        onSuccess: (attempt) => studio.attemptCreated(attempt.id),
        onError: (error) => setActionError(designStudioErrorMessage(classifyDesignStudioError(error as never))),
      },
    );
  }

  function handleRegenerate() {
    if (!studio.sessionId || !activeTurnId || !latestTurn?.planFingerprint || !draft) return;
    setActionError(null);
    regeneratePlan.mutate(
      {
        turnId: activeTurnId,
        body: {
          clientRequestId: crypto.randomUUID(),
          planFingerprint: latestTurn.planFingerprint,
          expectedRoomDraftRevision: draft.revision,
        },
      },
      {
        onSuccess: (attempt) => studio.attemptCreated(attempt.id),
        onError: (error) => setActionError(designStudioErrorMessage(classifyDesignStudioError(error as never))),
      },
    );
  }

  function handleCancel() {
    if (!studio.sessionId || !studio.activeAttemptId) return;
    cancelAttempt.mutate(
      { attemptId: studio.activeAttemptId, body: { clientRequestId: crypto.randomUUID() } },
      { onSuccess: () => studio.setComparisonMode("current") },
    );
  }

  function handleUseDesign() {
    if (!studio.sessionId || !studio.activeAttemptId || !latestTurn?.planFingerprint || !draft) return;
    setActionError(null);
    const before = draft;
    useDesignPlanMutation.mutate(
      {
        attemptId: studio.activeAttemptId,
        body: {
          clientRequestId: crypto.randomUUID(),
          planFingerprint: latestTurn.planFingerprint,
          expectedRoomDraftRevision: draft.revision,
        },
      },
      {
        onSuccess: (result) => {
          studio.accepted();
          if (selection && result?.roomDraft) {
            onDesignCommitted?.({ selection, before, after: result.roomDraft });
          }
        },
        onError: (error) => setActionError(designStudioErrorMessage(classifyDesignStudioError(error as never))),
      },
    );
  }

  if (studio.collapsed) {
    return (
      <div className="flex items-start justify-end">
        <Button
          size="sm"
          className="gap-1.5 rounded-full shadow-sm"
          onClick={() => studio.setCollapsed(false)}
          aria-label="Reopen AI Design"
        >
          <Sparkles className="size-4 stroke-[1.75]" aria-hidden="true" />
          AI Design
        </Button>
      </div>
    );
  }

  const category = selection && draft ? categoryFor(draft, selection) : undefined;

  return (
    <div className="flex h-full flex-col gap-3 overflow-y-auto rounded-xl border border-border/70 bg-card p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <Sparkles className="size-4 stroke-[1.75] text-primary" aria-hidden="true" />
          <h1 className="text-sm font-semibold text-foreground">AI Design</h1>
        </div>
        <Button size="icon-sm" variant="ghost" onClick={() => studio.setCollapsed(true)} aria-label="Collapse AI Design">
          <ChevronRight className="size-4 stroke-[1.75]" aria-hidden="true" />
        </Button>
      </div>

      {selection && category && <SelectedObjectLabel category={category} />}

      {actionError && (
        <p role="alert" className="flex items-start gap-1.5 rounded-lg border border-destructive/20 bg-destructive/5 p-2.5 text-xs leading-4 text-destructive">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0 stroke-[1.75]" aria-hidden="true" />
          {actionError}
        </p>
      )}

      <StudioBody
        state={studioState}
        category={category}
        promptDraft={studio.promptDraft}
        onPromptDraftChange={studio.setPromptDraft}
        onSubmitPrompt={handleSubmitPrompt}
        promptPending={createTurn.isPending}
        latestTurn={latestTurn}
        attempt={attemptQuery.data}
        onConfirm={handleConfirm}
        onRegenerate={handleRegenerate}
        onCancel={handleCancel}
        onUseDesign={handleUseDesign}
        onRefine={(chip) => {
          studio.openRefinementDraft();
          studio.setPromptDraft(chip);
        }}
        onEditPrompt={() => studio.openRefinementDraft()}
        confirmPending={confirmPlan.isPending}
        regeneratePending={regeneratePlan.isPending}
        useDesignPending={useDesignPlanMutation.isPending}
      />

      <Separator />
      <ObjectControlsDisclosure draft={draft} selection={selection} onSubmit={onSubmitEdit} disabled={editingDisabled} />
    </div>
  );
}

function categoryFor(draft: RoomDraft, selection: NonNullable<Selection>): string | undefined {
  if (selection.kind === "object") return draft.objects?.find((o) => o.id === selection.id)?.category;
  if (selection.kind === "fixture") return draft.fixtures?.find((f) => f.id === selection.id)?.category;
  return undefined;
}

function StudioBody(props: {
  state: ReturnType<typeof deriveStudioState>;
  category: string | undefined;
  promptDraft: string;
  onPromptDraftChange: (value: string) => void;
  onSubmitPrompt: () => void;
  promptPending: boolean;
  latestTurn: ReturnType<typeof useDesignTurns>["data"] extends (infer T)[] | undefined ? T | undefined : never;
  attempt: ReturnType<typeof useDesignGenerationAttempt>["data"];
  onConfirm: () => void;
  onRegenerate: () => void;
  onCancel: () => void;
  onUseDesign: () => void;
  onRefine: (chipText: string) => void;
  onEditPrompt: () => void;
  confirmPending: boolean;
  regeneratePending: boolean;
  useDesignPending: boolean;
}) {
  const { state } = props;

  switch (state) {
    case "empty":
      return <p className="text-sm leading-5 text-muted-foreground">Choose a piece in the room to imagine a change.</p>;

    case "unsupported":
      return (
        <p className="text-sm leading-5 text-muted-foreground">
          AI Design currently works with movable objects and fixtures. Use the manual controls below for this element.
        </p>
      );

    case "restoring":
      return (
        <div className="grid gap-2">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-16 w-full" />
        </div>
      );

    case "welcome":
      return (
        <div className="grid gap-3">
          <DesignStudioWelcome category={props.category ?? "item"} onSelectChip={props.onPromptDraftChange} />
          <DesignPromptComposer
            value={props.promptDraft}
            onChange={props.onPromptDraftChange}
            onSubmit={props.onSubmitPrompt}
            disabled={props.promptPending}
          />
        </div>
      );

    case "planning":
      return (
        <div className="grid gap-2" role="status" aria-live="polite">
          <p className="text-sm font-medium text-foreground">Understanding your idea…</p>
          <p className="text-xs leading-4 text-muted-foreground">Checking fit and preparing the change plan.</p>
        </div>
      );

    case "plan_blocked":
      return (
        <div className="grid gap-3">
          <p role="alert" className="text-sm leading-5 text-destructive">
            {props.latestTurn?.review?.notes?.[0] ?? "This idea doesn't fit the room as described."}
          </p>
          <DesignPromptComposer
            value={props.promptDraft}
            onChange={props.onPromptDraftChange}
            onSubmit={props.onSubmitPrompt}
            disabled={props.promptPending}
            placeholder="Try describing it differently…"
          />
        </div>
      );

    case "plan_ready":
      if (!props.latestTurn?.changePlan) return null;
      return (
        <AIChangePlanCard
          plan={props.latestTurn.changePlan}
          execution={props.latestTurn.execution}
          fitAnalysis={props.latestTurn.fitAnalysis}
          onConfirm={props.onConfirm}
          onEditPrompt={props.onEditPrompt}
          confirmDisabled={props.confirmPending}
        />
      );

    case "geometry_generating":
    case "regenerating":
      return <DesignProgress status={(props.attempt?.status as DesignGenerationStatus) ?? "reserved"} />;

    case "concept_ready":
    case "accepted":
      return (
        <div className="grid gap-3">
          {state === "accepted" && (
            <p role="status" className="text-sm font-medium text-foreground">
              Design applied. Refine this design, or select something else.
            </p>
          )}
          <ConceptActions
            onUseDesign={props.onUseDesign}
            onRegenerate={props.onRegenerate}
            onCancel={props.onCancel}
            onRefine={props.onRefine}
            useDesignDisabled={props.useDesignPending || state === "accepted"}
            regenerateDisabled={props.regeneratePending}
          />
        </div>
      );

    case "failed":
      return (
        <p role="alert" className="text-sm leading-5 text-destructive">
          This concept could not be created. You can try again with the same or a new idea.
        </p>
      );

    case "stale":
      return <p className="text-sm leading-5 text-muted-foreground">The room changed. Review this idea again.</p>;

    default:
      return null;
  }
}
