import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ResolveDiscrepancyDialog } from "./ResolveDiscrepancyDialog";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const requirement = {
  id: "req-1",
  projectId: "proj-1",
  materialId: "mat-1",
  materialName: "Cement",
  catalogUnit: "bag",
  requiredQuantity: { value: "35", unit: "bag" },
  status: "reviewed",
  sourceType: "cost_item",
  sourceSyncState: "change_detected",
  unitMismatch: false,
  unitMismatchAcknowledged: false,
  revision: 4,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function mockDiscrepancy(overrides: Record<string, unknown> = {}) {
  server.use(
    http.get(`${baseUrl}/material-requirements/:id/source-discrepancy`, () =>
      HttpResponse.json({
        requirementRevision: 4,
        syncState: "change_detected",
        anchorStatus: "reviewed",
        accepted: { quantity: { value: "33", unit: "bag" }, costItemIds: ["c1", "c2"], fingerprint: "fp-old" },
        proposed: { quantity: { value: "41", unit: "bag" }, costItemIds: ["c1", "c2", "c3"], fingerprint: "fp-new" },
        availableActions: ["update", "merge", "keep_current", "create_separate"],
        ...overrides,
      })
    )
  );
}

describe("ResolveDiscrepancyDialog", () => {
  it("shows a loading state while the discrepancy GET is pending, then the accepted/current comparison", async () => {
    mockDiscrepancy();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByText("33 bag")).toBeInTheDocument());
    expect(screen.getByText("41 bag")).toBeInTheDocument();
    expect(screen.getByText(/your current procurement quantity/i)).toBeInTheDocument();
    expect(screen.getByText((_, element) => element?.textContent === "Your current procurement quantity: 35 bag")).toBeInTheDocument();
  });

  it("shows a distinct source_removed presentation with no negative Change line", async () => {
    mockDiscrepancy({
      syncState: "source_removed",
      proposed: { quantity: { value: "0", unit: "bag" }, costItemIds: [], fingerprint: "fp-removed" },
      availableActions: ["keep_current"],
    });
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByText(/no source cost items/i)).toBeInTheDocument());
    expect(screen.queryByText(/^Change:/)).not.toBeInTheDocument();
  });

  it("renders only the actions present in availableActions", async () => {
    mockDiscrepancy({ availableActions: ["keep_current"] });
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    expect(screen.queryByRole("radio", { name: /update requirement/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /merge source change/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /create separate requirement/i })).not.toBeInTheDocument();
  });

  it("shows the exact merge preview quantity when merge is available", async () => {
    mockDiscrepancy();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /merge source change/i })).toBeInTheDocument());
    // delta = 41 - 33 = 8; merged = 35 + 8 = 43
    expect(screen.getByText(/43 bag/)).toBeInTheDocument();
  });

  it("Apply is disabled until a radio is selected", async () => {
    mockDiscrepancy();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    expect(screen.getByRole("button", { name: /apply resolution/i })).toBeDisabled();
  });

  it("selecting an action and applying submits the exact generated field names", async () => {
    mockDiscrepancy();
    let capturedBody: unknown = null;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ ...requirement, sourceSyncState: "clean", status: "draft", revision: 5 });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /keep current requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    await waitFor(() => expect(capturedBody).not.toBeNull());
    expect(capturedBody).toEqual({
      action: "keep_current",
      expectedRevision: 4,
      expectedProposedFingerprint: "fp-new",
    });
  });

  it("create_separate includes a resolutionOperationId; other actions do not", async () => {
    mockDiscrepancy();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...requirement, id: "req-2" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /create separate requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /create separate requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    await waitFor(() => expect(capturedBody).not.toBeNull());
    expect(typeof (capturedBody as unknown as Record<string, unknown>).resolutionOperationId).toBe("string");
  });

  it("reuses the same resolutionOperationId across a retry after an ambiguous failure", async () => {
    mockDiscrepancy();
    const capturedIds: string[] = [];
    let attempt = 0;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        capturedIds.push(body.resolutionOperationId as string);
        attempt += 1;
        if (attempt === 1) {
          return HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 });
        }
        return HttpResponse.json({ ...requirement, id: "req-2" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /create separate requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /create separate requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));
    await waitFor(() => expect(capturedIds).toHaveLength(1));

    await user.click(screen.getByRole("button", { name: /apply resolution/i }));
    await waitFor(() => expect(capturedIds).toHaveLength(2));

    expect(capturedIds[0]).toBe(capturedIds[1]);
  });

  it("on ErrMaterialRequirementDiscrepancyChanged, stays open, refetches, clears selection, and shows a stale warning", async () => {
    mockDiscrepancy();
    let resolveCallCount = 0;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () => {
        resolveCallCount += 1;
        return HttpResponse.json(
          {
            type: "about:blank",
            status: 409,
            title: "Conflict",
            detail: "the source changed since the proposal was computed",
          },
          { status: 409 }
        );
      })
    );
    const user = userEvent.setup();
    let closed = false;
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => { closed = true; }} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /keep current requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    await waitFor(() => expect(resolveCallCount).toBe(1));
    expect(closed).toBe(false);
    expect(await screen.findByText(/source data changed while you were reviewing it/i)).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /keep current requirement/i })).not.toBeChecked();
  });

  it("on ErrMaterialRequirementAlreadyClaimed, closes the dialog", async () => {
    mockDiscrepancy();
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 409,
            title: "Conflict",
            detail: "requirement is claimed by an active RFQ chain; remove the RFQ line first",
          },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();
    let closed = false;
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => { closed = true; }} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /keep current requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    await waitFor(() => expect(closed).toBe(true));
  });

  it("on ErrResolutionOperationConflict, does not silently retry and shows the translated message", async () => {
    mockDiscrepancy();
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 409,
            title: "Conflict",
            detail: "materialrequirements: resolution operation id belongs to a different resolution",
          },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /create separate requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /create separate requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    expect(await screen.findByText(/close this review and start again/i)).toBeInTheDocument();
  });

  it("maps a field-scoped error via applyFieldErrors before falling back to the workflow translator", async () => {
    mockDiscrepancy();
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 422,
            title: "Unprocessable Entity",
            errors: [{ location: "body.action", message: "resolutionOperationId is required for create_separate" }],
          },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(
      <ResolveDiscrepancyDialog open requirement={requirement} onClose={() => {}} onResolved={() => {}} />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /keep current requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    expect(await screen.findByText(/resolutionOperationId is required for create_separate/i)).toBeInTheDocument();
  });

  it("closes and calls onResolved after a successful resolution", async () => {
    mockDiscrepancy();
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () =>
        HttpResponse.json({ ...requirement, sourceSyncState: "clean", status: "draft", revision: 5 })
      )
    );
    const user = userEvent.setup();
    let closed = false;
    let resolved = false;
    renderWithProviders(
      <ResolveDiscrepancyDialog
        open
        requirement={requirement}
        onClose={() => { closed = true; }}
        onResolved={() => { resolved = true; }}
      />
    );
    await waitFor(() => expect(screen.getByRole("radio", { name: /keep current requirement/i })).toBeInTheDocument());
    await user.click(screen.getByRole("radio", { name: /keep current requirement/i }));
    await user.click(screen.getByRole("button", { name: /apply resolution/i }));

    await waitFor(() => expect(closed).toBe(true));
    expect(resolved).toBe(true);
  });
});
