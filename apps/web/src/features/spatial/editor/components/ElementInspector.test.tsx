import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ElementInspector } from "./ElementInspector";
import type { RoomDraft } from "../../api";

const draft: RoomDraft = {
  id: "draft_1",
  captureId: "capture_1",
  revision: 2,
  canResetToScan: true,
  walls: [
    {
      id: "wall_1",
      start: { x: 0, y: 0, z: 0 },
      end: { x: 4, y: 0, z: 0 },
      thickness: 0.15,
      thicknessStatus: "unconfirmed",
      provenance: { provider: "roomplan", sourceElementIdentifier: "w1" },
    },
  ],
  openings: [
    {
      id: "opening_1",
      kind: "door",
      profile: "rectangle",
      parentWallId: "wall_1",
      width: 0.9,
      height: 2.1,
      transform: { position: { x: 2, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      provenance: { provider: "roomplan", sourceElementIdentifier: "o1" },
      door: { leafCount: 1, hinge: "left", swing: "inward", openDirection: "north" },
    },
    {
      id: "opening_2",
      kind: "window",
      profile: "rectangle",
      parentWallId: "wall_1",
      transform: { position: { x: 3, y: 0, z: 0 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      provenance: { provider: "roomplan", sourceElementIdentifier: "o2" },
    },
  ],
  objects: [
    { id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" } },
    {
      id: "object_2",
      category: "sofa",
      transform: { position: { x: 2, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      provenance: { provider: "roomplan", sourceElementIdentifier: "obj2" },
      visualAsset: { assetId: "custom-sofa-asset", version: 1 },
    },
  ],
  fixtures: [
    {
      id: "fixture_1",
      category: "boiler",
      transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      dimensions: { x: 0.6, y: 0.8, z: 0.4 },
      createdBy: "contractor",
    },
    {
      id: "fixture_2",
      category: "boiler",
      transform: { position: { x: 2, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      dimensions: { x: 0.6, y: 0.8, z: 0.4 },
      createdBy: "contractor",
      visualAsset: { assetId: "custom-boiler-asset", version: 3 },
    },
    {
      id: "fixture_3",
      category: "unheard_of_category",
      transform: { position: { x: 3, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
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
      transform: { position: { x: 2, y: 0, z: 2 }, rotation: { x: 0, y: 0, z: 0, w: 1 } },
      createdBy: "contractor",
    },
  ],
};

describe("ElementInspector — no selection", () => {
  it("shows a prompt to select an element", () => {
    render(<ElementInspector draft={draft} selection={null} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/select an element/i)).toBeInTheDocument();
  });
});

describe("ElementInspector — canonical field display", () => {
  it("shows the wall's id and thickness fields", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "wall", id: "wall_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText("wall_1")).toBeInTheDocument();
  });

  it("shows the service point's kind and createdBy", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "servicePoint", id: "sp_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText("plumbing")).toBeInTheDocument();
    expect(screen.getByText("contractor")).toBeInTheDocument();
  });

  it("shows 'no longer exists' when the selected id is missing from the draft", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "wall", id: "wall_gone" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/no longer exists/i)).toBeInTheDocument();
  });
});

describe("ElementInspector — wall (set_wall_thickness)", () => {
  it("committing a new thickness value submits set_wall_thickness", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "wall", id: "wall_1" }} onSubmit={onSubmit} disabled={false} />);

    const input = screen.getByLabelText(/thickness/i);
    await user.clear(input);
    await user.type(input, "0.2");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "set_wall_thickness",
      payload: { wallId: "wall_1", thickness: 0.2, status: "estimated" },
    });
  });
});

describe("ElementInspector — reclassify_opening", () => {
  it("changing the Kind select submits a reclassify_opening operation preserving the current profile", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.selectOptions(screen.getByLabelText(/^kind$/i), "window");

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "reclassify_opening",
      payload: { openingId: "opening_1", kind: "window", profile: "rectangle" },
    });
  });

  it("changing the Profile select submits a reclassify_opening operation preserving the current kind", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.selectOptions(screen.getByLabelText(/^profile$/i), "arch");

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "reclassify_opening",
      payload: { openingId: "opening_1", kind: "door", profile: "arch" },
    });
  });

  it("disables the selects while a submission is pending", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={vi.fn()} disabled />);
    expect(screen.getByLabelText(/^kind$/i)).toBeDisabled();
    expect(screen.getByLabelText(/^profile$/i)).toBeDisabled();
  });

  it("clicking Remove opening submits remove_opening", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /remove opening/i }));

    expect(onSubmit).toHaveBeenCalledWith({ kind: "remove_opening", payload: { openingId: "opening_1" } });
  });

  it("shows door-specific fields for a door opening", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByLabelText(/hinge/i)).toBeInTheDocument();
  });

  it("hides door-specific fields for a non-door opening", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_2" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.queryByLabelText(/hinge/i)).not.toBeInTheDocument();
  });

  it("changing the door hinge select submits set_door_hinge", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.selectOptions(screen.getByLabelText(/hinge/i), "right");

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "set_door_hinge",
      payload: { openingId: "opening_1", hinge: "right" },
    });
  });

  it("changing the door leaf count select submits set_door_leaf_count", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "opening", id: "opening_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.selectOptions(screen.getByLabelText(/leaf count/i), "2");

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "set_door_leaf_count",
      payload: { openingId: "opening_1", leafCount: 2 },
    });
  });
});

describe("ElementInspector — fixture", () => {
  it("shows canonical fixture fields", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText("fixture_1")).toBeInTheDocument();
    expect(screen.getByText("contractor")).toBeInTheDocument();
  });

  it("changing the category select submits reclassify_fixture", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.selectOptions(screen.getByLabelText(/category/i), "ac");

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "reclassify_fixture",
      payload: { fixtureId: "fixture_1", category: "ac" },
    });
  });

  it("committing a new width submits resize_fixture preserving other dimensions", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_1" }} onSubmit={onSubmit} disabled={false} />);

    const widthInput = screen.getByLabelText(/^w \(m\)$/i);
    await user.clear(widthInput);
    await user.type(widthInput, "1");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "resize_fixture",
      payload: { fixtureId: "fixture_1", dimensions: { x: 1, y: 0.8, z: 0.4 } },
    });
  });

  it("clicking Remove fixture submits remove_fixture", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /remove fixture/i }));

    expect(onSubmit).toHaveBeenCalledWith({ kind: "remove_fixture", payload: { fixtureId: "fixture_1" } });
  });
});

describe("ElementInspector — visual-asset status (RP4D)", () => {
  it("shows 'Category default' for a fixture with a registered category and no explicit binding", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/category default/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /use default visual/i })).not.toBeInTheDocument();
  });

  it("shows 'Procedural fallback' for a fixture with an unregistered category and no explicit binding", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_3" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/procedural fallback/i)).toBeInTheDocument();
  });

  it("shows 'Custom asset v3' and a 'Use default visual' action for a fixture with an explicit binding", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_2" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/custom asset v3/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /use default visual/i })).toBeInTheDocument();
  });

  it("clicking 'Use default visual' on a bound fixture submits clear_visual_asset with the fixture target", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "fixture", id: "fixture_2" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /use default visual/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "clear_visual_asset",
      payload: { targetKind: "fixture", targetId: "fixture_2" },
    });
  });

  it("shows 'Category default' for an object with a registered category and no explicit binding", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/category default/i)).toBeInTheDocument();
  });

  it("shows 'Custom asset v1' and a 'Use default visual' action for an object with an explicit binding", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_2" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText(/custom asset v1/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /use default visual/i })).toBeInTheDocument();
  });

  it("clicking 'Use default visual' on a bound object submits clear_visual_asset with the object target", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_2" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /use default visual/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "clear_visual_asset",
      payload: { targetKind: "object", targetId: "object_2" },
    });
  });

});

describe("ElementInspector — service point remove", () => {
  it("clicking Remove service point submits remove_service_point", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "servicePoint", id: "sp_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /remove service point/i }));

    expect(onSubmit).toHaveBeenCalledWith({ kind: "remove_service_point", payload: { servicePointId: "sp_1" } });
  });
});

describe("ElementInspector — constraint", () => {
  it("shows kind read-only (no reclassify control — RP4A has no reclassify_constraint operation)", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "constraint", id: "constraint_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText("column")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("clicking Remove constraint submits remove_constraint", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "constraint", id: "constraint_1" }} onSubmit={onSubmit} disabled={false} />);

    await user.click(screen.getByRole("button", { name: /remove constraint/i }));

    expect(onSubmit).toHaveBeenCalledWith({ kind: "remove_constraint", payload: { constraintId: "constraint_1" } });
  });
});

// T1B: objects gain real manual editing (move/rotate/resize via the same
// canonical operations fixtures already use) — the previous "not part of
// this slice" dead-end no longer reflects the actual canonical vocabulary
// (backend/internal/spatial/editoperation.go already had MoveObjectOperation/
// RotateObjectOperation/ResizeObjectOperation; only the Web builders/UI were
// missing). Position/rotation stay drag-driven (matching the fixture
// convention — fixtures expose no numeric position field either, only
// dimensions), so this inspector exposes Dimensions numerically plus a
// yaw-only Rotation field for precise adjustment, mirroring fixture's own
// W/H/D layout exactly.
describe("ElementInspector — object (T1B: real manual editing, no longer a dead-end)", () => {
  it("no longer shows the stale 'not part of this slice' message", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.queryByText(/not part of this slice/i)).not.toBeInTheDocument();
  });

  it("shows category/provenance plus editable Dimensions and Rotation controls", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={false} />);
    expect(screen.getByText("sofa")).toBeInTheDocument();
    expect(screen.getByLabelText(/^w \(m\)$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^h \(m\)$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^d \(m\)$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/rotation.*°/i)).toBeInTheDocument();
  });

  it("committing a Dimensions field submits resize_object with the full merged dimensions", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={onSubmit} disabled={false} />);

    const widthField = screen.getByLabelText(/^w \(m\)$/i);
    await user.clear(widthField);
    await user.type(widthField, "2");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledWith({
      kind: "resize_object",
      payload: { objectId: "object_1", dimensions: { x: 2, y: 0.5, z: 0.5 } },
    });
  });

  it("committing the Rotation field submits rotate_object with a yaw-only quaternion", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={onSubmit} disabled={false} />);

    const rotationField = screen.getByLabelText(/rotation.*°/i);
    await user.clear(rotationField);
    await user.type(rotationField, "90");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const call = onSubmit.mock.calls[0][0];
    expect(call.kind).toBe("rotate_object");
    expect(call.payload.objectId).toBe("object_1");
    expect(call.payload.rotation.y).toBeCloseTo(Math.sqrt(2) / 2, 4);
    expect(call.payload.rotation.w).toBeCloseTo(Math.sqrt(2) / 2, 4);
  });

  it("committing a Rotation value of exactly 0 degrees still submits rotate_object (0 is a valid angle, unlike a dimension)", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={onSubmit} disabled={false} />);

    const rotationField = screen.getByLabelText(/rotation.*°/i);
    await user.clear(rotationField);
    await user.type(rotationField, "0");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const call = onSubmit.mock.calls[0][0];
    expect(call.kind).toBe("rotate_object");
    expect(call.payload.rotation).toEqual({ x: 0, y: 0, z: 0, w: 1 });
  });

  it("committing a negative Rotation value submits rotate_object (negative yaw is a valid angle)", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={onSubmit} disabled={false} />);

    const rotationField = screen.getByLabelText(/rotation.*°/i);
    await user.clear(rotationField);
    await user.type(rotationField, "-90");
    await user.tab();

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const call = onSubmit.mock.calls[0][0];
    expect(call.kind).toBe("rotate_object");
    expect(call.payload.rotation.y).toBeCloseTo(-Math.sqrt(2) / 2, 4);
    expect(call.payload.rotation.w).toBeCloseTo(Math.sqrt(2) / 2, 4);
  });

  it("keeps the existing visual-asset status/clear behavior unchanged", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_2" }} onSubmit={onSubmit} disabled={false} />);

    expect(screen.getByText(/custom asset v1/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /use default visual/i }));
    expect(onSubmit).toHaveBeenCalledWith({
      kind: "clear_visual_asset",
      payload: { targetKind: "object", targetId: "object_2" },
    });
  });

  it("disables the manual controls when disabled=true", () => {
    render(<ElementInspector draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={true} />);
    expect(screen.getByLabelText(/^w \(m\)$/i)).toBeDisabled();
    expect(screen.getByLabelText(/rotation.*°/i)).toBeDisabled();
  });
});
