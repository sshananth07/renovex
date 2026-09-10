import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SupplierDetail } from "./SupplierDetail";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const supplier = {
  id: "sup1",
  name: "ABC Materials",
  nameNormalized: "abc materials",
  active: true,
  materialCategories: [],
  revision: 1,
  createdAt: "2026-01-15T00:00:00Z",
  updatedAt: "2026-01-15T00:00:00Z",
};

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/suppliers/:id`, () => HttpResponse.json(supplier)),
    http.get(`${baseUrl}/supplier-offerings`, () => HttpResponse.json({ offerings: [] }))
  );
}

async function openOfferingDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: /add offering/i }));
  return screen.findByRole("dialog");
}

describe("SupplierDetail offering form", () => {
  it("omits price fields entirely when no price is entered", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/supplier-offerings`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "off1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SupplierDetail supplierId="sup1" />);
    const dialog = await openOfferingDialog(user);

    await user.type(dialog.querySelector('input[name="productName"]') as HTMLElement, "Generic Cement");
    await user.click(within(dialog).getByRole("button", { name: /save offering/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ productName: "Generic Cement" });
    });
    const body = capturedBody as unknown as Record<string, unknown>;
    expect(body.indicativePriceAmount).toBeUndefined();
    expect(body.indicativePriceCurrency).toBeUndefined();
    expect(body.indicativePriceAsOf).toBeUndefined();
  });

  it("requires the as-of date and unit once a price is entered, and blocks submission client-side without them", async () => {
    mockBase();
    let requestMade = false;
    server.use(http.post(`${baseUrl}/supplier-offerings`, () => { requestMade = true; return HttpResponse.json({ id: "off1" }); }));
    const user = userEvent.setup();
    renderWithProviders(<SupplierDetail supplierId="sup1" />);
    const dialog = await openOfferingDialog(user);

    await user.type(dialog.querySelector('input[name="productName"]') as HTMLElement, "OPC Cement");
    await user.type(dialog.querySelector('input[name="price"]') as HTMLElement, "100.00");
    // Leave unit blank; priceAsOf is prefilled with today's date by default.
    await user.clear(dialog.querySelector('input[name="priceAsOf"]') as HTMLElement);
    await user.click(within(dialog).getByRole("button", { name: /save offering/i }));

    expect(await within(dialog).findByText(/unit is required when an indicative price is provided/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/price as-of date is required when an indicative price is provided/i)).toBeInTheDocument();
    expect(requestMade).toBe(false);
  });

  it("sends the complete price state (amount, currency, unit, asOf) as an integer minor-unit amount when priced", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/supplier-offerings`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "off1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SupplierDetail supplierId="sup1" />);
    const dialog = await openOfferingDialog(user);

    await user.type(dialog.querySelector('input[name="productName"]') as HTMLElement, "OPC Cement");
    await user.type(dialog.querySelector('input[name="unit"]') as HTMLElement, "bag");
    await user.type(dialog.querySelector('input[name="price"]') as HTMLElement, "100.00");
    await user.click(within(dialog).getByRole("button", { name: /save offering/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({
        productName: "OPC Cement",
        unit: "bag",
        indicativePriceAmount: 10000,
        indicativePriceCurrency: "MYR",
      });
    });
    const body = capturedBody as unknown as Record<string, unknown>;
    expect(typeof body.indicativePriceAsOf).toBe("string");
    expect(typeof body.indicativePriceAmount).toBe("number");
  });

  it("clearing the price also clears the as-of field from the request", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/supplier-offerings`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "off1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SupplierDetail supplierId="sup1" />);
    const dialog = await openOfferingDialog(user);

    await user.type(dialog.querySelector('input[name="productName"]') as HTMLElement, "Generic Cement");
    const priceInput = dialog.querySelector('input[name="price"]') as HTMLElement;
    await user.type(priceInput, "100.00");
    await user.clear(priceInput);
    await user.click(within(dialog).getByRole("button", { name: /save offering/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ productName: "Generic Cement" });
    });
    expect((capturedBody as unknown as Record<string, unknown>).indicativePriceAsOf).toBeUndefined();
  });

  it("maps a body.indicativePriceAsOf 422 error from the backend onto the price-as-of field", async () => {
    mockBase();
    server.use(
      http.post(`${baseUrl}/supplier-offerings`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 422,
            title: "Unprocessable Entity",
            errors: [{ location: "body.indicativePriceAsOf", message: "price as-of date is required when an indicative price is provided" }],
          },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<SupplierDetail supplierId="sup1" />);
    const dialog = await openOfferingDialog(user);

    await user.type(dialog.querySelector('input[name="productName"]') as HTMLElement, "OPC Cement");
    await user.type(dialog.querySelector('input[name="unit"]') as HTMLElement, "bag");
    await user.type(dialog.querySelector('input[name="price"]') as HTMLElement, "100.00");
    await user.click(within(dialog).getByRole("button", { name: /save offering/i }));

    expect(await within(dialog).findByText(/price as-of date is required when an indicative price is provided/i)).toBeInTheDocument();
  });
});
