import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FloorPlanViewport } from "./FloorPlanViewport";
import type { RoomDraft } from "../../api";

const fixtureDraft: RoomDraft = {
  id: "draft_1",
  captureId: "capture_1",
  revision: 3,
  canResetToScan: true,
  walls: [
    {
      id: "wall_1",
      start: { x: 0, y: 0, z: 0 },
      end: { x: 4, y: 0, z: 0 },
      provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
      thicknessStatus: "estimated",
    },
  ],
  openings: [],
  objects: [
    {
      id: "object_1",
      category: "sofa",
      transform: { position: { x: -1, y: 0, z: -1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      dimensions: { x: 2, y: 0.8, z: 1 },
      provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
    },
  ],
  fixtures: [
    {
      id: "fixture_1",
      category: "boiler",
      transform: { position: { x: 2, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      dimensions: { x: 0.6, y: 0.8, z: 0.4 },
      createdBy: "contractor",
    },
  ],
  servicePoints: [
    { id: "sp_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 }, createdBy: "contractor" },
  ],
  constraints: [
    {
      id: "constraint_1",
      kind: "column",
      transform: { position: { x: 3, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      createdBy: "contractor",
    },
  ],
};

const defaultViewport = { panX: 0, panY: 0, zoom: 40 };

function renderViewport(overrides: Partial<React.ComponentProps<typeof FloorPlanViewport>> = {}) {
  const onSelect = vi.fn();
  const onHover = vi.fn();
  const onViewportChange = vi.fn();
  const onDragEnd = vi.fn();
  const onObjectResizeEnd = vi.fn();
  const onObjectRotateEnd = vi.fn();
  render(
    <FloorPlanViewport
      draft={fixtureDraft}
      viewport={defaultViewport}
      onViewportChange={onViewportChange}
      selection={null}
      onSelect={onSelect}
      hoverId={null}
      onHover={onHover}
      onDragEnd={onDragEnd}
      onObjectResizeEnd={onObjectResizeEnd}
      onObjectRotateEnd={onObjectRotateEnd}
      dragDisabled={false}
      {...overrides}
    />,
  );
  return { onSelect, onHover, onViewportChange, onDragEnd, onObjectResizeEnd, onObjectRotateEnd };
}

describe("FloorPlanViewport — rendering", () => {
  it("renders a wall from the fixture RoomDraft with its canonical id", () => {
    renderViewport();
    const wall = document.querySelector('[data-element-kind="wall"][data-element-id="wall_1"]');
    expect(wall).not.toBeNull();
  });

  it("renders a service point from the fixture RoomDraft with its canonical id", () => {
    renderViewport();
    const sp = document.querySelector('[data-element-kind="servicePoint"][data-element-id="sp_1"]');
    expect(sp).not.toBeNull();
  });

  it("renders nothing for a null draft without throwing", () => {
    renderViewport({ draft: null });
    expect(screen.getByRole("img", { name: /floor plan/i })).toBeInTheDocument();
  });

  it("renders a fixture from the fixture RoomDraft with its canonical id", () => {
    renderViewport();
    const fixture = document.querySelector('[data-element-kind="fixture"][data-element-id="fixture_1"]');
    expect(fixture).not.toBeNull();
  });

  it("renders a constraint from the fixture RoomDraft with its canonical id", () => {
    renderViewport();
    const constraint = document.querySelector('[data-element-kind="constraint"][data-element-id="constraint_1"]');
    expect(constraint).not.toBeNull();
  });
});

describe("FloorPlanViewport — selection", () => {
  it("clicking a wall selects it by its canonical id, not array position", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderViewport();
    // The visible <line data-element-kind="wall"> is pointer-events:none —
    // it's purely decorative. The actual click/hover/drag hit target is the
    // wider invisible wallBody line underneath (so the thin wall is easy to
    // grab for a move_wall drag without changing its visible width).
    const wallHitTarget = document.querySelector('[data-element-kind="wallBody"][data-wall-id="wall_1"]') as SVGElement;
    await user.click(wallHitTarget);
    expect(onSelect).toHaveBeenCalledWith("wall", "wall_1");
  });

  it("clicking a service point selects it distinctly from a wall", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderViewport();
    const sp = document.querySelector('[data-element-kind="servicePoint"][data-element-id="sp_1"]') as SVGElement;
    await user.click(sp);
    expect(onSelect).toHaveBeenCalledWith("servicePoint", "sp_1");
  });

  it("clicking a fixture selects it by its canonical id", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderViewport();
    const fixture = document.querySelector('[data-element-kind="fixture"][data-element-id="fixture_1"]') as SVGElement;
    await user.click(fixture);
    expect(onSelect).toHaveBeenCalledWith("fixture", "fixture_1");
  });

  it("clicking a constraint selects it by its canonical id", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderViewport();
    const constraint = document.querySelector('[data-element-kind="constraint"][data-element-id="constraint_1"]') as SVGElement;
    await user.click(constraint);
    expect(onSelect).toHaveBeenCalledWith("constraint", "constraint_1");
  });
});

describe("FloorPlanViewport — drag targets", () => {
  it("renders a draggable corner handle for each wall endpoint with the wall's canonical id", () => {
    renderViewport();
    const startHandle = document.querySelector('[data-element-kind="wallCorner"][data-wall-id="wall_1"][data-endpoint="start"]');
    const endHandle = document.querySelector('[data-element-kind="wallCorner"][data-wall-id="wall_1"][data-endpoint="end"]');
    expect(startHandle).not.toBeNull();
    expect(endHandle).not.toBeNull();
  });

  it("dragging the wall body (not a corner) reports a wallBody target on drag end", async () => {
    const user = userEvent.setup();
    const { onDragEnd } = renderViewport();
    const wallHitTarget = document.querySelector('[data-element-kind="wallBody"][data-wall-id="wall_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: wallHitTarget, coords: { x: 100, y: 100 } },
      { coords: { x: 130, y: 100 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onDragEnd).toHaveBeenCalledTimes(1);
    const [target] = onDragEnd.mock.calls[0] as [{ kind: string; wallId: string }, unknown];
    expect(target.kind).toBe("wallBody");
    expect(target.wallId).toBe("wall_1");
  });

  it("dragging a fixture reports a fixture target on drag end", async () => {
    const user = userEvent.setup();
    const { onDragEnd } = renderViewport();
    const fixture = document.querySelector('[data-element-kind="fixture"][data-element-id="fixture_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: fixture, coords: { x: 100, y: 100 } },
      { coords: { x: 120, y: 100 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onDragEnd).toHaveBeenCalledTimes(1);
    const [target] = onDragEnd.mock.calls[0] as [{ kind: string; fixtureId: string }, unknown];
    expect(target.kind).toBe("fixture");
    expect(target.fixtureId).toBe("fixture_1");
  });

  it("dragging a constraint reports a constraint target on drag end", async () => {
    const user = userEvent.setup();
    const { onDragEnd } = renderViewport();
    const constraint = document.querySelector('[data-element-kind="constraint"][data-element-id="constraint_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: constraint, coords: { x: 100, y: 100 } },
      { coords: { x: 120, y: 100 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onDragEnd).toHaveBeenCalledTimes(1);
    const [target] = onDragEnd.mock.calls[0] as [{ kind: string; constraintId: string }, unknown];
    expect(target.kind).toBe("constraint");
    expect(target.constraintId).toBe("constraint_1");
  });
});

// M8.5C reversible-editing patch §16: wheel zoom must work via a native,
// explicitly non-passive listener — React's onWheel is passive by default
// (React 17+), so event.preventDefault() inside it silently no-ops and
// logs "Unable to preventDefault inside passive event listener invocation."
describe("FloorPlanViewport — wheel zoom (native non-passive listener)", () => {
  it("a real (non-cancelable-by-React) wheel event still zooms, proving a native listener handles it", () => {
    const { onViewportChange } = renderViewport();
    const svg = screen.getByRole("img", { name: /floor plan/i });

    // A genuinely dispatched, cancelable WheelEvent — exercises the SAME
    // path the browser uses for a real trackpad/mouse wheel gesture,
    // unlike fireEvent.wheel's React synthetic event.
    const wheelEvent = new WheelEvent("wheel", { deltaY: -100, cancelable: true, bubbles: true });
    svg.dispatchEvent(wheelEvent);

    // The component's own auto-fit-to-room effect (unrelated to wheel
    // zoom — see FloorPlanViewport.tsx's ResizeObserver effect) also calls
    // onViewportChange once on mount against jsdom's real (nonzero)
    // measured container size, so the wheel zoom is whichever call used
    // the ORIGINAL defaultViewport.zoom (40) as its base, not just "the
    // last call".
    const zoomCall = onViewportChange.mock.calls.map((args) => args[0] as { zoom: number }).find((v) => v.zoom !== defaultViewport.zoom && Math.abs(v.zoom - defaultViewport.zoom * 1.1) < 0.01);
    expect(zoomCall).toBeDefined();
    expect(zoomCall!.zoom).toBeGreaterThan(defaultViewport.zoom);
    // preventDefault genuinely took effect (not silently swallowed by a
    // passive listener) — confirmed via the event's own defaultPrevented
    // flag, not a console-warning proxy.
    expect(wheelEvent.defaultPrevented).toBe(true);
  });

  it("zooms out on a positive deltaY", () => {
    const { onViewportChange } = renderViewport();
    const svg = screen.getByRole("img", { name: /floor plan/i });
    svg.dispatchEvent(new WheelEvent("wheel", { deltaY: 100, cancelable: true, bubbles: true }));
    const zoomCall = onViewportChange.mock.calls.map((args) => args[0] as { zoom: number }).find((v) => Math.abs(v.zoom - defaultViewport.zoom * 0.9) < 0.01);
    expect(zoomCall).toBeDefined();
  });
});

// M8.5C closure patch §16-§20: real object footprint + resize/rotate
// handles, shown only once selected. Body-drag (move) is unaffected —
// covered separately below to prove it still works alongside the new
// handles rather than being displaced by them.
describe("FloorPlanViewport — object footprint + resize/rotate handles (§16-§20)", () => {
  it("does not render resize/rotate handles when the object is not selected", () => {
    renderViewport({ selection: null });
    expect(document.querySelector('[data-element-kind="objectResizeHandle"]')).toBeNull();
    expect(document.querySelector('[data-element-kind="objectRotateHandle"]')).toBeNull();
  });

  it("renders resize and rotate handles once the object is selected", () => {
    renderViewport({ selection: { kind: "object", id: "object_1" } });
    expect(document.querySelector('[data-element-kind="objectResizeHandle"][data-object-id="object_1"]')).not.toBeNull();
    expect(document.querySelector('[data-element-kind="objectRotateHandle"][data-object-id="object_1"]')).not.toBeNull();
  });

  it("dragging the resize handle submits objectResizeEnd with center-anchored canonical dimensions, exactly once on release", async () => {
    const user = userEvent.setup();
    const { onObjectResizeEnd } = renderViewport({ selection: { kind: "object", id: "object_1" } });
    const handle = document.querySelector('[data-element-kind="objectResizeHandle"][data-object-id="object_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: handle, coords: { x: 100, y: 100 } },
      { coords: { x: 160, y: 160 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onObjectResizeEnd).toHaveBeenCalledTimes(1);
    const [end] = onObjectResizeEnd.mock.calls[0] as [{ objectId: string; dimensions: { x: number; y: number; z: number } }];
    expect(end.objectId).toBe("object_1");
    // Height is preserved from the object's own dimensions — this handle
    // never touches vertical extent.
    expect(end.dimensions.y).toBe(0.8);
    expect(end.dimensions.x).toBeGreaterThan(0);
    expect(end.dimensions.z).toBeGreaterThan(0);
  });

  it("dragging the rotation handle submits objectRotateEnd with a yaw-only canonical quaternion, exactly once on release", async () => {
    const user = userEvent.setup();
    const { onObjectRotateEnd } = renderViewport({ selection: { kind: "object", id: "object_1" } });
    const handle = document.querySelector('[data-element-kind="objectRotateHandle"][data-object-id="object_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: handle, coords: { x: 100, y: 100 } },
      { coords: { x: 100, y: 160 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onObjectRotateEnd).toHaveBeenCalledTimes(1);
    const [end] = onObjectRotateEnd.mock.calls[0] as [{ objectId: string; rotation: { x: number; y: number; z: number; w: number } }];
    expect(end.objectId).toBe("object_1");
    // Yaw-only: no X/Z rotation component, matching the numeric Rotation
    // inspector field's own convention exactly.
    expect(end.rotation.x).toBe(0);
    expect(end.rotation.z).toBe(0);
  });

  it("body drag still emits a plain move for a selected object (handles do not displace the existing move gesture)", async () => {
    const user = userEvent.setup();
    const { onDragEnd, onObjectResizeEnd, onObjectRotateEnd } = renderViewport({ selection: { kind: "object", id: "object_1" } });
    const body = document.querySelector('[data-element-kind="object"][data-element-id="object_1"]') as SVGElement;

    await user.pointer([
      { keys: "[MouseLeft>]", target: body, coords: { x: 100, y: 100 } },
      { coords: { x: 130, y: 100 } },
      { keys: "[/MouseLeft]" },
    ]);

    expect(onDragEnd).toHaveBeenCalledTimes(1);
    const [target] = onDragEnd.mock.calls[0] as [{ kind: string; objectId: string }, unknown];
    expect(target.kind).toBe("object");
    expect(target.objectId).toBe("object_1");
    expect(onObjectResizeEnd).not.toHaveBeenCalled();
    expect(onObjectRotateEnd).not.toHaveBeenCalled();
  });
});
