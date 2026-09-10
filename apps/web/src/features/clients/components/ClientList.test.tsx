import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ClientList } from "./ClientList";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const setParamsMock = vi.fn();
const currentParams = { page: 1, pageSize: 25 };

vi.mock("@/lib/url/listParams", () => ({
  useListSearchParams: () => ({ params: currentParams, setParams: setParamsMock }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

function mockClientsList(query: (url: URL) => boolean = () => true) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/clients`, ({ request }) => {
      const url = new URL(request.url);
      if (!query(url)) return HttpResponse.json({ items: [], page: 1, pageSize: 25, total: 0 });
      return HttpResponse.json({
        items: [
          {
            id: "c1",
            name: "Ahmad Residence",
            email: "ahmad@example.com",
            createdAt: "2026-01-15T00:00:00Z",
          },
        ],
        page: currentParams.page,
        pageSize: 25,
        total: 1,
      });
    })
  );
}

describe("ClientList", () => {
  it("renders rows from the canonical paginated envelope", async () => {
    mockClientsList();
    renderWithProviders(<ClientList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    });
  });

  it("updates the URL search param when the search box changes", async () => {
    mockClientsList();
    const user = userEvent.setup();
    renderWithProviders(<ClientList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    });

    await user.type(screen.getByPlaceholderText(/search/i), "kitchen");

    await waitFor(
      () => {
        expect(setParamsMock).toHaveBeenCalledWith(
          expect.objectContaining({ search: "kitchen" })
        );
      },
      { timeout: 2000 }
    );
  });

  it("opens a create dialog and refreshes the list after a successful create", async () => {
    mockClientsList();
    server.use(
      http.post(`${baseUrl}/clients`, async ({ request }) => {
        const body = (await request.json()) as { name: string };
        return HttpResponse.json(
          { id: "c2", name: body.name, createdAt: "2026-02-01T00:00:00Z" },
          { status: 201 }
        );
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /add client/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/^name/i), "New Client");
    await user.click(within(dialog).getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("sends address, billing address, and notes in the create request body — every visible field survives submission", async () => {
    mockClientsList();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/clients`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { id: "c3", name: "New Client", createdAt: "2026-02-01T00:00:00Z" },
          { status: 201 }
        );
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ClientList />);

    await waitFor(() => {
      expect(screen.getByText("Ahmad Residence")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /add client/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(/^name/i), "New Client");
    await user.type(within(dialog).getByLabelText(/^address/i), "12 Jalan Ampang");
    await user.type(within(dialog).getByLabelText(/billing address/i), "PO Box 100");
    await user.type(within(dialog).getByLabelText(/notes/i), "Prefers WhatsApp");
    await user.click(within(dialog).getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({
        name: "New Client",
        address: "12 Jalan Ampang",
        billingAddress: "PO Box 100",
        notes: "Prefers WhatsApp",
      });
    });
  });
});
