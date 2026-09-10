import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { SupplierRFQPortal } from "./SupplierRFQPortal";

const replaceMock = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
}));

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const invitation = {
  invitationId: "inv-1",
  status: "sent",
  expiresAt: "2026-12-01T00:00:00Z",
  currentRfqVersion: {
    id: "version-1",
    rfqNumber: "RFQ-000001",
    versionNumber: 1,
    currency: "MYR",
    title: "Test RFQ",
    deliveryAddress: "12 Site Road",
    responseDeadline: "2026-09-01T00:00:00Z",
    supplierInstructions: "",
    lines: [],
  },
  responseWindow: { status: "open", deadline: "2026-09-01T00:00:00Z", canRespond: true },
};

const draft = {
  id: "draft-1",
  revision: 0,
  status: "draft",
  currency: "MYR",
  lines: [],
};

// Does not stub the verify endpoint itself — callers provide their own
// verify handler so that server.use()'s most-recently-added-wins ordering
// can't silently override a test's intentional verify-response sequence.
function mockBootstrapEndpoints() {
  server.use(
    http.get(`${baseUrl}/supplier-access/session`, () => HttpResponse.json({ invitationId: "inv-1" })),
    http.get(`${baseUrl}/supplier-access/invitations/:invitationId`, () => HttpResponse.json(invitation)),
    http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, () => HttpResponse.json(draft)),
    http.get(`${baseUrl}/supplier-access/invitations/:invitationId/offer/versions`, () => HttpResponse.json({ versions: [] }))
  );
}

async function renderAtVerificationStep() {
  server.use(
    http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
    http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" }))
  );
  render(<SupplierRFQPortal token="test-token" />);
  await screen.findByText(/verification code/i);
}

function codeInputField() {
  return screen.getByRole("textbox");
}

describe("SupplierRFQPortal verification", () => {
  it("a first invalid attempt shows an error; a second valid attempt clears it and shows the portal without a reload", async () => {
    const user = userEvent.setup();
    let verifyCallCount = 0;
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => {
        verifyCallCount += 1;
        if (verifyCallCount === 1) {
          return HttpResponse.json({ detail: "invalid code" }, { status: 422 });
        }
        return HttpResponse.json({ ok: true });
      })
    );
    await renderAtVerificationStep();
    mockBootstrapEndpoints();

    const codeInput = codeInputField();
    await user.type(codeInput, "000000");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    expect(await screen.findByText(/verification code is invalid/i)).toBeInTheDocument();

    await user.clear(codeInput);
    await user.type(codeInput, "111111");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    await waitFor(() => expect(screen.queryByText(/verification code is invalid/i)).not.toBeInTheDocument());
    expect(await screen.findByText("RFQ-000001")).toBeInTheDocument();
  });

  it("does not request offer versions until after the offer draft (which creates the offer chain) has been created — first-visit bootstrap race regression", async () => {
    // Reproduces the real backend dependency: listSupplierOfferVersions
    // looks up an "offer chain" by scope and 404s if it doesn't exist yet;
    // that chain is created as a side effect of createSupplierDraft
    // (CreateOrGetActiveDraft -> EnsureOfferChain) on a genuine first visit.
    // If /offer/versions is requested before /offer (the draft POST) has
    // actually resolved, it must be treated as a bug, not tolerated.
    const user = userEvent.setup();
    let draftCreated = false;
    let versionsRequestedBeforeDraftExisted = false;
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => HttpResponse.json({ ok: true })),
      http.get(`${baseUrl}/supplier-access/session`, () => HttpResponse.json({ invitationId: "inv-1" })),
      http.get(`${baseUrl}/supplier-access/invitations/:invitationId`, () => HttpResponse.json(invitation)),
      http.post(`${baseUrl}/supplier-access/invitations/:invitationId/offer`, async () => {
        // Simulate real network/server latency on the call that creates the
        // offer chain — long enough that a wrongly-parallel versions request
        // would reliably arrive first if the race were still present.
        await new Promise((resolve) => setTimeout(resolve, 30));
        draftCreated = true;
        return HttpResponse.json(draft);
      }),
      http.get(`${baseUrl}/supplier-access/invitations/:invitationId/offer/versions`, () => {
        if (!draftCreated) versionsRequestedBeforeDraftExisted = true;
        return HttpResponse.json({ versions: [] });
      })
    );
    await renderAtVerificationStep();

    await user.type(codeInputField(), "333333");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    expect(await screen.findByText("RFQ-000001")).toBeInTheDocument();
    expect(versionsRequestedBeforeDraftExisted).toBe(false);
    expect(screen.queryByText(/portal could not be loaded/i)).not.toBeInTheDocument();
  });

  it("does not show the invalid-code message when verification succeeds but the subsequent portal bootstrap fails", async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => HttpResponse.json({ ok: true })),
      http.get(`${baseUrl}/supplier-access/session`, () =>
        HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 })
      )
    );
    await renderAtVerificationStep();

    await user.type(codeInputField(), "222222");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    expect(await screen.findByText(/portal could not be loaded/i)).toBeInTheDocument();
    expect(screen.queryByText(/verification code is invalid/i)).not.toBeInTheDocument();
  });
});

describe("SupplierRFQPortal returnTo", () => {
  it("redirects to a safe returnTo once the session is established, without loading the RFQ portal", async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => HttpResponse.json({ ok: true })),
    );
    server.use(
      http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
      http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" })),
    );
    replaceMock.mockClear();
    render(
      <SupplierRFQPortal
        token="test-token"
        returnTo="/supplier-access/invitations/inv-1/outcomes/out-1"
      />,
    );
    await screen.findByText(/verification code/i);

    await user.type(codeInputField(), "444444");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith(
      "/supplier-access/invitations/inv-1/outcomes/out-1",
    ));
    expect(screen.queryByText("RFQ-000001")).not.toBeInTheDocument();
  });

  it("falls through to the normal RFQ portal bootstrap when returnTo is unsafe", async () => {
    const user = userEvent.setup();
    server.use(
      http.post(`${baseUrl}/supplier-access/challenges/verify`, () => HttpResponse.json({ ok: true })),
    );
    await renderAtVerificationStepWithReturnTo("https://evil.com");
    mockBootstrapEndpoints();
    replaceMock.mockClear();

    await user.type(codeInputField(), "555555");
    await user.click(screen.getByRole("button", { name: /verify and continue/i }));

    expect(await screen.findByText("RFQ-000001")).toBeInTheDocument();
    expect(replaceMock).not.toHaveBeenCalled();
  });
});

async function renderAtVerificationStepWithReturnTo(returnTo: string) {
  server.use(
    http.get(`${baseUrl}/supplier-access/open`, () => HttpResponse.json({ ok: true })),
    http.post(`${baseUrl}/supplier-access/challenges`, () => HttpResponse.json({ challengeId: "chal-1" })),
  );
  render(<SupplierRFQPortal token="test-token" returnTo={returnTo} />);
  await screen.findByText(/verification code/i);
}
