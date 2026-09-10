import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SpaceSuggestionCard } from "./SpaceSuggestionCard";
import type { Suggestion } from "../presentation";

function makeSuggestion(overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id: "sug1",
    batchId: "batch1",
    type: "space",
    status: "pending",
    revision: 1,
    createdAt: "2026-08-16T00:00:00Z",
    rationale: "Kitchen renovation is explicitly mentioned.",
    confidence: 0.94,
    suggestedData: { name: "Kitchen", spaceType: "kitchen" },
    ...overrides,
  };
}

describe("SpaceSuggestionCard", () => {
  it("shows a coarse confidence label, not a raw percentage", () => {
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion()} onAccept={vi.fn()} onReject={vi.fn()} accepting={false} rejecting={false} />
    );
    expect(screen.getByText(/confidence:\s*high/i)).toBeInTheDocument();
    expect(screen.queryByText("94%")).not.toBeInTheDocument();
    expect(screen.queryByText(/0\.94/)).not.toBeInTheDocument();
  });

  it("calls onAccept with no override when Accept is clicked unchanged", async () => {
    const onAccept = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion()} onAccept={onAccept} onReject={vi.fn()} accepting={false} rejecting={false} />
    );
    await user.click(screen.getByRole("button", { name: /^accept$/i }));
    expect(onAccept).toHaveBeenCalledWith(undefined);
  });

  it("lets the contractor edit name/type before accepting", async () => {
    const onAccept = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion()} onAccept={onAccept} onReject={vi.fn()} accepting={false} rejecting={false} />
    );
    await user.click(screen.getByRole("button", { name: /edit/i }));
    const nameInput = screen.getByLabelText(/name/i);
    await user.clear(nameInput);
    await user.type(nameInput, "Master Bathroom");
    await user.click(screen.getByRole("button", { name: /accept edits/i }));

    expect(onAccept).toHaveBeenCalledWith({ name: "Master Bathroom", spaceType: "kitchen", description: "" });
  });

  it("calls onReject when Reject is clicked", async () => {
    const onReject = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion()} onAccept={vi.fn()} onReject={onReject} accepting={false} rejecting={false} />
    );
    await user.click(screen.getByRole("button", { name: /reject/i }));
    expect(onReject).toHaveBeenCalled();
  });

  it("shows an accepted confirmation with a link to the created Space", () => {
    renderWithProviders(
      <SpaceSuggestionCard
        suggestion={makeSuggestion({ status: "accepted", acceptedDomainObjectId: "space_1" })}
        onAccept={vi.fn()}
        onReject={vi.fn()}
        accepting={false}
        rejecting={false}
      />
    );
    expect(screen.getByText(/added to project/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /view space/i })).toHaveAttribute("href", "/spaces/space_1");
  });

  it("keeps a rejected suggestion visible in a collapsed/quiet state rather than disappearing", () => {
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion({ status: "rejected" })} onAccept={vi.fn()} onReject={vi.fn()} accepting={false} rejecting={false} />
    );
    expect(screen.getByText("Kitchen")).toBeInTheDocument();
    expect(screen.getByText(/rejected/i)).toBeInTheDocument();
  });

  it("never renders AI-verified or confirmed-omission language", () => {
    renderWithProviders(
      <SpaceSuggestionCard suggestion={makeSuggestion()} onAccept={vi.fn()} onReject={vi.fn()} accepting={false} rejecting={false} />
    );
    expect(screen.queryByText(/ai verified/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/required by renovex/i)).not.toBeInTheDocument();
  });
});
