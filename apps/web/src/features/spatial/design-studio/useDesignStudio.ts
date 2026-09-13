import { useCallback, useReducer } from "react";
import type { Selection } from "../editor/types";
import type { ComparisonMode, StudioState } from "./types";

// The AI Design Studio's own reducer — UI orchestration state ONLY. It
// never fetches, never merges server data into itself, and never holds a
// generated asset reference: attempt/session data lives in React Query
// (design-studio queries, added alongside the first UI slice that consumes
// them), and this reducer only tracks which target/session/turn/attempt the
// UI is currently looking at and what the composer/comparison controls are
// doing. AIDesignStudio (the component that owns this hook) derives
// StudioState from a combination of this reducer's fields and the fetched
// server data — see conceptProjection.ts for the read side of that
// derivation.
//
// One session/turn/attempt triple is tracked PER canonical session context
// {kind,id,revision} — switching the RoomDraft selection to a different
// supported element, OR the RoomDraft advancing to a new revision under the
// SAME element, must restore or start a session for that new context, never
// carry over the previous context's prompt draft, comparison mode, attempt,
// or (critically) sessionId. A session is revision-bound server-side
// (SpatialDesignSession.BasedOnRoomDraftRevision) — reusing a tracked
// sessionId/clientSessionId across a revision change is exactly the bug
// this contextKey closes (see AIDesignStudio.tsx's sessionContextKey).
type SessionTracking = {
  target: Selection;
  contextKey: string | null;
  sessionId: string | null;
  activeTurnId: string | null;
  activeAttemptId: string | null;
};

type StudioUiState = {
  tracking: SessionTracking | null;
  promptDraft: string;
  comparisonMode: ComparisonMode;
  collapsed: boolean;
  refinementDraftOpen: boolean;
};

type StudioAction =
  | { type: "selectTarget"; target: Selection; contextKey: string | null }
  | { type: "setPromptDraft"; value: string }
  | { type: "clearPromptDraft" }
  | { type: "restoreSession"; sessionId: string; activeTurnId: string | null; activeAttemptId: string | null }
  | { type: "turnCreated"; turnId: string }
  | { type: "attemptCreated"; attemptId: string }
  | { type: "attemptTerminal" }
  | { type: "setComparisonMode"; mode: ComparisonMode }
  | { type: "toggleCollapsed" }
  | { type: "setCollapsed"; collapsed: boolean }
  | { type: "openRefinementDraft" }
  | { type: "closeRefinementDraft" }
  | { type: "accepted" };

function reducer(state: StudioUiState, action: StudioAction): StudioUiState {
  switch (action.type) {
    case "selectTarget": {
      // A genuinely new session context — a different target OR the same
      // target at a new RoomDraft revision — always starts fresh — no
      // prompt draft, comparison mode, attempt, or (critically) sessionId
      // carries over (plan's explicit "selecting object B clears A's
      // temporary concept and starts/restores B independently", extended to
      // a revision bump under the SAME target — see SessionTracking's doc
      // comment).
      if (state.tracking?.contextKey === action.contextKey) return state;
      return {
        tracking: action.target ? { target: action.target, contextKey: action.contextKey, sessionId: null, activeTurnId: null, activeAttemptId: null } : null,
        promptDraft: "",
        comparisonMode: "current",
        collapsed: state.collapsed,
        refinementDraftOpen: false,
      };
    }
    case "setPromptDraft":
      return { ...state, promptDraft: action.value };
    case "clearPromptDraft":
      return { ...state, promptDraft: "" };
    case "restoreSession":
      if (!state.tracking) return state;
      return {
        ...state,
        tracking: { ...state.tracking, sessionId: action.sessionId, activeTurnId: action.activeTurnId, activeAttemptId: action.activeAttemptId },
      };
    case "turnCreated":
      if (!state.tracking) return state;
      return { ...state, tracking: { ...state.tracking, activeTurnId: action.turnId, activeAttemptId: null }, promptDraft: "" };
    case "attemptCreated":
      if (!state.tracking) return state;
      return { ...state, tracking: { ...state.tracking, activeAttemptId: action.attemptId } };
    case "attemptTerminal":
      // Kept for symmetry/clarity at call sites — terminal status itself is
      // server data (fetched, not stored here); this action currently has
      // no state to change but documents the transition point explicitly
      // rather than being silently absent.
      return state;
    case "setComparisonMode":
      return { ...state, comparisonMode: action.mode };
    case "toggleCollapsed":
      return { ...state, collapsed: !state.collapsed };
    case "setCollapsed":
      return { ...state, collapsed: action.collapsed };
    case "openRefinementDraft":
      return { ...state, refinementDraftOpen: true, promptDraft: "" };
    case "closeRefinementDraft":
      return { ...state, refinementDraftOpen: false };
    case "accepted":
      // The session stays active after acceptance for continued refinement
      // (plan invariant) — comparison mode returns to "current" (the newly
      // accepted state IS current now) but sessionId/target tracking is
      // untouched.
      return { ...state, comparisonMode: "current", promptDraft: "", refinementDraftOpen: false };
    default:
      return state;
  }
}

const initialState: StudioUiState = {
  tracking: null,
  promptDraft: "",
  comparisonMode: "current",
  collapsed: false,
  refinementDraftOpen: false,
};

export function useDesignStudio() {
  const [state, dispatch] = useReducer(reducer, initialState);

  const selectTarget = useCallback(
    (target: Selection, contextKey: string | null) => dispatch({ type: "selectTarget", target, contextKey }),
    [],
  );
  const setPromptDraft = useCallback((value: string) => dispatch({ type: "setPromptDraft", value }), []);
  const clearPromptDraft = useCallback(() => dispatch({ type: "clearPromptDraft" }), []);
  const restoreSession = useCallback(
    (sessionId: string, activeTurnId: string | null, activeAttemptId: string | null) =>
      dispatch({ type: "restoreSession", sessionId, activeTurnId, activeAttemptId }),
    [],
  );
  const turnCreated = useCallback((turnId: string) => dispatch({ type: "turnCreated", turnId }), []);
  const attemptCreated = useCallback((attemptId: string) => dispatch({ type: "attemptCreated", attemptId }), []);
  const setComparisonMode = useCallback((mode: ComparisonMode) => dispatch({ type: "setComparisonMode", mode }), []);
  const toggleCollapsed = useCallback(() => dispatch({ type: "toggleCollapsed" }), []);
  const setCollapsed = useCallback((collapsed: boolean) => dispatch({ type: "setCollapsed", collapsed }), []);
  const openRefinementDraft = useCallback(() => dispatch({ type: "openRefinementDraft" }), []);
  const closeRefinementDraft = useCallback(() => dispatch({ type: "closeRefinementDraft" }), []);
  const accepted = useCallback(() => dispatch({ type: "accepted" }), []);

  return {
    target: state.tracking?.target ?? null,
    sessionId: state.tracking?.sessionId ?? null,
    activeTurnId: state.tracking?.activeTurnId ?? null,
    activeAttemptId: state.tracking?.activeAttemptId ?? null,
    promptDraft: state.promptDraft,
    comparisonMode: state.comparisonMode,
    collapsed: state.collapsed,
    refinementDraftOpen: state.refinementDraftOpen,
    selectTarget,
    setPromptDraft,
    clearPromptDraft,
    restoreSession,
    turnCreated,
    attemptCreated,
    setComparisonMode,
    toggleCollapsed,
    setCollapsed,
    openRefinementDraft,
    closeRefinementDraft,
    accepted,
  };
}

// deriveStudioState computes the state-machine value a component switches
// on, from the reducer's own UI state plus the minimal server-derived facts
// a caller already has in hand (never re-fetched here — pure function).
// Kept separate from the reducer itself so it stays trivially unit-testable
// against plain inputs, matching elementStillExistsInDraft's precedent in
// useEditorState.ts.
export function deriveStudioState(input: {
  target: Selection;
  isSupportedKind: boolean;
  isRestoring: boolean;
  isSubmittingTurn?: boolean;
  turnStatus?: "reserved" | "reasoning" | "proposed" | "blocked" | "failed" | "needs_attention" | "superseded" | "stale";
  attemptStatus?: import("./types").DesignGenerationStatus;
  isStalePlan: boolean;
  isRegenerating: boolean;
}): StudioState {
  if (!input.target) return "empty";
  if (!input.isSupportedKind) return "unsupported";
  if (input.isRestoring) return "restoring";
  if (input.isStalePlan) return "stale";
  if (input.isSubmittingTurn) return "planning";

  if (input.attemptStatus === "accepted") return "accepted";
  if (input.isRegenerating) return "regenerating";
  if (input.attemptStatus === "concept_ready") return "concept_ready";
  if (
    input.attemptStatus === "reserved" ||
    input.attemptStatus === "generating_reference" ||
    input.attemptStatus === "reference_ready" ||
    input.attemptStatus === "asset_generation_pending" ||
    input.attemptStatus === "asset_generation_processing"
  ) {
    return "geometry_generating";
  }
  if (input.attemptStatus === "failed" || input.attemptStatus === "needs_attention" || input.attemptStatus === "abandoned") {
    return "failed";
  }

  if (input.turnStatus === "reserved" || input.turnStatus === "reasoning") return "planning";
  if (input.turnStatus === "blocked") return "plan_blocked";
  if (input.turnStatus === "proposed") return "plan_ready";
  if (input.turnStatus === "failed") return "failed";
  if (input.turnStatus === "needs_attention") return "needs_attention";

  return "welcome";
}
