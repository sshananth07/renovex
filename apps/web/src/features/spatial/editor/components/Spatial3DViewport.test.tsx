import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import Spatial3DViewport, { withConceptOverride } from "./Spatial3DViewport";
import { projectScene } from "../three/sceneProjection";
import type { RoomDraft } from "../../api";
import type { ConceptRenderState } from "../../design-studio/types";

// Spatial3DViewport mounts a real R3F <Canvas>. jsdom has no WebGL, but R3F
// itself mounts cleanly (confirmed by an implementation-time spike) —
// everything OUTSIDE the <canvas> element (the toolbar overlay, the
// first-visit hint) is real DOM and testable via RTL exactly like any
// other component. Everything INSIDE the canvas (walls, fixtures, etc.) is
// Three.js scene-graph state, not DOM — mesh/group elements are NOT real
// DOM nodes and cannot carry data-* HTML attributes (confirmed live: R3F
// throws "Cannot set 'data-element-id'" if attempted) — per the plan's
// explicit testing-architecture note ("do not attempt to prove all
// Three.js behavior through jsdom snapshots"), that is covered
// by sceneProjection.test.ts/cameraMath.test.ts's pure-function tests
// instead, not here.

function baseDraft(overrides: Partial<RoomDraft> = {}): RoomDraft {
  return {
    id: "draft_1",
    captureId: "capture_1",
    revision: 1,
    canResetToScan: false,
    walls: [
      {
        id: "wall_1",
        start: { x: 0, y: 0, z: 0 },
        end: { x: 4, y: 0, z: 0 },
        thicknessStatus: "estimated",
        provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
      },
    ],
    openings: [],
    objects: [],
    fixtures: [],
    servicePoints: [],
    constraints: [],
    ...overrides,
  };
}

function renderViewport(overrides: Partial<React.ComponentProps<typeof Spatial3DViewport>> = {}) {
  const onSelect = vi.fn();
  const onClearSelection = vi.fn();
  const onHover = vi.fn();
  const onDragEnd = vi.fn();
  const onCameraPoseSettled = vi.fn();
  render(
    <Spatial3DViewport
      draft={baseDraft()}
      selection={null}
      onSelect={onSelect}
      onClearSelection={onClearSelection}
      onHover={onHover}
      hoverId={null}
      onDragEnd={onDragEnd}
      dragDisabled={false}
      cameraPose={null}
      onCameraPoseSettled={onCameraPoseSettled}
      reducedMotion={false}
      {...overrides}
    />,
  );
  return { onSelect, onClearSelection, onHover, onDragEnd, onCameraPoseSettled };
}

const IDENTITY_TRANSFORM = { position: { x: 5, y: 0, z: 5 }, rotation: { x: 0, y: 0, z: 0, w: 1 } };
const APPEARANCE = { baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "matte", metallic: false };

describe("withConceptOverride — pure candidate-substitution logic (RP4E2/E3)", () => {
  const scene = projectScene(
    baseDraft({
      objects: [
        { id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 2, y: 0.8, z: 1 }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } },
      ],
      fixtures: [
        { id: "fixture_1", category: "boiler", transform: { position: { x: 3, y: 0, z: 3 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.5, y: 0.5, z: 0.5 }, createdBy: "contractor" },
      ],
    }),
  );

  it("is a no-op for kind 'none' or 'stale'", () => {
    expect(withConceptOverride(scene, { kind: "none" })).toBe(scene);
    expect(withConceptOverride(scene, { kind: "stale", attemptId: "a1" })).toBe(scene);
  });

  it("substitutes only the exact selected object's transform, leaving every other element (and its own other fields) untouched", () => {
    const preview: ConceptRenderState = {
      kind: "material_preview",
      attemptId: "a1",
      target: { kind: "object", id: "object_1" },
      appearance: APPEARANCE,
      transform: IDENTITY_TRANSFORM,
    };
    const result = withConceptOverride(scene, preview);
    expect(result.objects[0]!.position).toEqual(IDENTITY_TRANSFORM.position);
    expect(result.objects[0]!.appearance).toEqual(APPEARANCE);
    // Dimensions/category untouched since the preview carried none.
    expect(result.objects[0]!.dimensions).toEqual(scene.objects[0]!.dimensions);
    expect(result.objects[0]!.category).toBe("sofa");
    // The fixture is a completely different element — unaffected.
    expect(result.fixtures[0]).toBe(scene.fixtures[0]);
  });

  it("substitutes the exact selected fixture, not the object, when the target is a fixture", () => {
    const preview: ConceptRenderState = {
      kind: "geometry_pending",
      attemptId: "a1",
      target: { kind: "fixture", id: "fixture_1" },
      transform: IDENTITY_TRANSFORM,
      dimensions: { x: 1, y: 1, z: 1 },
    };
    const result = withConceptOverride(scene, preview);
    expect(result.fixtures[0]!.position).toEqual(IDENTITY_TRANSFORM.position);
    expect(result.fixtures[0]!.dimensions).toEqual({ x: 1, y: 1, z: 1 });
    expect(result.objects[0]).toBe(scene.objects[0]);
  });

  it("carries the candidate's visualAsset into the target's visualAsset field for a concept_ready preview", () => {
    const preview: ConceptRenderState = {
      kind: "concept_ready",
      attemptId: "a1",
      target: { kind: "object", id: "object_1" },
      assetRef: { assetId: "generated-asset", version: 1 },
      transform: IDENTITY_TRANSFORM,
    };
    const result = withConceptOverride(scene, preview);
    expect(result.objects[0]!.visualAsset).toEqual({ assetId: "generated-asset", version: 1 });
  });

  it("does not touch anything when the preview targets an element id not present in the scene", () => {
    const preview: ConceptRenderState = {
      kind: "material_preview",
      attemptId: "a1",
      target: { kind: "object", id: "object_missing" },
      appearance: APPEARANCE,
      transform: IDENTITY_TRANSFORM,
    };
    const result = withConceptOverride(scene, preview);
    expect(result.objects[0]).toBe(scene.objects[0]);
  });
});

describe("Spatial3DViewport — mount and chrome", () => {
  it("mounts without throwing and renders the camera toolbar", () => {
    renderViewport();
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Overview" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Top" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Eye level" })).toBeInTheDocument();
  });

  it("does not show Focus Selected when nothing is selected", () => {
    renderViewport({ selection: null });
    expect(screen.queryByRole("button", { name: "Focus selected" })).not.toBeInTheDocument();
  });

  it("shows Focus Selected when an element is selected", () => {
    renderViewport({ selection: { kind: "wall", id: "wall_1" } });
    expect(screen.getByRole("button", { name: "Focus selected" })).toBeInTheDocument();
  });

  it("camera preset buttons do not throw when clicked", async () => {
    const user = userEvent.setup();
    renderViewport();
    await user.click(screen.getByRole("button", { name: "Fit room" }));
    await user.click(screen.getByRole("button", { name: "Overview" }));
    await user.click(screen.getByRole("button", { name: "Top" }));
    await user.click(screen.getByRole("button", { name: "Eye level" }));
  });
});

describe("Spatial3DViewport — first-visit hint", () => {
  it("shows the hint on first render and dismisses it, persisting the dismissal", async () => {
    window.localStorage.removeItem("renovex.spatial3d.hintDismissed");
    const user = userEvent.setup();
    renderViewport();

    expect(screen.getByText(/drag to orbit/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Got it" }));
    expect(screen.queryByText(/drag to orbit/i)).not.toBeInTheDocument();
    expect(window.localStorage.getItem("renovex.spatial3d.hintDismissed")).toBe("true");
  });

  it("does not show the hint again once previously dismissed", () => {
    window.localStorage.setItem("renovex.spatial3d.hintDismissed", "true");
    renderViewport();
    expect(screen.queryByText(/drag to orbit/i)).not.toBeInTheDocument();
    window.localStorage.removeItem("renovex.spatial3d.hintDismissed");
  });
});

describe("Spatial3DViewport — RP4C4 real-asset resolution (mount-level smoke)", () => {
  // Per RP4C4's own testing-scope decision (jsdom/RTL cannot see inside
  // the R3F canvas, confirmed here exactly as RP4C3 already established
  // for TransformControls): these prove the component TREE mounts and
  // unmounts cleanly when a fixture/object resolves to a REAL registered
  // asset (boiler/sofa) vs. an unregistered category (procedural
  // fallback) — not the internal scene-graph shape (dispatch, overlay
  // placement, TransformControls targeting), which the mandatory
  // real-browser verification pass covers instead.
  it("mounts without throwing when a fixture resolves to a real registered asset (boiler)", () => {
    renderViewport({
      draft: baseDraft({
        fixtures: [
          {
            id: "fixture_1",
            category: "boiler",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            createdBy: "contractor",
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });

  it("mounts without throwing when a fixture has NO registered asset (procedural fallback)", () => {
    renderViewport({
      draft: baseDraft({
        fixtures: [
          {
            id: "fixture_1",
            category: "ac",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            createdBy: "contractor",
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });

  it("mounts without throwing when an object resolves to a real registered asset (sofa)", () => {
    renderViewport({
      draft: baseDraft({
        objects: [
          {
            id: "object_1",
            category: "sofa",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });

  it("mounts without throwing for a wall-attached fixture resolving to a real registered asset (electrical_panel)", () => {
    renderViewport({
      draft: baseDraft({
        walls: [
          {
            id: "wall_1",
            start: { x: 0, y: 0, z: 0 },
            end: { x: 4, y: 0, z: 0 },
            thicknessStatus: "estimated",
            provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
          },
        ],
        fixtures: [
          {
            id: "fixture_1",
            category: "electrical_panel",
            transform: { position: { x: 1, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            createdBy: "contractor",
            parentWallId: "wall_1",
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });
});

describe("Spatial3DViewport — RP4D canonical visual-asset binding (mount-level smoke)", () => {
  // Same testing-scope decision as the RP4C4 block above: jsdom/RTL cannot
  // see inside the R3F canvas, so these prove the component tree mounts
  // cleanly with an explicit canonical binding present (which now routes
  // through the "authorized" VisualResolution tier and AuthorizedVisualAsset,
  // rather than the plain builtin-registry lookup) — not the internal
  // scene-graph shape, which the mandatory real-browser verification pass
  // covers instead.
  it("mounts without throwing when a fixture has an explicit canonical visualAsset binding, and does not swallow an unrelated render error as a false-positive fallback", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    renderViewport({
      draft: baseDraft({
        fixtures: [
          {
            id: "fixture_1",
            category: "boiler",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            createdBy: "contractor",
            visualAsset: { assetId: "custom-boiler-asset", version: 3 },
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
    // AuthorizedVisualAsset calls useAuth()/useQuery() with no provider in
    // this test tree — if that threw, AssetErrorBoundary (a sibling
    // wrapper) would silently swallow it into the fallback UI, and this
    // mount-smoke test would report a false "mounts without throwing"
    // pass. Asserting zero console.error calls proves that did NOT happen.
    expect(errorSpy).not.toHaveBeenCalled();
    errorSpy.mockRestore();
  });

  it("mounts without throwing when an object has an explicit canonical visualAsset binding", () => {
    renderViewport({
      draft: baseDraft({
        objects: [
          {
            id: "object_1",
            category: "sofa",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
            visualAsset: { assetId: "custom-sofa-asset", version: 1 },
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });

  it("mounts without throwing when a fixture is bound to an unregistered category with no builtin fallback", () => {
    renderViewport({
      draft: baseDraft({
        fixtures: [
          {
            id: "fixture_1",
            category: "unheard_of_category",
            transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
            createdBy: "contractor",
            visualAsset: { assetId: "custom-asset", version: 1 },
          },
        ],
      }),
    });
    expect(screen.getByRole("button", { name: "Fit room" })).toBeInTheDocument();
  });
});

describe("Spatial3DViewport — CurrentConceptToggle overlay (RP4E2/E3)", () => {
  it("does not render the toggle when there is no concept preview", () => {
    renderViewport({ conceptPreview: { kind: "none" }, comparisonMode: "current", onComparisonModeChange: vi.fn() });
    expect(screen.queryByRole("group", { name: /compare current and concept/i })).not.toBeInTheDocument();
  });

  it("does not render the toggle when the caller passes no onComparisonModeChange, even with a preview present", () => {
    renderViewport({
      conceptPreview: { kind: "material_preview", attemptId: "a1", target: { kind: "object", id: "o1" }, appearance: { baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "matte", metallic: false }, transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } },
      comparisonMode: "current",
    });
    expect(screen.queryByRole("group", { name: /compare current and concept/i })).not.toBeInTheDocument();
  });

  it("renders the toggle when a concept preview exists, reflecting the current comparisonMode", () => {
    renderViewport({
      conceptPreview: { kind: "material_preview", attemptId: "a1", target: { kind: "object", id: "o1" }, appearance: { baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "matte", metallic: false }, transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } },
      comparisonMode: "current",
      onComparisonModeChange: vi.fn(),
    });
    expect(screen.getByRole("button", { name: "Current" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Concept" })).toHaveAttribute("aria-pressed", "false");
  });

  it("calls onComparisonModeChange when the Concept button is clicked", async () => {
    const user = userEvent.setup();
    const onComparisonModeChange = vi.fn();
    renderViewport({
      conceptPreview: { kind: "concept_ready", attemptId: "a1", target: { kind: "object", id: "o1" }, transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } },
      comparisonMode: "current",
      onComparisonModeChange,
    });
    await user.click(screen.getByRole("button", { name: "Concept" }));
    expect(onComparisonModeChange).toHaveBeenCalledWith("concept");
  });

  it("disables the Concept option while the preview is only geometry_pending (nothing to compare against yet)", () => {
    renderViewport({
      conceptPreview: { kind: "geometry_pending", attemptId: "a1", target: { kind: "object", id: "o1" }, transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } },
      comparisonMode: "current",
      onComparisonModeChange: vi.fn(),
    });
    expect(screen.getByRole("button", { name: "Concept" })).toBeDisabled();
  });
});

describe("Spatial3DViewport — camera pose lifecycle", () => {
  // jsdom has no real WebGL render loop, so @react-three/fiber's reconciler
  // never actually commits the CameraControls ref here — this is exactly
  // the kind of Canvas-internal behavior the plan calls out as untestable
  // in jsdom ("do not attempt to prove all Three.js behavior through jsdom
  // snapshots"). What IS verifiable without a real ref: mounting alone
  // never calls onCameraPoseSettled (it's a lifecycle-boundary write, never
  // a per-render one) — the real "settles on unmount with a real
  // controls ref" behavior is covered by the RP4C3 real-browser
  // verification pass instead.
  it("never calls onCameraPoseSettled just from mounting or re-rendering", () => {
    const onCameraPoseSettled = vi.fn();
    const { rerender } = render(
      <Spatial3DViewport
        draft={baseDraft()}
        selection={null}
        onSelect={vi.fn()}
        onClearSelection={vi.fn()}
        onHover={vi.fn()}
        hoverId={null}
        onDragEnd={vi.fn()}
        dragDisabled={false}
        cameraPose={null}
        onCameraPoseSettled={onCameraPoseSettled}
        reducedMotion={false}
      />,
    );
    expect(onCameraPoseSettled).not.toHaveBeenCalled();

    rerender(
      <Spatial3DViewport
        draft={baseDraft({ revision: 2 })}
        selection={null}
        onSelect={vi.fn()}
        onClearSelection={vi.fn()}
        onHover={vi.fn()}
        hoverId={null}
        onDragEnd={vi.fn()}
        dragDisabled={false}
        cameraPose={null}
        onCameraPoseSettled={onCameraPoseSettled}
        reducedMotion={false}
      />,
    );
    expect(onCameraPoseSettled).not.toHaveBeenCalled();
  });
});

// M8.5C closure patch §9: the Move/Rotate/Resize mode toolbar is real DOM
// (outside the <canvas>), so it's directly testable via RTL exactly like
// the camera preset buttons above — the actual per-mode TransformControls
// math (translate→position, rotate→canonical yaw quaternion, scale→
// canonical dimensions) is covered separately by sceneProjection.test.ts's
// pure-function tests (dimensionsFromScale/quaternionToYawDegrees/
// yawDegreesToQuaternion), per this file's own established convention of
// not attempting to prove Three.js scene-graph behavior through jsdom.
describe("Spatial3DViewport — object transform-mode toolbar (§9)", () => {
  it("does not show the Move/Rotate/Resize toolbar when nothing is selected", () => {
    renderViewport();
    expect(screen.queryByRole("button", { name: "Move" })).not.toBeInTheDocument();
  });

  it("does not show the toolbar for a non-object selection", () => {
    renderViewport({
      draft: baseDraft({ fixtures: [{ id: "fixture_1", category: "boiler", transform: IDENTITY_TRANSFORM, createdBy: "contractor" }] }),
      selection: { kind: "fixture", id: "fixture_1" },
    });
    expect(screen.queryByRole("button", { name: "Move" })).not.toBeInTheDocument();
  });

  it("shows Move/Rotate/Resize with Move active by default when an object is selected", () => {
    renderViewport({
      draft: baseDraft({ objects: [{ id: "object_1", category: "sofa", transform: IDENTITY_TRANSFORM, dimensions: { x: 1, y: 1, z: 1 }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }] }),
      selection: { kind: "object", id: "object_1" },
    });
    expect(screen.getByRole("button", { name: "Move" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Rotate" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Resize" })).toBeInTheDocument();
  });

  it("switches mode when Rotate/Resize is clicked, without submitting anything itself (mode is local UI state only)", async () => {
    const user = userEvent.setup();
    const { onDragEnd } = renderViewport({
      draft: baseDraft({ objects: [{ id: "object_1", category: "sofa", transform: IDENTITY_TRANSFORM, dimensions: { x: 1, y: 1, z: 1 }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }] }),
      selection: { kind: "object", id: "object_1" },
    });

    await user.click(screen.getByRole("button", { name: "Rotate" }));
    await user.click(screen.getByRole("button", { name: "Resize" }));

    expect(onDragEnd).not.toHaveBeenCalled();
  });

  it("disables the mode toolbar while dragDisabled (a mutation is in flight)", () => {
    renderViewport({
      draft: baseDraft({ objects: [{ id: "object_1", category: "sofa", transform: IDENTITY_TRANSFORM, dimensions: { x: 1, y: 1, z: 1 }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }] }),
      selection: { kind: "object", id: "object_1" },
      dragDisabled: true,
    });
    expect(screen.getByRole("button", { name: "Move" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Rotate" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Resize" })).toBeDisabled();
  });
});
