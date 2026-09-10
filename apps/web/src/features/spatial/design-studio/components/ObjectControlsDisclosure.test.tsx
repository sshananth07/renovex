import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ObjectControlsDisclosure } from "./ObjectControlsDisclosure";
import type { RoomDraft } from "../../api";

const draft: RoomDraft = {
  id: "draft_1",
  captureId: "capture_1",
  revision: 2,
  canResetToScan: true,
  walls: [],
  openings: [],
  objects: [
    { id: "object_1", category: "sofa", transform: { position: { x: 1, y: 0, z: 1 }, rotation: { x: 0, y: 0, z: 0, w: 1 } }, provenance: { provider: "roomplan", sourceElementIdentifier: "obj1" } },
  ],
  fixtures: [],
  servicePoints: [],
  constraints: [],
} as unknown as RoomDraft;

describe("ObjectControlsDisclosure", () => {
  it("renders collapsed by default with the ElementInspector content hidden", () => {
    render(
      <ObjectControlsDisclosure draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={false} />,
    );
    const details = screen.getByText("Manual controls").closest("details");
    expect(details).not.toBeNull();
    expect((details as HTMLDetailsElement).open).toBe(false);
  });

  it("opens to reveal the ElementInspector's controls when the summary is clicked", async () => {
    const user = userEvent.setup();
    render(
      <ObjectControlsDisclosure draft={draft} selection={{ kind: "object", id: "object_1" }} onSubmit={vi.fn()} disabled={false} />,
    );
    await user.click(screen.getByText("Manual controls"));
    const details = screen.getByText("Manual controls").closest("details");
    expect((details as HTMLDetailsElement).open).toBe(true);
  });
});
