import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { WorkItemForm } from "./WorkItemForm";

const spaces = [
  { id: "s1", projectId: "p1", name: "Kitchen", createdAt: "2026-01-15T00:00:00Z" },
  { id: "s2", projectId: "p1", name: "Master Bedroom", createdAt: "2026-01-16T00:00:00Z" },
];

describe("WorkItemForm", () => {
  it("shows validation errors and does not submit when required fields are empty", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemForm spaces={spaces} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/description is required/i)).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("rejects a non-positive quantity", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemForm spaces={spaces} onSubmit={onSubmit} />);

    await user.type(screen.getByLabelText(/description/i), "Install cabinets");
    await user.type(screen.getByLabelText(/quantity/i), "0");
    await user.type(screen.getByLabelText(/unit/i), "sqft");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/enter a positive number/i)).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits description, quantity, unit, workType, and spaceId", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<WorkItemForm spaces={spaces} onSubmit={onSubmit} />);

    await user.type(screen.getByLabelText(/description/i), "Install cabinets");
    await user.type(screen.getByLabelText(/quantity/i), "12.5");
    await user.type(screen.getByLabelText(/unit/i), "sqft");
    await user.type(screen.getByLabelText(/work type/i), "Carpentry");
    await user.click(screen.getByRole("combobox", { name: /space/i }));
    await user.click(screen.getByRole("option", { name: "Kitchen" }));
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        description: "Install cabinets",
        quantityValue: "12.5",
        quantityUnit: "sqft",
        workType: "Carpentry",
        spaceId: "s1",
      }),
      expect.anything()
    );
  });

  it("prefills fields when editing an existing work item", () => {
    renderWithProviders(
      <WorkItemForm
        spaces={spaces}
        onSubmit={vi.fn()}
        defaultValues={{
          description: "Install cabinets",
          quantityValue: "12.5",
          quantityUnit: "sqft",
          workType: "Carpentry",
          spaceId: "s1",
        }}
      />
    );

    expect(screen.getByLabelText(/description/i)).toHaveValue("Install cabinets");
    expect(screen.getByLabelText(/quantity/i)).toHaveValue("12.5");
  });

  it("lets an assigned work item explicitly clear its Space", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <WorkItemForm
        spaces={spaces}
        onSubmit={onSubmit}
        defaultValues={{
          description: "Install cabinets",
          quantityValue: "12.5",
          quantityUnit: "sqft",
          spaceId: "s1",
        }}
      />
    );

    const trigger = screen.getByRole("combobox", { name: /space/i });
    await user.click(trigger);

    const noSpaceOption = await screen.findByRole("option", { name: "No space assigned" });
    await user.click(noSpaceOption);
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ spaceId: "" }),
      expect.anything()
    );
  });
});
