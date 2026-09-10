import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { CostsView } from "./CostsView";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const cementMaterial = { id: "mat-cement", name: "Cement", unit: "bag", createdAt: "2026-01-01T00:00:00Z" };
const floorTilingWorkItem = { id: "wi1", projectId: "proj1", description: "Floor tiling", status: "in_progress", quantityValue: "1", quantityUnit: "unit", source: "manual", createdAt: "2026-01-01T00:00:00Z" };

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [] })),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [cementMaterial] })),
    http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [floorTilingWorkItem], page: 1, pageSize: 100, total: 1 }))
  );
}

describe("CostsView create cost item form", () => {
  it("sends the transport value 'material' when the user selects the Material label", async () => {
    mockBase();
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/cost-items`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ id: "ci1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /add cost item/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getAllByRole("textbox")[0], "Tiles");
    await user.click(within(dialog).getByRole("button", { name: /create cost item/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ category: "material" });
    });
  });

  it("links a material and quantity, auto-fills the unit from the catalog, and submits materialId/quantityValue/quantityUnit/unitPriceAmount as separate fields", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/cost-items`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "ci1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /add cost item/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getAllByRole("textbox")[0], "Ceramic Tiles");
    const [, workItemCombobox, materialCombobox] = within(dialog).getAllByRole("combobox");
    await user.click(workItemCombobox);
    await user.click(await screen.findByRole("option", { name: /floor tiling/i }));
    await user.click(materialCombobox);
    await user.click(await screen.findByRole("option", { name: /cement/i }));

    expect(within(dialog).getByLabelText(/^unit$/i)).toHaveValue("bag");

    await user.type(within(dialog).getByLabelText(/^quantity$/i), "12");
    await user.type(within(dialog).getByLabelText(/unit price/i), "45.00");
    await user.click(within(dialog).getByRole("button", { name: /create cost item/i }));

    await waitFor(() => expect(capturedBody).toMatchObject({
      materialId: "mat-cement",
      quantityValue: "12",
      quantityUnit: "bag",
      unitPriceAmount: 4500,
      workItemId: "wi1",
    }));
    expect(capturedBody).not.toMatchObject({ quantityUnit: "12 bag" });
  });

  it("does not require material linkage — a cost item with no material fields submits without them", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/cost-items`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "ci1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /add cost item/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getAllByRole("textbox")[0], "Site supervision");
    await user.click(within(dialog).getByRole("button", { name: /create cost item/i }));

    await waitFor(() => expect(capturedBody).toMatchObject({ description: "Site supervision" }));
    expect(capturedBody).not.toHaveProperty("materialId");
    expect(capturedBody).not.toHaveProperty("quantityValue");
    expect(capturedBody).not.toHaveProperty("quantityUnit");
    expect(capturedBody).not.toHaveProperty("unitPriceAmount");
  });

  it("does not offer Labour as a selectable category", async () => {
    mockBase();
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /add cost item/i }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getAllByRole("combobox")[0]);

    expect(screen.queryByRole("option", { name: /^labour$/i })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: /^material$/i })).toBeInTheDocument();
  });

  it("maps a body.category 422 error onto the category field", async () => {
    mockBase();
    server.use(
      http.post(`${baseUrl}/cost-items`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 422,
            title: "Unprocessable Entity",
            detail: "Invalid cost category.",
            errors: [{ location: "body.category", message: "Invalid cost category." }],
          },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /add cost item/i }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getAllByRole("textbox")[0], "Tiles");
    await user.click(within(dialog).getByRole("button", { name: /create cost item/i }));

    expect(await within(dialog).findByText(/invalid cost category/i)).toBeInTheDocument();
  });
});

const cementCostItem = {
  id: "ci1",
  projectId: "proj1",
  category: "material",
  description: "Cement",
  currency: "MYR",
  date: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  revision: 0,
  estimated: { amount: 50000, currency: "MYR" },
};

const cementCostItemWithActual = {
  ...cementCostItem,
  revision: 1,
  actual: { amount: 850000, currency: "MYR" },
};

describe("CostsView cost detail drawer", () => {
  it("clicking a row opens the drawer with existing values, and Edit on Estimated saves a new amount", async () => {
    mockBase();
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [cementCostItem] })));
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.patch(`${baseUrl}/cost-items/ci1/lifecycle`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...cementCostItem, revision: 1, estimated: { amount: 60000, currency: "MYR" } });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByText("Cement")).toBeInTheDocument();

    await user.click(within(drawer).getAllByRole("button", { name: /^edit$/i })[0]);
    const amountInput = within(drawer).getByLabelText(/amount \(myr\)/i);
    await user.clear(amountInput);
    await user.type(amountInput, "600.00");
    await user.click(within(drawer).getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ stage: "estimated", amount: 60000, expectedRevision: 0 });
    });
  });

  it("pressing Enter on a focused row opens the drawer", async () => {
    mockBase();
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [cementCostItem] })));
    renderWithProviders(<CostsView projectId="proj1" />);

    const row = await screen.findByRole("button", { name: /view cost item cement/i });
    row.focus();
    await userEvent.setup().keyboard("{Enter}");

    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByText("Cement")).toBeInTheDocument();
  });

  it("Actual shows Record actual (not Correct) when Actual is nil, and the Record form has no reason field", async () => {
    mockBase();
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [cementCostItem] })));
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    const recordButton = within(drawer).getByRole("button", { name: /record actual/i });
    expect(recordButton).toBeInTheDocument();
    expect(within(drawer).queryByRole("button", { name: /^correct$/i })).not.toBeInTheDocument();

    await user.click(recordButton);
    expect(within(drawer).getByLabelText(/amount \(myr\)/i)).toBeInTheDocument();
    expect(within(drawer).queryByLabelText(/reason/i)).not.toBeInTheDocument();
  });

  it("Actual shows Correct (not Record actual) when Actual is already set", async () => {
    mockBase();
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [cementCostItemWithActual] })));
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByRole("button", { name: /^correct$/i })).toBeInTheDocument();
    expect(within(drawer).queryByRole("button", { name: /record actual/i })).not.toBeInTheDocument();
  });

  it("Correct on Actual requires a non-blank reason before the confirmation dialog appears", async () => {
    mockBase();
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [cementCostItemWithActual] })));
    let correctRequestSent = false;
    server.use(
      http.post(`${baseUrl}/cost-items/ci1/correct-actual`, () => {
        correctRequestSent = true;
        return HttpResponse.json(cementCostItemWithActual);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    await user.click(within(drawer).getByRole("button", { name: /^correct$/i }));
    const amountInput = within(drawer).getByLabelText(/corrected amount/i);
    await user.clear(amountInput);
    await user.type(amountInput, "85000.00");
    await user.click(within(drawer).getByRole("button", { name: /review correction/i }));

    expect(await within(drawer).findByText(/a reason is required/i)).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: /confirm correction/i })).not.toBeInTheDocument();
    expect(correctRequestSent).toBe(false);
  });

  it("Correct on Actual shows a confirmation dialog stating the previous and new amount before submitting", async () => {
    mockBase();
    let currentCostItem: Record<string, unknown> = cementCostItemWithActual;
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [currentCostItem] })));
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/cost-items/ci1/correct-actual`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        currentCostItem = {
          ...cementCostItemWithActual,
          revision: 2,
          actual: { amount: 85000, currency: "MYR" },
          actualCorrections: [{
            previousAmount: { amount: 850000, currency: "MYR" },
            newAmount: { amount: 85000, currency: "MYR" },
            reason: "Decimal point error",
            correctedByUser: "user_1",
            correctedAt: "2026-01-02T00:00:00Z",
          }],
        };
        return HttpResponse.json(currentCostItem);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    await user.click(within(drawer).getByRole("button", { name: /^correct$/i }));
    const amountInput = within(drawer).getByLabelText(/corrected amount/i);
    await user.clear(amountInput);
    await user.type(amountInput, "850.00");
    await user.type(within(drawer).getByLabelText(/reason/i), "Decimal point error");
    await user.click(within(drawer).getByRole("button", { name: /review correction/i }));

    const confirmDialogTitle = await screen.findByRole("heading", { name: /confirm correction/i });
    expect(confirmDialogTitle).toBeInTheDocument();
    expect(capturedBody).toBeNull();

    await user.click(screen.getByRole("button", { name: /confirm correction/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ expectedRevision: 1, reason: "Decimal point error" });
    });
    expect(await screen.findByText(/decimal point error/i)).toBeInTheDocument();
  });

  it("Paid shows a View-only explanation with no Edit/Correct action", async () => {
    mockBase();
    const paidCostItem = { ...cementCostItem, paid: { amount: 500000, currency: "MYR" } };
    server.use(http.get(`${baseUrl}/cost-items`, () => HttpResponse.json({ costItems: [paidCostItem] })));
    const user = userEvent.setup();
    renderWithProviders(<CostsView projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /view cost item cement/i }));
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByText(/view only/i)).toBeInTheDocument();
  });
});
