"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { objectFootprintCorners, worldToScreen, screenToWorld, fitToRoom, clampZoom, type ContainerSize } from "../coordinates";
import { quaternionToYawDegrees, yawDegreesToQuaternion } from "../three/sceneProjection";
import type { ElementKind, RoomLocalPoint, RoomLocalQuaternion, Selection, Viewport } from "../types";
import type { RoomDraft } from "../../api";

// A draggable target. Kept distinct from Selection/ElementKind's broader
// vocabulary because not every selectable element kind is draggable, and
// wallEndpoint (move_corner) vs wallBody (move_wall) are two DISTINCT drag
// targets on the same wall — dragging a corner handle moves just that
// corner (an absolute new position); dragging the wall's own line
// translates the whole wall (a delta), matching MoveWallOperation's exact
// {wallId, delta} wire shape, never overlapping with move_corner's
// semantics. objectResizeHandle/objectRotateHandle (closure patch §17-§20)
// are the 2D counterpart of Spatial3DViewport's TransformControls
// rotate/scale modes — same canonical resize_object/rotate_object
// operations, same "one gesture, one canonical mutation on release"
// discipline, driven by SVG handles instead of a 3D gizmo.
export type DragTarget =
  | { kind: "wallEndpoint"; wallId: string; endpoint: "start" | "end" }
  | { kind: "wallBody"; wallId: string }
  | { kind: "servicePoint"; servicePointId: string }
  | { kind: "fixture"; fixtureId: string }
  | { kind: "object"; objectId: string }
  | { kind: "objectResizeHandle"; objectId: string }
  | { kind: "objectRotateHandle"; objectId: string }
  | { kind: "constraint"; constraintId: string };

// onDragEnd's second argument is a RoomLocalPoint for every DragTarget
// except wallBody, where it's the drag's raw world-space delta (final
// pointer world position minus the position at drag-start) — the exact
// shape MoveWallOperation's payload needs. SpatialEditor's onDragEnd
// switches on target.kind to know which interpretation applies.
export type DragEndPosition = RoomLocalPoint;

// Reported once per completed resize/rotate handle gesture — never on
// every pointer move (§19/§20: "one user gesture = one canonical mutation
// = one Undo entry"). Kept as separate callbacks from onDragEnd rather than
// overloading its RoomLocalPoint parameter with more union shapes, since
// dimensions/rotation are genuinely different payloads from every existing
// DragTarget's plain position.
export type ObjectResizeEnd = { objectId: string; dimensions: RoomLocalPoint };
export type ObjectRotateEnd = { objectId: string; rotation: RoomLocalQuaternion };

// Renders the canonical RoomDraft geometry as a top-down 2D floor plan.
// Prioritizes geometric correctness and interaction architecture (stable
// selection under pan/zoom, hit-testing via canonical IDs) over visual
// polish, per plan §3. All coordinate math goes through coordinates.ts —
// nothing here computes screen positions ad hoc.
//
// Dragging a wall endpoint or service point is transient, visual-preview-
// only local state (dragPreview below) — it never touches the authoritative
// RoomDraft. On release, onDragEnd reports the final world position; the
// caller (SpatialEditor) decides whether to build and submit a PendingEdit.
export function FloorPlanViewport({
  draft,
  viewport,
  onViewportChange,
  selection,
  onSelect,
  hoverId,
  onHover,
  onDragEnd,
  onObjectResizeEnd,
  onObjectRotateEnd,
  dragDisabled,
}: {
  draft: RoomDraft | null | undefined;
  viewport: Viewport;
  onViewportChange: (viewport: Viewport) => void;
  selection: Selection;
  onSelect: (kind: ElementKind, id: string) => void;
  hoverId: string | null;
  onHover: (id: string | null) => void;
  onDragEnd: (target: DragTarget, newPosition: RoomLocalPoint) => void;
  // §17-§20: fired once on release of the resize/rotate handle — same
  // "one gesture, one canonical mutation" discipline as onDragEnd, kept
  // separate since their payloads (dimensions/rotation) don't fit
  // onDragEnd's RoomLocalPoint shape. Optional so every existing
  // FloorPlanViewport usage that doesn't select objects is unaffected.
  onObjectResizeEnd?: (end: ObjectResizeEnd) => void;
  onObjectRotateEnd?: (end: ObjectRotateEnd) => void;
  dragDisabled: boolean;
}) {
  const [container, setContainer] = useState<ContainerSize>({ width: 800, height: 600 });
  const [isPanning, setIsPanning] = useState(false);
  const [panAnchor, setPanAnchor] = useState<{ x: number; y: number; panX: number; panY: number } | null>(null);
  // §11/§18/§20: objectSnapshot is the CANONICAL center/dimensions/yaw
  // captured at gesture START (populated only for objectResizeHandle/
  // objectRotateHandle) — resize must scale from these, never from the
  // live in-progress preview, or repeated small drags would compound.
  const [drag, setDrag] = useState<{
    target: DragTarget;
    startWorldPosition: RoomLocalPoint;
    worldPosition: RoomLocalPoint;
    objectSnapshot?: { center: RoomLocalPoint; dimensions: RoomLocalPoint; yawRadians: number };
  } | null>(null);
  const svgElementRef = useRef<SVGSVGElement | null>(null);
  // Tracks genuine user interaction (pan/zoom/drag start), NOT "has fit run
  // once" — the container can legitimately resize/settle more than once
  // before the user ever touches the viewport (e.g. sibling layout
  // finishing), and each of those should still re-fit. Auto-fit only stops
  // once the user has actually taken control.
  const hasUserInteracted = useRef(false);

  const toScreen = useMemo(() => (p: { x: number; y: number; z: number }) => worldToScreen(p, viewport, container), [viewport, container]);

  function measureContainer(el: SVGSVGElement | null) {
    svgElementRef.current = el;
  }

  // A plain ref callback only fires once at mount with whatever size the
  // SVG happens to have at that instant — which can be smaller than its
  // final laid-out size (flex/grid siblings, like the inspector panel,
  // haven't necessarily settled yet). A ResizeObserver keeps `container`
  // accurate as layout actually settles/changes, which the one-shot
  // auto-fit effect below depends on to frame against the REAL size rather
  // than a transient early one.
  useEffect(() => {
    const el = svgElementRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) {
        setContainer((current) => (current.width === width && current.height === height ? current : { width, height }));
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Fit-to-room framing must use the SVG's REAL measured size, not a
  // guessed constant. Re-fits on every container size change (the
  // ResizeObserver above may fire more than once as layout settles) as
  // long as the user hasn't started interacting with the viewport yet —
  // once they have, their pan/zoom is authoritative and auto-fit stops.
  useEffect(() => {
    if (hasUserInteracted.current) return;
    if (container.width <= 0 || container.height <= 0) return;
    if (!draft?.walls || draft.walls.length === 0) return;
    onViewportChange(fitToRoom(draft.walls, container));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [container, draft]);

  // Every drag-capable element (corner handle, service point) is a direct
  // child of the <svg>, so its own event.currentTarget can find the SVG via
  // a plain DOM traversal — no ref, no waiting for the event to bubble to a
  // separate handler. This keeps the rect read inside the actual
  // event-handler invocation that's already running (satisfying the "no ref
  // access during render" rule) without the closure-staleness hazard of
  // handing off through state across two handlers in the same dispatch.
  function worldFromEvent(event: { clientX: number; clientY: number; currentTarget: Element }): RoomLocalPoint {
    const svg = event.currentTarget.closest("svg");
    const rect = svg?.getBoundingClientRect();
    const screenPoint = rect ? { x: event.clientX - rect.left, y: event.clientY - rect.top } : { x: 0, y: 0 };
    return screenToWorld(screenPoint, viewport, container);
  }

  function requestDrag(
    target: DragTarget,
    event: React.PointerEvent,
    objectSnapshot?: { center: RoomLocalPoint; dimensions: RoomLocalPoint; yawRadians: number },
  ) {
    if (dragDisabled) return;
    const start = worldFromEvent(event);
    setDrag({ target, startWorldPosition: start, worldPosition: start, objectSnapshot });
  }

  function handlePointerDown(event: React.PointerEvent<SVGSVGElement>) {
    if (event.target !== event.currentTarget) return; // clicked an element, not empty canvas — its own onPointerDown (requestDrag) handles that
    hasUserInteracted.current = true;
    setIsPanning(true);
    setPanAnchor({ x: event.clientX, y: event.clientY, panX: viewport.panX, panY: viewport.panY });
  }

  function handlePointerMove(event: React.PointerEvent<SVGSVGElement>) {
    if (drag) {
      setDrag({
        target: drag.target,
        startWorldPosition: drag.startWorldPosition,
        worldPosition: worldFromEvent(event),
        objectSnapshot: drag.objectSnapshot,
      });
      return;
    }
    if (!isPanning || !panAnchor) return;
    const dx = event.clientX - panAnchor.x;
    const dy = event.clientY - panAnchor.y;
    onViewportChange({ ...viewport, panX: panAnchor.panX + dx, panY: panAnchor.panY + dy });
  }

  // Below this world-space displacement, a pointerdown+pointerup on a
  // drag-capable element is treated as a plain click/select, not a drag —
  // otherwise EVERY click on a fixture/service point/constraint/corner/wall
  // (a pointerdown immediately followed by a pointerup at the same
  // location) would spuriously submit a zero-distance move_* operation.
  const CLICK_VS_DRAG_THRESHOLD_METERS = 0.001;

  // §18: minimum canonical footprint dimension a resize handle can produce
  // — matches Spatial3DViewport's own MIN_OBJECT_DIMENSION_METERS-style
  // floor (via dimensionsFromScale there); this 2D path computes width/
  // depth directly rather than through a Three scale factor, so it needs
  // its own equivalent floor rather than importing a Three-adjacent helper
  // into this pure-SVG file.
  const MIN_OBJECT_FOOTPRINT_METERS = 0.05;

  function handlePointerUp() {
    if (drag) {
      const start = drag.startWorldPosition;
      const end = drag.worldPosition;
      const moved = distanceMeters(start, end) > CLICK_VS_DRAG_THRESHOLD_METERS;
      if (moved) {
        if (drag.target.kind === "wallBody") {
          // MoveWallOperation's payload is a DELTA, not an absolute
          // position — compute it here so the reported value matches
          // MoveWallOperation's exact {wallId, delta} wire shape.
          onDragEnd(drag.target, { x: end.x - start.x, y: end.y - start.y, z: end.z - start.z });
        } else if (drag.target.kind === "objectResizeHandle" && drag.objectSnapshot && onObjectResizeEnd) {
          // §18: center-anchored resize — project the released pointer
          // into the object's OWN local (yaw-rotated) frame, then the
          // local X/Z magnitude doubled is the new width/depth. Height
          // (Y) is preserved unchanged — this handle is a top-down 2D
          // footprint control, it has no notion of vertical extent.
          const { center, dimensions: startDimensions, yawRadians } = drag.objectSnapshot;
          const dx = end.x - center.x;
          const dz = end.z - center.z;
          const cos = Math.cos(yawRadians);
          const sin = Math.sin(yawRadians);
          const localX = cos * dx + sin * dz;
          const localZ = -sin * dx + cos * dz;
          onObjectResizeEnd({
            objectId: drag.target.objectId,
            dimensions: {
              x: Math.max(Math.abs(localX) * 2, MIN_OBJECT_FOOTPRINT_METERS),
              y: startDimensions.y,
              z: Math.max(Math.abs(localZ) * 2, MIN_OBJECT_FOOTPRINT_METERS),
            },
          });
        } else if (drag.target.kind === "objectRotateHandle" && drag.objectSnapshot && onObjectRotateEnd) {
          // §20: yaw derived directly from the released pointer's angle
          // around the object's center — not a delta from the drag's
          // start angle, so the object always ends up facing exactly
          // where the handle was released, matching how the 3D rotate
          // gizmo and the numeric Rotation field both work (absolute
          // target orientation, not an incremental nudge).
          const { center } = drag.objectSnapshot;
          const angleRadians = Math.atan2(end.z - center.z, end.x - center.x);
          const yawDegrees = (angleRadians * 180) / Math.PI;
          onObjectRotateEnd({ objectId: drag.target.objectId, rotation: yawDegreesToQuaternion(yawDegrees) });
        } else {
          onDragEnd(drag.target, end);
        }
      }
      setDrag(null);
    }
    setIsPanning(false);
    setPanAnchor(null);
  }

  // M8.5C reversible-editing patch §16: React's onWheel is registered as a
  // passive listener (React 17+), so event.preventDefault() inside it is a
  // silent no-op that also logs "Unable to preventDefault inside passive
  // event listener invocation" — zoom still worked, but the warning is real
  // and the browser could start ignoring the call outright. A native,
  // explicitly non-passive listener on the SVG element is the only way to
  // keep preventDefault effective. Reads the latest viewport/onViewportChange
  // via a ref (rather than in the effect's dependency array) so the listener
  // is attached exactly once per mount, not re-attached on every zoom.
  const wheelStateRef = useRef({ viewport, onViewportChange });
  useLayoutEffect(() => {
    wheelStateRef.current = { viewport, onViewportChange };
  });

  useEffect(() => {
    const el = svgElementRef.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      hasUserInteracted.current = true;
      const { viewport: currentViewport, onViewportChange: setViewport } = wheelStateRef.current;
      const factor = event.deltaY > 0 ? 0.9 : 1.1;
      setViewport({ ...currentViewport, zoom: clampZoom(currentViewport.zoom * factor) });
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const walls = draft?.walls ?? [];
  const openings = draft?.openings ?? [];
  const servicePoints = draft?.servicePoints ?? [];
  const objects = draft?.objects ?? [];
  const fixtures = draft?.fixtures ?? [];
  const constraints = draft?.constraints ?? [];

  return (
    <div className="relative h-full min-h-96 w-full overflow-hidden rounded-lg border border-border/70 bg-background">
      <svg
        role="img"
        aria-label="Floor plan"
        className="h-full w-full touch-none"
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerLeave={handlePointerUp}
        ref={measureContainer}
      >
        {walls.map((wall) => {
          const isDraggingThisWallBody = drag && drag.target.kind === "wallBody" && drag.target.wallId === wall.id;
          // Preview-only translation while dragging the wall's own line —
          // never touches the authoritative draft; the committed delta is
          // computed once, on release, in handlePointerUp.
          const dragDelta =
            isDraggingThisWallBody && drag
              ? {
                  x: drag.worldPosition.x - drag.startWorldPosition.x,
                  y: drag.worldPosition.y - drag.startWorldPosition.y,
                  z: drag.worldPosition.z - drag.startWorldPosition.z,
                }
              : { x: 0, y: 0, z: 0 };
          const start = toScreen({ x: wall.start.x + dragDelta.x, y: wall.start.y + dragDelta.y, z: wall.start.z + dragDelta.z });
          const end = toScreen({ x: wall.end.x + dragDelta.x, y: wall.end.y + dragDelta.y, z: wall.end.z + dragDelta.z });
          const isSelected = selection?.kind === "wall" && selection.id === wall.id;
          return (
            <g key={wall.id}>
              {/* Wide, invisible hit-stroke: the actual click/hover/drag target so the thin visible line underneath is still easy to grab for a move_wall drag. */}
              <line
                data-element-kind="wallBody"
                data-wall-id={wall.id}
                x1={start.x}
                y1={start.y}
                x2={end.x}
                y2={end.y}
                stroke="transparent"
                strokeWidth={16}
                onClick={(e) => {
                  e.stopPropagation();
                  onSelect("wall", wall.id);
                }}
                onPointerDown={(e) => requestDrag({ kind: "wallBody", wallId: wall.id }, e)}
                onPointerEnter={() => onHover(wall.id)}
                onPointerLeave={() => onHover(null)}
                style={{ cursor: dragDisabled ? "default" : "grab" }}
              />
              <line
                data-element-kind="wall"
                data-element-id={wall.id}
                x1={start.x}
                y1={start.y}
                x2={end.x}
                y2={end.y}
                stroke={isSelected ? "var(--color-primary, #2563eb)" : hoverId === wall.id ? "#94a3b8" : "#334155"}
                strokeWidth={isSelected ? 5 : 3}
                strokeLinecap="round"
                pointerEvents="none"
              />
            </g>
          );
        })}

        {walls.flatMap((wall) => {
          const endpoints: { endpoint: "start" | "end"; point: RoomLocalPoint }[] = [
            { endpoint: "start", point: wall.start },
            { endpoint: "end", point: wall.end },
          ];
          return endpoints.map(({ endpoint, point }) => {
            const isDraggingThis =
              drag?.target.kind === "wallEndpoint" && drag.target.wallId === wall.id && drag.target.endpoint === endpoint;
            const displayPoint = isDraggingThis ? drag.worldPosition : point;
            const pos = toScreen(displayPoint);
            return (
              <circle
                key={`${wall.id}-${endpoint}`}
                data-element-kind="wallCorner"
                data-wall-id={wall.id}
                data-endpoint={endpoint}
                cx={pos.x}
                cy={pos.y}
                r={isDraggingThis ? 6 : 4}
                fill={isDraggingThis ? "var(--color-primary, #2563eb)" : "#ffffff"}
                stroke="#334155"
                strokeWidth={1.5}
                onPointerDown={(e) => requestDrag({ kind: "wallEndpoint", wallId: wall.id, endpoint }, e)}
                style={{ cursor: dragDisabled ? "default" : "grab" }}
              />
            );
          });
        })}

        {openings.map((opening) => {
          if (!opening.transform) return null;
          const pos = toScreen(opening.transform.position);
          const isSelected = selection?.kind === "opening" && selection.id === opening.id;
          return (
            <rect
              key={opening.id}
              data-element-kind="opening"
              data-element-id={opening.id}
              x={pos.x - 6}
              y={pos.y - 6}
              width={12}
              height={12}
              fill={isSelected ? "var(--color-primary, #2563eb)" : "#f59e0b"}
              onClick={(e) => {
                e.stopPropagation();
                onSelect("opening", opening.id);
              }}
              onPointerEnter={() => onHover(opening.id)}
              onPointerLeave={() => onHover(null)}
              style={{ cursor: "pointer" }}
            />
          );
        })}

        {servicePoints.map((sp) => {
          const isDraggingThis = drag?.target.kind === "servicePoint" && drag.target.servicePointId === sp.id;
          const displayPoint = isDraggingThis ? drag.worldPosition : sp.position;
          const pos = toScreen(displayPoint);
          const isSelected = selection?.kind === "servicePoint" && selection.id === sp.id;
          return (
            <circle
              key={sp.id}
              data-element-kind="servicePoint"
              data-element-id={sp.id}
              cx={pos.x}
              cy={pos.y}
              r={isSelected || isDraggingThis ? 7 : 5}
              fill={isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#10b981"}
              onClick={(e) => {
                e.stopPropagation();
                onSelect("servicePoint", sp.id);
              }}
              onPointerDown={(e) => requestDrag({ kind: "servicePoint", servicePointId: sp.id }, e)}
              onPointerEnter={() => onHover(sp.id)}
              onPointerLeave={() => onHover(null)}
              style={{ cursor: dragDisabled ? "pointer" : "grab" }}
            />
          );
        })}

        {objects.flatMap((object) => {
          if (!object.transform) return [];
          const isTargetingThis = drag?.target.kind === "object" && drag.target.objectId === object.id;
          // Only enter the visual drag-preview once the pointer has actually
          // moved past the same click-vs-drag threshold handlePointerUp uses
          // to decide whether to commit — a bare pointerdown-then-click
          // (zero displacement) must render identically to a plain select,
          // so a real click's mouseup always lands back on this exact rect.
          const isDraggingThis = isTargetingThis && drag && distanceMeters(drag.startWorldPosition, drag.worldPosition) > CLICK_VS_DRAG_THRESHOLD_METERS;
          const displayPoint = isDraggingThis && drag ? drag.worldPosition : object.transform.position;
          const pos = toScreen(displayPoint);
          const isSelected = selection?.kind === "object" && selection.id === object.id;
          const color = isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#64748b";

          // §17: original fill-less selection/click rect kept EXACTLY as
          // it was — real SVG hit-testing on a fill-less shape only
          // registers the stroke, and existing E2E fixtures/click math
          // depend on this rect's precise corner-stroke behavior. The real
          // footprint/handles below are purely additive, layered ONLY when
          // selected, and never replace this element's hit target.
          const elements = [
            <rect
              key={object.id}
              data-element-kind="object"
              data-element-id={object.id}
              x={pos.x - 8}
              y={pos.y - 8}
              width={16}
              height={16}
              fill="none"
              stroke={color}
              strokeWidth={2}
              onClick={(e) => {
                e.stopPropagation();
                onSelect("object", object.id);
              }}
              onPointerDown={(e) => requestDrag({ kind: "object", objectId: object.id }, e)}
              onPointerEnter={() => onHover(object.id)}
              onPointerLeave={() => onHover(null)}
              style={{ cursor: dragDisabled ? "pointer" : "grab" }}
            />,
          ];

          // §16-§20: real footprint + resize/rotate handles, shown only
          // once selected — object.dimensions may be absent (never
          // measured), in which case there is no real footprint to show
          // and the plain selection rect above is all there is (matching
          // fixture/constraint's own "fixed marker when no dimensions"
          // precedent).
          if (isSelected && object.dimensions) {
            const yawRadians = (quaternionToYawDegrees(object.transform.rotation) * Math.PI) / 180;
            const corners = objectFootprintCorners({ x: displayPoint.x, z: displayPoint.z }, object.dimensions.x, object.dimensions.z, yawRadians);
            const screenCorners = corners.map((c) => toScreen({ x: c.x, y: displayPoint.y, z: c.z }));

            elements.push(
              <polygon
                key={`${object.id}-footprint`}
                points={screenCorners.map((p) => `${p.x},${p.y}`).join(" ")}
                fill="none"
                stroke={color}
                strokeWidth={1.5}
                strokeDasharray="3 2"
                pointerEvents="none"
              />,
            );

            // Resize handle: the corner in the object's own +X/+Z local
            // quadrant (screenCorners[2], matching objectFootprintCorners'
            // fixed local-corner ordering) — dragging it resizes
            // center-anchored per §18, so only one handle is needed.
            const resizeHandle = screenCorners[2];
            if (resizeHandle) {
              elements.push(
                <rect
                  key={`${object.id}-resize-handle`}
                  data-element-kind="objectResizeHandle"
                  data-object-id={object.id}
                  x={resizeHandle.x - 4}
                  y={resizeHandle.y - 4}
                  width={8}
                  height={8}
                  fill={color}
                  stroke="#ffffff"
                  strokeWidth={1}
                  onPointerDown={(e) =>
                    requestDrag({ kind: "objectResizeHandle", objectId: object.id }, e, {
                      center: displayPoint,
                      dimensions: object.dimensions!,
                      yawRadians,
                    })
                  }
                  style={{ cursor: dragDisabled ? "default" : "nwse-resize" }}
                />,
              );
            }

            // Rotation handle: a small circle offset along the object's
            // local +X axis beyond its footprint, so it visibly rotates
            // WITH the object as yaw changes (never a fixed screen-space
            // position).
            const rotateHandleWorld = {
              x: displayPoint.x + (object.dimensions.x / 2 + 0.3) * Math.cos(yawRadians),
              y: displayPoint.y,
              z: displayPoint.z + (object.dimensions.x / 2 + 0.3) * Math.sin(yawRadians),
            };
            const rotateHandle = toScreen(rotateHandleWorld);
            elements.push(
              <circle
                key={`${object.id}-rotate-handle`}
                data-element-kind="objectRotateHandle"
                data-object-id={object.id}
                cx={rotateHandle.x}
                cy={rotateHandle.y}
                r={5}
                fill={color}
                stroke="#ffffff"
                strokeWidth={1}
                onPointerDown={(e) =>
                  requestDrag({ kind: "objectRotateHandle", objectId: object.id }, e, {
                    center: displayPoint,
                    dimensions: object.dimensions!,
                    yawRadians,
                  })
                }
                style={{ cursor: dragDisabled ? "default" : "grab" }}
              />,
            );
          }

          return elements;
        })}

        {fixtures.map((fixture) => {
          const isDraggingThis = drag?.target.kind === "fixture" && drag.target.fixtureId === fixture.id;
          const displayPoint = isDraggingThis ? drag.worldPosition : fixture.transform.position;
          const pos = toScreen(displayPoint);
          const isSelected = selection?.kind === "fixture" && selection.id === fixture.id;
          // A fixed minimum hit/visual size when dimensions is absent —
          // matches the service-point circle's fixed-radius precedent so
          // fixtures without contractor-supplied dimensions are still
          // comfortably clickable/draggable.
          const halfWidth = Math.max((fixture.dimensions?.x ?? 0.4) * viewport.zoom, 10) / 2;
          const halfDepth = Math.max((fixture.dimensions?.z ?? 0.4) * viewport.zoom, 10) / 2;
          return (
            <rect
              key={fixture.id}
              data-element-kind="fixture"
              data-element-id={fixture.id}
              x={pos.x - halfWidth}
              y={pos.y - halfDepth}
              width={halfWidth * 2}
              height={halfDepth * 2}
              fill={isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#8b5cf6"}
              fillOpacity={0.25}
              stroke={isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#8b5cf6"}
              strokeWidth={2}
              onClick={(e) => {
                e.stopPropagation();
                onSelect("fixture", fixture.id);
              }}
              onPointerDown={(e) => requestDrag({ kind: "fixture", fixtureId: fixture.id }, e)}
              onPointerEnter={() => onHover(fixture.id)}
              onPointerLeave={() => onHover(null)}
              style={{ cursor: dragDisabled ? "pointer" : "grab" }}
            />
          );
        })}

        {constraints.map((constraint) => {
          const isDraggingThis = drag?.target.kind === "constraint" && drag.target.constraintId === constraint.id;
          const displayPoint = isDraggingThis ? drag.worldPosition : constraint.transform.position;
          const pos = toScreen(displayPoint);
          const isSelected = selection?.kind === "constraint" && selection.id === constraint.id;
          const halfWidth = Math.max((constraint.dimensions?.x ?? 0.4) * viewport.zoom, 10) / 2;
          const halfDepth = Math.max((constraint.dimensions?.z ?? 0.4) * viewport.zoom, 10) / 2;
          return (
            <rect
              key={constraint.id}
              data-element-kind="constraint"
              data-element-id={constraint.id}
              x={pos.x - halfWidth}
              y={pos.y - halfDepth}
              width={halfWidth * 2}
              height={halfDepth * 2}
              fill={isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#ef4444"}
              fillOpacity={0.2}
              stroke={isSelected || isDraggingThis ? "var(--color-primary, #2563eb)" : "#ef4444"}
              strokeWidth={2}
              strokeDasharray="4 2"
              onClick={(e) => {
                e.stopPropagation();
                onSelect("constraint", constraint.id);
              }}
              onPointerDown={(e) => requestDrag({ kind: "constraint", constraintId: constraint.id }, e)}
              onPointerEnter={() => onHover(constraint.id)}
              onPointerLeave={() => onHover(null)}
              style={{ cursor: dragDisabled ? "pointer" : "grab" }}
            />
          );
        })}
      </svg>
    </div>
  );
}

function distanceMeters(a: RoomLocalPoint, b: RoomLocalPoint): number {
  return Math.sqrt((a.x - b.x) ** 2 + (a.y - b.y) ** 2 + (a.z - b.z) ** 2);
}
