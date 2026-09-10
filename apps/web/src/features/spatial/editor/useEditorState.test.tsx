import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { elementStillExistsInDraft, useEditorState } from "./useEditorState";
import { addServicePointOperation, buildPendingEdit, buildPendingReset } from "./operations";

describe("useEditorState — selection", () => {
  it("selects an element by canonical kind+id", () => {
    const { result } = renderHook(() => useEditorState());
    act(() => result.current.select("wall", "wall_1"));
    expect(result.current.selection).toEqual({ kind: "wall", id: "wall_1" });
  });

  it("clears selection", () => {
    const { result } = renderHook(() => useEditorState());
    act(() => result.current.select("servicePoint", "sp_1"));
    act(() => result.current.clearSelection());
    expect(result.current.selection).toBeNull();
  });
});

describe("useEditorState — pendingEdit lifecycle", () => {
  it("starts with no pending edit", () => {
    const { result } = renderHook(() => useEditorState());
    expect(result.current.pendingEdit).toBeNull();
  });

  it("startPendingEdit stores the immutable logical edit", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingEdit(addServicePointOperation({ kind: "data", position: { x: 0, y: 0, z: 0 } }), 4);
    act(() => result.current.startPendingEdit(pending));
    expect(result.current.pendingEdit).toEqual(pending);
  });

  it("markPendingEditRetryable flips status without changing operationId/payload", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingEdit(addServicePointOperation({ kind: "data", position: { x: 0, y: 0, z: 0 } }), 4);
    act(() => result.current.startPendingEdit(pending));
    act(() => result.current.markPendingEditRetryable());
    expect(result.current.pendingEdit?.status).toBe("retryable-error");
    expect(result.current.pendingEdit?.operationId).toBe(pending.operationId);
    expect(result.current.pendingEdit?.operation).toEqual(pending.operation);
  });

  it("clearPendingEdit removes it entirely", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingEdit(addServicePointOperation({ kind: "data", position: { x: 0, y: 0, z: 0 } }), 4);
    act(() => result.current.startPendingEdit(pending));
    act(() => result.current.clearPendingEdit());
    expect(result.current.pendingEdit).toBeNull();
  });
});

describe("useEditorState — pendingReset lifecycle (separate slot from pendingEdit)", () => {
  it("starts with no pending reset", () => {
    const { result } = renderHook(() => useEditorState());
    expect(result.current.pendingReset).toBeNull();
  });

  it("startPendingReset stores the immutable logical reset request", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingReset(4);
    act(() => result.current.startPendingReset(pending));
    expect(result.current.pendingReset).toEqual(pending);
  });

  it("markPendingResetRetryable flips status without changing operationId/baseRevision", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingReset(4);
    act(() => result.current.startPendingReset(pending));
    act(() => result.current.markPendingResetRetryable());
    expect(result.current.pendingReset?.status).toBe("retryable-error");
    expect(result.current.pendingReset?.operationId).toBe(pending.operationId);
    expect(result.current.pendingReset?.baseRevision).toBe(pending.baseRevision);
  });

  it("clearPendingReset removes it entirely", () => {
    const { result } = renderHook(() => useEditorState());
    const pending = buildPendingReset(4);
    act(() => result.current.startPendingReset(pending));
    act(() => result.current.clearPendingReset());
    expect(result.current.pendingReset).toBeNull();
  });

  it("pendingEdit and pendingReset are independent slots", () => {
    const { result } = renderHook(() => useEditorState());
    const pendingEdit = buildPendingEdit(addServicePointOperation({ kind: "data", position: { x: 0, y: 0, z: 0 } }), 4);
    const pendingReset = buildPendingReset(4);
    act(() => result.current.startPendingEdit(pendingEdit));
    act(() => result.current.startPendingReset(pendingReset));
    expect(result.current.pendingEdit).toEqual(pendingEdit);
    expect(result.current.pendingReset).toEqual(pendingReset);
    act(() => result.current.clearPendingEdit());
    expect(result.current.pendingEdit).toBeNull();
    expect(result.current.pendingReset).toEqual(pendingReset);
  });
});

describe("useEditorState — selection survives/clears on authoritative refresh", () => {
  it("reconcileSelection(true) keeps the current selection", () => {
    const { result } = renderHook(() => useEditorState());
    act(() => result.current.select("wall", "wall_1"));
    act(() => result.current.reconcileSelection(true));
    expect(result.current.selection).toEqual({ kind: "wall", id: "wall_1" });
  });

  it("reconcileSelection(false) clears the selection", () => {
    const { result } = renderHook(() => useEditorState());
    act(() => result.current.select("wall", "wall_1"));
    act(() => result.current.reconcileSelection(false));
    expect(result.current.selection).toBeNull();
  });
});

describe("elementStillExistsInDraft", () => {
  const draft = {
    walls: [{ id: "wall_1" }],
    openings: [{ id: "opening_1" }],
    objects: null,
    fixtures: [],
    servicePoints: [{ id: "sp_1" }],
    constraints: undefined,
  };

  it("returns true when the selected element is present in its kind's list", () => {
    expect(elementStillExistsInDraft({ kind: "wall", id: "wall_1" }, draft)).toBe(true);
    expect(elementStillExistsInDraft({ kind: "servicePoint", id: "sp_1" }, draft)).toBe(true);
  });

  it("returns false when the selected element is missing", () => {
    expect(elementStillExistsInDraft({ kind: "wall", id: "wall_gone" }, draft)).toBe(false);
  });

  it("returns false when the kind's list is null/undefined", () => {
    expect(elementStillExistsInDraft({ kind: "object", id: "obj_1" }, draft)).toBe(false);
    expect(elementStillExistsInDraft({ kind: "constraint", id: "c_1" }, draft)).toBe(false);
  });

  it("returns false when there is no selection or no draft", () => {
    expect(elementStillExistsInDraft(null, draft)).toBe(false);
    expect(elementStillExistsInDraft({ kind: "wall", id: "wall_1" }, null)).toBe(false);
  });
});

describe("useEditorState — activeView (RP4C3)", () => {
  it("starts on the 3D view (M8.5C RP4E3: 3D is the primary AI Design surface)", () => {
    const { result } = renderHook(() => useEditorState());
    expect(result.current.activeView).toBe("3d");
  });

  it("switches to 3D and back without touching selection or pendingEdit", () => {
    const { result } = renderHook(() => useEditorState());
    act(() => result.current.select("fixture", "fixture_1"));
    const pending = buildPendingEdit(addServicePointOperation({ kind: "data", position: { x: 0, y: 0, z: 0 } }), 4);
    act(() => result.current.startPendingEdit(pending));

    act(() => result.current.setActiveView("3d"));
    expect(result.current.activeView).toBe("3d");
    expect(result.current.selection).toEqual({ kind: "fixture", id: "fixture_1" });
    expect(result.current.pendingEdit).toEqual(pending);

    act(() => result.current.setActiveView("2d"));
    expect(result.current.activeView).toBe("2d");
    expect(result.current.selection).toEqual({ kind: "fixture", id: "fixture_1" });
    expect(result.current.pendingEdit).toEqual(pending);
  });
});

describe("useEditorState — cameraPose (RP4C3)", () => {
  it("starts with no stored camera pose", () => {
    const { result } = renderHook(() => useEditorState());
    expect(result.current.cameraPose).toBeNull();
  });

  it("stores a camera pose and it survives a view switch", () => {
    const { result } = renderHook(() => useEditorState());
    const pose = { position: { x: 3, y: 4, z: 5 }, target: { x: 0, y: 0, z: 0 } };
    act(() => result.current.setCameraPose(pose));
    expect(result.current.cameraPose).toEqual(pose);

    act(() => result.current.setActiveView("2d"));
    act(() => result.current.setActiveView("3d"));
    expect(result.current.cameraPose).toEqual(pose);
  });
});
