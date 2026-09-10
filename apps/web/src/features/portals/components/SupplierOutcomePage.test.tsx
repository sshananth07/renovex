import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { SupplierOutcomePage } from "./SupplierOutcomePage";

const replaceMock = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
}));

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const selectedOutcome = {
  id: "out-1",
  result: "selected",
  rfqNumber: "RFQ-000001",
  rfqTitle: "Taman Tun Condo Refresh",
  awardedLines: [
    {
      issuedRfqLineId: "line-1",
      materialName: "Cement",
      quantityValue: "25",
      quantityUnit: "bags",
      unitPriceMinorUnits: 2280,
      lineSubtotalMinorUnits: 57000,
      lineTaxMinorUnits: 0,
    },
    {
      issuedRfqLineId: "line-2",
      materialName: "Sand",
      quantityValue: "2.5",
      quantityUnit: "tonnes",
      unitPriceMinorUnits: 7900,
      lineSubtotalMinorUnits: 19750,
      lineTaxMinorUnits: 0,
    },
  ],
  lineSubtotalMinorUnits: 76750,
  taxTotalMinorUnits: 0,
  chargeTotalMinorUnits: 0,
  deliveryChargeMinorUnits: 0,
  awardTotalMinorUnits: 76750,
  currency: "MYR",
  createdAt: "2026-08-23T04:19:00Z",
};

const unsuccessfulOutcome = {
  id: "out-2",
  result: "unsuccessful",
  rfqNumber: "RFQ-000002",
  rfqTitle: "Porcelain Tile Package",
  awardedLines: [],
  lineSubtotalMinorUnits: 0,
  taxTotalMinorUnits: 0,
  chargeTotalMinorUnits: 0,
  deliveryChargeMinorUnits: 0,
  awardTotalMinorUnits: 0,
  currency: "MYR",
  createdAt: "2026-08-23T04:19:00Z",
};

function mockSessionOk() {
  server.use(
    http.get(`${baseUrl}/supplier-access/session`, () => HttpResponse.json({ invitationId: "inv-1" })),
  );
}

function mockSessionMissing() {
  server.use(
    http.get(`${baseUrl}/supplier-access/session`, () =>
      HttpResponse.json({ type: "about:blank", status: 401, title: "Unauthorized" }, { status: 401 })),
  );
}

function mockOutcome(outcome: typeof selectedOutcome | typeof unsuccessfulOutcome) {
  server.use(
    http.get(`${baseUrl}/supplier-access/outcomes/:outcomeId`, () => HttpResponse.json(outcome)),
  );
}

describe("SupplierOutcomePage session gating", () => {
  it("redirects to the Supplier Access gateway with a safe returnTo when no session exists", async () => {
    mockSessionMissing();
    replaceMock.mockClear();

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);

    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith(
      "/supplier-access/open?returnTo=%2Fsupplier-access%2Finvitations%2Finv-1%2Foutcomes%2Fout-1",
    ));
  });

  it("loads the outcome once a session already exists", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);

    expect(await screen.findByText("Taman Tun Condo Refresh")).toBeInTheDocument();
  });
});

describe("SupplierOutcomePage selected outcome", () => {
  it("shows Selected status, awarded items, and the awarded total", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);

    expect(await screen.findByText("Selected")).toBeInTheDocument();
    expect(screen.getByText("Cement")).toBeInTheDocument();
    expect(screen.getByText("Sand")).toBeInTheDocument();
    expect(screen.getByText(/RM\s*570\.00/)).toBeInTheDocument();
    expect(screen.getByText(/RM\s*197\.50/)).toBeInTheDocument();
    expect(screen.getByText(/RM\s*767\.50/)).toBeInTheDocument();
  });

  it("never renders acceptance/commitment terminology", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);
    await screen.findByText("Selected");

    const forbidden = [/accept award/i, /accept contract/i, /accept purchase order/i,
      /confirm order/i, /commit supply/i, /sign agreement/i];
    for (const pattern of forbidden) {
      expect(screen.queryByText(pattern)).not.toBeInTheDocument();
    }
  });

  it("shows the sourcing-decision disclaimer with the required exact wording", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);
    await screen.findByText("Selected");

    expect(screen.getByText(
      "Acknowledgement confirms receipt of this sourcing outcome only. " +
      "It does not constitute acceptance of a Purchase Order or contractual commitment.",
    )).toBeInTheDocument();
  });

  it("does not prominently expose the raw outcome or invitation IDs", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);
    await screen.findByText("Selected");

    expect(screen.queryByText("out-1")).not.toBeInTheDocument();
    expect(screen.queryByText("inv-1")).not.toBeInTheDocument();
  });
});

describe("SupplierOutcomePage unsuccessful outcome", () => {
  it("shows Not selected status and no awarded items table", async () => {
    mockSessionOk();
    mockOutcome(unsuccessfulOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-2" />);

    expect(await screen.findByText("Not selected")).toBeInTheDocument();
    expect(screen.getByText(/your quotation was not selected for this request/i)).toBeInTheDocument();
    expect(screen.queryByText("Awarded items")).not.toBeInTheDocument();
    expect(screen.queryByText("Awarded total")).not.toBeInTheDocument();
  });
});

describe("SupplierOutcomePage acknowledgement", () => {
  it("shows an Acknowledge receipt button before acknowledgement", async () => {
    mockSessionOk();
    mockOutcome(selectedOutcome);

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);

    expect(await screen.findByRole("button", { name: /acknowledge receipt/i })).toBeInTheDocument();
  });

  it("calls the existing acknowledgement endpoint and shows the acknowledged state on success", async () => {
    const user = userEvent.setup();
    mockSessionOk();
    mockOutcome(selectedOutcome);
    let acknowledgeCalled = false;
    server.use(
      http.post(`${baseUrl}/supplier-access/outcomes/:outcomeId/acknowledgements`, () => {
        acknowledgeCalled = true;
        return HttpResponse.json({
          outcomeId: "out-1",
          acknowledgedAt: "2026-08-25T10:00:00Z",
          meaning: "Acknowledgement confirms receipt only.",
        }, { status: 201 });
      }),
    );

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);
    await user.click(await screen.findByRole("button", { name: /acknowledge receipt/i }));

    await waitFor(() => expect(screen.getByText(/acknowledged/i)).toBeInTheDocument());
    expect(acknowledgeCalled).toBe(true);
    expect(screen.queryByRole("button", { name: /acknowledge receipt/i })).not.toBeInTheDocument();
  });

  it("shows an error and keeps the button available when acknowledgement fails", async () => {
    const user = userEvent.setup();
    mockSessionOk();
    mockOutcome(selectedOutcome);
    server.use(
      http.post(`${baseUrl}/supplier-access/outcomes/:outcomeId/acknowledgements`, () =>
        HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 })),
    );

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="out-1" />);
    await user.click(await screen.findByRole("button", { name: /acknowledge receipt/i }));

    expect(await screen.findByText(/could not be recorded/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /acknowledge receipt/i })).toBeInTheDocument();
  });
});

describe("SupplierOutcomePage invalid/foreign outcome", () => {
  it("shows a safe not-found state instead of leaking a competitor's outcome", async () => {
    mockSessionOk();
    server.use(
      http.get(`${baseUrl}/supplier-access/outcomes/:outcomeId`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })),
    );

    render(<SupplierOutcomePage invitationId="inv-1" outcomeId="foreign-outcome" />);

    expect(await screen.findByText(/outcome unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText("Selected")).not.toBeInTheDocument();
    expect(screen.queryByText("Not selected")).not.toBeInTheDocument();
  });
});
