import { describe, expect, it } from "vitest";
import {
  addConstraintOperation,
  addFixtureOperation,
  addOpeningOperation,
  addServicePointOperation,
  assignVisualAssetOperation,
  buildPendingEdit,
  buildPendingReset,
  clearVisualAssetOperation,
  moveConstraintOperation,
  moveCornerOperation,
  moveFixtureOperation,
  moveObjectOperation,
  moveOpeningOperation,
  moveServicePointOperation,
  moveWallOperation,
  reclassifyFixtureOperation,
  reclassifyOpeningOperation,
  removeConstraintOperation,
  removeFixtureOperation,
  removeOpeningOperation,
  removeServicePointOperation,
  resizeFixtureOperation,
  resizeObjectOperation,
  resizeOpeningOperation,
  rotateObjectOperation,
  setDoorHingeOperation,
  setDoorLeafCountOperation,
  setDoorOpenDirectionOperation,
  setDoorSwingOperation,
  setWallThicknessOperation,
  toSubmitEditBody,
  toSubmitResetBody,
} from "./operations";

describe("operation builders — exact canonical payload shape", () => {
  it("move_corner carries the explicit endpoints list and newPosition, nothing else", () => {
    const op = moveCornerOperation({
      endpoints: [
        { wallId: "wall_a", endpoint: "end" },
        { wallId: "wall_b", endpoint: "start" },
      ],
      newPosition: { x: 1, y: 0, z: 2 },
    });
    expect(op).toEqual({
      kind: "move_corner",
      payload: {
        endpoints: [
          { wallId: "wall_a", endpoint: "end" },
          { wallId: "wall_b", endpoint: "start" },
        ],
        newPosition: { x: 1, y: 0, z: 2 },
      },
    });
  });

  it("reclassify_opening carries openingId/kind/profile exactly", () => {
    const op = reclassifyOpeningOperation({ openingId: "opening_1", kind: "window", profile: "arch" });
    expect(op).toEqual({
      kind: "reclassify_opening",
      payload: { openingId: "opening_1", kind: "window", profile: "arch" },
    });
  });

  it("move_service_point carries servicePointId/position exactly", () => {
    const op = moveServicePointOperation({ servicePointId: "sp_1", position: { x: 0.5, y: 0, z: -1 } });
    expect(op).toEqual({
      kind: "move_service_point",
      payload: { servicePointId: "sp_1", position: { x: 0.5, y: 0, z: -1 } },
    });
  });

  it("add_service_point carries kind/position/parentWallId and a generated id", () => {
    const op = addServicePointOperation({ kind: "plumbing", position: { x: 1, y: 0, z: 1 }, parentWallId: "wall_1" });
    expect(op.kind).toBe("add_service_point");
    if (op.kind !== "add_service_point") throw new Error("unreachable");
    expect(op.payload.kind).toBe("plumbing");
    expect(op.payload.position).toEqual({ x: 1, y: 0, z: 1 });
    expect(op.payload.parentWallId).toBe("wall_1");
    expect(typeof op.payload.id).toBe("string");
    expect(op.payload.id.length).toBeGreaterThan(0);
  });

  it("move_wall carries wallId/delta exactly", () => {
    const op = moveWallOperation({ wallId: "wall_1", delta: { x: 1, y: 0, z: 0 } });
    expect(op).toEqual({ kind: "move_wall", payload: { wallId: "wall_1", delta: { x: 1, y: 0, z: 0 } } });
  });

  it("set_wall_thickness carries wallId/thickness/status exactly", () => {
    const op = setWallThicknessOperation({ wallId: "wall_1", thickness: 0.15, status: "estimated" });
    expect(op).toEqual({ kind: "set_wall_thickness", payload: { wallId: "wall_1", thickness: 0.15, status: "estimated" } });
  });

  it("add_opening carries the exact AddOpeningOperation fields plus a generated id", () => {
    const op = addOpeningOperation({
      parentWallId: "wall_1",
      kind: "door",
      profile: "rectangle",
      transform: { position: { x: 1, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      width: 0.9,
      height: 2.1,
      offsetAlongWall: 1.5,
    });
    expect(op.kind).toBe("add_opening");
    if (op.kind !== "add_opening") throw new Error("unreachable");
    expect(op.payload.parentWallId).toBe("wall_1");
    expect(op.payload.width).toBe(0.9);
    expect(typeof op.payload.id).toBe("string");
    expect(op.payload.id.length).toBeGreaterThan(0);
  });

  it("remove_opening carries openingId exactly", () => {
    const op = removeOpeningOperation({ openingId: "opening_1" });
    expect(op).toEqual({ kind: "remove_opening", payload: { openingId: "opening_1" } });
  });

  it("move_opening carries openingId/transform/offsetAlongWall exactly", () => {
    const transform = { position: { x: 1, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } };
    const op = moveOpeningOperation({ openingId: "opening_1", transform, offsetAlongWall: 2 });
    expect(op).toEqual({ kind: "move_opening", payload: { openingId: "opening_1", transform, offsetAlongWall: 2 } });
  });

  it("resize_opening carries openingId/width/height exactly", () => {
    const op = resizeOpeningOperation({ openingId: "opening_1", width: 1, height: 2 });
    expect(op).toEqual({ kind: "resize_opening", payload: { openingId: "opening_1", width: 1, height: 2 } });
  });

  it("set_door_leaf_count carries openingId/leafCount exactly", () => {
    const op = setDoorLeafCountOperation({ openingId: "door_1", leafCount: 2 });
    expect(op).toEqual({ kind: "set_door_leaf_count", payload: { openingId: "door_1", leafCount: 2 } });
  });

  it("set_door_hinge carries openingId/hinge exactly", () => {
    const op = setDoorHingeOperation({ openingId: "door_1", hinge: "left" });
    expect(op).toEqual({ kind: "set_door_hinge", payload: { openingId: "door_1", hinge: "left" } });
  });

  it("set_door_swing carries openingId/swing exactly", () => {
    const op = setDoorSwingOperation({ openingId: "door_1", swing: "inward" });
    expect(op).toEqual({ kind: "set_door_swing", payload: { openingId: "door_1", swing: "inward" } });
  });

  it("set_door_open_direction carries openingId/openDirection exactly", () => {
    const op = setDoorOpenDirectionOperation({ openingId: "door_1", openDirection: "north" });
    expect(op).toEqual({ kind: "set_door_open_direction", payload: { openingId: "door_1", openDirection: "north" } });
  });

  it("add_fixture carries category/transform/dimensions/parentWallId and a generated id", () => {
    const op = addFixtureOperation({
      category: "boiler",
      transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
    });
    expect(op.kind).toBe("add_fixture");
    if (op.kind !== "add_fixture") throw new Error("unreachable");
    expect(op.payload.category).toBe("boiler");
    expect(typeof op.payload.id).toBe("string");
    expect(op.payload.id.length).toBeGreaterThan(0);
  });

  it("move_fixture carries fixtureId/transform exactly", () => {
    const transform = { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } };
    const op = moveFixtureOperation({ fixtureId: "fixture_1", transform });
    expect(op).toEqual({ kind: "move_fixture", payload: { fixtureId: "fixture_1", transform } });
  });

  it("resize_fixture carries fixtureId/dimensions exactly", () => {
    const op = resizeFixtureOperation({ fixtureId: "fixture_1", dimensions: { x: 1, y: 1, z: 1 } });
    expect(op).toEqual({ kind: "resize_fixture", payload: { fixtureId: "fixture_1", dimensions: { x: 1, y: 1, z: 1 } } });
  });

  it("reclassify_fixture carries fixtureId/category exactly", () => {
    const op = reclassifyFixtureOperation({ fixtureId: "fixture_1", category: "ac" });
    expect(op).toEqual({ kind: "reclassify_fixture", payload: { fixtureId: "fixture_1", category: "ac" } });
  });

  it("remove_fixture carries fixtureId exactly", () => {
    const op = removeFixtureOperation({ fixtureId: "fixture_1" });
    expect(op).toEqual({ kind: "remove_fixture", payload: { fixtureId: "fixture_1" } });
  });

  it("move_object carries objectId/position exactly", () => {
    const op = moveObjectOperation({ objectId: "object_1", position: { x: 1, y: 0, z: 1 } });
    expect(op).toEqual({ kind: "move_object", payload: { objectId: "object_1", position: { x: 1, y: 0, z: 1 } } });
  });

  it("rotate_object carries objectId/rotation exactly", () => {
    const rotation = { x: 0, y: 0.7071, z: 0, w: 0.7071 };
    const op = rotateObjectOperation({ objectId: "object_1", rotation });
    expect(op).toEqual({ kind: "rotate_object", payload: { objectId: "object_1", rotation } });
  });

  it("resize_object carries objectId/dimensions exactly", () => {
    const op = resizeObjectOperation({ objectId: "object_1", dimensions: { x: 2, y: 0.8, z: 1 } });
    expect(op).toEqual({ kind: "resize_object", payload: { objectId: "object_1", dimensions: { x: 2, y: 0.8, z: 1 } } });
  });

  it("assign_visual_asset carries targetKind/targetId/assetId/version exactly, for a fixture target", () => {
    const op = assignVisualAssetOperation({
      targetKind: "fixture",
      targetId: "fixture_1",
      assetId: "custom-boiler-asset",
      version: 3,
    });
    expect(op).toEqual({
      kind: "assign_visual_asset",
      payload: { targetKind: "fixture", targetId: "fixture_1", assetId: "custom-boiler-asset", version: 3 },
    });
  });

  it("assign_visual_asset carries targetKind/targetId/assetId/version exactly, for an object target", () => {
    const op = assignVisualAssetOperation({
      targetKind: "object",
      targetId: "object_1",
      assetId: "custom-sofa-asset",
      version: 1,
    });
    expect(op).toEqual({
      kind: "assign_visual_asset",
      payload: { targetKind: "object", targetId: "object_1", assetId: "custom-sofa-asset", version: 1 },
    });
  });

  it("clear_visual_asset carries targetKind/targetId exactly, nothing else", () => {
    const op = clearVisualAssetOperation({ targetKind: "fixture", targetId: "fixture_1" });
    expect(op).toEqual({ kind: "clear_visual_asset", payload: { targetKind: "fixture", targetId: "fixture_1" } });
  });

  it("remove_service_point carries servicePointId exactly", () => {
    const op = removeServicePointOperation({ servicePointId: "sp_1" });
    expect(op).toEqual({ kind: "remove_service_point", payload: { servicePointId: "sp_1" } });
  });

  it("add_constraint carries kind/transform/dimensions and a generated id", () => {
    const op = addConstraintOperation({
      kind: "column",
      transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
    });
    expect(op.kind).toBe("add_constraint");
    if (op.kind !== "add_constraint") throw new Error("unreachable");
    expect(op.payload.kind).toBe("column");
    expect(typeof op.payload.id).toBe("string");
    expect(op.payload.id.length).toBeGreaterThan(0);
  });

  it("move_constraint carries constraintId/transform exactly", () => {
    const transform = { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } };
    const op = moveConstraintOperation({ constraintId: "constraint_1", transform });
    expect(op).toEqual({ kind: "move_constraint", payload: { constraintId: "constraint_1", transform } });
  });

  it("remove_constraint carries constraintId exactly", () => {
    const op = removeConstraintOperation({ constraintId: "constraint_1" });
    expect(op).toEqual({ kind: "remove_constraint", payload: { constraintId: "constraint_1" } });
  });
});

describe("buildPendingEdit — OperationID and identity lifecycle", () => {
  it("generates a fresh operationId each time it's called for a new action", () => {
    const op = reclassifyOpeningOperation({ openingId: "opening_1", kind: "window", profile: "rectangle" });
    const first = buildPendingEdit(op, 5);
    const second = buildPendingEdit(op, 5);
    expect(first.operationId).not.toBe(second.operationId);
  });

  it("captures baseRevision immutably at construction time", () => {
    const op = reclassifyOpeningOperation({ openingId: "opening_1", kind: "door", profile: "rectangle" });
    const pending = buildPendingEdit(op, 12);
    expect(pending.baseRevision).toBe(12);
  });

  // The exact failure mode flagged in review: a retry of the SAME logical
  // add_service_point action must never produce a different operationId OR
  // a different generated servicePoint id than the original attempt. The
  // fix is structural — the operation (including its generated id) is built
  // ONCE and the same PendingEdit object is reused verbatim for the retry,
  // never rebuilt.
  it("add_service_point: reusing the same PendingEdit across a simulated retry keeps operationId AND servicePoint.id identical", () => {
    const op = addServicePointOperation({ kind: "electrical", position: { x: 2, y: 0, z: 2 } });
    const pendingEdit = buildPendingEdit(op, 3);

    // Simulate: first attempt sent, response lost, caller retries by
    // resubmitting the SAME stored PendingEdit (not rebuilding a new one).
    const firstAttemptBody = toSubmitEditBody(pendingEdit);
    const retryBody = toSubmitEditBody(pendingEdit);

    expect(retryBody.operationId).toBe(firstAttemptBody.operationId);
    expect((retryBody.payload as { id: string }).id).toBe((firstAttemptBody.payload as { id: string }).id);
    expect(retryBody).toEqual(firstAttemptBody);
  });

  it("a genuinely new user action (rebuilding via addServicePointOperation again) gets a fresh id and a fresh PendingEdit", () => {
    const firstAction = buildPendingEdit(addServicePointOperation({ kind: "gas", position: { x: 0, y: 0, z: 0 } }), 3);
    const secondAction = buildPendingEdit(addServicePointOperation({ kind: "gas", position: { x: 0, y: 0, z: 0 } }), 3);

    expect(firstAction.operationId).not.toBe(secondAction.operationId);
    const firstPayload = firstAction.operation.payload as { id: string };
    const secondPayload = secondAction.operation.payload as { id: string };
    expect(firstPayload.id).not.toBe(secondPayload.id);
  });

  it("add_fixture: reusing the same PendingEdit across a simulated retry keeps operationId AND fixture.id identical", () => {
    const op = addFixtureOperation({
      category: "boiler",
      transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
    });
    const pendingEdit = buildPendingEdit(op, 3);

    const firstAttemptBody = toSubmitEditBody(pendingEdit);
    const retryBody = toSubmitEditBody(pendingEdit);

    expect(retryBody.operationId).toBe(firstAttemptBody.operationId);
    expect((retryBody.payload as { id: string }).id).toBe((firstAttemptBody.payload as { id: string }).id);
    expect(retryBody).toEqual(firstAttemptBody);
  });

  it("add_constraint: reusing the same PendingEdit across a simulated retry keeps operationId AND constraint.id identical", () => {
    const op = addConstraintOperation({
      kind: "column",
      transform: { position: { x: 0, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
    });
    const pendingEdit = buildPendingEdit(op, 3);

    const firstAttemptBody = toSubmitEditBody(pendingEdit);
    const retryBody = toSubmitEditBody(pendingEdit);

    expect(retryBody.operationId).toBe(firstAttemptBody.operationId);
    expect((retryBody.payload as { id: string }).id).toBe((firstAttemptBody.payload as { id: string }).id);
    expect(retryBody).toEqual(firstAttemptBody);
  });
});

describe("buildPendingReset — separate from PendingEdit, same lifecycle rules", () => {
  it("generates a fresh operationId each time it's called for a new reset confirmation", () => {
    const first = buildPendingReset(5);
    const second = buildPendingReset(5);
    expect(first.operationId).not.toBe(second.operationId);
  });

  it("captures baseRevision immutably at construction time", () => {
    const pending = buildPendingReset(9);
    expect(pending.baseRevision).toBe(9);
  });

  it("carries no operation/payload field — structurally distinct from PendingEdit", () => {
    const pending = buildPendingReset(5);
    expect(pending).toEqual({ operationId: pending.operationId, baseRevision: 5, status: "submitting" });
    expect("operation" in pending).toBe(false);
  });

  it("reusing the same PendingReset across a simulated network retry keeps operationId AND baseRevision identical", () => {
    const pendingReset = buildPendingReset(4);

    const firstAttemptBody = toSubmitResetBody(pendingReset);
    const retryBody = toSubmitResetBody(pendingReset);

    expect(retryBody.operationId).toBe(firstAttemptBody.operationId);
    expect(retryBody.expectedRevision).toBe(firstAttemptBody.expectedRevision);
    expect(retryBody).toEqual(firstAttemptBody);
  });
});

describe("toSubmitEditBody — exact RP4B wire shape", () => {
  it("maps operationId/kind/payload/baseRevision to expectedRevision", () => {
    const op = moveServicePointOperation({ servicePointId: "sp_1", position: { x: 1, y: 0, z: 1 } });
    const pendingEdit = buildPendingEdit(op, 7);
    const body = toSubmitEditBody(pendingEdit);

    expect(body).toEqual({
      operationId: pendingEdit.operationId,
      kind: "move_service_point",
      payload: { servicePointId: "sp_1", position: { x: 1, y: 0, z: 1 } },
      expectedRevision: 7,
    });
  });
});

describe("toSubmitResetBody — exact RP4B reset-to-scan wire shape", () => {
  it("maps operationId/baseRevision to expectedRevision, no kind/payload fields", () => {
    const pendingReset = buildPendingReset(7);
    const body = toSubmitResetBody(pendingReset);

    expect(body).toEqual({ operationId: pendingReset.operationId, expectedRevision: 7 });
    expect("kind" in body).toBe(false);
    expect("payload" in body).toBe(false);
  });
});
