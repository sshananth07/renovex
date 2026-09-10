import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ResourceSuggestionCard } from "./ResourceSuggestionCard";
import type { Suggestion } from "../presentation";

function makeSuggestion(overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id: "sug1",
    batchId: "batch1",
    type: "material_resource",
    status: "pending",
    revision: 1,
    createdAt: "2026-08-16T00:00:00Z",
    rationale: "Needed for tiling work.",
    confidence: 0.72,
    suggestedData: {
      workItemId: "wi1",
      name: "Tile Adhesive",
      candidateMaterialId: null,
    },
    ...overrides,
  };
}

describe("ResourceSuggestionCard", () => {
  it("renders a MaterialResourceAcceptDialog for material_resource suggestions", () => {
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion()}
        otherMaterials={[]}
        onAcceptExistingMaterial={vi.fn()}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText(/no catalog match/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create & add/i })).toBeInTheDocument();
  });

  it("shows Use Existing when candidateMaterialId is present among otherMaterials", async () => {
    const onAcceptExistingMaterial = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion({
          suggestedData: { workItemId: "wi1", name: "Tile Adhesive", candidateMaterialId: "material_1" },
        })}
        otherMaterials={[{ id: "material_1", name: "Premium Tile Adhesive" }]}
        onAcceptExistingMaterial={onAcceptExistingMaterial}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /use existing/i }));
    expect(onAcceptExistingMaterial).toHaveBeenCalledWith("material_1");
  });

  it("renders a simple Accept/Reject for trade_resource suggestions", async () => {
    const onAcceptResource = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion({ type: "trade_resource", suggestedData: { workItemId: "wi1", name: "Tiler" } })}
        otherMaterials={[]}
        onAcceptExistingMaterial={vi.fn()}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={onAcceptResource}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText("Tiler")).toBeInTheDocument();
    expect(screen.getByText("Trade")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^accept$/i }));
    expect(onAcceptResource).toHaveBeenCalled();
  });

  it("renders a simple Accept/Reject for equipment_resource suggestions", () => {
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion({ type: "equipment_resource", suggestedData: { workItemId: "wi1", name: "Tile Cutter" } })}
        otherMaterials={[]}
        onAcceptExistingMaterial={vi.fn()}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText("Equipment")).toBeInTheDocument();
  });

  it("shows Added to project once accepted", () => {
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion({ status: "accepted" })}
        otherMaterials={[]}
        onAcceptExistingMaterial={vi.fn()}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText("Added to project")).toBeInTheDocument();
  });

  it("shows Rejected once rejected", () => {
    renderWithProviders(
      <ResourceSuggestionCard
        suggestion={makeSuggestion({ status: "rejected" })}
        otherMaterials={[]}
        onAcceptExistingMaterial={vi.fn()}
        onCreateAndAddMaterial={vi.fn()}
        onAcceptResource={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText("Rejected")).toBeInTheDocument();
  });
});
