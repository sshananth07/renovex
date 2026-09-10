"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  clearVisualAssetOperation,
  reclassifyFixtureOperation,
  reclassifyOpeningOperation,
  removeConstraintOperation,
  removeFixtureOperation,
  removeOpeningOperation,
  resizeFixtureOperation,
  resizeObjectOperation,
  resizeOpeningOperation,
  rotateObjectOperation,
  setDoorHingeOperation,
  setDoorLeafCountOperation,
  setDoorOpenDirectionOperation,
  setDoorSwingOperation,
  setWallThicknessOperation,
} from "../operations";
import { resolveVisualAsset } from "../three/assets/resolveAsset";
import { quaternionToYawDegrees, yawDegreesToQuaternion } from "../three/sceneProjection";
import type { EditOperationPayload, Selection, VisualAssetTargetKind } from "../types";
import type { RoomDraft } from "../../api";

const OPENING_KINDS = ["door", "window", "archway", "other"] as const;
const OPENING_PROFILES = ["rectangle", "arch"] as const;
const FIXTURE_CATEGORIES = ["ac", "boiler", "built_in_cabinetry", "wall_fixture", "electrical_panel", "other"] as const;
const DOOR_HINGES = ["left", "right"] as const;
const DOOR_SWINGS = ["inward", "outward"] as const;

// Reads the selected element straight from the authoritative RoomDraft in
// cache — no duplicated copy in editor state. Every editable control here
// calls onSubmit with the exact canonical operation payload; SpatialEditor
// owns building the PendingEdit and submitting it. Some element kinds have
// deliberately narrower editing surfaces than others because that's what
// RP4A's canonical vocabulary actually supports — constraints, for
// instance, have no reclassify/resize operation, only add/move/remove — so
// this component does not invent controls for operations that don't exist.
export function ElementInspector({
  draft,
  selection,
  onSubmit,
  disabled,
}: {
  draft: RoomDraft | null | undefined;
  selection: Selection;
  onSubmit: (operation: EditOperationPayload) => void;
  disabled: boolean;
}) {
  if (!selection) {
    return (
      <div className="surface-card p-4 text-sm text-muted-foreground">Select an element on the plan to inspect it.</div>
    );
  }

  if (selection.kind === "wall") {
    const wall = draft?.walls?.find((w) => w.id === selection.id);
    if (!wall) return <MissingElement />;
    return (
      <div className="surface-card grid gap-3 p-4 text-sm">
        <Header kind="Wall" id={wall.id} />
        <NumberField
          label="Thickness (m)"
          value={wall.thickness}
          disabled={disabled}
          onCommit={(thickness) =>
            onSubmit(setWallThicknessOperation({ wallId: wall.id, thickness, status: "estimated" }))
          }
        />
        <Field label="Thickness status" value={wall.thicknessStatus} />
        <Field label="Provenance" value={wall.provenance.provider} />
        <p className="mt-1 text-xs text-muted-foreground">Drag a corner to move just that corner, or drag the wall itself to move the whole wall.</p>
      </div>
    );
  }

  if (selection.kind === "opening") {
    const opening = draft?.openings?.find((o) => o.id === selection.id);
    if (!opening) return <MissingElement />;
    const isDoor = opening.kind === "door";
    return (
      <div className="surface-card grid gap-3 p-4 text-sm">
        <Header kind="Opening" id={opening.id} />
        <SelectField
          label="Kind"
          value={opening.kind}
          options={OPENING_KINDS}
          disabled={disabled}
          onChange={(kind) => onSubmit(reclassifyOpeningOperation({ openingId: opening.id, kind, profile: opening.profile }))}
        />
        <SelectField
          label="Profile"
          value={opening.profile}
          options={OPENING_PROFILES}
          disabled={disabled}
          onChange={(profile) => onSubmit(reclassifyOpeningOperation({ openingId: opening.id, kind: opening.kind, profile }))}
        />
        <div className="grid grid-cols-2 gap-2">
          <NumberField
            label="Width (m)"
            value={opening.width}
            disabled={disabled}
            onCommit={(width) => onSubmit(resizeOpeningOperation({ openingId: opening.id, width, height: opening.height ?? 2 }))}
          />
          <NumberField
            label="Height (m)"
            value={opening.height}
            disabled={disabled}
            onCommit={(height) => onSubmit(resizeOpeningOperation({ openingId: opening.id, width: opening.width ?? 0.9, height }))}
          />
        </div>
        {isDoor && (
          <div className="grid gap-3 border-t pt-3">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Door</p>
            <SelectField
              label="Leaf count"
              value={String(opening.door?.leafCount ?? 1)}
              options={["1", "2"]}
              disabled={disabled}
              onChange={(v) => onSubmit(setDoorLeafCountOperation({ openingId: opening.id, leafCount: Number(v) }))}
            />
            <SelectField
              label="Hinge"
              value={opening.door?.hinge ?? "left"}
              options={DOOR_HINGES}
              disabled={disabled}
              onChange={(hinge) => onSubmit(setDoorHingeOperation({ openingId: opening.id, hinge }))}
            />
            <SelectField
              label="Swing"
              value={opening.door?.swing ?? "inward"}
              options={DOOR_SWINGS}
              disabled={disabled}
              onChange={(swing) => onSubmit(setDoorSwingOperation({ openingId: opening.id, swing }))}
            />
            <TextField
              label="Open direction"
              value={opening.door?.openDirection ?? ""}
              disabled={disabled}
              onCommit={(openDirection) => onSubmit(setDoorOpenDirectionOperation({ openingId: opening.id, openDirection }))}
            />
          </div>
        )}
        <Button
          size="sm"
          variant="outline"
          className="mt-1 text-destructive"
          disabled={disabled}
          onClick={() => onSubmit(removeOpeningOperation({ openingId: opening.id }))}
        >
          Remove opening
        </Button>
      </div>
    );
  }

  if (selection.kind === "servicePoint") {
    const sp = draft?.servicePoints?.find((s) => s.id === selection.id);
    if (!sp) return <MissingElement />;
    return (
      <div className="surface-card grid gap-2 p-4 text-sm">
        <Header kind="Service point" id={sp.id} />
        <Field label="Kind" value={sp.kind} />
        <Field label="Created by" value={sp.createdBy} />
        {sp.parentWallId && <Field label="Parent wall" value={sp.parentWallId} />}
        <p className="mt-1 text-xs text-muted-foreground">Drag it on the plan to move it.</p>
        <Button
          size="sm"
          variant="outline"
          className="mt-1 text-destructive"
          disabled={disabled}
          onClick={() => onSubmit({ kind: "remove_service_point", payload: { servicePointId: sp.id } })}
        >
          Remove service point
        </Button>
      </div>
    );
  }

  if (selection.kind === "object") {
    const object = draft?.objects?.find((o) => o.id === selection.id);
    if (!object) return <MissingElement />;
    return (
      <div className="surface-card grid gap-3 p-4 text-sm">
        <Header kind="Object" id={object.id} />
        <Field label="Category" value={object.category} />
        <div className="grid grid-cols-3 gap-2">
          <NumberField
            label="W (m)"
            value={object.dimensions?.x}
            disabled={disabled}
            onCommit={(x) =>
              onSubmit(
                resizeObjectOperation({
                  objectId: object.id,
                  dimensions: { x, y: object.dimensions?.y ?? 0.5, z: object.dimensions?.z ?? 0.5 },
                }),
              )
            }
          />
          <NumberField
            label="H (m)"
            value={object.dimensions?.y}
            disabled={disabled}
            onCommit={(y) =>
              onSubmit(
                resizeObjectOperation({
                  objectId: object.id,
                  dimensions: { x: object.dimensions?.x ?? 0.5, y, z: object.dimensions?.z ?? 0.5 },
                }),
              )
            }
          />
          <NumberField
            label="D (m)"
            value={object.dimensions?.z}
            disabled={disabled}
            onCommit={(z) =>
              onSubmit(
                resizeObjectOperation({
                  objectId: object.id,
                  dimensions: { x: object.dimensions?.x ?? 0.5, y: object.dimensions?.y ?? 0.5, z },
                }),
              )
            }
          />
        </div>
        <AngleField
          label="Rotation (yaw °)"
          value={quaternionToYawDegrees(object.transform.rotation)}
          disabled={disabled}
          onCommit={(degrees) =>
            onSubmit(rotateObjectOperation({ objectId: object.id, rotation: yawDegreesToQuaternion(degrees) }))
          }
        />
        <Field label="Provenance" value={object.provenance.provider} />
        <VisualAssetStatus
          elementKind="object"
          targetId={object.id}
          category={object.category}
          visualAsset={object.visualAsset}
          onSubmit={onSubmit}
          disabled={disabled}
        />
        <p className="mt-1 text-xs text-muted-foreground">Drag it on the plan to move it.</p>
      </div>
    );
  }

  if (selection.kind === "fixture") {
    const fixture = draft?.fixtures?.find((f) => f.id === selection.id);
    if (!fixture) return <MissingElement />;
    return (
      <div className="surface-card grid gap-3 p-4 text-sm">
        <Header kind="Fixture" id={fixture.id} />
        <SelectField
          label="Category"
          value={fixture.category}
          options={FIXTURE_CATEGORIES}
          disabled={disabled}
          onChange={(category) => onSubmit(reclassifyFixtureOperation({ fixtureId: fixture.id, category }))}
        />
        <div className="grid grid-cols-3 gap-2">
          <NumberField
            label="W (m)"
            value={fixture.dimensions?.x}
            disabled={disabled}
            onCommit={(x) =>
              onSubmit(
                resizeFixtureOperation({
                  fixtureId: fixture.id,
                  dimensions: { x, y: fixture.dimensions?.y ?? 0.5, z: fixture.dimensions?.z ?? 0.5 },
                }),
              )
            }
          />
          <NumberField
            label="H (m)"
            value={fixture.dimensions?.y}
            disabled={disabled}
            onCommit={(y) =>
              onSubmit(
                resizeFixtureOperation({
                  fixtureId: fixture.id,
                  dimensions: { x: fixture.dimensions?.x ?? 0.5, y, z: fixture.dimensions?.z ?? 0.5 },
                }),
              )
            }
          />
          <NumberField
            label="D (m)"
            value={fixture.dimensions?.z}
            disabled={disabled}
            onCommit={(z) =>
              onSubmit(
                resizeFixtureOperation({
                  fixtureId: fixture.id,
                  dimensions: { x: fixture.dimensions?.x ?? 0.5, y: fixture.dimensions?.y ?? 0.5, z },
                }),
              )
            }
          />
        </div>
        <Field label="Created by" value={fixture.createdBy} />
        {fixture.parentWallId && <Field label="Parent wall" value={fixture.parentWallId} />}
        <VisualAssetStatus
          elementKind="fixture"
          targetId={fixture.id}
          category={fixture.category}
          visualAsset={fixture.visualAsset}
          onSubmit={onSubmit}
          disabled={disabled}
        />
        <p className="mt-1 text-xs text-muted-foreground">Drag it on the plan to move it.</p>
        <Button
          size="sm"
          variant="outline"
          className="mt-1 text-destructive"
          disabled={disabled}
          onClick={() => onSubmit(removeFixtureOperation({ fixtureId: fixture.id }))}
        >
          Remove fixture
        </Button>
      </div>
    );
  }

  const constraint = draft?.constraints?.find((c) => c.id === selection.id);
  if (!constraint) return <MissingElement />;
  return (
    <div className="surface-card grid gap-2 p-4 text-sm">
      <Header kind="Constraint" id={constraint.id} />
      {/* Kind is read-only: RP4A's canonical vocabulary has no
          reclassify_constraint operation — only add/move/remove — so this
          intentionally does not offer a kind selector. */}
      <Field label="Kind" value={constraint.kind} />
      <Field label="Created by" value={constraint.createdBy} />
      <p className="mt-1 text-xs text-muted-foreground">Drag it on the plan to move it.</p>
      <Button
        size="sm"
        variant="outline"
        className="mt-1 text-destructive"
        disabled={disabled}
        onClick={() => onSubmit(removeConstraintOperation({ constraintId: constraint.id }))}
      >
        Remove constraint
      </Button>
    </div>
  );
}

function Header({ kind, id }: { kind: string; id: string }) {
  return (
    <div className="border-b pb-2">
      <p className="font-heading text-sm font-semibold">{kind}</p>
      <p className="text-xs text-muted-foreground">{id}</p>
    </div>
  );
}

// Shared visual-asset status line for fixture AND object (RP4D §31): no
// asset-picker/marketplace UI, no raw asset-ID/version/storage-key entry —
// only a restrained status readout plus, when an explicit canonical
// binding exists, a "Use default visual" action that submits
// clear_visual_asset through the SAME onSubmit()/PendingEdit/RP4B mutation
// path every other operation in this component uses. Deliberately does
// NOT resolve or display the actual loaded asset's success/failure state
// (that is a Three.js-runtime concern the 3D viewport owns) — this only
// reflects RoomDraft's own canonical binding truth.
function VisualAssetStatus({
  elementKind,
  targetId,
  category,
  visualAsset,
  onSubmit,
  disabled,
}: {
  elementKind: VisualAssetTargetKind;
  targetId: string;
  category: string;
  visualAsset: { assetId: string; version: number } | undefined;
  onSubmit: (operation: EditOperationPayload) => void;
  disabled: boolean;
}) {
  const resolution = resolveVisualAsset({ elementKind, category, visualAsset });
  const statusText =
    resolution.tier === "authorized"
      ? `Custom asset v${resolution.ref.version}`
      : resolution.tier === "builtin"
        ? "Category default"
        : "Procedural fallback";

  return (
    <div className="flex items-center justify-between gap-2 border-t pt-3">
      <Field label="Visual" value={statusText} />
      {resolution.tier === "authorized" && (
        <Button
          size="sm"
          variant="ghost"
          disabled={disabled}
          onClick={() => onSubmit(clearVisualAssetOperation({ targetKind: elementKind, targetId }))}
        >
          Use default visual
        </Button>
      )}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className="text-sm text-foreground">{value}</span>
    </div>
  );
}

// A numeric field that commits only on blur/Enter, not on every keystroke —
// avoids submitting a canonical operation per character typed. Local draft
// text is UI-only state; the committed value always comes from the
// authoritative draft via `value`, matching the "no second mutable client
// RoomDraft" invariant.
function NumberField({
  label,
  value,
  disabled,
  onCommit,
}: {
  label: string;
  value: number | undefined;
  disabled: boolean;
  onCommit: (value: number) => void;
}) {
  const [draftValue, setDraftValue] = useState<string | null>(null);
  const displayValue = draftValue ?? (value !== undefined ? String(value) : "");

  function commit() {
    const parsed = Number(draftValue);
    if (draftValue !== null && draftValue !== "" && Number.isFinite(parsed) && parsed > 0) {
      onCommit(parsed);
    }
    setDraftValue(null);
  }

  return (
    <label className="grid gap-1">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
      <input
        type="number"
        step="0.01"
        min="0"
        className="rounded-md border border-input bg-background px-2 py-1.5 text-sm"
        value={displayValue}
        disabled={disabled}
        onChange={(e) => setDraftValue(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.currentTarget.blur();
          }
        }}
      />
    </label>
  );
}

// T1B: a rotation angle is not a physical measurement — 0 and negative
// values are perfectly valid (0° is "no rotation", -90° is a real distinct
// orientation from 270°) — so this cannot reuse NumberField's `parsed > 0`
// guard, which exists specifically to reject a zero/negative width, height,
// or thickness. Same commit-on-blur/Enter discipline as NumberField
// otherwise.
function AngleField({
  label,
  value,
  disabled,
  onCommit,
}: {
  label: string;
  value: number | undefined;
  disabled: boolean;
  onCommit: (value: number) => void;
}) {
  const [draftValue, setDraftValue] = useState<string | null>(null);
  const displayValue = draftValue ?? (value !== undefined ? String(value) : "");

  function commit() {
    const parsed = Number(draftValue);
    if (draftValue !== null && draftValue !== "" && Number.isFinite(parsed)) {
      onCommit(parsed);
    }
    setDraftValue(null);
  }

  return (
    <label className="grid gap-1">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
      <input
        type="number"
        step="1"
        className="rounded-md border border-input bg-background px-2 py-1.5 text-sm"
        value={displayValue}
        disabled={disabled}
        onChange={(e) => setDraftValue(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.currentTarget.blur();
          }
        }}
      />
    </label>
  );
}

function TextField({
  label,
  value,
  disabled,
  onCommit,
}: {
  label: string;
  value: string;
  disabled: boolean;
  onCommit: (value: string) => void;
}) {
  const [draftValue, setDraftValue] = useState<string | null>(null);
  const displayValue = draftValue ?? value;

  function commit() {
    if (draftValue !== null && draftValue !== value) {
      onCommit(draftValue);
    }
    setDraftValue(null);
  }

  return (
    <label className="grid gap-1">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
      <input
        type="text"
        className="rounded-md border border-input bg-background px-2 py-1.5 text-sm"
        value={displayValue}
        disabled={disabled}
        onChange={(e) => setDraftValue(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.currentTarget.blur();
          }
        }}
      />
    </label>
  );
}

function SelectField<T extends string>({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly T[];
  disabled: boolean;
  onChange: (value: T) => void;
}) {
  return (
    <label className="grid gap-1">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
      <select
        className="rounded-md border border-input bg-background px-2 py-1.5 text-sm"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value as T)}
      >
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}

function MissingElement() {
  return (
    <div className="surface-card p-4 text-sm text-muted-foreground">
      This element no longer exists in the current draft.
    </div>
  );
}
