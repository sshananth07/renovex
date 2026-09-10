import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SpaceForm } from "./SpaceForm";

describe("SpaceForm", () => {
  it("shows a validation error and does not submit when name is empty", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<SpaceForm onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/name is required/i)).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits name, type, and description", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<SpaceForm onSubmit={onSubmit} />);

    await user.type(screen.getByLabelText(/name/i), "Kitchen");
    await user.type(screen.getByLabelText(/type/i), "Kitchen");
    await user.type(screen.getByLabelText(/description/i), "Main kitchen");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Kitchen", type: "Kitchen", description: "Main kitchen" }),
      expect.anything()
    );
  });

  it("prefills fields when editing an existing space", () => {
    renderWithProviders(
      <SpaceForm onSubmit={vi.fn()} defaultValues={{ name: "Kitchen", type: "Kitchen", description: "" }} />
    );

    expect(screen.getByLabelText(/name/i)).toHaveValue("Kitchen");
    expect(screen.getByLabelText(/type/i)).toHaveValue("Kitchen");
  });
});
