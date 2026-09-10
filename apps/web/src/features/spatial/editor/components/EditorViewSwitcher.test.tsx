import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EditorViewSwitcher } from "./EditorViewSwitcher";

describe("EditorViewSwitcher", () => {
  it("marks the active view as checked and the inactive view as not checked", () => {
    render(<EditorViewSwitcher activeView="2d" onChange={vi.fn()} />);
    expect(screen.getByRole("radio", { name: "2D" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: "3D" })).toHaveAttribute("aria-checked", "false");
  });

  it("calls onChange with the clicked view", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<EditorViewSwitcher activeView="2d" onChange={onChange} />);

    await user.click(screen.getByRole("radio", { name: "3D" }));
    expect(onChange).toHaveBeenCalledWith("3d");
  });

  it("disables both options when disabled", () => {
    render(<EditorViewSwitcher activeView="2d" onChange={vi.fn()} disabled />);
    expect(screen.getByRole("radio", { name: "2D" })).toBeDisabled();
    expect(screen.getByRole("radio", { name: "3D" })).toBeDisabled();
  });
});
