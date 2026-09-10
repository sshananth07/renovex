import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { PropertyForm } from "./PropertyForm";

describe("PropertyForm", () => {
  it("shows a validation error and does not submit when address is empty", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<PropertyForm onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/address is required/i)).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits address, propertyType, and notes", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<PropertyForm onSubmit={onSubmit} />);

    await user.type(screen.getByLabelText(/address/i), "1 Jalan Ampang, KL");
    await user.type(screen.getByLabelText(/property type/i), "Landed");
    await user.type(screen.getByLabelText(/notes/i), "Gated community");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        address: "1 Jalan Ampang, KL",
        propertyType: "Landed",
        notes: "Gated community",
      }),
      expect.anything()
    );
  });

  it("prefills fields when editing an existing property", () => {
    renderWithProviders(
      <PropertyForm
        onSubmit={vi.fn()}
        defaultValues={{ address: "1 Jalan Ampang, KL", propertyType: "Landed", notes: "" }}
      />
    );

    expect(screen.getByLabelText(/address/i)).toHaveValue("1 Jalan Ampang, KL");
    expect(screen.getByLabelText(/property type/i)).toHaveValue("Landed");
  });
});
