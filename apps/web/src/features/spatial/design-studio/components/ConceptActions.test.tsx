import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConceptActions } from "./ConceptActions";

describe("ConceptActions", () => {
  it("renders Use Design, Regenerate, and Cancel in that fixed order", () => {
    render(<ConceptActions onUseDesign={vi.fn()} onRegenerate={vi.fn()} onCancel={vi.fn()} onRefine={vi.fn()} />);
    const buttons = screen.getAllByRole("button").filter((b) => ["Use Design", "Regenerate", "Cancel"].includes(b.textContent ?? ""));
    expect(buttons.map((b) => b.textContent)).toEqual(["Cancel", "Regenerate", "Use Design"]);
  });

  it("renders up to three refinement chips and calls onRefine with the chip text", () => {
    const onRefine = vi.fn();
    render(<ConceptActions onUseDesign={vi.fn()} onRegenerate={vi.fn()} onCancel={vi.fn()} onRefine={onRefine} />);
    const group = screen.getByRole("group", { name: "Refinement suggestions" });
    const chips = group.querySelectorAll("button");
    expect(chips.length).toBeLessThanOrEqual(3);

    fireEvent.click(screen.getByRole("button", { name: "Softer curves" }));
    expect(onRefine).toHaveBeenCalledWith("Softer curves");
  });

  it("wires each action button to its own callback", () => {
    const onUseDesign = vi.fn();
    const onRegenerate = vi.fn();
    const onCancel = vi.fn();
    render(<ConceptActions onUseDesign={onUseDesign} onRegenerate={onRegenerate} onCancel={onCancel} onRefine={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Use Design" }));
    fireEvent.click(screen.getByRole("button", { name: "Regenerate" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onUseDesign).toHaveBeenCalledOnce();
    expect(onRegenerate).toHaveBeenCalledOnce();
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("respects useDesignDisabled and regenerateDisabled", () => {
    render(
      <ConceptActions
        onUseDesign={vi.fn()}
        onRegenerate={vi.fn()}
        onCancel={vi.fn()}
        onRefine={vi.fn()}
        useDesignDisabled
        regenerateDisabled
      />,
    );
    expect(screen.getByRole("button", { name: "Use Design" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Regenerate" })).toBeDisabled();
  });
});
