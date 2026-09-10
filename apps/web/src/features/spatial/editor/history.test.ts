import { describe, expect, it } from "vitest";
import { buildCommittedHistoryEntry, buildInverseEdit } from "./history";
import {
  addConstraintOperation,
  addFixtureOperation,
  addServicePointOperation,
  assignVisualAssetOperation,
  clearVisualAssetOperation,
  moveConstraintOperation,
  moveCornerOperation,
  moveFixtureOperation,
  moveObjectOperation,
  moveServicePointOperation,
  moveWallOperation,
  reclassifyFixtureOperation,
  reclassifyOpeningOperation,
  removeConstraintOperation,
  removeFixtureOperation,
  removeObjectOperation,
  removeOpeningOperation,
  removeServicePointOperation,
  resizeFixtureOperation,
  resizeObjectOperation,
  resizeOpeningOperation,
  restoreElementOperation,
  rotateObjectOperation,
  setDoorHingeOperation,
  setDoorLeafCountOperation,
  setDoorOpenDirectionOperation,
  setDoorSwingOperation,
  setWallThicknessOperation,
} from "./operations";
import type { RoomDraft } from "../api";

function baseDraft(overrides: Partial<RoomDraft> = {}): RoomDraft {
  return {
    id: "draft_1",
    captureId: "capture_1",
    revision: 3,
    canResetToScan: true,
    walls: [],
    openings: [],
    objects: [],
    fixtures: [],
    servicePoints: [],
    constraints: [],
    ...overrides,
  } as RoomDraft;
}

describe("buildInverseEdit — captures the BEFORE state, never the after", () => {
  it("move_object inverts to the object's previous position", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const forward = moveObjectOperation({ objectId: "object_1", position: { x: 5, y: 0, z: 5 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(moveObjectOperation({ objectId: "object_1", position: { x: 1, y: 0, z: 1 } }));
  });

  it("rotate_object inverts to the object's previous rotation", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0.7071, z: 0, w: 0.7071 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const forward = rotateObjectOperation({ objectId: "object_1", rotation: { x: 0, y: 0, z: 0, w: 1 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(rotateObjectOperation({ objectId: "object_1", rotation: { x: 0, y: 0.7071, z: 0, w: 0.7071 } }));
  });

  it("resize_object inverts to the object's previous dimensions", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 2, y: 0.8, z: 1 }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const forward = resizeObjectOperation({ objectId: "object_1", dimensions: { x: 3, y: 1, z: 1.5 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(resizeObjectOperation({ objectId: "object_1", dimensions: { x: 2, y: 0.8, z: 1 } }));
  });

  it("move_wall (delta-based) inverts to the negated delta", () => {
    const before = baseDraft();
    const forward = moveWallOperation({ wallId: "wall_1", delta: { x: 2, y: 0, z: -1 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(moveWallOperation({ wallId: "wall_1", delta: { x: -2, y: 0, z: 1 } }));
  });

  it("set_wall_thickness inverts to the previous thickness AND status", () => {
    const before = baseDraft({
      walls: [{ id: "wall_1", start: { x: 0, y: 0, z: 0 }, end: { x: 4, y: 0, z: 0 }, thickness: 0.1, thicknessStatus: "unconfirmed", provenance: { provider: "roomplan", sourceElementIdentifier: "w1" } }],
    });
    const forward = setWallThicknessOperation({ wallId: "wall_1", thickness: 0.2, status: "estimated" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(setWallThicknessOperation({ wallId: "wall_1", thickness: 0.1, status: "unconfirmed" }));
  });

  it("resize_opening inverts to previous width/height", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", width: 0.9, height: 2.1, transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" } }],
    });
    const forward = resizeOpeningOperation({ openingId: "opening_1", width: 1.2, height: 2.4 });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(resizeOpeningOperation({ openingId: "opening_1", width: 0.9, height: 2.1 }));
  });

  it("reclassify_opening inverts to previous kind/profile", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" } }],
    });
    const forward = reclassifyOpeningOperation({ openingId: "opening_1", kind: "window", profile: "arch" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(reclassifyOpeningOperation({ openingId: "opening_1", kind: "door", profile: "rectangle" }));
  });

  it("move_fixture inverts to the previous transform", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" }],
    });
    const forward = moveFixtureOperation({ fixtureId: "fixture_1", transform: { position: { x: 9, y: 0, z: 9 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(moveFixtureOperation({ fixtureId: "fixture_1", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } }));
  });

  it("resize_fixture inverts to previous dimensions", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.6, y: 0.8, z: 0.4 }, createdBy: "contractor" }],
    });
    const forward = resizeFixtureOperation({ fixtureId: "fixture_1", dimensions: { x: 1, y: 1, z: 1 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(resizeFixtureOperation({ fixtureId: "fixture_1", dimensions: { x: 0.6, y: 0.8, z: 0.4 } }));
  });

  it("move_service_point inverts to previous position", () => {
    const before = baseDraft({
      servicePoints: [{ id: "sp_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 }, createdBy: "contractor" }],
    });
    const forward = moveServicePointOperation({ servicePointId: "sp_1", position: { x: 5, y: 0, z: 5 } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(moveServicePointOperation({ servicePointId: "sp_1", position: { x: 1, y: 0, z: 1 } }));
  });

  it("move_constraint inverts to previous transform", () => {
    const before = baseDraft({
      constraints: [{ id: "constraint_1", kind: "column", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" }],
    });
    const forward = moveConstraintOperation({ constraintId: "constraint_1", transform: { position: { x: 9, y: 0, z: 9 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(moveConstraintOperation({ constraintId: "constraint_1", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } } }));
  });

  it("remove_object inverts to a restore_element carrying the exact removed object record", () => {
    const removedObject = { id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan" as const, sourceElementIdentifier: "o1" } };
    const before = baseDraft({ objects: [removedObject] });
    const forward = removeObjectOperation({ objectId: "object_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual({ kind: "restore_element", payload: { kind: "object", object: removedObject } });
  });

  it("remove_opening inverts to a restore_element carrying the exact removed opening record", () => {
    const removedOpening = { id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan" as const, sourceElementIdentifier: "op1" } };
    const before = baseDraft({ openings: [removedOpening] });
    const forward = removeOpeningOperation({ openingId: "opening_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual({ kind: "restore_element", payload: { kind: "opening", opening: removedOpening } });
  });

  it("remove_fixture (always contractor-created) inverts to re-adding the same fixture via add_fixture with its exact id", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.6, y: 0.8, z: 0.4 }, createdBy: "contractor" }],
    });
    const forward = removeFixtureOperation({ fixtureId: "fixture_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual({
      kind: "add_fixture",
      payload: { id: "fixture_1", category: "boiler", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.6, y: 0.8, z: 0.4 } },
    });
  });

  it("remove_service_point inverts to re-adding the same service point via add_service_point with its exact id", () => {
    const before = baseDraft({
      servicePoints: [{ id: "sp_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 }, createdBy: "contractor" }],
    });
    const forward = removeServicePointOperation({ servicePointId: "sp_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual({ kind: "add_service_point", payload: { id: "sp_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 } } });
  });

  it("returns null when the target element cannot be found in the BEFORE draft (no safe inverse)", () => {
    const before = baseDraft();
    const forward = moveObjectOperation({ objectId: "object_missing", position: { x: 1, y: 0, z: 1 } });
    expect(buildInverseEdit(forward, before)).toBeNull();
  });

  // --- closure patch: completing coverage for every user-reachable edit ---

  it("move_corner inverts to the exact previous corner position for every coincident endpoint", () => {
    const before = baseDraft({
      walls: [
        { id: "wall_1", start: { x: 0, y: 0, z: 0 }, end: { x: 4, y: 0, z: 0 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w1" }, thicknessStatus: "estimated" },
        { id: "wall_2", start: { x: 4, y: 0, z: 0 }, end: { x: 4, y: 0, z: 4 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w2" }, thicknessStatus: "estimated" },
      ],
    });
    const forward = moveCornerOperation({
      endpoints: [
        { wallId: "wall_1", endpoint: "end" },
        { wallId: "wall_2", endpoint: "start" },
      ],
      newPosition: { x: 9, y: 0, z: 9 },
    });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(
      moveCornerOperation({
        endpoints: [
          { wallId: "wall_1", endpoint: "end" },
          { wallId: "wall_2", endpoint: "start" },
        ],
        newPosition: { x: 4, y: 0, z: 0 },
      }),
    );
  });

  it("move_corner returns null when the endpoints' current positions are not coincident (no single safe previous corner)", () => {
    const before = baseDraft({
      walls: [
        { id: "wall_1", start: { x: 0, y: 0, z: 0 }, end: { x: 4, y: 0, z: 0 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w1" }, thicknessStatus: "estimated" },
        { id: "wall_2", start: { x: 9, y: 0, z: 9 }, end: { x: 4, y: 0, z: 4 }, provenance: { provider: "roomplan", sourceElementIdentifier: "w2" }, thicknessStatus: "estimated" },
      ],
    });
    const forward = moveCornerOperation({
      endpoints: [
        { wallId: "wall_1", endpoint: "end" },
        { wallId: "wall_2", endpoint: "start" },
      ],
      newPosition: { x: 1, y: 0, z: 1 },
    });
    expect(buildInverseEdit(forward, before)).toBeNull();
  });

  it("set_door_leaf_count inverts to previous leaf count", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" }, door: { leafCount: 1, hinge: "left", swing: "inward", openDirection: "north" } }],
    });
    const forward = setDoorLeafCountOperation({ openingId: "opening_1", leafCount: 2 });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(setDoorLeafCountOperation({ openingId: "opening_1", leafCount: 1 }));
  });

  it("set_door_hinge inverts to previous hinge", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" }, door: { leafCount: 1, hinge: "left", swing: "inward", openDirection: "north" } }],
    });
    const forward = setDoorHingeOperation({ openingId: "opening_1", hinge: "right" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(setDoorHingeOperation({ openingId: "opening_1", hinge: "left" }));
  });

  it("set_door_swing inverts to previous swing", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" }, door: { leafCount: 1, hinge: "left", swing: "inward", openDirection: "north" } }],
    });
    const forward = setDoorSwingOperation({ openingId: "opening_1", swing: "outward" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(setDoorSwingOperation({ openingId: "opening_1", swing: "inward" }));
  });

  it("set_door_open_direction inverts to previous open direction", () => {
    const before = baseDraft({
      openings: [{ id: "opening_1", kind: "door", profile: "rectangle", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "op1" }, door: { leafCount: 1, hinge: "left", swing: "inward", openDirection: "north" } }],
    });
    const forward = setDoorOpenDirectionOperation({ openingId: "opening_1", openDirection: "south" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(setDoorOpenDirectionOperation({ openingId: "opening_1", openDirection: "north" }));
  });

  it("reclassify_fixture inverts to previous category", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" }],
    });
    const forward = reclassifyFixtureOperation({ fixtureId: "fixture_1", category: "ac" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(reclassifyFixtureOperation({ fixtureId: "fixture_1", category: "boiler" }));
  });

  it("clear_visual_asset inverts to re-assigning the previous binding when one existed", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" }, visualAsset: { assetId: "asset_1", version: 2 } }],
    });
    const forward = clearVisualAssetOperation({ targetKind: "object", targetId: "object_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(assignVisualAssetOperation({ targetKind: "object", targetId: "object_1", assetId: "asset_1", version: 2 }));
  });

  it("clear_visual_asset returns null when there was no previous binding to restore", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const forward = clearVisualAssetOperation({ targetKind: "object", targetId: "object_1" });
    expect(buildInverseEdit(forward, before)).toBeNull();
  });

  it("assign_visual_asset inverts to clear_visual_asset when there was no previous binding", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" }],
    });
    const forward = assignVisualAssetOperation({ targetKind: "fixture", targetId: "fixture_1", assetId: "asset_2", version: 1 });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(clearVisualAssetOperation({ targetKind: "fixture", targetId: "fixture_1" }));
  });

  it("assign_visual_asset inverts to re-assigning the previous binding when one already existed", () => {
    const before = baseDraft({
      fixtures: [{ id: "fixture_1", category: "boiler", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor", visualAsset: { assetId: "asset_old", version: 1 } }],
    });
    const forward = assignVisualAssetOperation({ targetKind: "fixture", targetId: "fixture_1", assetId: "asset_new", version: 3 });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual(assignVisualAssetOperation({ targetKind: "fixture", targetId: "fixture_1", assetId: "asset_old", version: 1 }));
  });

  it("remove_constraint (always contractor-created) inverts to re-adding the same constraint via add_constraint with its exact id", () => {
    const before = baseDraft({
      constraints: [{ id: "constraint_1", kind: "column", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.3, y: 2, z: 0.3 }, createdBy: "contractor" }],
    });
    const forward = removeConstraintOperation({ constraintId: "constraint_1" });
    const inverse = buildInverseEdit(forward, before);
    expect(inverse).toEqual({
      kind: "add_constraint",
      payload: { id: "constraint_1", kind: "column", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, dimensions: { x: 0.3, y: 2, z: 0.3 } },
    });
  });
});

describe("buildCommittedHistoryEntry — before+after model for add_* (server-assigned identity)", () => {
  it("add_fixture: Undo removes the exact authoritative id, Redo restores the exact same id (never a new one)", () => {
    const before = baseDraft({ fixtures: [{ id: "fixture_existing", category: "boiler", transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" }] });
    const createdFixture = { id: "fixture_new_123", category: "other", transform: { position: { x: 2, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" as const };
    const after = baseDraft({ fixtures: [...(before.fixtures ?? []), createdFixture] });
    const forward = addFixtureOperation({ category: "other", transform: createdFixture.transform });

    const entry = buildCommittedHistoryEntry({ operation: forward, before, after });
    expect(entry).not.toBeNull();
    expect(entry!.undo).toEqual({ kind: "edit", operation: removeFixtureOperation({ fixtureId: "fixture_new_123" }) });
    expect(entry!.redo).toEqual({ kind: "edit", operation: restoreElementOperation({ kind: "fixture", fixture: createdFixture }) });
  });

  it("add_service_point: Undo removes the exact authoritative id, Redo restores the exact same id", () => {
    const before = baseDraft();
    const createdSp = { id: "sp_new_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 }, createdBy: "contractor" as const };
    const after = baseDraft({ servicePoints: [createdSp] });
    const forward = addServicePointOperation({ kind: "plumbing", position: { x: 1, y: 0, z: 1 } });

    const entry = buildCommittedHistoryEntry({ operation: forward, before, after });
    expect(entry).not.toBeNull();
    expect(entry!.undo).toEqual({ kind: "edit", operation: removeServicePointOperation({ servicePointId: "sp_new_1" }) });
    // service points have no restore_element support (never needed — their
    // own add_service_point with the exact id already restores them
    // exactly, matching the fixture-redo shortcut this closure patch found
    // unnecessary to route through restore_element).
    expect(entry!.redo).toEqual({
      kind: "edit",
      operation: { kind: "add_service_point", payload: { id: "sp_new_1", kind: "plumbing", position: { x: 1, y: 0, z: 1 } } },
    });
  });

  it("add_constraint: Undo removes the exact authoritative id, Redo restores the exact same id", () => {
    const before = baseDraft();
    const createdConstraint = { id: "constraint_new_1", kind: "column", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, createdBy: "contractor" as const };
    const after = baseDraft({ constraints: [createdConstraint] });
    const forward = addConstraintOperation({ kind: "column", transform: createdConstraint.transform });

    const entry = buildCommittedHistoryEntry({ operation: forward, before, after });
    expect(entry).not.toBeNull();
    expect(entry!.undo).toEqual({ kind: "edit", operation: removeConstraintOperation({ constraintId: "constraint_new_1" }) });
    expect(entry!.redo).toEqual({
      kind: "edit",
      operation: { kind: "add_constraint", payload: { id: "constraint_new_1", kind: "column", transform: createdConstraint.transform } },
    });
  });

  it("falls back to buildInverseEdit's before-only model for non-add operations", () => {
    const before = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const forward = moveObjectOperation({ objectId: "object_1", position: { x: 5, y: 0, z: 5 } });
    const after = baseDraft({
      objects: [{ id: "object_1", category: "sofa", transform: { position: { x: 5, y: 0, z: 5 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "o1" } }],
    });
    const entry = buildCommittedHistoryEntry({ operation: forward, before, after });
    expect(entry!.undo).toEqual({ kind: "edit", operation: moveObjectOperation({ objectId: "object_1", position: { x: 1, y: 0, z: 1 } }) });
    expect(entry!.redo).toEqual({ kind: "edit", operation: forward });
  });

  it("returns null when no reversible mapping exists for the operation", () => {
    const before = baseDraft();
    const after = baseDraft();
    const forward = { kind: "assign_visual_asset" as const, payload: { targetKind: "object" as const, targetId: "object_1", assetId: "a1", version: 1 } };
    expect(buildCommittedHistoryEntry({ operation: forward, before, after })).toBeNull();
  });
});
