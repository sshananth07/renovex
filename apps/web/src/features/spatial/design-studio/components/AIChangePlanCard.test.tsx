import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AIChangePlanCard } from "./AIChangePlanCard";
import type { components } from "@/lib/api/generated/schema";

type ChangePlan = components["schemas"]["ChangePlanDTO"];

function makePlan(overrides: Partial<ChangePlan> = {}): ChangePlan {
  return {
    summary: ["Curve the backrest", "Switch to deep green fabric"],
    turnDelta: {
      geometry: { mode: "replace" },
      material: { mode: "replace" },
      spatial: { mode: "preserve" },
    },
    workingDesign: {
      geometry: { category: "sofa", preserveCanonicalDimensions: true, shapeDescription: "Curved-back three-seat sofa" },
      material: { baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "matte", metallic: false },
      resolvedSpatialOperations: [],
    },
    ...overrides,
  } as ChangePlan;
}

describe("AIChangePlanCard", () => {
  it("renders all four rows when the plan touches shape, look, and fit", () => {
    render(
      <AIChangePlanCard
        plan={makePlan()}
        fitAnalysis={{ status: "passed", blockers: null, warnings: null, observations: null }}
        onConfirm={vi.fn()}
        onEditPrompt={vi.fn()}
      />,
    );
    expect(screen.getByText("Curved-back three-seat sofa")).toBeInTheDocument();
    expect(screen.getByText(/fabric, matte/)).toBeInTheDocument();
    expect(screen.getByText("Stay in the current position")).toBeInTheDocument();
    expect(screen.getByText("Passed all fit checks")).toBeInTheDocument();
  });

  it("falls back to preserve copy when geometry and material sections are omitted", () => {
    render(
      <AIChangePlanCard
        plan={makePlan({
          workingDesign: { resolvedSpatialOperations: [] },
        })}
        onConfirm={vi.fn()}
        onEditPrompt={vi.fn()}
      />,
    );
    expect(screen.getByText("Keep the current shape")).toBeInTheDocument();
    expect(screen.getByText("Keep the current finish")).toBeInTheDocument();
  });

  it("shows blocker badges and disables nothing extra when fit analysis reports blockers", () => {
    render(
      <AIChangePlanCard
        plan={makePlan()}
        fitAnalysis={{
          status: "blocked",
          blockers: [{ code: "collision", message: "Overlaps the doorway swing" }],
          warnings: null,
          observations: null,
        }}
        onConfirm={vi.fn()}
        onEditPrompt={vi.fn()}
      />,
    );
    expect(screen.getByText("Overlaps the doorway swing")).toBeInTheDocument();
  });

  it("shows the GPU window notice only when hunyuanRequired is true", () => {
    const { rerender } = render(
      <AIChangePlanCard
        plan={makePlan()}
        execution={{ executable: true, hunyuanRequired: true, requiresConfirmation: true, turnRequiresAssetGeneration: true }}
        onConfirm={vi.fn()}
        onEditPrompt={vi.fn()}
      />,
    );
    expect(screen.getByText(/shared GPU capacity/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create concept" })).toBeInTheDocument();

    rerender(
      <AIChangePlanCard
        plan={makePlan()}
        execution={{ executable: true, hunyuanRequired: false, requiresConfirmation: false, turnRequiresAssetGeneration: false }}
        onConfirm={vi.fn()}
        onEditPrompt={vi.fn()}
      />,
    );
    expect(screen.queryByText(/shared GPU capacity/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Preview change" })).toBeInTheDocument();
  });

  it("calls onConfirm and onEditPrompt from their respective buttons", () => {
    const onConfirm = vi.fn();
    const onEditPrompt = vi.fn();
    render(<AIChangePlanCard plan={makePlan()} onConfirm={onConfirm} onEditPrompt={onEditPrompt} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit prompt" }));
    expect(onEditPrompt).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Preview change" }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });
});
