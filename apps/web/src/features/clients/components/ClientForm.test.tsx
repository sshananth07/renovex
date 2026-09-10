import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ClientForm } from "./ClientForm";

describe("ClientForm", () => {
  it("shows a validation error and does not submit when name is empty", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<ClientForm onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/name is required/i)).toBeInTheDocument();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits exactly the fields the user filled in", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<ClientForm onSubmit={onSubmit} />);

    await user.type(screen.getByLabelText(/^name/i), "Ahmad Residence");
    await user.type(screen.getByLabelText(/^email/i), "ahmad@example.com");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Ahmad Residence", email: "ahmad@example.com" }),
      expect.anything()
    );
  });

  it("prefills fields when editing an existing client", () => {
    renderWithProviders(
      <ClientForm
        onSubmit={vi.fn()}
        defaultValues={{ name: "Ahmad Residence", email: "ahmad@example.com" }}
      />
    );

    expect(screen.getByLabelText(/^name/i)).toHaveValue("Ahmad Residence");
    expect(screen.getByLabelText(/^email/i)).toHaveValue("ahmad@example.com");
  });
});
