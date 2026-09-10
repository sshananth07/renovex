import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { WorkItemSuggestionCard } from "./WorkItemSuggestionCard";
import type { Suggestion } from "../presentation";

function makeSuggestion(overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id: "sug1",
    batchId: "batch1",
    type: "work_item",
    status: "pending",
    revision: 1,
    createdAt: "2026-08-16T00:00:00Z",
    rationale: "Explicitly requested in the brief.",
    confidence: 0.9,
    suggestedData: {
      description: "Install ceramic floor tiles",
      workType: "tile_installation",
      scopeLevel: "space",
      spaceId: "space_1",
      scopeOrigin: "explicit_scope",
    },
    ...overrides,
  };
}

describe("WorkItemSuggestionCard", () => {
  it("never prefills quantity or unit", () => {
    renderWithProviders(<WorkItemSuggestionCard suggestion={makeSuggestion()} spaces={[]} onAccept={vi.fn()} onReject={vi.fn()} accepting={false} rejecting={false} />);
    const quantityInput = screen.getByLabelText(/quantity/i) as HTMLInputElement;
    const unitInput = screen.getByLabelText(/unit/i) as HTMLInputElement;
    expect(quantityInput.value).toBe("");
    expect(unitInput.value).toBe("");
  });

  it("requires quantity and unit before accept is allowed", async () => {
    const onAccept = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemSuggestionCard suggestion={makeSuggestion()} spaces={[]} onAccept={onAccept} onReject={vi.fn()} accepting={false} rejecting={false} />);

    await user.click(screen.getByRole("button", { name: /^accept$/i }));
    expect(await screen.findByText(/quantity is required/i)).toBeInTheDocument();
    expect(onAccept).not.toHaveBeenCalled();
  });

  it("accepts with contractor-supplied quantity/unit, unedited description/scope", async () => {
    const onAccept = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemSuggestionCard suggestion={makeSuggestion()} spaces={[]} onAccept={onAccept} onReject={vi.fn()} accepting={false} rejecting={false} />);

    await user.type(screen.getByLabelText(/quantity/i), "30");
    await user.type(screen.getByLabelText(/unit/i), "m2");
    await user.click(screen.getByRole("button", { name: /^accept$/i }));

    expect(onAccept).toHaveBeenCalledWith({
      description: "Install ceramic floor tiles",
      workType: "tile_installation",
      scopeLevel: "space",
      spaceId: "space_1",
      quantityValue: "30",
      quantityUnit: "m2",
    });
  });

  it("shows the Requested scope label for explicit_scope", () => {
    renderWithProviders(<WorkItemSuggestionCard suggestion={makeSuggestion()} spaces={[]} onAccept={vi.fn()} onReject={vi.fn()} accepting={false} rejecting={false} />);
    expect(screen.getByText("Requested scope")).toBeInTheDocument();
  });

  it("shows the Possible missing scope label without confirming an omission", () => {
    renderWithProviders(
      <WorkItemSuggestionCard
        suggestion={makeSuggestion({
          suggestedData: {
            description: "Install ceramic floor tiles",
            workType: "tile_installation",
            scopeLevel: "space",
            spaceId: "space_1",
            scopeOrigin: "possible_missing_scope",
          },
        })}
        spaces={[]}
        onAccept={vi.fn()}
        onReject={vi.fn()}
        accepting={false}
        rejecting={false}
      />
    );
    expect(screen.getByText("Possible missing scope")).toBeInTheDocument();
    expect(screen.queryByText(/you forgot/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/confirmed missing/i)).not.toBeInTheDocument();
  });

  it("rejects on click", async () => {
    const onReject = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemSuggestionCard suggestion={makeSuggestion()} spaces={[]} onAccept={vi.fn()} onReject={onReject} accepting={false} rejecting={false} />);
    await user.click(screen.getByRole("button", { name: /reject/i }));
    expect(onReject).toHaveBeenCalled();
  });
});
