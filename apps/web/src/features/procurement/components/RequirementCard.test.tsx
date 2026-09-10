import { screen } from "@testing-library/react";
import { render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { components } from "@/lib/api/generated/schema";
import { RequirementCard } from "./RequirementCard";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

function makeRequirement(overrides: Partial<MaterialRequirement> = {}): MaterialRequirement {
  return {
    id: "req-1",
    projectId: "proj-1",
    materialId: "mat-1",
    materialName: "Cement",
    catalogUnit: "bag",
    requiredQuantity: { value: "35", unit: "bag" },
    specification: "Need cement",
    status: "draft",
    sourceType: "manual",
    sourceSyncState: "clean",
    unitMismatch: false,
    unitMismatchAcknowledged: false,
    revision: 3,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("RequirementCard", () => {
  it("draft requirement shows identity, Draft badge, and Review action", () => {
    render(<RequirementCard requirement={makeRequirement()} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.getByText("Cement")).toBeInTheDocument();
    expect(screen.getByText(/35 bag/)).toBeInTheDocument();
    expect(screen.getByText("Need cement")).toBeInTheDocument();
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /confirm requirement/i })).toBeInTheDocument();
  });

  it("draft requirement also shows a Remove action that calls onSecondaryAction with the requirement id and revision", async () => {
    const user = userEvent.setup();
    const onSecondaryAction = vi.fn();
    render(<RequirementCard requirement={makeRequirement({ id: "req-9", revision: 5 })} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={onSecondaryAction} />);
    const removeButton = screen.getByRole("button", { name: /remove requirement/i });
    await user.click(removeButton);
    expect(onSecondaryAction).toHaveBeenCalledWith({ type: "reject", requirementId: "req-9", expectedRevision: 5 });
  });

  it("reviewed requirement shows no Remove action", () => {
    render(<RequirementCard requirement={makeRequirement({ status: "reviewed" })} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /remove requirement/i })).not.toBeInTheDocument();
  });

  it("reviewed + eligible + draft selected RFQ shows Add to RFQ", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({ status: "reviewed" })}
        context={{ selectedRfq: { id: "rfq-1", number: "RFQ-000001", status: "draft", revision: 1 } }}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.getByRole("button", { name: /add to rfq/i })).toBeInTheDocument();
  });

  it("reviewed + eligible + no selected draft RFQ shows no add button and a selection message", () => {
    render(<RequirementCard requirement={makeRequirement({ status: "reviewed" })} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /add to rfq/i })).not.toBeInTheDocument();
    expect(screen.getByText(/select a draft rfq/i)).toBeInTheDocument();
  });

  it("unit mismatch shows only Acknowledge unit", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({ unitMismatch: true, unitMismatchAcknowledged: false })}
        context={{}}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.getByRole("button", { name: /acknowledge unit/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /confirm requirement/i })).not.toBeInTheDocument();
  });

  it("source discrepancy shows only Review source change", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({ status: "reviewed", sourceSyncState: "change_detected" })}
        context={{}}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.getByRole("button", { name: /review source change/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add to rfq/i })).not.toBeInTheDocument();
  });

  it("claimed + source change shows only View claiming RFQ, with the source-changed badge still visible", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({
          status: "reviewed",
          activeRfqChainId: "chain-1",
          activeRfqNumber: "RFQ-000003",
          sourceSyncState: "change_detected",
        })}
        context={{}}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.getByRole("button", { name: /view rfq-000003/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /review source change/i })).not.toBeInTheDocument();
    expect(screen.getByText("Source changed")).toBeInTheDocument();
  });

  it("archived + clean shows no primary mutation action", () => {
    render(<RequirementCard requirement={makeRequirement({ status: "archived" })} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("split source shows View split batches", () => {
    render(<RequirementCard requirement={makeRequirement({ status: "split" })} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.getByRole("button", { name: /view split batches/i })).toBeInTheDocument();
  });

  it("split-incomplete shows no primary action and reconciliation copy", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({ status: "split", splitState: "creating" })}
        context={{}}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(/requires reconciliation/i)).toBeInTheDocument();
  });

  it("create_separate provenance shows 'Created from source change' even though sourceType is manual", () => {
    render(
      <RequirementCard
        requirement={makeRequirement({ sourceType: "manual", createdFromDiscrepancyRequirementId: "req-original" })}
        context={{}}
        pending={false}
        onAction={vi.fn()} onSecondaryAction={vi.fn()}
      />
    );
    expect(screen.getByText("Created from source change")).toBeInTheDocument();
    expect(screen.queryByText(/^Manual$/)).not.toBeInTheDocument();
  });

  it("project-level requirement (no workItemId) shows the project-level label", () => {
    render(<RequirementCard requirement={makeRequirement()} context={{}} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    expect(screen.getByText("Project-level requirement")).toBeInTheDocument();
  });

  it("shows the supplied work item name from context", () => {
    render(
      <RequirementCard requirement={makeRequirement({ workItemId: "wi-1" })} context={{ workItemName: "Kitchen renovation" }} pending={false} onAction={vi.fn()} onSecondaryAction={vi.fn()} />
    );
    expect(screen.getByText("Kitchen renovation")).toBeInTheDocument();
  });

  it("clicking the primary action calls onAction with the derived action", async () => {
    const onAction = vi.fn();
    const user = userEvent.setup();
    render(<RequirementCard requirement={makeRequirement()} context={{}} pending={false} onAction={onAction} onSecondaryAction={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: /confirm requirement/i }));
    expect(onAction).toHaveBeenCalledWith({ type: "review", requirementId: "req-1", expectedRevision: 3 });
  });

  it("shows the pending label and disables the button while pending", () => {
    render(<RequirementCard requirement={makeRequirement()} context={{}} pending={true} onAction={vi.fn()} onSecondaryAction={vi.fn()} />);
    const button = screen.getByRole("button", { name: /confirming/i });
    expect(button).toBeDisabled();
  });
});
