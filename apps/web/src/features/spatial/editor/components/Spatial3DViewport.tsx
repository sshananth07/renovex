"use client";

import { Suspense, useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { Canvas, invalidate, type ThreeEvent } from "@react-three/fiber";
import { CameraControls as DreiCameraControls, GizmoHelper, GizmoViewport, Grid, TransformControls } from "@react-three/drei";
import type { CameraControls as CameraControlsImpl } from "@react-three/drei";
import * as THREE from "three";
import { Button } from "@/components/ui/button";
import {
  computeEyeLevelPreset,
  computeFitBounds,
  computeOverviewPreset,
  computeTopPreset,
  type CameraPose as CameraPoseTuple,
} from "../three/cameraMath";
import {
  dimensionsFromScale,
  projectScene,
  quaternionToYawDegrees,
  yawDegreesToQuaternion,
  type AppearanceSpec,
  type ConstraintVisualSpec,
  type FixtureVisualSpec,
  type ObjectVisualSpec,
  type OpeningVisualSpec,
  type ServicePointVisualSpec,
  type WallMeshSpec,
} from "../three/sceneProjection";
import { resolveVisualAsset } from "../three/assets/resolveAsset";
import type { VisualResolution } from "../three/assets/types";
import type { CameraPose, ElementKind, RoomLocalPoint, RoomLocalQuaternion, Selection } from "../types";
import type { RoomDraft } from "../../api";
import type { ComparisonMode, ConceptRenderState } from "../../design-studio/types";
import { CurrentConceptToggle } from "../../design-studio/components/CurrentConceptToggle";
import AssetErrorBoundary from "./AssetErrorBoundary";
import AssetFallback from "./AssetFallback";
import AuthorizedVisualAsset from "./AuthorizedVisualAsset";
import VisualAsset from "./VisualAsset";

// Renders the resolved VisualResolution for a fixture/object into the
// (AssetErrorBoundary > Suspense > loader) subtree, or the plain
// procedural box — the ONE place FixtureMesh/ObjectMesh's dispatch logic
// lives, so the two call sites can never drift. assetKey includes the
// tier prefix (per RP4D's plan) so a descriptor's identity for
// error-boundary-reset purposes is never ambiguous between an authorized
// and a builtin asset that happen to share an id/version number space.
function ResolvedVisual({
  resolution,
  dimensions,
  color,
  appearance,
}: {
  resolution: VisualResolution;
  dimensions: RoomLocalPoint;
  color: string;
  appearance?: AppearanceSpec;
}) {
  if (resolution.tier === "procedural") {
    return <AssetFallback dimensions={dimensions} color={color} appearance={appearance} />;
  }

  const fallback = resolution.tier === "authorized" && resolution.builtinFallback ? (
    <VisualAsset descriptor={resolution.builtinFallback} dimensions={dimensions} appearance={appearance} />
  ) : (
    <AssetFallback dimensions={dimensions} color={color} appearance={appearance} />
  );

  const assetKey =
    resolution.tier === "authorized"
      ? `authorized:${resolution.ref.assetId}:${resolution.ref.version}`
      : `builtin:${resolution.descriptor.id}:${resolution.descriptor.version}`;

  return (
    <AssetErrorBoundary assetKey={assetKey} fallback={fallback}>
      <Suspense fallback={fallback}>
        {resolution.tier === "authorized" ? (
          <AuthorizedVisualAsset
            assetRef={resolution.ref}
            builtinFallback={resolution.builtinFallback}
            dimensions={dimensions}
            color={color}
            appearance={appearance}
          />
        ) : (
          <VisualAsset descriptor={resolution.descriptor} dimensions={dimensions} appearance={appearance} />
        )}
      </Suspense>
    </AssetErrorBoundary>
  );
}

// Distance below which a completed drag is treated as a plain click, not a
// real move — mirrors FloorPlanViewport.tsx's CLICK_VS_DRAG_THRESHOLD_METERS
// exactly (RP4C2 fixed a real bug where a plain click synthesized a
// spurious zero-distance move_* submission; 3D must not reintroduce it).
const CLICK_VS_DRAG_THRESHOLD_METERS = 0.001;

const SELECTED_COLOR = "#c2410c"; // matches --primary/--ring: oklch(0.57 0.18 39)
const HOVER_COLOR = "#fdba74";
const WALL_COLOR = "#94a3b8";
const FIXTURE_COLOR = "#8b5cf6";
const CONSTRAINT_COLOR = "#ef4444";
const OBJECT_COLOR = "#64748b";
const OPENING_COLOR = "#38bdf8";
const SERVICE_POINT_COLOR = "#22c55e";

// Closure patch §10: objectRotate/objectResize added alongside the
// existing move-only variants — object/fixture/servicePoint (translate)
// keep their exact original shape/callers; only objects gain rotate/scale
// direct manipulation (§13/§14), since RoomDraftObject is the only kind
// this patch's ObjectMesh gizmo mode-switching targets.
export type Spatial3DDragEnd =
  | { kind: "fixture"; fixtureId: string; position: RoomLocalPoint }
  | { kind: "servicePoint"; servicePointId: string; position: RoomLocalPoint }
  | { kind: "object"; objectId: string; position: RoomLocalPoint }
  | { kind: "objectRotate"; objectId: string; rotation: RoomLocalQuaternion }
  | { kind: "objectResize"; objectId: string; dimensions: RoomLocalPoint };

// §9: which of TransformControls' three modes the selected object's gizmo
// currently exposes — viewport-owned UI-only state (not part of Selection/
// RoomDraft), matching CameraPose's own "plain UI state, not authoritative"
// precedent. Resets implicitly whenever selection changes, since a
// non-object selection has no mode toolbar to begin with.
export type ObjectTransformMode = "translate" | "rotate" | "scale";

export type Spatial3DViewportProps = {
  draft: RoomDraft;
  selection: Selection;
  onSelect: (kind: ElementKind, id: string) => void;
  onClearSelection: () => void;
  onHover: (id: string | null) => void;
  hoverId: string | null;
  onDragEnd: (drag: Spatial3DDragEnd) => void;
  dragDisabled: boolean;
  cameraPose: CameraPose | null;
  onCameraPoseSettled: (pose: CameraPose) => void;
  reducedMotion: boolean;
  // RP4E2/E3: the studio's derived candidate override for the currently
  // selected object/fixture (a pure projection — see conceptProjection.ts's
  // doc comment) plus which of Current/Concept the canvas should currently
  // show. Defaults ("none"/"current") when the caller passes nothing, so
  // every existing non-studio usage of this component is unaffected. The
  // CurrentConceptToggle overlay renders only when a concept preview
  // actually exists (kind !== "none") — never a disabled toggle with
  // nothing to compare against.
  conceptPreview?: ConceptRenderState;
  comparisonMode?: ComparisonMode;
  onComparisonModeChange?: (mode: ComparisonMode) => void;
};

// The Canvas/R3F mount point. RP4C3 originally scoped "three"/"@react-three/*"
// imports to this single file; RP4C4 widens that boundary to this entire
// components/ directory (VisualAsset.tsx and friends also need "three"/
// "@react-three/drei" for GLTF/USD loading) — the invariant that actually
// matters is preserved: ../three/sceneProjection.ts and ../three/assets/*
// (the domain-projection and asset-metadata/normalization layers) remain
// pure, jsdom-testable, and never import "three" or "@react-three/*".
// This file's job is wiring those pure results into an actual Three.js
// scene.
export default function Spatial3DViewport(props: Spatial3DViewportProps) {
  const { draft, cameraPose, onCameraPoseSettled, reducedMotion, conceptPreview, comparisonMode, onComparisonModeChange } = props;
  const projected = useMemo(() => projectScene(draft), [draft]);
  // Concept mode substitutes ONLY the exact selected target's spec with the
  // candidate's transform/dimensions/appearance/asset — every other
  // element, and Current mode entirely, render the authoritative RoomDraft
  // unchanged (plan: "Candidate geometry replaces only the exact selected
  // RoomDraft identity"). withConceptOverride is a pure function so this
  // stays trivially memoizable and matches sceneProjection.ts's own
  // "pure projection" contract rather than mutating `projected` in place.
  const scene = useMemo(
    () => (comparisonMode === "concept" && conceptPreview ? withConceptOverride(projected, conceptPreview) : projected),
    [projected, comparisonMode, conceptPreview],
  );
  const fitBounds = useMemo(() => computeFitBounds(scene.walls), [scene.walls]);
  const controlsRef = useRef<CameraControlsImpl | null>(null);
  const [objectTransformMode, setObjectTransformMode] = useState<ObjectTransformMode>("translate");
  const selectedObjectId = props.selection?.kind === "object" ? props.selection.id : null;
  // Selecting a DIFFERENT object (or deselecting) always starts fresh at
  // translate — a leftover "rotate"/"scale" mode from a previous selection
  // would otherwise attach the wrong gizmo the instant a new object is
  // clicked, with no visible mode-switch action to explain why.
  useEffect(() => {
    setObjectTransformMode("translate");
  }, [selectedObjectId]);
  const [hintDismissed, setHintDismissed] = useState(() => {
    // Lazy initializer, not an effect: this component is only ever mounted
    // client-side (dynamically imported with ssr:false), so there is no
    // hydration mismatch to guard against, and reading synchronously avoids
    // an extra render pass.
    try {
      return window.localStorage.getItem("renovex.spatial3d.hintDismissed") === "true";
    } catch {
      // localStorage can throw in restricted/private-browsing contexts —
      // fall back to showing the hint once per session rather than crashing.
      return false;
    }
  });

  const dismissHint = useCallback(() => {
    setHintDismissed(true);
    try {
      window.localStorage.setItem("renovex.spatial3d.hintDismissed", "true");
    } catch {
      // Best-effort persistence only.
    }
  }, []);

  // Camera/gizmo interaction rule (hard requirement): orbit disables the
  // instant a TransformControls drag starts, and re-enables the instant it
  // ends. No intermediate React state drives this — it toggles the live
  // CameraControls ref's `enabled` property directly.
  const setCameraEnabled = useCallback((enabled: boolean) => {
    const controls = controlsRef.current;
    if (controls) controls.enabled = enabled;
  }, []);

  // Restore the last SETTLED camera pose on mount, or fit-to-room if this is
  // the first time 3D has been shown; write the settled pose back on
  // unmount so switching to 2D and back to 3D resumes correctly. Both the
  // setup and cleanup close over the SAME captured `controls` reference
  // (read once, at effect-body time) rather than re-reading controlsRef in
  // the cleanup, since the ref could in principle have changed by then.
  // Mount/unmount-only by design — camera pose is never written on any
  // other cadence (see CameraPose's doc comment in types.ts).
  useEffect(() => {
    const controls = controlsRef.current;
    if (!controls) return;
    if (cameraPose) {
      controls.setLookAt(
        cameraPose.position.x,
        cameraPose.position.y,
        cameraPose.position.z,
        cameraPose.target.x,
        cameraPose.target.y,
        cameraPose.target.z,
        false,
      );
    } else {
      applyPreset(controls, computeOverviewPreset(fitBounds), false);
    }
    return () => {
      const position = controls.getPosition(new THREE.Vector3());
      const target = controls.getTarget(new THREE.Vector3());
      onCameraPoseSettled({
        position: { x: position.x, y: position.y, z: position.z },
        target: { x: target.x, y: target.y, z: target.z },
      });
    };
    // Mount/unmount-only: subsequent draft/cameraPose changes must not yank
    // the camera around while the user is exploring.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRest = useCallback(() => {
    const controls = controlsRef.current;
    if (!controls) return;
    const position = controls.getPosition(new THREE.Vector3());
    const target = controls.getTarget(new THREE.Vector3());
    onCameraPoseSettled({
      position: { x: position.x, y: position.y, z: position.z },
      target: { x: target.x, y: target.y, z: target.z },
    });
  }, [onCameraPoseSettled]);

  const fitRoom = useCallback(() => {
    const controls = controlsRef.current;
    if (!controls) return;
    // fitToBox reorients the camera to look straight at the box's nearest
    // axis-aligned face — confirmed live: from an angled Overview-style
    // position it snaps the camera to a flat, face-on view that reads as
    // "stuck inside a wall," not a genuine "fit the whole room" behavior.
    // fitToSphere instead keeps the CURRENT viewing direction and only
    // adjusts distance/target, which is what "Fit Room" should mean while
    // exploring — never yanking the camera to a different angle.
    const sphere = new THREE.Sphere(
      new THREE.Vector3(fitBounds.center.x, fitBounds.center.y + 1, fitBounds.center.z),
      Math.max(fitBounds.radius, 1) * 1.15,
    );
    controls.fitToSphere(sphere, !reducedMotion);
  }, [fitBounds, reducedMotion]);

  const goToPreset = useCallback(
    (preset: CameraPoseTuple) => {
      applyPreset(controlsRef.current, preset, !reducedMotion);
    },
    [reducedMotion],
  );

  const focusSelected = useCallback(() => {
    const controls = controlsRef.current;
    if (!controls || !props.selection) return;
    const target = findSelectedPosition(scene, props.selection);
    if (!target) return;
    const distance = Math.max(fitBounds.radius * 0.4, 2);
    controls.setLookAt(
      target.x + distance,
      target.y + distance * 0.6,
      target.z + distance,
      target.x,
      target.y,
      target.z,
      !reducedMotion,
    );
  }, [fitBounds.radius, props.selection, reducedMotion, scene]);

  return (
    <div className="relative h-full min-h-96 w-full overflow-hidden rounded-lg border bg-muted/20">
      <div role="img" aria-label="3D room view" className="h-full w-full">
        <Canvas
          frameloop="demand"
          camera={{ position: [8, 6, 8], fov: 50 }}
          onPointerMissed={() => props.onClearSelection()}
        >
          <ambientLight intensity={0.7} />
          <directionalLight position={[6, 10, 4]} intensity={0.9} />
          <DreiCameraControls
            ref={controlsRef}
            onRest={handleRest}
            onSleep={handleRest}
            minDistance={0.5}
            maxDistance={Math.max(fitBounds.radius * 6, 20)}
          />
          <Grid
            args={[40, 40]}
            cellColor="#cbd5e1"
            sectionColor="#94a3b8"
            fadeDistance={30}
            infiniteGrid
            position={[0, -0.001, 0]}
          />
          <GizmoHelper alignment="bottom-right" margin={[64, 64]}>
            <GizmoViewport />
          </GizmoHelper>

          <SceneContents {...props} scene={scene} setCameraEnabled={setCameraEnabled} objectTransformMode={objectTransformMode} />
        </Canvas>
      </div>

      <div className="pointer-events-none absolute inset-0 flex flex-col justify-between p-3">
        <div className="pointer-events-auto flex flex-wrap items-center gap-1.5">
          <Button size="sm" variant="secondary" onClick={fitRoom}>
            Fit room
          </Button>
          <Button size="sm" variant="secondary" onClick={() => goToPreset(computeOverviewPreset(fitBounds))}>
            Overview
          </Button>
          <Button size="sm" variant="secondary" onClick={() => goToPreset(computeTopPreset(fitBounds))}>
            Top
          </Button>
          <Button size="sm" variant="secondary" onClick={() => goToPreset(computeEyeLevelPreset(fitBounds))}>
            Eye level
          </Button>
          {props.selection && (
            <Button size="sm" variant="secondary" onClick={focusSelected}>
              Focus selected
            </Button>
          )}
        </div>

        {/* §9: mode toolbar only for a transformable object — this patch's
            gizmo mode-switching targets RoomDraftObject exclusively. */}
        {selectedObjectId && (
          <div className="pointer-events-auto flex flex-wrap items-center gap-1.5 self-start">
            <Button size="sm" variant={objectTransformMode === "translate" ? "default" : "secondary"} onClick={() => setObjectTransformMode("translate")} disabled={props.dragDisabled}>
              Move
            </Button>
            <Button size="sm" variant={objectTransformMode === "rotate" ? "default" : "secondary"} onClick={() => setObjectTransformMode("rotate")} disabled={props.dragDisabled}>
              Rotate
            </Button>
            <Button size="sm" variant={objectTransformMode === "scale" ? "default" : "secondary"} onClick={() => setObjectTransformMode("scale")} disabled={props.dragDisabled}>
              Resize
            </Button>
          </div>
        )}

        {!hintDismissed && (
          <div className="pointer-events-auto max-w-xs rounded-lg border bg-popover/95 p-3 text-xs text-muted-foreground shadow-sm backdrop-blur-sm">
            <p>Drag to orbit</p>
            <p>Scroll to zoom</p>
            <p>Right-drag to pan</p>
            <p>Click an element to inspect it</p>
            <Button size="xs" variant="ghost" className="mt-1.5 h-auto p-0 text-xs underline" onClick={dismissHint}>
              Got it
            </Button>
          </div>
        )}
      </div>

      {conceptPreview && conceptPreview.kind !== "none" && onComparisonModeChange && (
        <div className="pointer-events-none absolute inset-x-0 bottom-4 flex justify-center">
          <CurrentConceptToggle
            mode={comparisonMode ?? "current"}
            onChange={onComparisonModeChange}
            conceptDisabled={conceptPreview.kind === "geometry_pending" || conceptPreview.kind === "stale"}
          />
        </div>
      )}
    </div>
  );
}

// Substitutes the exact selected object/fixture's spec with the concept
// candidate's transform/dimensions/appearance/asset, leaving every other
// element (and every other spec field on the target itself — category,
// createdBy, parentWallId, etc.) untouched. `none`/`stale` previews are a
// no-op (nothing to render — the caller's own studio UI communicates
// those states). geometry_pending/material_preview/concept_ready all
// apply the same override shape: whichever fields the attempt's candidate
// actually carries win, everything else falls back to the authoritative
// RoomDraft spec so a material-only concept never accidentally clears an
// object's existing dimensions.
// Exported for direct unit testing (see Spatial3DViewport.test.tsx) — this
// function itself touches no "three"/"@react-three/*" API, it only
// transforms plain ProjectedScene data, so it's testable the same way
// sceneProjection.ts's own pure functions are, without needing jsdom to see
// inside a mounted Canvas.
export function withConceptOverride(scene: ReturnType<typeof projectScene>, preview: ConceptRenderState): ReturnType<typeof projectScene> {
  if (preview.kind === "none" || preview.kind === "stale") return scene;
  const { target, transform, dimensions, appearance } = preview;
  const assetRef = preview.kind === "concept_ready" ? preview.assetRef : undefined;
  if (!target) return scene;

  if (target.kind === "object") {
    return {
      ...scene,
      objects: scene.objects.map((o) =>
        o.id === target.id
          ? { ...o, position: transform.position, rotation: transform.rotation, dimensions: dimensions ?? o.dimensions, appearance: appearance ?? o.appearance, visualAsset: assetRef ?? o.visualAsset }
          : o,
      ),
    };
  }
  if (target.kind === "fixture") {
    return {
      ...scene,
      fixtures: scene.fixtures.map((f) =>
        f.id === target.id
          ? { ...f, position: transform.position, rotation: transform.rotation, dimensions: dimensions ?? f.dimensions, appearance: appearance ?? f.appearance, visualAsset: assetRef ?? f.visualAsset }
          : f,
      ),
    };
  }
  return scene;
}

function applyPreset(controls: CameraControlsImpl | null, preset: CameraPoseTuple, enableTransition: boolean) {
  if (!controls) return;
  controls.setLookAt(
    preset.position.x,
    preset.position.y,
    preset.position.z,
    preset.target.x,
    preset.target.y,
    preset.target.z,
    enableTransition,
  );
}

function findSelectedPosition(scene: ReturnType<typeof projectScene>, selection: Selection): RoomLocalPoint | null {
  if (!selection) return null;
  switch (selection.kind) {
    case "wall":
      return scene.walls.find((w) => w.id === selection.id)?.center ?? null;
    case "opening":
      return scene.openings.find((o) => o.id === selection.id)?.position ?? null;
    case "object":
      return scene.objects.find((o) => o.id === selection.id)?.position ?? null;
    case "fixture":
      return scene.fixtures.find((f) => f.id === selection.id)?.position ?? null;
    case "servicePoint":
      return scene.servicePoints.find((s) => s.id === selection.id)?.position ?? null;
    case "constraint":
      return scene.constraints.find((c) => c.id === selection.id)?.position ?? null;
  }
}

// Split out so drag-vs-orbit wiring has a clean home distinct from the
// camera/canvas chrome above.
function SceneContents(
  props: Spatial3DViewportProps & {
    scene: ReturnType<typeof projectScene>;
    setCameraEnabled: (enabled: boolean) => void;
    objectTransformMode: ObjectTransformMode;
  },
) {
  const { scene, selection, hoverId, onSelect, onHover, onDragEnd, dragDisabled, draft, setCameraEnabled, objectTransformMode } = props;

  return (
    <group>
      {scene.walls.map((wall) => (
        <WallMesh
          key={wall.id}
          wall={wall}
          selected={selection?.kind === "wall" && selection.id === wall.id}
          hovered={hoverId === wall.id}
          onSelect={() => onSelect("wall", wall.id)}
          onHover={(v) => onHover(v ? wall.id : null)}
        />
      ))}
      {scene.openings.map((opening) => (
        <OpeningMesh
          key={opening.id}
          opening={opening}
          selected={selection?.kind === "opening" && selection.id === opening.id}
          hovered={hoverId === opening.id}
          onSelect={() => onSelect("opening", opening.id)}
          onHover={(v) => onHover(v ? opening.id : null)}
        />
      ))}
      {scene.objects.map((object) => {
        const original = draft.objects?.find((o) => o.id === object.id);
        return (
          <ObjectMesh
            key={object.id}
            object={object}
            selected={selection?.kind === "object" && selection.id === object.id}
            hovered={hoverId === object.id}
            draggable={!dragDisabled}
            mode={objectTransformMode}
            setCameraEnabled={setCameraEnabled}
            onSelect={() => onSelect("object", object.id)}
            onHover={(v) => onHover(v ? object.id : null)}
            onMoveEnd={(finalPosition) => {
              if (!original) return;
              const distance = Math.hypot(finalPosition.x - object.position.x, finalPosition.z - object.position.z);
              if (distance <= CLICK_VS_DRAG_THRESHOLD_METERS) return;
              // Same discipline as FixtureMesh/ServicePointMarker below:
              // reports the raw dragged position only, SpatialEditor.tsx
              // builds the updated RoomLocalTransform via withUpdatedPosition.
              onDragEnd({ kind: "object", objectId: object.id, position: finalPosition });
            }}
            onRotateEnd={(rotation) => onDragEnd({ kind: "objectRotate", objectId: object.id, rotation })}
            onResizeEnd={(dimensions) => onDragEnd({ kind: "objectResize", objectId: object.id, dimensions })}
          />
        );
      })}
      {scene.fixtures.map((fixture) => {
        const draggable = !dragDisabled && !fixture.parentWallId;
        const original = draft.fixtures?.find((f) => f.id === fixture.id);
        return (
          <FixtureMesh
            key={fixture.id}
            fixture={fixture}
            selected={selection?.kind === "fixture" && selection.id === fixture.id}
            hovered={hoverId === fixture.id}
            draggable={draggable}
            setCameraEnabled={setCameraEnabled}
            onSelect={() => onSelect("fixture", fixture.id)}
            onHover={(v) => onHover(v ? fixture.id : null)}
            onDragEnd={(finalPosition) => {
              if (!original) return;
              const distance = Math.hypot(finalPosition.x - fixture.position.x, finalPosition.z - fixture.position.z);
              if (distance <= CLICK_VS_DRAG_THRESHOLD_METERS) return;
              // Reports the raw dragged position only; SpatialEditor.tsx's
              // drag handler is the single place that builds the updated
              // RoomLocalTransform via withUpdatedPosition (preserving
              // rotation), matching the 2D drag handler's existing pattern
              // exactly and avoiding two call sites doing the same thing.
              onDragEnd({ kind: "fixture", fixtureId: fixture.id, position: finalPosition });
            }}
          />
        );
      })}
      {scene.servicePoints.map((sp) => {
        const draggable = !dragDisabled && !sp.parentWallId;
        return (
          <ServicePointMarker
            key={sp.id}
            servicePoint={sp}
            selected={selection?.kind === "servicePoint" && selection.id === sp.id}
            hovered={hoverId === sp.id}
            draggable={draggable}
            setCameraEnabled={setCameraEnabled}
            onSelect={() => onSelect("servicePoint", sp.id)}
            onHover={(v) => onHover(v ? sp.id : null)}
            onDragEnd={(finalPosition) => {
              const distance = Math.hypot(finalPosition.x - sp.position.x, finalPosition.z - sp.position.z);
              if (distance <= CLICK_VS_DRAG_THRESHOLD_METERS) return;
              onDragEnd({ kind: "servicePoint", servicePointId: sp.id, position: finalPosition });
            }}
          />
        );
      })}
      {scene.constraints.map((constraint) => (
        <ConstraintMesh
          key={constraint.id}
          constraint={constraint}
          selected={selection?.kind === "constraint" && selection.id === constraint.id}
          hovered={hoverId === constraint.id}
          onSelect={() => onSelect("constraint", constraint.id)}
          onHover={(v) => onHover(v ? constraint.id : null)}
        />
      ))}
    </group>
  );
}

function stop(e: ThreeEvent<MouseEvent | PointerEvent>) {
  e.stopPropagation();
}

function WallMesh({
  wall,
  selected,
  hovered,
  onSelect,
  onHover,
}: {
  wall: WallMeshSpec;
  selected: boolean;
  hovered: boolean;
  onSelect: () => void;
  onHover: (v: boolean) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : WALL_COLOR;
  return (
    <mesh
      position={[wall.center.x, wall.height / 2, wall.center.z]}
      rotation={[0, wall.yaw, 0]}
      onClick={(e) => {
        stop(e);
        onSelect();
      }}
      onPointerOver={(e) => {
        stop(e);
        onHover(true);
      }}
      onPointerOut={() => onHover(false)}
    >
      <boxGeometry args={[wall.length, wall.height, wall.thickness]} />
      <meshStandardMaterial color={color} emissive={selected ? SELECTED_COLOR : "#000000"} emissiveIntensity={selected ? 0.15 : 0} />
    </mesh>
  );
}

// Openings render as a distinct frame + semi-transparent panel attached to
// their parent wall's plane — NOT a boolean cut in the wall geometry (plan
// §"Wall/opening rendering": no wall-boolean/CSG infrastructure exists in
// this codebase, so this communicates the opening's existence/dimensions
// without pretending the wall has an actual cut hole).
function OpeningMesh({
  opening,
  selected,
  hovered,
  onSelect,
  onHover,
}: {
  opening: OpeningVisualSpec;
  selected: boolean;
  hovered: boolean;
  onSelect: () => void;
  onHover: (v: boolean) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : OPENING_COLOR;
  const euler = useMemo(() => quaternionToEuler(opening.rotation), [opening.rotation]);
  const edges = useMemo(() => new THREE.BoxGeometry(opening.width, opening.height, 0.06), [opening.width, opening.height]);
  return (
    <group position={[opening.position.x, opening.position.y + opening.height / 2, opening.position.z]} rotation={euler}>
      <mesh
        onClick={(e) => {
          stop(e);
          onSelect();
        }}
        onPointerOver={(e) => {
          stop(e);
          onHover(true);
        }}
        onPointerOut={() => onHover(false)}
      >
        <boxGeometry args={[opening.width, opening.height, 0.06]} />
        <meshStandardMaterial color={color} transparent opacity={0.35} />
      </mesh>
      <lineSegments>
        <edgesGeometry args={[edges]} />
        <lineBasicMaterial color={color} />
      </lineSegments>
      {opening.kind === "door" && opening.door && (
        <mesh position={[opening.door.hinge === "right" ? opening.width / 2 : -opening.width / 2, 0, 0.05]}>
          <planeGeometry args={[opening.width * 0.9, opening.height * 0.9]} />
          <meshStandardMaterial color={color} transparent opacity={0.15} side={THREE.DoubleSide} />
        </mesh>
      )}
    </group>
  );
}

function ObjectMesh({
  object,
  selected,
  hovered,
  draggable,
  mode,
  setCameraEnabled,
  onSelect,
  onHover,
  onMoveEnd,
  onRotateEnd,
  onResizeEnd,
}: {
  object: ObjectVisualSpec;
  selected: boolean;
  hovered: boolean;
  draggable: boolean;
  mode: ObjectTransformMode;
  setCameraEnabled: (enabled: boolean) => void;
  onSelect: () => void;
  onHover: (v: boolean) => void;
  onMoveEnd: (position: RoomLocalPoint) => void;
  onRotateEnd: (rotation: RoomLocalQuaternion) => void;
  onResizeEnd: (dimensions: RoomLocalPoint) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : OBJECT_COLOR;
  const euler = useMemo(() => quaternionToEuler(object.rotation), [object.rotation]);
  const groupRef = useRef<THREE.Group>(null);
  const resolution = useMemo(
    () => resolveVisualAsset({ elementKind: "object", category: object.category, visualAsset: object.visualAsset }),
    [object.category, object.visualAsset],
  );
  const hasAsset = resolution.tier !== "procedural";

  // §11: snapshotted at transform START (onMouseDown), never derived from
  // the live/already-transformed group — resize in particular must scale
  // from the CANONICAL pre-gesture dimensions, or repeated small drags
  // would compound scale-on-scale instead of each resolving to one clean
  // canonical value. object.dimensions is read fresh here because a new
  // gesture only ever starts after the previous one's authoritative result
  // has already re-rendered this mesh with the new canonical dimensions.
  const transformStartRef = useRef<{ dimensions: RoomLocalPoint }>({ dimensions: object.dimensions });

  const mesh = (
    <group
      // CanonicalElementRoot — TransformControls (below) always targets
      // THIS group via groupRef, matching FixtureMesh's exact convention:
      // bottom-anchored origin (position sits at the object's floor
      // position, geometry extends up by dimensions.y), whether procedural
      // or a loaded asset.
      ref={groupRef}
      position={[object.position.x, object.position.y + object.dimensions.y / 2, object.position.z]}
      rotation={euler}
      onClick={(e: ThreeEvent<MouseEvent>) => {
        stop(e);
        onSelect();
      }}
      onPointerOver={(e: ThreeEvent<PointerEvent>) => {
        stop(e);
        onHover(true);
      }}
      onPointerOut={() => onHover(false)}
    >
      {hasAsset ? (
        <ResolvedVisual resolution={resolution} dimensions={object.dimensions} color={color} appearance={object.appearance} />
      ) : (
        <mesh>
          <boxGeometry args={[object.dimensions.x, object.dimensions.y, object.dimensions.z]} />
          <meshStandardMaterial color={color} wireframe={!selected} transparent opacity={selected ? 0.8 : 0.5} />
        </mesh>
      )}
      {hasAsset && (selected || hovered) && (
        // Selection/hover overlay for a real-asset element — a SIBLING of
        // the VisualAsset/NormalizationRoot subtree above, never nested
        // inside it: the overlay is sized directly from the element's
        // authoritative `dimensions` (construction truth), so nesting it
        // inside the loaded asset's normalization scale would apply that
        // corrective scale a second time. This mesh's own material is
        // always fresh (a new JSX element per render), never the loaded
        // asset's shared, cached material — selecting one instance must
        // never bleed a color change onto every other instance sharing
        // that cached material.
        <mesh>
          <boxGeometry args={[object.dimensions.x, object.dimensions.y, object.dimensions.z]} />
          <meshBasicMaterial color={color} wireframe transparent opacity={0.9} />
        </mesh>
      )}
    </group>
  );

  if (!selected || !draggable) return mesh;

  return (
    <TransformControls
      object={groupRef as RefObject<THREE.Group>}
      mode={mode}
      // §12: translate stays floor-plan-only (no vertical drag). §13: yaw-
      // only rotation — only Y is exposed, matching the existing numeric
      // Rotation inspector field's own convention exactly (no separate
      // rotation representation invented here). §14: scale shows all three
      // handles; the resulting Three scale is converted to canonical
      // dimensions and never itself persisted (see onResizeEnd below).
      showX={mode !== "rotate"}
      showY={mode === "rotate"}
      showZ={mode !== "rotate"}
      onMouseDown={() => {
        transformStartRef.current = { dimensions: object.dimensions };
        setCameraEnabled(false);
      }}
      onMouseUp={() => {
        setCameraEnabled(true);
        const world = groupRef.current;
        if (!world) return;
        if (mode === "translate") {
          onMoveEnd({ x: world.position.x, y: object.position.y, z: world.position.z });
        } else if (mode === "rotate") {
          // Canonicalized to yaw-only, exactly like the numeric inspector's
          // AngleField commit path — never a second rotation convention.
          const yaw = quaternionToYawDegrees({ x: world.quaternion.x, y: world.quaternion.y, z: world.quaternion.z, w: world.quaternion.w });
          onRotateEnd(yawDegreesToQuaternion(yaw));
        } else {
          const nextDimensions = dimensionsFromScale(transformStartRef.current.dimensions, { x: world.scale.x, y: world.scale.y, z: world.scale.z });
          // Scale is transient Three.js UI state ONLY — reset immediately
          // so the next render (driven by the authoritative dimensions
          // this resize submits) starts from a clean 1/1/1, never
          // compounding with a leftover non-unit scale.
          world.scale.set(1, 1, 1);
          onResizeEnd(nextDimensions);
        }
        invalidate();
      }}
    >
      {mesh}
    </TransformControls>
  );
}

function FixtureMesh({
  fixture,
  selected,
  hovered,
  draggable,
  setCameraEnabled,
  onSelect,
  onHover,
  onDragEnd,
}: {
  fixture: FixtureVisualSpec;
  selected: boolean;
  hovered: boolean;
  draggable: boolean;
  setCameraEnabled: (enabled: boolean) => void;
  onSelect: () => void;
  onHover: (v: boolean) => void;
  onDragEnd: (position: RoomLocalPoint) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : FIXTURE_COLOR;
  const groupRef = useRef<THREE.Group>(null);
  const resolution = useMemo(
    () => resolveVisualAsset({ elementKind: "fixture", category: fixture.category, visualAsset: fixture.visualAsset }),
    [fixture.category, fixture.visualAsset],
  );
  const hasAsset = resolution.tier !== "procedural";

  const mesh = (
    <group
      // CanonicalElementRoot — TransformControls (below) always targets
      // THIS group via groupRef, never a nested asset child, regardless
      // of whether this fixture renders procedurally or via a loaded
      // asset. Its position convention (bottom-anchored: origin sits at
      // the fixture's floor position, geometry extends up by
      // dimensions.y) is unchanged from RP4C3 — a real asset's
      // NormalizationRoot (inside VisualAsset) is responsible for
      // aligning its own pivot to this same convention.
      ref={groupRef}
      position={[fixture.position.x, fixture.position.y + fixture.dimensions.y / 2, fixture.position.z]}
      onClick={(e: ThreeEvent<MouseEvent>) => {
        stop(e);
        onSelect();
      }}
      onPointerOver={(e: ThreeEvent<PointerEvent>) => {
        stop(e);
        onHover(true);
      }}
      onPointerOut={() => onHover(false)}
    >
      {hasAsset ? (
        <ResolvedVisual resolution={resolution} dimensions={fixture.dimensions} color={color} appearance={fixture.appearance} />
      ) : (
        <mesh>
          <boxGeometry args={[fixture.dimensions.x, fixture.dimensions.y, fixture.dimensions.z]} />
          <meshStandardMaterial color={color} transparent opacity={0.85} />
        </mesh>
      )}
      {hasAsset && (selected || hovered) && (
        // Sibling of the VisualAsset subtree, not nested inside it — see
        // the identical note in ObjectMesh above. Matches the procedural
        // box's own local-space placement exactly: centered at the
        // group's local origin (the group itself is already offset in
        // world space so this box's bottom lands at the fixture's floor
        // position — same convention the procedural mesh above uses).
        <mesh>
          <boxGeometry args={[fixture.dimensions.x, fixture.dimensions.y, fixture.dimensions.z]} />
          <meshBasicMaterial color={color} wireframe transparent opacity={0.9} />
        </mesh>
      )}
    </group>
  );

  if (!selected || !draggable) return mesh;

  return (
    <TransformControls
      object={groupRef as RefObject<THREE.Group>}
      mode="translate"
      showY={false}
      onMouseDown={() => setCameraEnabled(false)}
      onMouseUp={() => {
        setCameraEnabled(true);
        const world = groupRef.current;
        if (!world) return;
        onDragEnd({ x: world.position.x, y: fixture.position.y, z: world.position.z });
        invalidate();
      }}
    >
      {mesh}
    </TransformControls>
  );
}

function ServicePointMarker({
  servicePoint,
  selected,
  hovered,
  draggable,
  setCameraEnabled,
  onSelect,
  onHover,
  onDragEnd,
}: {
  servicePoint: ServicePointVisualSpec;
  selected: boolean;
  hovered: boolean;
  draggable: boolean;
  setCameraEnabled: (enabled: boolean) => void;
  onSelect: () => void;
  onHover: (v: boolean) => void;
  onDragEnd: (position: RoomLocalPoint) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : SERVICE_POINT_COLOR;
  const groupRef = useRef<THREE.Group>(null);

  const marker = (
    <group
      ref={groupRef}
      position={[servicePoint.position.x, servicePoint.position.y + 0.15, servicePoint.position.z]}
      onClick={(e: ThreeEvent<MouseEvent>) => {
        stop(e);
        onSelect();
      }}
      onPointerOver={(e: ThreeEvent<PointerEvent>) => {
        stop(e);
        onHover(true);
      }}
      onPointerOut={() => onHover(false)}
    >
      <mesh>
        <sphereGeometry args={[selected || hovered ? 0.09 : 0.07, 16, 16]} />
        <meshStandardMaterial color={color} emissive={color} emissiveIntensity={0.4} />
      </mesh>
    </group>
  );

  if (!selected || !draggable) return marker;

  return (
    <TransformControls
      object={groupRef as RefObject<THREE.Group>}
      mode="translate"
      showY={false}
      onMouseDown={() => setCameraEnabled(false)}
      onMouseUp={() => {
        setCameraEnabled(true);
        const world = groupRef.current;
        if (!world) return;
        onDragEnd({ x: world.position.x, y: servicePoint.position.y, z: world.position.z });
        invalidate();
      }}
    >
      {marker}
    </TransformControls>
  );
}

function ConstraintMesh({
  constraint,
  selected,
  hovered,
  onSelect,
  onHover,
}: {
  constraint: ConstraintVisualSpec;
  selected: boolean;
  hovered: boolean;
  onSelect: () => void;
  onHover: (v: boolean) => void;
}) {
  const color = selected ? SELECTED_COLOR : hovered ? HOVER_COLOR : CONSTRAINT_COLOR;
  const euler = useMemo(() => quaternionToEuler(constraint.rotation), [constraint.rotation]);
  const box = useMemo(
    () => new THREE.BoxGeometry(constraint.dimensions.x, constraint.dimensions.y, constraint.dimensions.z),
    [constraint.dimensions.x, constraint.dimensions.y, constraint.dimensions.z],
  );
  return (
    <group
      position={[constraint.position.x, constraint.position.y + constraint.dimensions.y / 2, constraint.position.z]}
      rotation={euler}
      onClick={(e) => {
        stop(e);
        onSelect();
      }}
      onPointerOver={(e) => {
        stop(e);
        onHover(true);
      }}
      onPointerOut={() => onHover(false)}
    >
      <lineSegments>
        <edgesGeometry args={[box]} />
        <lineBasicMaterial color={color} />
      </lineSegments>
    </group>
  );
}

function quaternionToEuler(q: { x: number; y: number; z: number; w: number }): [number, number, number] {
  const euler = new THREE.Euler().setFromQuaternion(new THREE.Quaternion(q.x, q.y, q.z, q.w));
  return [euler.x, euler.y, euler.z];
}
