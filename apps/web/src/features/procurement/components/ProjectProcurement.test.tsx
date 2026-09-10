import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectProcurement } from "./ProjectProcurement";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const existingRequirement = {
  id: "existing-1",
  projectId: "proj1",
  materialId: "mat-cement",
  materialName: "Cement",
  catalogUnit: "bag",
  requiredQuantity: { value: "1", unit: "bag" },
  specification: "Need cement",
  status: "draft",
  sourceType: "manual",
  sourceSyncState: "clean",
  unitMismatch: false,
  unitMismatchAcknowledged: false,
  revision: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const cementMaterial = { id: "mat-cement", name: "Cement", unit: "bag", createdAt: "2026-01-01T00:00:00Z" };
const tilesMaterial = { id: "mat-tiles", name: "Tiles", unit: "box", createdAt: "2026-01-01T00:00:00Z" };

function mockBase(requirements: unknown[] = [], rfqs: unknown[] = []) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs })),
    http.get(`${baseUrl}/material-requirements`, () => HttpResponse.json({ materialRequirements: requirements })),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [cementMaterial, tilesMaterial] })),
    http.get(`${baseUrl}/suppliers`, () => HttpResponse.json({ suppliers: [] })),
    http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: [], page: 1, pageSize: 100, total: 0 })),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
      HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/invitations`, () => HttpResponse.json({ invitations: [] }))
  );
}

async function openCreateRequirementDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: /^requirement$/i }));
  return screen.findByRole("dialog");
}

async function selectMaterial(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement, name: RegExp) {
  await user.click(within(dialog).getByRole("combobox"));
  await user.click(await screen.findByRole("option", { name }));
}

describe("ProjectProcurement create requirement duplicate warning", () => {
  it("first submit with an active matching requirement does not mutate; shows an inline warning", async () => {
    mockBase([existingRequirement]);
    let created = false;
    server.use(http.post(`${baseUrl}/material-requirements`, () => { created = true; return HttpResponse.json(existingRequirement); }));
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);

    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    expect(await within(dialog).findByText(/similar requirement already exists/i)).toBeInTheDocument();
    expect(created).toBe(false);
  });

  it("Create another anyway proceeds with exactly one mutation", async () => {
    mockBase([existingRequirement]);
    let createCount = 0;
    server.use(http.post(`${baseUrl}/material-requirements`, () => { createCount += 1; return HttpResponse.json(existingRequirement); }));
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));
    await within(dialog).findByText(/similar requirement already exists/i);

    await user.click(within(dialog).getByRole("button", { name: /create another anyway/i }));
    await waitFor(() => expect(createCount).toBe(0)); // approval alone does not submit
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    await waitFor(() => expect(createCount).toBe(1));
  });

  it("changing the material after approving a duplicate requires the warning again for the new material", async () => {
    mockBase([existingRequirement, { ...existingRequirement, id: "existing-2", materialId: "mat-tiles", materialName: "Tiles", requiredQuantity: { value: "1", unit: "box" } }]);
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));
    await within(dialog).findByText(/similar requirement already exists/i);
    await user.click(within(dialog).getByRole("button", { name: /create another anyway/i }));

    await selectMaterial(user, dialog, /tiles/i);
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    expect(await within(dialog).findByText(/similar requirement already exists/i)).toBeInTheDocument();
  });

  it("does not warn when no similar requirement exists", async () => {
    mockBase([]);
    let created = false;
    server.use(http.post(`${baseUrl}/material-requirements`, () => { created = true; return HttpResponse.json(existingRequirement); }));
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    await waitFor(() => expect(created).toBe(true));
    expect(screen.queryByText(/similar requirement already exists/i)).not.toBeInTheDocument();
  });

  it("disables the submit button and shows Creating… while the mutation is pending", async () => {
    mockBase([]);
    server.use(
      http.post(`${baseUrl}/material-requirements`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json(existingRequirement);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    expect(await within(dialog).findByRole("button", { name: /creating/i })).toBeDisabled();
  });
});

describe("ProjectProcurement create requirement quantity/unit contract", () => {
  it("submits requiredQuantity value and unit as separate fields, defaulting unit from the selected material's catalog unit", async () => {
    mockBase([]);
    let body: { quantityValue?: string; quantityUnit?: string } | undefined;
    server.use(
      http.post(`${baseUrl}/material-requirements`, async ({ request }) => {
        body = (await request.json()) as typeof body;
        return HttpResponse.json(existingRequirement);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);

    expect(within(dialog).getByLabelText(/unit/i)).toHaveValue("bag");

    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    await waitFor(() => expect(body).toMatchObject({ quantityValue: "1", quantityUnit: "bag" }));
  });

  it("changing the quantity value leaves the defaulted unit untouched", async () => {
    mockBase([]);
    let body: { quantityValue?: string; quantityUnit?: string } | undefined;
    server.use(
      http.post(`${baseUrl}/material-requirements`, async ({ request }) => {
        body = (await request.json()) as typeof body;
        return HttpResponse.json(existingRequirement);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);
    await selectMaterial(user, dialog, /cement/i);

    const quantityInput = within(dialog).getByLabelText(/quantity/i);
    await user.clear(quantityInput);
    await user.type(quantityInput, "15");
    await user.click(within(dialog).getByRole("button", { name: /create requirement/i }));

    await waitFor(() => expect(body).toMatchObject({ quantityValue: "15", quantityUnit: "bag" }));
    expect(body?.quantityUnit).not.toBe("15 bag");
  });

  it("shows unit-only helper text next to the manually editable unit field", async () => {
    mockBase([]);
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRequirementDialog(user);

    expect(within(dialog).getByText(/unit only, e\.g\. bag, kg, m/i)).toBeInTheDocument();
  });
});

const draftRfq = {
  id: "rfq-1",
  projectId: "proj1",
  rfqNumber: "RFQ-000001",
  status: "draft",
  revision: 1,
  lines: [],
  deliveryAddress: "123 Main St",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("ProjectProcurement state-aware requirement cards", () => {
  it("a draft requirement no longer renders an 'Add cement...' pill, and renders as a RequirementCard instead", async () => {
    mockBase([existingRequirement], [draftRfq]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await waitFor(() => expect(screen.getAllByText("RFQ-000001").length).toBeGreaterThan(0));
    expect(screen.queryByRole("button", { name: /^add cement/i })).not.toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /confirm requirement/i })).toBeInTheDocument();
  });

  it("Review -> after mocked success and awaited refetch, the card shows Add to RFQ only once the server record is reviewed", async () => {
    let currentStatus = "draft";
    let currentRevision = 0;
    mockBase([], [draftRfq]);
    server.use(
      http.get(`${baseUrl}/material-requirements`, () =>
        HttpResponse.json({ materialRequirements: [{ ...existingRequirement, status: currentStatus, revision: currentRevision }] })
      ),
      http.post(`${baseUrl}/material-requirements/:id/review`, () => {
        currentStatus = "reviewed";
        currentRevision = 1;
        return HttpResponse.json({ ...existingRequirement, status: currentStatus, revision: currentRevision });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const reviewButton = await screen.findByRole("button", { name: /confirm requirement/i });
    await user.click(reviewButton);

    expect(await screen.findByRole("button", { name: /^add to rfq/i })).toBeInTheDocument();
  });

  it("Remove -> Archive -> after mocked success and awaited refetch, the archived requirement no longer shows Confirm/Remove", async () => {
    let currentStatus = "draft";
    let currentRevision = 0;
    mockBase([], [draftRfq]);
    server.use(
      http.get(`${baseUrl}/material-requirements`, () =>
        HttpResponse.json({ materialRequirements: [{ ...existingRequirement, status: currentStatus, revision: currentRevision }] })
      ),
      http.post(`${baseUrl}/material-requirements/:id/archive`, () => {
        currentStatus = "archived";
        currentRevision = 1;
        return HttpResponse.json({ ...existingRequirement, status: currentStatus, revision: currentRevision });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const removeButton = await screen.findByRole("button", { name: /remove requirement/i });
    await user.click(removeButton);

    const dialog = await screen.findByRole("dialog", { name: /remove this requirement/i });
    await user.click(within(dialog).getByRole("button", { name: /^archive$/i }));

    await waitFor(() => expect(screen.queryByRole("button", { name: /confirm requirement/i })).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /remove requirement/i })).not.toBeInTheDocument();
  });

  it("Remove -> Delete permanently -> after mocked success and awaited refetch, the requirement is gone entirely", async () => {
    let requirements = [{ ...existingRequirement, status: "draft", revision: 0 }];
    mockBase([], [draftRfq]);
    server.use(
      http.get(`${baseUrl}/material-requirements`, () => HttpResponse.json({ materialRequirements: requirements })),
      http.delete(`${baseUrl}/material-requirements/:id`, () => {
        requirements = [];
        return new HttpResponse(null, { status: 204 });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const removeButton = await screen.findByRole("button", { name: /remove requirement/i });
    await user.click(removeButton);

    const dialog = await screen.findByRole("dialog", { name: /remove this requirement/i });
    await user.click(within(dialog).getByRole("button", { name: /delete permanently/i }));

    await waitFor(() => expect(screen.queryByRole("button", { name: /remove requirement/i })).not.toBeInTheDocument());
    expect(screen.queryByText(existingRequirement.materialName)).not.toBeInTheDocument();
  });

  it("claimed requirement's View RFQ action uses the requirement's own activeRfqChainId/Number", async () => {
    mockBase(
      [{ ...existingRequirement, status: "reviewed", activeRfqChainId: "chain-9", activeRfqNumber: "RFQ-000009" }],
      [draftRfq]
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText(/in rfq-000009/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /view rfq-000009/i })).toBeInTheDocument();
  });

  it("split-source requirement shows View split batches with no mutation action", async () => {
    mockBase([{ ...existingRequirement, status: "split" }], [draftRfq]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByRole("button", { name: /view split batches/i })).toBeInTheDocument();
  });

  it("split-incomplete requirement shows no mutation action", async () => {
    mockBase([{ ...existingRequirement, status: "split", splitState: "creating" }], [draftRfq]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/requires reconciliation/i);
    expect(screen.queryByRole("button", { name: /review|acknowledge|add to rfq/i })).not.toBeInTheDocument();
  });
});

describe("ProjectProcurement sync from project costs", () => {
  it("calls the generate endpoint for the project and refetches requirements on success", async () => {
    mockBase([], [draftRfq]);
    let generateCallCount = 0;
    let requirementsCallCount = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/material-requirements/generate`, ({ params }) => {
        generateCallCount += 1;
        expect(params.projectId).toBe("proj1");
        return HttpResponse.json({
          createdCount: 2,
          unchangedCount: 1,
          discrepancyCount: 0,
          sourceRemovedCount: 0,
          skippedCount: 0,
          created: [],
          discrepancies: [],
          sourceRemoved: [],
          skipped: [],
          intrinsicallyIneligible: { nonMaterialCategory: 0, missingMaterialID: 0, missingWorkItemID: 0, missingOrZeroQuantity: 0, blankUnit: 0 },
        });
      }),
      http.get(`${baseUrl}/material-requirements`, () => {
        requirementsCallCount += 1;
        return HttpResponse.json({ materialRequirements: [] });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const syncButton = await screen.findByRole("button", { name: /sync from project costs/i });
    const requirementsCallsBeforeClick = requirementsCallCount;
    await user.click(syncButton);

    await waitFor(() => expect(generateCallCount).toBe(1));
    await waitFor(() => expect(requirementsCallCount).toBeGreaterThan(requirementsCallsBeforeClick));
  });

  it("shows the pending label and disables the button while syncing", async () => {
    mockBase([], [draftRfq]);
    server.use(
      http.post(`${baseUrl}/projects/:projectId/material-requirements/generate`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({ createdCount: 0, unchangedCount: 0, discrepancyCount: 0, sourceRemovedCount: 0, skippedCount: 0, created: [], discrepancies: [], sourceRemoved: [], skipped: [], intrinsicallyIneligible: { nonMaterialCategory: 0, missingMaterialID: 0, missingWorkItemID: 0, missingOrZeroQuantity: 0, blankUnit: 0 } });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /sync from project costs/i }));

    expect(await screen.findByRole("button", { name: /syncing/i })).toBeDisabled();
  });
});

describe("ProjectProcurement View claiming RFQ navigation", () => {
  it("scrolls to RFQ scope without changing selection when the claiming RFQ is already selected", async () => {
    mockBase(
      [{ ...existingRequirement, status: "reviewed", activeRfqChainId: "rfq-1", activeRfqNumber: "RFQ-000001" }],
      [draftRfq]
    );
    const scrollIntoView = vi.fn();
    HTMLElement.prototype.scrollIntoView = scrollIntoView;
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const button = await screen.findByRole("button", { name: /view rfq scope/i });
    await user.click(button);

    expect(scrollIntoView).toHaveBeenCalledWith(expect.objectContaining({ behavior: "smooth" }));
  });

  it("selects the claiming RFQ then scrolls to its scope when a different RFQ is selected", async () => {
    const otherRfq = { ...draftRfq, id: "rfq-2", rfqNumber: "RFQ-000002" };
    mockBase(
      [{ ...existingRequirement, status: "reviewed", activeRfqChainId: "rfq-1", activeRfqNumber: "RFQ-000001" }],
      [otherRfq, draftRfq]
    );
    const scrollIntoView = vi.fn();
    HTMLElement.prototype.scrollIntoView = scrollIntoView;
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findAllByText("RFQ-000002");
    const button = await screen.findByRole("button", { name: /view rfq-000001/i });
    await user.click(button);

    await waitFor(() => expect(screen.getAllByText("RFQ-000001").length).toBeGreaterThan(0));
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalledWith(expect.objectContaining({ behavior: "smooth" })));
  });
});

describe("ProjectProcurement Add-to-RFQ serialization", () => {
  it("serializes concurrent Add-to-RFQ clicks against the same selected RFQ", async () => {
    let addLineCallCount = 0;
    let rfqRevision = 1;
    mockBase(
      [
        { ...existingRequirement, id: "req-a", status: "reviewed", revision: 0 },
        { ...existingRequirement, id: "req-b", materialName: "Tiles", materialId: "mat-tiles", status: "reviewed", revision: 0 },
      ],
      [draftRfq]
    );
    server.use(
      http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs: [{ ...draftRfq, revision: rfqRevision }] })),
      http.post(`${baseUrl}/rfqs/:id/lines`, async () => {
        addLineCallCount += 1;
        await new Promise((resolve) => setTimeout(resolve, 30));
        rfqRevision += 1;
        return HttpResponse.json({ ...draftRfq, revision: rfqRevision, lines: [{ id: "line-1" }] });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const addButtons = await screen.findAllByRole("button", { name: /^add to rfq$/i });
    expect(addButtons).toHaveLength(2);

    await user.click(addButtons[0]);
    // The second Add-to-RFQ button (for the other requirement, same RFQ)
    // must be disabled while the first is in flight.
    expect(addButtons[1]).toBeDisabled();

    await waitFor(() => expect(addLineCallCount).toBe(1));
  });

  it("rejects a second synchronous click on the same button before React has a chance to re-render it as disabled", async () => {
    let addLineCallCount = 0;
    mockBase([{ ...existingRequirement, id: "req-a", status: "reviewed", revision: 0 }], [draftRfq]);
    server.use(
      http.post(`${baseUrl}/rfqs/:id/lines`, async () => {
        addLineCallCount += 1;
        await new Promise((resolve) => setTimeout(resolve, 30));
        return HttpResponse.json({ ...draftRfq, revision: 2, lines: [{ id: "line-1" }] });
      })
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const addButton = await screen.findByRole("button", { name: /^add to rfq$/i });
    // fireEvent (unlike userEvent) does not await a microtask/act flush
    // between events, so both clicks land before React commits any
    // re-render — the disabled-prop alone could not stop the second one.
    fireEvent.click(addButton);
    fireEvent.click(addButton);

    await waitFor(() => expect(addLineCallCount).toBe(1));
    // Give any wrongly-fired second request time to have shown up.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(addLineCallCount).toBe(1);
  });
});

describe("ProjectProcurement Mark ready readiness", () => {
  it("disables Mark ready with a no-scope message for an empty draft RFQ", async () => {
    mockBase([], [{ ...draftRfq, lines: [] }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const markReady = await screen.findByRole("button", { name: /mark ready/i });
    expect(markReady).toBeDisabled();
    expect(screen.getByText(/add at least one reviewed material requirement/i)).toBeInTheDocument();
  });

  it("disables Mark ready with a delivery-address message when the RFQ has a line but no address", async () => {
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }], deliveryAddress: "" }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const markReady = await screen.findByRole("button", { name: /mark ready/i });
    expect(markReady).toBeDisabled();
    expect(screen.getByText(/add a delivery address/i)).toBeInTheDocument();
  });

  it("enables Mark ready when the RFQ has a line and a delivery address", async () => {
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }] }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const markReady = await screen.findByRole("button", { name: /mark ready/i });
    expect(markReady).not.toBeDisabled();
  });

  it("shows Edit RFQ details for a Draft RFQ even when delivery address and dates are already complete", async () => {
    // Regression: Edit RFQ details used to be gated on the
    // missing-delivery-address readiness blocker specifically, so a
    // contractor with a fully-complete draft RFQ (line + address already
    // set) had no way to edit it at all — readiness nudges and edit-action
    // visibility must be independent.
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }], deliveryAddress: "12 Site Road" }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const markReady = await screen.findByRole("button", { name: /^mark ready$/i });
    expect(markReady).not.toBeDisabled();
    expect(screen.queryByText(/add a delivery address/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /edit rfq details/i })).toBeInTheDocument();
  });

  it("still shows Edit RFQ details alongside the missing-delivery-address blocker", async () => {
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }], deliveryAddress: "" }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/add a delivery address/i);
    expect(screen.getByRole("button", { name: /edit rfq details/i })).toBeInTheDocument();
  });

  it("hides both Mark ready and Edit RFQ details once the RFQ is no longer in draft status", async () => {
    mockBase([], [{ ...draftRfq, status: "ready", lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }] }]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByRole("button", { name: /reopen draft/i });
    expect(screen.queryByRole("button", { name: /^mark ready$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /edit rfq details/i })).not.toBeInTheDocument();
  });

  it("shows Marking ready… and disables the button while the mutation is pending", async () => {
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }] }]);
    server.use(
      http.post(`${baseUrl}/rfqs/:id/ready`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({ ...draftRfq, status: "ready" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const markReady = await screen.findByRole("button", { name: /^mark ready$/i });
    await user.click(markReady);

    expect(await screen.findByRole("button", { name: /marking ready/i })).toBeDisabled();
  });
});

describe("ProjectProcurement mutation error banner", () => {
  it("surfaces the backend's actual domain message for a 422, not the generic staleness copy", async () => {
    mockBase([], [{ ...draftRfq, status: "ready", lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }] }]);
    server.use(
      http.post(`${baseUrl}/rfq-chains/:rfqChainId/issue`, () =>
        HttpResponse.json(
          { type: "about:blank", status: 422, title: "Unprocessable Entity", detail: "the required delivery date must be after the supplier response deadline" },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /^issue rfq$/i }));

    expect(await screen.findByText(/required delivery date must be after the supplier response deadline/i)).toBeInTheDocument();
    expect(screen.queryByText(/record may have changed/i)).not.toBeInTheDocument();
  });

  it("shows a conflict message for a genuine 409 stale-revision failure", async () => {
    mockBase([], [{ ...draftRfq, lines: [{ id: "line-1", materialName: "Cement", quantity: { value: "1", unit: "bag" } }] }]);
    server.use(
      http.post(`${baseUrl}/rfqs/:id/ready`, () =>
        HttpResponse.json(
          { type: "about:blank", status: 409, title: "Conflict", detail: "rfqs: rfq changed since it was read" },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /^mark ready$/i }));

    expect(await screen.findByText(/this record changed since you opened it/i)).toBeInTheDocument();
  });
});

describe("ProjectProcurement issued-versions empty state", () => {
  it("shows 'No issued versions yet' for a never-issued RFQ, and does not surface it as a page-level error", async () => {
    mockBase([], [draftRfq]);
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByRole("button", { name: /mark ready/i });
    expect(await screen.findByText(/no issued versions yet/i)).toBeInTheDocument();
    expect(screen.queryByText(/project procurement data could not be loaded/i)).not.toBeInTheDocument();
  });

  it("still surfaces a genuine non-404 versions failure as a page-level error", async () => {
    // A draft RFQ never requests /versions at all (see the "does not request
    // /versions for a draft RFQ" test below), so this exercises the
    // still-issued path where the request fires and a genuine failure must
    // surface.
    mockBase([], [{ ...draftRfq, status: "ready" }]);
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 })
      )
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText(/project procurement data could not be loaded/i)).toBeInTheDocument();
  });

  it("treats a 404 award-revisions response (no award yet) as an empty state, not a page-level load failure", async () => {
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({ versions: [{ id: "version-1", versionNumber: 1, issuedAt: "2026-01-01T00:00:00Z" }] })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found", detail: "award resource not found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/comparison`, () =>
        HttpResponse.json({ offers: [], currency: "MYR" })
      )
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findAllByText("RFQ-000001");
    expect(screen.queryByText(/project procurement data could not be loaded/i)).not.toBeInTheDocument();
  });

  it("does not request /versions for a draft RFQ", async () => {
    mockBase([], [draftRfq]);
    let versionsRequested = false;
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () => {
        versionsRequested = true;
        return HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 });
      })
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText(/no issued versions yet/i)).toBeInTheDocument();
    expect(versionsRequested).toBe(false);
  });
});

describe("ProjectProcurement comparison scope-line identity", () => {
  it("shows the issued line's material name and quantity, not the fallback placeholder", async () => {
    // The comparison's offer lines are keyed by the ISSUED version's line
    // IDs (immutable snapshot), not the mutable draft RFQ's own line IDs —
    // these are two different ID spaces. Regression for a bug where the
    // panel joined against the draft RFQ (always lines: [] once issued) and
    // silently rendered every row as the literal "RFQ line" placeholder
    // with quantity "—", never the real material.
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({
          versions: [{
            id: "version-1", versionNumber: 1, issuedAt: "2026-01-01T00:00:00Z",
            lines: [{ id: "issued-line-1", materialName: "Cement", quantity: { value: "25", unit: "bag" }, specification: "OPC Type I" }],
          }],
        })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/comparison`, () =>
        HttpResponse.json({
          currency: "MYR",
          offers: [{
            offerVersionId: "offer-v1", supplierName: "DemoBuild Materials Sdn Bhd", versionNumber: 1,
            selectable: true, validUntil: "2026-12-01T00:00:00Z",
            indicativeGrandTotal: { amount: { amount: 245000, currency: "MYR" } },
            indicativeQuotedSubtotal: { amount: { amount: 245000, currency: "MYR" } },
            offerLevelTaxAmount: { amount: 0, currency: "MYR" },
            lines: [{ issuedRfqLineId: "issued-line-1", offerLineId: "offer-line-1", selectable: true, responseStatus: "quoted", unitPrice: { amount: 9800, currency: "MYR" } }],
          }],
        })
      )
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText("Cement")).toBeInTheDocument();
    expect(screen.getByText("25 bag")).toBeInTheDocument();
    expect(screen.queryByText("RFQ line")).not.toBeInTheDocument();
    expect(screen.queryByText("—")).not.toBeInTheDocument();
  });
});

describe("ProjectProcurement pre-finalisation comparison (interactive, not yet finalised)", () => {
  function mockTwoSupplierComparison() {
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
        HttpResponse.json({
          versions: [{
            id: "version-1", versionNumber: 1, issuedAt: "2026-01-01T00:00:00Z",
            lines: [{ id: "issued-line-1", materialName: "Cement", quantity: { value: "25", unit: "bag" }, specification: "OPC Type I" }],
          }],
        })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions`, () =>
        HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
      ),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/comparison`, () =>
        HttpResponse.json({
          currency: "MYR",
          offers: [
            {
              offerVersionId: "offer-v1", supplierName: "DemoBuild Materials Sdn Bhd", versionNumber: 1,
              selectable: true, validUntil: "2026-12-01T00:00:00Z",
              indicativeGrandTotal: { amount: { amount: 245000, currency: "MYR" } },
              indicativeQuotedSubtotal: { amount: { amount: 245000, currency: "MYR" } },
              offerLevelTaxAmount: { amount: 0, currency: "MYR" },
              lines: [{ issuedRfqLineId: "issued-line-1", offerLineId: "offer-line-1", selectable: true, responseStatus: "quoted", unitPrice: { amount: 9800, currency: "MYR" } }],
            },
            {
              offerVersionId: "offer-v2", supplierName: "Metro Tile Supply Sdn Bhd", versionNumber: 1,
              selectable: false, validUntil: "2026-12-01T00:00:00Z",
              indicativeGrandTotal: { amount: { amount: 0, currency: "MYR" } },
              indicativeQuotedSubtotal: { amount: { amount: 0, currency: "MYR" } },
              offerLevelTaxAmount: { amount: 0, currency: "MYR" },
              lines: [{ issuedRfqLineId: "issued-line-1", offerLineId: "offer-line-2", selectable: false, responseStatus: "no_bid" }],
            },
          ],
        })
      )
    );
  }

  it("renders each supplier's real name as the primary label, with Offer v1 shown only as secondary metadata", async () => {
    mockTwoSupplierComparison();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await waitFor(() => expect(screen.getAllByText("DemoBuild Materials Sdn Bhd").length).toBeGreaterThan(0));
    // "Offer v1" exists, but only as secondary text alongside the real name —
    // never as a standalone primary identifier for a supplier with a known
    // name (the pre-redesign symptom this replaces).
    expect(screen.getAllByText(/offer v1/i).length).toBeGreaterThan(0);
    expect(screen.queryByText(/^offer v1$/i, { selector: "p.font-semibold, strong" })).not.toBeInTheDocument();
  });

  it("still supports selecting a line and shows the lowest-line-price indicator on the cheaper offer", async () => {
    mockTwoSupplierComparison();
    let draftState: Record<string, unknown> = {
      id: "draft-1", awardChainId: "chain-1", issuedRfqVersionId: "version-1", revision: 1,
      status: "open", createdByUserId: "user-1", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
      lineDecisions: [],
    };
    server.use(
      http.post(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () => HttpResponse.json(draftState)),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () => HttpResponse.json(draftState)),
      http.put(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft/lines/:lineId/selection`, () => {
        draftState = {
          ...draftState, revision: 2,
          lineDecisions: [{ issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", decision: "selected", offerVersionId: "offer-v1", offerLineId: "offer-line-1" }],
        };
        return HttpResponse.json(draftState);
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    const selectButton = await screen.findByRole("button", { name: /98\.00.*select/i });
    expect(selectButton).toBeEnabled();
    expect(screen.getByText(/lowest line price/i)).toBeInTheDocument();

    await user.click(selectButton);
    await waitFor(() => expect(screen.getByRole("button", { name: /selected — unselect/i })).toBeInTheDocument());
  });

  it("represents a Supplier that did not bid on a line as No bid, with no price or Select control for that cell", async () => {
    mockTwoSupplierComparison();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await waitFor(() => expect(screen.getAllByText("Metro Tile Supply Sdn Bhd").length).toBeGreaterThan(0));
    expect(screen.getByText(/no bid/i)).toBeInTheDocument();
  });
});

function mockComparisonRoutes(overrides: { awardDraft?: unknown; awardRevisions?: unknown } = {}) {
  server.use(
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
      HttpResponse.json({
        versions: [{
          id: "version-1", versionNumber: 1, issuedAt: "2026-01-01T00:00:00Z",
          lines: [{ id: "issued-line-1", materialName: "Cement", quantity: { value: "25", unit: "bag" }, specification: "OPC Type I" }],
        }],
      })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () =>
      overrides.awardDraft
        ? HttpResponse.json(overrides.awardDraft)
        : HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions`, () =>
      overrides.awardRevisions
        ? HttpResponse.json(overrides.awardRevisions)
        : HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/comparison`, () =>
      HttpResponse.json({
        currency: "MYR",
        offers: [{
          offerVersionId: "offer-v1", supplierName: "DemoBuild Materials Sdn Bhd", versionNumber: 1,
          selectable: true, validUntil: "2026-12-01T00:00:00Z",
          indicativeGrandTotal: { amount: { amount: 245000, currency: "MYR" } },
          indicativeQuotedSubtotal: { amount: { amount: 245000, currency: "MYR" } },
          offerLevelTaxAmount: { amount: 0, currency: "MYR" },
          lines: [{ issuedRfqLineId: "issued-line-1", offerLineId: "offer-line-1", selectable: true, responseStatus: "quoted", unitPrice: { amount: 9800, currency: "MYR" } }],
        }],
      })
    )
  );
}

describe("ProjectProcurement Award selection persistence and finalisation gating", () => {
  it("shows a line as already selected from the server-fetched draft, with no click and no page refresh", async () => {
    // Regression: selection state used to live only in local React state,
    // set as a side effect of clicking Select in the current session — a
    // genuinely-selected line from a prior session (or before a refresh)
    // rendered as un-selected until the user clicked Select again.
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    mockComparisonRoutes({
      awardDraft: {
        id: "draft-1", awardChainId: "chain-1", issuedRfqVersionId: "version-1", revision: 1,
        status: "open", createdByUserId: "user-1", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
        lineDecisions: [{ issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", decision: "selected", offerVersionId: "offer-v1", offerLineId: "offer-line-1" }],
      },
    });
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByRole("button", { name: /selected — unselect/i })).toBeInTheDocument();
  });

  it("shows the finalised-award result view once an Award revision is finalised, with no disabled Select controls", async () => {
    // Regression: the finalised screen used to render the same interactive
    // comparison matrix as before finalisation, just with a disabled Select
    // button reading "Awarded" — looking broken rather than final. The
    // redesigned finalised view is result-oriented and never renders a
    // disabled Select as its primary interface.
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    mockComparisonRoutes({
      awardDraft: {
        id: "draft-1", awardChainId: "chain-1", issuedRfqVersionId: "version-1", revision: 1,
        status: "open", createdByUserId: "user-1", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
        lineDecisions: [{ issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", decision: "selected", offerVersionId: "offer-v1", offerLineId: "offer-line-1" }],
      },
      awardRevisions: {
        revisions: [{
          id: "revision-1", revisionNumber: 1, issuedRfqVersionId: "version-1", finalisedAt: "2026-01-02T00:00:00Z",
          grandAwardTotal: { amount: 245000, currency: "MYR" },
          awardedLines: [{
            issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", materialName: "Cement",
            quantityValue: "25", quantityUnit: "bag", supplierId: "supplier-1", supplierName: "DemoBuild Materials Sdn Bhd",
            invitationId: "invitation-1", offerVersionId: "offer-v1", offerLineId: "offer-line-1",
            unitPriceExcludingTax: { amount: 9800, currency: "MYR" }, lineSubtotal: { amount: 245000, currency: "MYR" }, lineTaxAmount: { amount: 0, currency: "MYR" },
          }],
          supplierSummaries: [{
            supplierId: "supplier-1", supplierName: "DemoBuild Materials Sdn Bhd", invitationId: "invitation-1", offerVersionId: "offer-v1",
            awardedLineIds: ["issued-line-1"], lineSubtotal: { amount: 245000, currency: "MYR" }, taxTotal: { amount: 0, currency: "MYR" },
            chargeTotal: { amount: 0, currency: "MYR" }, deliveryCharge: { amount: 0, currency: "MYR" }, supplierTotal: { amount: 245000, currency: "MYR" },
          }],
          unawardedLines: [],
        }],
      },
    });
    server.use(
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions/:revisionId/outcomes`, () => HttpResponse.json({ outcomes: [] })),
      http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions/:revisionId/notifications`, () => HttpResponse.json({ deliveries: [] }))
    );
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText(/award finalised/i)).toBeInTheDocument();
    expect(screen.getAllByText("DemoBuild Materials Sdn Bhd").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /^select$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /awarded/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /create award/i })).not.toBeInTheDocument();
  });
});

// Two-supplier, two-line finalised revision — the fixture the rest of the
// finalised-award detail tests below share.
const twoSupplierFinalisedRevision = {
  id: "revision-1", revisionNumber: 1, issuedRfqVersionId: "version-1", finalisedAt: "2026-08-23T12:19:00Z",
  grandAwardTotal: { amount: 274750, currency: "MYR" },
  awardedLines: [
    {
      issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", materialName: "Cement",
      quantityValue: "25", quantityUnit: "bag", supplierId: "supplier-1", supplierName: "ABC Building Materials",
      invitationId: "invitation-1", offerVersionId: "offer-v1", offerLineId: "offer-line-1",
      unitPriceExcludingTax: { amount: 2280, currency: "MYR" }, lineSubtotal: { amount: 57000, currency: "MYR" }, lineTaxAmount: { amount: 0, currency: "MYR" },
    },
    {
      issuedRfqLineId: "issued-line-2", stableLineageId: "lineage-2", materialName: "Porcelain Floor Tile",
      quantityValue: "45", quantityUnit: "sqm", supplierId: "supplier-2", supplierName: "TilePro Supplies",
      invitationId: "invitation-2", offerVersionId: "offer-v2", offerLineId: "offer-line-2",
      unitPriceExcludingTax: { amount: 4400, currency: "MYR" }, lineSubtotal: { amount: 198000, currency: "MYR" }, lineTaxAmount: { amount: 0, currency: "MYR" },
    },
  ],
  supplierSummaries: [
    {
      supplierId: "supplier-1", supplierName: "ABC Building Materials", invitationId: "invitation-1", offerVersionId: "offer-v1",
      awardedLineIds: ["issued-line-1"], lineSubtotal: { amount: 57000, currency: "MYR" }, taxTotal: { amount: 0, currency: "MYR" },
      chargeTotal: { amount: 0, currency: "MYR" }, deliveryCharge: { amount: 0, currency: "MYR" }, supplierTotal: { amount: 76750, currency: "MYR" },
    },
    {
      supplierId: "supplier-2", supplierName: "TilePro Supplies", invitationId: "invitation-2", offerVersionId: "offer-v2",
      awardedLineIds: ["issued-line-2"], lineSubtotal: { amount: 198000, currency: "MYR" }, taxTotal: { amount: 0, currency: "MYR" },
      chargeTotal: { amount: 0, currency: "MYR" }, deliveryCharge: { amount: 0, currency: "MYR" }, supplierTotal: { amount: 198000, currency: "MYR" },
    },
  ],
  unawardedLines: [],
  changeReason: "Initial award.",
};

function mockFinalisedAwardScenario(options: { outcomes?: unknown[]; deliveries?: unknown[]; invitations?: unknown[] } = {}) {
  const issuedRfq = { ...draftRfq, status: "ready" };
  mockBase([], [issuedRfq]);
  server.use(
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/versions`, () =>
      HttpResponse.json({
        versions: [{
          id: "version-1", versionNumber: 1, issuedAt: "2026-01-01T00:00:00Z",
          lines: [
            { id: "issued-line-1", materialName: "Cement", quantity: { value: "25", unit: "bag" }, specification: "OPC Type I" },
            { id: "issued-line-2", materialName: "Porcelain Floor Tile", quantity: { value: "45", unit: "sqm" }, specification: "600x600mm" },
          ],
        }],
      })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/invitations`, () => HttpResponse.json({
      invitations: options.invitations ?? [
        { id: "invitation-1", rfqChainId: "rfq-1", supplierId: "supplier-1", recipientName: "Ali", recipientEmail: "ali@abcbuilding.example", status: "responded", accessGeneration: 1, currentIssuedRfqVersionId: "version-1", expiresAt: "2026-12-01T00:00:00Z", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z", revision: 1 },
        { id: "invitation-2", rfqChainId: "rfq-1", supplierId: "supplier-2", recipientName: "Bee", recipientEmail: "bee@tilepro.example", status: "responded", accessGeneration: 1, currentIssuedRfqVersionId: "version-1", expiresAt: "2026-12-01T00:00:00Z", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z", revision: 1 },
      ],
    })),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft`, () =>
      HttpResponse.json({ type: "about:blank", status: 404, title: "Not Found" }, { status: 404 })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions`, () =>
      HttpResponse.json({ revisions: [twoSupplierFinalisedRevision] })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/comparison`, () =>
      HttpResponse.json({
        currency: "MYR",
        offers: [
          {
            offerVersionId: "offer-v1", supplierName: "ABC Building Materials", versionNumber: 1,
            selectable: true, validUntil: "2026-12-01T00:00:00Z",
            indicativeGrandTotal: { amount: { amount: 57000, currency: "MYR" } },
            indicativeQuotedSubtotal: { amount: { amount: 57000, currency: "MYR" } },
            offerLevelTaxAmount: { amount: 0, currency: "MYR" },
            lines: [{ issuedRfqLineId: "issued-line-1", offerLineId: "offer-line-1", selectable: true, responseStatus: "quoted", unitPrice: { amount: 2280, currency: "MYR" } }],
          },
          {
            offerVersionId: "offer-v2", supplierName: "TilePro Supplies", versionNumber: 1,
            selectable: true, validUntil: "2026-12-01T00:00:00Z",
            indicativeGrandTotal: { amount: { amount: 198000, currency: "MYR" } },
            indicativeQuotedSubtotal: { amount: { amount: 198000, currency: "MYR" } },
            offerLevelTaxAmount: { amount: 0, currency: "MYR" },
            lines: [{ issuedRfqLineId: "issued-line-2", offerLineId: "offer-line-2", selectable: true, responseStatus: "quoted", unitPrice: { amount: 4400, currency: "MYR" } }],
          },
        ],
      })
    ),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions/:revisionId/outcomes`, () => HttpResponse.json({
      outcomes: options.outcomes ?? [
        { id: "outcome-1", awardRevisionId: "revision-1", issuedRfqVersionId: "version-1", supplierId: "supplier-1", invitationId: "invitation-1", result: "selected", projection: {}, createdAt: "2026-08-23T12:19:00Z" },
        { id: "outcome-2", awardRevisionId: "revision-1", issuedRfqVersionId: "version-1", supplierId: "supplier-2", invitationId: "invitation-2", result: "selected", projection: {}, createdAt: "2026-08-23T12:19:00Z" },
      ],
    })),
    http.get(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions/:revisionId/notifications`, () => HttpResponse.json({ deliveries: options.deliveries ?? [] }))
  );
}

describe("ProjectProcurement finalised Award view (result-oriented, per the redesign)", () => {
  it("shows the finalised summary: total, line count, supplier count, and finalised date", async () => {
    mockFinalisedAwardScenario();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    expect(await screen.findByText(/award finalised/i)).toBeInTheDocument();
    expect(screen.getAllByText(/274,?750|2,?747\.50/).length).toBeGreaterThan(0);
    expect(screen.getByText(/2 material lines? · 2 suppliers?/i)).toBeInTheDocument();
    expect(screen.getByText(/^Finalised /)).toHaveTextContent(/23 aug 2026/i);
  });

  it("shows each Supplier's real name, item count, and awarded total on its own card", async () => {
    mockFinalisedAwardScenario();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    expect(screen.getAllByText("ABC Building Materials").length).toBeGreaterThan(0);
    expect(screen.getAllByText("TilePro Supplies").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/1 item awarded/i).length).toBe(2);
    expect(screen.getAllByText(/76,?750|767\.50/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/198,?000|1,?980\.00/).length).toBeGreaterThan(0);
  });

  it("shows the awarded items table with material, quantity, awarded supplier, unit price, and line total", async () => {
    mockFinalisedAwardScenario();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    expect(screen.getByText(/awarded items/i)).toBeInTheDocument();
    expect(screen.getAllByText("Cement").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Porcelain Floor Tile").length).toBeGreaterThan(0);
    expect(screen.getAllByText("25 bag").length).toBeGreaterThan(0);
    expect(screen.getAllByText("45 sqm").length).toBeGreaterThan(0);
  });

  it("never renders a disabled Select control as the primary interface for a finalised Award", async () => {
    mockFinalisedAwardScenario();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    expect(screen.queryByRole("button", { name: /^select$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /create award/i })).not.toBeInTheDocument();
  });

  it("keeps the original offer comparison available but collapsed and read-only, with awarded lines visually marked", async () => {
    mockFinalisedAwardScenario();
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    const toggle = screen.getByText(/view original offer comparison/i);
    // Collapsed by default: it's a native <details>, so its content exists in
    // the DOM either way — what's asserted is that it starts closed and opens
    // on click.
    const disclosure = toggle.closest("details");
    expect(disclosure).not.toBeNull();
    expect(disclosure).not.toHaveAttribute("open");

    await user.click(toggle);
    await waitFor(() => expect(disclosure).toHaveAttribute("open"));
    expect(screen.getByText(/read-only/i)).toBeInTheDocument();
    expect(screen.getAllByText(/✓ awarded/i).length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /select/i })).not.toBeInTheDocument();
  });

  it("keeps Award History collapsed by default, showing only revision/date/total until expanded", async () => {
    mockFinalisedAwardScenario();
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    expect(screen.getByText(/award history/i)).toBeInTheDocument();
    expect(screen.getByText(/revision 1 · finalised/i)).toBeInTheDocument();
    // Per-supplier detail is not shown until the entry is expanded.
    expect(screen.queryByText(/1 awarded line/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /revision 1 · finalised/i }));
    expect((await screen.findAllByText(/1 awarded line/i)).length).toBe(2);
  });

  it("does not prominently show raw technical identifiers (revision/RFQ version IDs) in Award History", async () => {
    mockFinalisedAwardScenario();
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    await user.click(screen.getByRole("button", { name: /revision 1 · finalised/i }));
    await screen.findAllByText(/1 awarded line/i);

    // The raw ID is not prominently visible ...
    expect(screen.queryByText("revision-1")).not.toBeInTheDocument();
    // ... it exists only behind the secondary "Technical details" disclosure.
    const technicalDetails = screen.getByText(/technical details/i);
    expect(technicalDetails).toBeInTheDocument();
    await user.click(technicalDetails);
    expect(await screen.findByText(/revision-1/i)).toBeInTheDocument();
  });

  it("shows 'No notification yet' with a Review & notify action when no delivery exists yet for a Supplier", async () => {
    mockFinalisedAwardScenario({ deliveries: [] });
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    await waitFor(() => expect(screen.getAllByText(/no notification yet/i).length).toBe(2));
    expect(screen.getAllByRole("button", { name: /review & notify/i }).length).toBe(2);
  });

  it("Review & notify sends via the real notification endpoint, using the Supplier's invitation identity — finalising itself never sent anything", async () => {
    mockFinalisedAwardScenario({ deliveries: [] });
    let sendBody: Record<string, unknown> | undefined;
    let sendCount = 0;
    server.use(
      http.post(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-revisions/:revisionId/outcomes/:outcomeId/notifications`, async ({ request }) => {
        sendCount += 1;
        sendBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: "delivery-1", awardOutcomeId: "outcome-1", awardRevisionId: "revision-1", supplierId: "supplier-1",
          recipientIdentity: "ali@abcbuilding.example", accessGeneration: 1, deliveryOperationId: "op-1", channel: "email",
          status: "sent", createdAt: "2026-08-23T12:20:00Z", sentAt: "2026-08-23T12:20:01Z",
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    // Finalising the Award (mocked as already-finalised on load) never called
    // the notification endpoint by itself.
    expect(sendCount).toBe(0);

    await waitFor(() => expect(screen.getAllByRole("button", { name: /review & notify/i }).length).toBe(2));
    const [firstReviewAndNotify] = screen.getAllByRole("button", { name: /review & notify/i });
    await user.click(firstReviewAndNotify);

    await waitFor(() => expect(sendCount).toBe(1));
    expect(sendBody).toMatchObject({ recipientIdentity: "ali@abcbuilding.example", accessGeneration: 1 });
  });

  it("shows Delivered status with a details action once a successful delivery exists, and no Send action", async () => {
    mockFinalisedAwardScenario({
      deliveries: [
        { id: "delivery-1", awardOutcomeId: "outcome-1", awardRevisionId: "revision-1", supplierId: "supplier-1", recipientIdentity: "ali@abcbuilding.example", accessGeneration: 1, deliveryOperationId: "op-1", channel: "email", status: "delivered", createdAt: "2026-08-23T12:20:00Z", sentAt: "2026-08-23T12:20:01Z" },
      ],
    });
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    await waitFor(() => expect(screen.getByText(/^delivered$/i)).toBeInTheDocument());
    // Only supplier-1's outcome has a delivery record — supplier-2 still
    // shows Review & notify, since notification is per-Supplier.
    expect(screen.getAllByRole("button", { name: /review & notify/i }).length).toBe(1);
  });

  it("shows Delivery failed with a Resend action, and technical failure detail only behind View delivery details", async () => {
    mockFinalisedAwardScenario({
      deliveries: [
        { id: "delivery-1", awardOutcomeId: "outcome-1", awardRevisionId: "revision-1", supplierId: "supplier-1", recipientIdentity: "ali@abcbuilding.example", accessGeneration: 1, deliveryOperationId: "op-1", channel: "email", status: "failed", failureCode: "smtp_rejected", createdAt: "2026-08-23T12:20:00Z" },
      ],
    });
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await screen.findByText(/award finalised/i);
    await waitFor(() => expect(screen.getByText(/delivery failed/i)).toBeInTheDocument());
    expect(screen.queryByText(/smtp_rejected/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^resend$/i })).toBeInTheDocument();
  });
});

describe("ProjectProcurement Award selection unaward flow", () => {
  it("opens an unaward dialog requiring a reason, and submits the reason via the real unaward endpoint", async () => {
    const issuedRfq = { ...draftRfq, status: "ready" };
    mockBase([], [issuedRfq]);
    mockComparisonRoutes({
      awardDraft: {
        id: "draft-1", awardChainId: "chain-1", issuedRfqVersionId: "version-1", revision: 1,
        status: "open", createdByUserId: "user-1", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
        lineDecisions: [{ issuedRfqLineId: "issued-line-1", stableLineageId: "lineage-1", decision: "selected", offerVersionId: "offer-v1", offerLineId: "offer-line-1" }],
      },
    });
    let unawardBody: Record<string, unknown> | undefined;
    server.use(
      http.put(`${baseUrl}/rfq-chains/:rfqChainId/issued-versions/:versionId/award-draft/lines/:lineId/unawarded`, async ({ request }) => {
        unawardBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          id: "draft-1", awardChainId: "chain-1", issuedRfqVersionId: "version-1", revision: 2,
          status: "open", createdByUserId: "user-1", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
          lineDecisions: [],
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);

    await user.click(await screen.findByRole("button", { name: /selected — unselect/i }));
    const dialog = await screen.findByRole("dialog", { name: /unselect this line/i });
    await user.click(within(dialog).getByRole("combobox"));
    await user.click(await screen.findByRole("option", { name: /no acceptable offer/i }));
    await user.click(within(dialog).getByRole("button", { name: /unselect line/i }));

    await waitFor(() => expect(unawardBody).toMatchObject({ reason: "no_acceptable_offer", expectedRevision: 1 }));
  });
});
