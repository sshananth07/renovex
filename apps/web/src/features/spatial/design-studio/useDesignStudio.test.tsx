import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { deriveStudioState, useDesignStudio } from "./useDesignStudio";

describe("useDesignStudio — target tracking", () => {
  it("starts with no target/session", () => {
    const { result } = renderHook(() => useDesignStudio());
    expect(result.current.target).toBeNull();
    expect(result.current.sessionId).toBeNull();
  });

  it("selecting a target starts fresh tracking with no session yet", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    expect(result.current.target).toEqual({ kind: "object", id: "object_sofa_123" });
    expect(result.current.sessionId).toBeNull();
  });

  it("selecting the SAME target again is a no-op (does not reset an in-progress session)", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", "turn_1", "attempt_1"));
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    expect(result.current.sessionId).toBe("session_1");
  });

  it("selecting a DIFFERENT target clears the previous target's session/prompt/comparison mode", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", "turn_1", "attempt_1"));
    act(() => result.current.setPromptDraft("make it curved"));
    act(() => result.current.setComparisonMode("concept"));

    act(() => result.current.selectTarget({ kind: "fixture", id: "fixture_ac_1" }));

    expect(result.current.target).toEqual({ kind: "fixture", id: "fixture_ac_1" });
    expect(result.current.sessionId).toBeNull();
    expect(result.current.activeTurnId).toBeNull();
    expect(result.current.activeAttemptId).toBeNull();
    expect(result.current.promptDraft).toBe("");
    expect(result.current.comparisonMode).toBe("current");
  });

  it("clearing the selection (null target) clears tracking entirely", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", null, null));
    act(() => result.current.selectTarget(null));
    expect(result.current.target).toBeNull();
    expect(result.current.sessionId).toBeNull();
  });
});

describe("useDesignStudio — session/turn/attempt lifecycle", () => {
  it("restoreSession populates session/turn/attempt for the current target", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", "turn_1", "attempt_1"));
    expect(result.current.sessionId).toBe("session_1");
    expect(result.current.activeTurnId).toBe("turn_1");
    expect(result.current.activeAttemptId).toBe("attempt_1");
  });

  it("restoreSession before any target is selected is a no-op", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.restoreSession("session_1", "turn_1", "attempt_1"));
    expect(result.current.sessionId).toBeNull();
  });

  it("turnCreated sets the active turn and clears any active attempt + prompt draft", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", null, "attempt_stale"));
    act(() => result.current.setPromptDraft("make it curved"));

    act(() => result.current.turnCreated("turn_new"));

    expect(result.current.activeTurnId).toBe("turn_new");
    expect(result.current.activeAttemptId).toBeNull();
    expect(result.current.promptDraft).toBe("");
  });

  it("attemptCreated sets the active attempt without touching the turn", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", "turn_1", null));
    act(() => result.current.attemptCreated("attempt_1"));
    expect(result.current.activeTurnId).toBe("turn_1");
    expect(result.current.activeAttemptId).toBe("attempt_1");
  });
});

describe("useDesignStudio — comparison mode and collapse", () => {
  it("defaults to current comparison mode and expanded dock", () => {
    const { result } = renderHook(() => useDesignStudio());
    expect(result.current.comparisonMode).toBe("current");
    expect(result.current.collapsed).toBe(false);
  });

  it("setComparisonMode switches between current and concept", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.setComparisonMode("concept"));
    expect(result.current.comparisonMode).toBe("concept");
    act(() => result.current.setComparisonMode("current"));
    expect(result.current.comparisonMode).toBe("current");
  });

  it("toggleCollapsed flips the dock's collapsed state", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.toggleCollapsed());
    expect(result.current.collapsed).toBe(true);
    act(() => result.current.toggleCollapsed());
    expect(result.current.collapsed).toBe(false);
  });

  it("setCollapsed sets an explicit value", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.setCollapsed(true));
    expect(result.current.collapsed).toBe(true);
  });
});

describe("useDesignStudio — acceptance", () => {
  it("accepted() forces comparison mode back to current, clears prompt draft, closes refinement", () => {
    const { result } = renderHook(() => useDesignStudio());
    act(() => result.current.selectTarget({ kind: "object", id: "object_sofa_123" }));
    act(() => result.current.restoreSession("session_1", "turn_1", "attempt_1"));
    act(() => result.current.setComparisonMode("concept"));
    act(() => result.current.openRefinementDraft());

    act(() => result.current.accepted());

    expect(result.current.comparisonMode).toBe("current");
    expect(result.current.promptDraft).toBe("");
    expect(result.current.refinementDraftOpen).toBe(false);
    // The session stays active after acceptance for continued refinement
    // (plan invariant) — target/session/turn/attempt tracking is untouched.
    expect(result.current.sessionId).toBe("session_1");
  });
});

describe("deriveStudioState", () => {
  const base = {
    target: { kind: "object" as const, id: "object_sofa_123" },
    isSupportedKind: true,
    isRestoring: false,
    isStalePlan: false,
    isRegenerating: false,
  };

  it("returns empty when nothing is selected", () => {
    expect(deriveStudioState({ ...base, target: null })).toBe("empty");
  });

  it("returns unsupported for a structural (non-object/fixture) selection", () => {
    expect(deriveStudioState({ ...base, isSupportedKind: false })).toBe("unsupported");
  });

  it("returns restoring while a prior session is being loaded", () => {
    expect(deriveStudioState({ ...base, isRestoring: true })).toBe("restoring");
  });

  it("returns stale when the RoomDraft revision changed since the plan was made, regardless of other state", () => {
    expect(deriveStudioState({ ...base, isStalePlan: true, attemptStatus: "concept_ready" })).toBe("stale");
  });

  it("returns welcome with no turn/attempt yet", () => {
    expect(deriveStudioState(base)).toBe("welcome");
  });

  it("returns planning while a turn is reserved/reasoning", () => {
    expect(deriveStudioState({ ...base, turnStatus: "reasoning" })).toBe("planning");
  });

  it("returns planning immediately while the design-turn submission is pending", () => {
    expect(deriveStudioState({ ...base, isSubmittingTurn: true })).toBe("planning");
  });

  it("returns plan_blocked for a blocked turn", () => {
    expect(deriveStudioState({ ...base, turnStatus: "blocked" })).toBe("plan_blocked");
  });

  it("keeps terminal reasoning failures visible", () => {
    expect(deriveStudioState({ ...base, turnStatus: "failed" })).toBe("failed");
    expect(deriveStudioState({ ...base, turnStatus: "needs_attention" })).toBe("needs_attention");
  });

  it("returns plan_ready for a proposed turn with no attempt yet", () => {
    expect(deriveStudioState({ ...base, turnStatus: "proposed" })).toBe("plan_ready");
  });

  it("returns geometry_generating for a non-terminal attempt", () => {
    expect(deriveStudioState({ ...base, attemptStatus: "asset_generation_processing" })).toBe("geometry_generating");
  });

  it("returns concept_ready once the attempt reaches concept_ready, regardless of kind (material-only or geometry)", () => {
    expect(deriveStudioState({ ...base, attemptStatus: "concept_ready" })).toBe("concept_ready");
  });

  it("returns failed for a failed/needs_attention/abandoned attempt", () => {
    expect(deriveStudioState({ ...base, attemptStatus: "failed" })).toBe("failed");
    expect(deriveStudioState({ ...base, attemptStatus: "needs_attention" })).toBe("failed");
    expect(deriveStudioState({ ...base, attemptStatus: "abandoned" })).toBe("failed");
  });

  it("returns accepted once the attempt is accepted", () => {
    expect(deriveStudioState({ ...base, attemptStatus: "accepted" })).toBe("accepted");
  });

  it("returns regenerating when a new attempt is running after Regenerate, even with a prior ready concept", () => {
    expect(deriveStudioState({ ...base, isRegenerating: true, attemptStatus: "concept_ready" })).toBe("regenerating");
  });
});
