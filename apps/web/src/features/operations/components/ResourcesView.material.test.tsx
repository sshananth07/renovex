import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ResourcesView } from "./ResourcesView";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function mockBase(materials: unknown[] = []) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials })),
    http.get(`${baseUrl}/workers`, () => HttpResponse.json({ workers: [] })),
    http.get(`${baseUrl}/labour-entries`, () => HttpResponse.json({ labourEntries: [] })),
    http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 100, total: 0 }))
  );
}

// The Material form's Field wrapper previously rendered a bare <Label> as a
// sibling of its input with no htmlFor/id association, so Testing
// Library's/Playwright's getByLabel could never find these fields —
// discovered via the M8.5B-A E2E (Task 17), which timed out indefinitely on
// materialDialog.getByLabel("Name") against the real form.
describe("ResourcesView Material form field labeling", () => {
  it("associates the Name/Unit/Reference price/Category/Specification labels with their inputs", async () => {
    mockBase();
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="p1" />);

    await user.click(await screen.findByRole("button", { name: "Add material" }));
    const dialog = screen.getByRole("dialog");

    await user.type(within(dialog).getByLabelText("Name"), "Adhesive Compound");
    await user.type(within(dialog).getByLabelText("Unit"), "bag");
    await user.type(within(dialog).getByLabelText("Reference price (MYR)"), "8.50");
    await user.type(within(dialog).getByLabelText("Category"), "Adhesives");
    await user.type(within(dialog).getByLabelText("Specification"), "High bond strength");

    expect(within(dialog).getByLabelText("Name")).toHaveValue("Adhesive Compound");
  });

  it("renders persisted MYR material prices as RM and preserves MYR on manual save", async () => {
    const material = {
      id: "material_ai", name: "AI Tile Spacers", category: "tile", specification: "2mm", unit: "bag",
      referencePriceAmount: 9000, referencePriceCurrency: "MYR",
      referencePriceAsOf: "2026-09-11T00:00:00Z", createdAt: "2026-09-11T00:00:00Z",
    };
    let savedBody: unknown = null;
    mockBase([material]);
    server.use(
      http.patch(`${baseUrl}/materials/:id`, async ({ request }) => {
        savedBody = await request.json();
        return HttpResponse.json(material);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="p1" />);

    const materialName = await screen.findByText("AI Tile Spacers");
    const materialRow = materialName.closest("tr");
    expect(materialRow).not.toBeNull();
    expect(within(materialRow!).getByText(/RM\s+90\.00/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Edit AI Tile Spacers" }));
    await user.click(screen.getByRole("button", { name: "Save material" }));

    await waitFor(() => {
      expect(savedBody).toMatchObject({ referencePriceAmount: 9000, referencePriceCurrency: "MYR" });
    });
  });
});
