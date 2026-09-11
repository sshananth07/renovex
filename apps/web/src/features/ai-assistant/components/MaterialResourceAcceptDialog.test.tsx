import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { MaterialResourceAcceptDialog } from "./MaterialResourceAcceptDialog";
import type { Material } from "./types";

const candidateMaterial: Material = { id: "material_1", name: "Premium Tile Adhesive" };

describe("MaterialResourceAcceptDialog", () => {
  it("offers Use Existing when a candidate match is present", () => {
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Adhesive"
        candidate={candidateMaterial}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByRole("button", { name: /use existing/i })).toBeInTheDocument();
  });

  it("calls onAcceptExisting with the candidate material id", async () => {
    const onAcceptExisting = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Adhesive"
        candidate={candidateMaterial}
        otherMaterials={[]}
        onAcceptExisting={onAcceptExisting}
        onCreateAndAdd={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /use existing/i }));
    expect(onAcceptExisting).toHaveBeenCalledWith("material_1");
  });

  it("shows No catalog match and only Create & Add / Reject when there is no candidate", () => {
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    expect(screen.getByText(/no catalog match/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /use existing/i })).not.toBeInTheDocument();
  });

  it("Create & Add prefills only the name, never price or unit", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /create & add/i }));

    const nameInput = screen.getByLabelText(/^name/i) as HTMLInputElement;
    const unitInput = screen.getByLabelText(/unit/i) as HTMLInputElement;
    const priceInput = screen.getByLabelText(/reference price/i) as HTMLInputElement;
    expect(nameInput.value).toBe("Tile Spacers");
    expect(unitInput.value).toBe("");
    expect(priceInput.value).toBe("");
  });

  it("requires unit before submitting the new Material form", async () => {
    const onCreateAndAdd = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={onCreateAndAdd}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /create & add/i }));
    await user.click(screen.getByRole("button", { name: /^add$/i }));

    expect(await screen.findByText(/unit is required/i)).toBeInTheDocument();
    expect(onCreateAndAdd).not.toHaveBeenCalled();
  });

  it("submits the full contractor-controlled Material fields via onCreateAndAdd", async () => {
    const onCreateAndAdd = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={onCreateAndAdd}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /create & add/i }));
    await user.type(screen.getByLabelText(/unit/i), "bag");
    await user.type(screen.getByLabelText(/reference price/i), "5.00");
    await user.click(screen.getByRole("button", { name: /^add$/i }));

    expect(onCreateAndAdd).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Tile Spacers", unit: "bag", referencePriceAmount: 500, referencePriceCurrency: "MYR" })
    );
  });

  it("preserves an explicitly configured non-MYR currency", async () => {
    const onCreateAndAdd = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={onCreateAndAdd}
        onReject={vi.fn()}
        submitting={false}
        currency="EUR"
      />
    );
    await user.click(screen.getByRole("button", { name: /create & add/i }));
    await user.type(screen.getByLabelText(/unit/i), "bag");
    await user.type(screen.getByLabelText(/reference price/i), "5.00");
    await user.click(screen.getByRole("button", { name: /^add$/i }));

    expect(onCreateAndAdd).toHaveBeenCalledWith(
      expect.objectContaining({ referencePriceCurrency: "EUR" })
    );
  });

  it("calls onReject when rejecting", async () => {
    const onReject = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Spacers"
        candidate={null}
        otherMaterials={[]}
        onAcceptExisting={vi.fn()}
        onCreateAndAdd={vi.fn()}
        onReject={onReject}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /reject/i }));
    expect(onReject).toHaveBeenCalled();
  });

  it("Choose Another lets the contractor pick from otherMaterials", async () => {
    const onAcceptExisting = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(
      <MaterialResourceAcceptDialog
        suggestedName="Tile Adhesive"
        candidate={candidateMaterial}
        otherMaterials={[{ id: "material_2", name: "Standard Tile Adhesive" }]}
        onAcceptExisting={onAcceptExisting}
        onCreateAndAdd={vi.fn()}
        onReject={vi.fn()}
        submitting={false}
      />
    );
    await user.click(screen.getByRole("button", { name: /choose another/i }));
    await user.click(screen.getByRole("combobox", { name: /material/i }));
    await user.click(screen.getByRole("option", { name: "Standard Tile Adhesive" }));
    await user.click(screen.getByRole("button", { name: /use selected/i }));

    expect(onAcceptExisting).toHaveBeenCalledWith("material_2");
  });

  it("uses unique field ids when multiple dialogs render on the same page", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <>
        <MaterialResourceAcceptDialog
          suggestedName="Tile Spacers"
          candidate={null}
          otherMaterials={[]}
          onAcceptExisting={vi.fn()}
          onCreateAndAdd={vi.fn()}
          onReject={vi.fn()}
          submitting={false}
        />
        <MaterialResourceAcceptDialog
          suggestedName="Grout Sealant"
          candidate={null}
          otherMaterials={[]}
          onAcceptExisting={vi.fn()}
          onCreateAndAdd={vi.fn()}
          onReject={vi.fn()}
          submitting={false}
        />
      </>
    );
    const createButtons = screen.getAllByRole("button", { name: /create & add/i });
    await user.click(createButtons[0]);
    await user.click(createButtons[1]);

    const nameInputs = screen.getAllByLabelText(/^name/i) as HTMLInputElement[];
    expect(nameInputs).toHaveLength(2);
    const ids = nameInputs.map((el) => el.id);
    expect(new Set(ids).size).toBe(2);
  });
});
