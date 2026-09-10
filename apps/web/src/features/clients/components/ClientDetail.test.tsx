import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ClientDetail } from "./ClientDetail";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const client = {
  id: "c1",
  name: "Ahmad Residence",
  email: "ahmad@example.com",
  phone: "0123456789",
  address: "12 Jalan Damansara",
  billingAddress: "Accounts, 12 Jalan Damansara",
  notes: "Prefers WhatsApp",
  createdAt: "2026-01-15T00:00:00Z",
};

function mockClient(overrides: Partial<typeof client> = {}) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/clients/:id`, () => HttpResponse.json({ ...client, ...overrides })),
    http.get(`${baseUrl}/projects`, () => HttpResponse.json({ items: [], page: 1, pageSize: 100, total: 0 }))
  );
}

describe("ClientDetail", () => {
  it("renders a read-first summary with friendly fallbacks and no Client status", async () => {
    mockClient({ email: "", phone: "", address: "", notes: "" });
    renderWithProviders(<ClientDetail clientId="c1" />);

    expect(await screen.findByRole("heading", { name: "Ahmad Residence", level: 1 })).toBeInTheDocument();
    expect(screen.getAllByText("Not provided")).toHaveLength(3);
    expect(screen.getByText("No notes provided")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Client" })).toBeInTheDocument();
    expect(screen.queryByLabelText(/phone/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Lead")).not.toBeInTheDocument();
  });

  it("sends a PATCH containing only the changed field from the Edit dialog", async () => {
    mockClient();
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/clients/:id`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ ...client, phone: "0199999999" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientDetail clientId="c1" />);

    await user.click(await screen.findByRole("button", { name: "Edit Client" }));
    const dialog = await screen.findByRole("dialog");
    const phoneInput = within(dialog).getByLabelText(/phone/i);
    await user.clear(phoneInput);
    await user.type(phoneInput, "0199999999");
    await user.click(within(dialog).getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(capturedBody).toEqual({ phone: "0199999999" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("sends an explicit empty string to clear a field", async () => {
    mockClient();
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/clients/:id`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ ...client, notes: undefined });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientDetail clientId="c1" />);

    await user.click(await screen.findByRole("button", { name: "Edit Client" }));
    const dialog = await screen.findByRole("dialog");
    await user.clear(within(dialog).getByLabelText(/notes/i));
    await user.click(within(dialog).getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(capturedBody).toEqual({ notes: "" }));
  });

  it("keeps failed edits open and shows the backend message", async () => {
    mockClient();
    server.use(
      http.patch(`${baseUrl}/clients/:id`, () => HttpResponse.json(
        { title: "Unprocessable Entity", status: 422, detail: "Email is already used by another Client." },
        { status: 422 }
      ))
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientDetail clientId="c1" />);

    await user.click(await screen.findByRole("button", { name: "Edit Client" }));
    const dialog = await screen.findByRole("dialog");
    const email = within(dialog).getByLabelText(/email/i);
    await user.clear(email);
    await user.type(email, "duplicate@example.com");
    await user.click(within(dialog).getByRole("button", { name: /^save$/i }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Email is already used by another Client.");
    expect(email).toHaveValue("duplicate@example.com");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("renders associated Projects as compact authoritative rows", async () => {
    mockClient();
    server.use(
      http.get(`${baseUrl}/projects`, () => HttpResponse.json({
        items: [{ id: "p1", clientId: "c1", name: "Kitchen Renovation", status: "in_progress", createdAt: "2026-02-01T00:00:00Z" }],
        page: 1,
        pageSize: 100,
        total: 1,
      })),
      http.get(`${baseUrl}/quotations`, () => HttpResponse.json({ quotations: [{ id: "q1", projectId: "p1", quotationNumber: "QT-001", status: "finalized", createdAt: "2026-02-05T00:00:00Z" }] })),
      http.get(`${baseUrl}/quotations/:id/share`, () => HttpResponse.json({ effectiveStatus: "active", decision: { status: "accepted" } }))
    );
    renderWithProviders(<ClientDetail clientId="c1" />);

    expect(await screen.findByText("Kitchen Renovation")).toBeInTheDocument();
    expect(screen.getByText("In Progress")).toBeInTheDocument();
    expect(await screen.findByText(/QT-001/)).toBeInTheDocument();
    expect(await screen.findByText(/Client response: accepted/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /open project/i })).toHaveAttribute("href", "/projects/p1");
    expect(screen.queryByText(/last activity/i)).not.toBeInTheDocument();
  });

  it("creates a new Project already associated with the current Client", async () => {
    mockClient();
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/projects`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ id: "p2", clientId: "c1", name: "Bathroom Upgrade", status: "lead", createdAt: "2026-02-10T00:00:00Z" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientDetail clientId="c1" />);

    await user.click(await screen.findByRole("button", { name: /new project/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/^name$/i), "Bathroom Upgrade");
    await user.click(within(dialog).getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(capturedBody).toEqual({ clientId: "c1", name: "Bathroom Upgrade" }));
  });
});
