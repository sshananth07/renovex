import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { QuotationsView } from "./QuotationsView";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const money = (amount: number) => ({ amount, currency: "MYR" });

const quotation = {
  id: "quo1",
  projectId: "proj1",
  clientId: "client1",
  estimateId: "est1",
  quotationNumber: "QT-000001",
  status: "finalized",
  version: 1,
  revision: 2,
  currency: "MYR",
  generatedSubtotal: money(100000),
  subtotal: money(100000),
  taxAmount: money(0),
  total: money(100000),
  lines: [],
  createdAt: "2026-01-01T00:00:00Z",
};

const client = {
  id: "client1",
  name: "Priya Nair",
  createdAt: "2026-01-01T00:00:00Z",
};

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/quotations`, () => HttpResponse.json({ quotations: [quotation] })),
    http.get(`${baseUrl}/estimates`, () => HttpResponse.json({ estimates: [] })),
    http.get(`${baseUrl}/clients/:id`, () => HttpResponse.json(client))
  );
}

describe("QuotationsView client label resolution", () => {
  it("resolves clientId to the client's name rather than showing the raw id", async () => {
    mockBase();
    renderWithProviders(<QuotationsView projectId="proj1" />);

    expect(await screen.findByText(/client priya nair/i)).toBeInTheDocument();
    expect(screen.queryByText(/client client1/i)).not.toBeInTheDocument();
  });

  it("falls back to showing the id while the client name is still loading", async () => {
    mockBase();
    server.use(
      http.get(`${baseUrl}/clients/:id`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json(client);
      })
    );
    renderWithProviders(<QuotationsView projectId="proj1" />);

    expect(await screen.findByText(/client client1/i)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText(/client priya nair/i)).toBeInTheDocument());
  });
});
