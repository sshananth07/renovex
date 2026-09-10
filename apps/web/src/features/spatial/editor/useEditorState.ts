import { useCallback, useReducer } from "react";
import type { ActiveEditorView, CameraPose, ElementKind, PendingEdit, PendingReset, Selection, Viewport } from "./types";

type EditorState = {
  selection: Selection;
  hoverId: string | null;
  viewport: Viewport;
  pendingEdit: PendingEdit | null;
  // Separate from pendingEdit — see PendingReset's doc comment in types.ts.
  // Both slots exist simultaneously but only one is ever populated in
  // practice; the UI only ever offers one active mutation at a time.
  pendingReset: PendingReset | null;
  // RP4C3: which viewport is currently shown. Switching this never
  // refetches the RoomDraft — draft/selection/pendingEdit are all shared
  // between both views unchanged.
  activeView: ActiveEditorView;
  // RP4C3: last SETTLED 3D camera pose, or null before the 3D view has
  // ever been shown (Spatial3DViewport fits-to-room on first mount in that
  // case). Written only at stable lifecycle boundaries — see CameraPose's
  // doc comment in types.ts.
  cameraPose: CameraPose | null;
};

type EditorAction =
  | { type: "select"; kind: ElementKind; id: string }
  | { type: "clearSelection" }
  | { type: "hover"; id: string | null }
  | { type: "setViewport"; viewport: Viewport }
  | { type: "startPendingEdit"; pendingEdit: PendingEdit }
  | { type: "markPendingEditRetryable" }
  | { type: "clearPendingEdit" }
  | { type: "startPendingReset"; pendingReset: PendingReset }
  | { type: "markPendingResetRetryable" }
  | { type: "clearPendingReset" }
  // Reconciliation after an authoritative refresh: drop the selection only
  // if the previously-selected element no longer exists in the new draft.
  | { type: "reconcileSelection"; stillExists: boolean }
  | { type: "setActiveView"; view: ActiveEditorView }
  | { type: "setCameraPose"; pose: CameraPose };

function reducer(state: EditorState, action: EditorAction): EditorState {
  switch (action.type) {
    case "select":
      return { ...state, selection: { kind: action.kind, id: action.id } };
    case "clearSelection":
      return { ...state, selection: null };
    case "hover":
      return { ...state, hoverId: action.id };
    case "setViewport":
      return { ...state, viewport: action.viewport };
    case "startPendingEdit":
      return { ...state, pendingEdit: action.pendingEdit };
    case "markPendingEditRetryable":
      return state.pendingEdit ? { ...state, pendingEdit: { ...state.pendingEdit, status: "retryable-error" } } : state;
    case "clearPendingEdit":
      return { ...state, pendingEdit: null };
    case "startPendingReset":
      return { ...state, pendingReset: action.pendingReset };
    case "markPendingResetRetryable":
      return state.pendingReset ? { ...state, pendingReset: { ...state.pendingReset, status: "retryable-error" } } : state;
    case "clearPendingReset":
      return { ...state, pendingReset: null };
    case "reconcileSelection":
      return action.stillExists ? state : { ...state, selection: null };
    case "setActiveView":
      return { ...state, activeView: action.view };
    case "setCameraPose":
      return { ...state, cameraPose: action.pose };
    default:
      return state;
  }
}

const initialViewport: Viewport = { panX: 0, panY: 0, zoom: 40 };

export function useEditorState() {
  const [state, dispatch] = useReducer(reducer, {
    selection: null,
    hoverId: null,
    viewport: initialViewport,
    pendingEdit: null,
    pendingReset: null,
    activeView: "3d" as ActiveEditorView,
    cameraPose: null,
  });

  const select = useCallback((kind: ElementKind, id: string) => dispatch({ type: "select", kind, id }), []);
  const clearSelection = useCallback(() => dispatch({ type: "clearSelection" }), []);
  const setHover = useCallback((id: string | null) => dispatch({ type: "hover", id }), []);
  const setViewport = useCallback((viewport: Viewport) => dispatch({ type: "setViewport", viewport }), []);
  const startPendingEdit = useCallback((pendingEdit: PendingEdit) => dispatch({ type: "startPendingEdit", pendingEdit }), []);
  const markPendingEditRetryable = useCallback(() => dispatch({ type: "markPendingEditRetryable" }), []);
  const clearPendingEdit = useCallback(() => dispatch({ type: "clearPendingEdit" }), []);
  const startPendingReset = useCallback((pendingReset: PendingReset) => dispatch({ type: "startPendingReset", pendingReset }), []);
  const markPendingResetRetryable = useCallback(() => dispatch({ type: "markPendingResetRetryable" }), []);
  const clearPendingReset = useCallback(() => dispatch({ type: "clearPendingReset" }), []);
  const reconcileSelection = useCallback((stillExists: boolean) => dispatch({ type: "reconcileSelection", stillExists }), []);
  const setActiveView = useCallback((view: ActiveEditorView) => dispatch({ type: "setActiveView", view }), []);
  const setCameraPose = useCallback((pose: CameraPose) => dispatch({ type: "setCameraPose", pose }), []);

  return {
    selection: state.selection,
    hoverId: state.hoverId,
    viewport: state.viewport,
    pendingEdit: state.pendingEdit,
    pendingReset: state.pendingReset,
    activeView: state.activeView,
    cameraPose: state.cameraPose,
    select,
    clearSelection,
    setHover,
    setViewport,
    startPendingEdit,
    markPendingEditRetryable,
    clearPendingEdit,
    startPendingReset,
    markPendingResetRetryable,
    clearPendingReset,
    reconcileSelection,
    setActiveView,
    setCameraPose,
  };
}

// Given the previously-selected element and the newly-loaded authoritative
// RoomDraft, decides whether that element still exists — pure so it's
// testable without mounting the hook.
export function elementStillExistsInDraft(
  selection: Selection,
  draft: {
    walls?: { id: string }[] | null;
    openings?: { id: string }[] | null;
    objects?: { id: string }[] | null;
    fixtures?: { id: string }[] | null;
    servicePoints?: { id: string }[] | null;
    constraints?: { id: string }[] | null;
  } | null,
): boolean {
  if (!selection || !draft) return false;
  const listByKind: Record<ElementKind, { id: string }[] | null | undefined> = {
    wall: draft.walls,
    opening: draft.openings,
    object: draft.objects,
    fixture: draft.fixtures,
    servicePoint: draft.servicePoints,
    constraint: draft.constraints,
  };
  return (listByKind[selection.kind] ?? []).some((el) => el.id === selection.id);
}
