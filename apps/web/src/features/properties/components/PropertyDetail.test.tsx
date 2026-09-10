import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { PropertyDetail } from "./PropertyDetail";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function mockAuthBootstrap() {
  server.use(http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })));
}

describe("PropertyDetail", () => {
  it("shows a meaningful empty state with a create CTA when no Property exists", async () => {
    mockAuthBootstrap();
    server.use(http.get(`${baseUrl}/properties`, () => HttpResponse.json({ properties: [] })));

    renderWithProviders(<PropertyDetail projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /add property/i })).toBeInTheDocument();
    });
    expect(screen.queryByLabelText(/address/i)).toBeNull();
  });

  it("shows the address, property type, and notes once a Property exists", async () => {
    mockAuthBootstrap();
    server.use(
      http.get(`${baseUrl}/properties`, () =>
        HttpResponse.json({
          properties: [
            {
              id: "prop1",
              projectId: "proj1",
              address: "1 Jalan Ampang, Kuala Lumpur",
              propertyType: "Landed",
              notes: "Gated community, security clearance needed",
              createdAt: "2026-01-16T00:00:00Z",
            },
          ],
        })
      )
    );

    renderWithProviders(<PropertyDetail projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByText("1 Jalan Ampang, Kuala Lumpur")).toBeInTheDocument();
    });
    expect(screen.getByText("Landed")).toBeInTheDocument();
    expect(screen.getByText("Gated community, security clearance needed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /edit/i })).toBeInTheDocument();
  });

  it("always resubmits address even when only propertyType changed, matching the non-partial PATCH contract", async () => {
    mockAuthBootstrap();
    server.use(
      http.get(`${baseUrl}/properties`, () =>
        HttpResponse.json({
          properties: [
            {
              id: "prop1",
              projectId: "proj1",
              address: "1 Jalan Ampang, Kuala Lumpur",
              propertyType: "Landed",
              createdAt: "2026-01-16T00:00:00Z",
            },
          ],
        })
      )
    );
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/properties/:id`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({
          id: "prop1",
          projectId: "proj1",
          address: "1 Jalan Ampang, Kuala Lumpur",
          propertyType: "Condominium",
          createdAt: "2026-01-16T00:00:00Z",
        });
      })
    );

    const user = userEvent.setup();
    renderWithProviders(<PropertyDetail projectId="proj1" />);

    await waitFor(() => {
      expect(screen.getByText("1 Jalan Ampang, Kuala Lumpur")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /edit/i }));
    const typeInput = await screen.findByLabelText(/property type/i);
    await user.clear(typeInput);
    await user.type(typeInput, "Condominium");
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(capturedBody).toEqual(
        expect.objectContaining({ address: "1 Jalan Ampang, Kuala Lumpur", propertyType: "Condominium" })
      );
    });
  });
});
