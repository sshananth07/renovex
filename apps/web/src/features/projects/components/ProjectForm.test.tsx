import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectForm } from "./ProjectForm";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const clients = [
  { id: "c1", name: "Ahmad Residence", createdAt: "2026-01-01T00:00:00Z" },
  { id: "c2", name: "Lim Family Home", createdAt: "2026-01-02T00:00:00Z" },
];

function mockAuthBootstrap() {
  server.use(http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })));
}

describe("ProjectForm", () => {
  it("requires selecting a client and entering a name", async () => {
    mockAuthBootstrap();
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<ProjectForm clients={clients} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: /save/i }));

    const alerts = await screen.findAllByRole("alert");
    expect(alerts.map((el) => el.textContent)).toEqual(
      expect.arrayContaining([expect.stringMatching(/select a client/i), "Name is required"])
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits the selected clientId and entered name", async () => {
    mockAuthBootstrap();
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<ProjectForm clients={clients} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("combobox", { name: /client/i }));
    await user.click(await screen.findByRole("option", { name: "Lim Family Home" }));
    await user.type(screen.getByLabelText(/^name/i), "Lim Kitchen Reno");
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ clientId: "c2", name: "Lim Kitchen Reno" }),
        expect.anything()
      );
    });
  });

  it("submits a supplied default Client without requiring reselection", async () => {
    mockAuthBootstrap();
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    renderWithProviders(<ProjectForm clients={[clients[0]]} defaultClientId="c1" onSubmit={onSubmit} />);

    expect(screen.getByRole("combobox", { name: /client/i })).toHaveTextContent("Ahmad Residence");
    await user.type(screen.getByLabelText(/^name/i), "Bathroom Upgrade");
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ clientId: "c1", name: "Bathroom Upgrade" }),
      expect.anything()
    ));
  });
});
