import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ResourcesView } from "./ResourcesView";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

const worker = {
  id: "worker1",
  name: "Ahmad",
  trade: "Tiler",
  rateType: "daily",
  defaultRate: { amount: 15000, currency: "MYR" },
  createdAt: "2026-01-15T00:00:00Z",
};

const workItem = {
  id: "wi1",
  projectId: "proj1",
  description: "Retile bathroom",
  status: "in_progress",
  quantityValue: "1",
  quantityUnit: "unit",
  source: "manual",
  createdAt: "2026-01-15T00:00:00Z",
};

// Seven work items — enough to prove the selector isn't silently truncated to
// the first one by a mistaken pageSize:1 request (the regression this file
// guards against).
const sevenWorkItems = Array.from({ length: 7 }, (_, index) => ({
  ...workItem,
  id: `wi${index + 1}`,
  description: `Work item ${index + 1}`,
}));

function mockBase(workItems: typeof sevenWorkItems = [workItem]) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [] })),
    http.get(`${baseUrl}/workers`, () => HttpResponse.json({ workers: [worker] })),
    http.get(`${baseUrl}/labour-entries`, () => HttpResponse.json({ labourEntries: [] })),
    http.get(`${baseUrl}/work-items`, () => HttpResponse.json({ items: workItems, page: 1, pageSize: 100, total: workItems.length }))
  );
}

async function openLabourDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("tab", { name: /labour/i }));
  await user.click(await screen.findByRole("button", { name: /log labour/i }));
  const dialog = await screen.findByRole("dialog");
  await user.click(within(dialog).getAllByRole("combobox")[0]);
  await user.click(await screen.findByRole("option", { name: /retile bathroom/i }));
  return dialog;
}

describe("ResourcesView Log Labour work item selector", () => {
  it("offers all 7 work items, not just the first, and submits the id of the one actually selected", async () => {
    mockBase(sevenWorkItems);
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/labour-entries`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "le1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    await user.click(await screen.findByRole("tab", { name: /labour/i }));
    await user.click(await screen.findByRole("button", { name: /log labour/i }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getAllByRole("combobox")[0]);

    for (const item of sevenWorkItems) {
      expect(await screen.findByRole("option", { name: item.description })).toBeInTheDocument();
    }
    await user.click(await screen.findByRole("option", { name: "Work item 7" }));

    await user.type(dialog.querySelector('input[name="workerName"]') as HTMLElement, "Day Labourer");
    await user.type(dialog.querySelector('input[name="trade"]') as HTMLElement, "General");
    await user.click(within(dialog).getByRole("button", { name: /create labour entry/i }));

    await waitFor(() => expect(capturedBody).toMatchObject({ workItemId: "wi7" }));
  });
});

describe("ResourcesView Log Labour tab/field synchronization", () => {
  it("defaults to Ad-hoc active with ad-hoc fields visible, and switching tabs keeps active state and rendered fields in agreement", async () => {
    mockBase();
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    await user.click(await screen.findByRole("tab", { name: /labour/i }));
    await user.click(await screen.findByRole("button", { name: /log labour/i }));
    const dialog = await screen.findByRole("dialog");

    const existingTab = within(dialog).getByRole("button", { name: /existing worker/i });
    const adhocTab = within(dialog).getByRole("button", { name: /^ad-hoc labour$/i });

    // Default: ad-hoc fields (Worker name/Trade) visible, no existing-worker
    // combobox for a worker — the rendered fields are the ground truth for
    // which mode is actually active, since that's what a submit would use.
    expect(dialog.querySelector('input[name="workerName"]')).toBeInTheDocument();
    expect(dialog.querySelector('input[name="trade"]')).toBeInTheDocument();
    expect(within(dialog).queryByText(/select worker/i)).not.toBeInTheDocument();

    // Switch to Existing worker — fields must flip together with the tab.
    await user.click(existingTab);
    expect(within(dialog).getByText(/select worker/i)).toBeInTheDocument();
    expect(dialog.querySelector('input[name="workerName"]')).not.toBeInTheDocument();
    expect(dialog.querySelector('input[name="trade"]')).not.toBeInTheDocument();

    // Switch back to Ad-hoc — fields must flip back together too.
    await user.click(adhocTab);
    expect(dialog.querySelector('input[name="workerName"]')).toBeInTheDocument();
    expect(within(dialog).queryByText(/select worker/i)).not.toBeInTheDocument();
  });
});

describe("ResourcesView ad-hoc labour form", () => {
  it("cannot submit ad-hoc labour without a trade", async () => {
    mockBase();
    let requestMade = false;
    server.use(http.post(`${baseUrl}/labour-entries`, () => { requestMade = true; return HttpResponse.json({ id: "le1" }); }));
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    const dialog = await openLabourDialog(user);

    // Default mode is ad-hoc; fill worker name but leave trade blank.
    const nameInput = dialog.querySelector('input[name="workerName"]') as HTMLElement;
    await user.type(nameInput, "Day Labourer");
    await user.click(within(dialog).getByRole("button", { name: /create labour entry/i }));

    expect(await within(dialog).findByText(/trade is required for ad-hoc labour/i)).toBeInTheDocument();
    expect(requestMade).toBe(false);
  });

  it("submits workerName + trade + rateAmount for ad-hoc mode, with quantity as a string", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/labour-entries`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "le1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    const dialog = await openLabourDialog(user);

    await user.type(dialog.querySelector('input[name="workerName"]') as HTMLElement, "Day Labourer");
    await user.type(dialog.querySelector('input[name="trade"]') as HTMLElement, "General");
    await user.click(within(dialog).getByRole("button", { name: /create labour entry/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({
        workerName: "Day Labourer",
        trade: "General",
        quantityValue: "1",
      });
    });
    const body = capturedBody as unknown as Record<string, unknown>;
    expect(typeof body.quantityValue).toBe("string");
    expect(typeof body.rateAmount).toBe("number");
  });

  it("existing-worker mode submits workerId without workerName/trade", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/labour-entries`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "le1" });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    const dialog = await openLabourDialog(user);

    await user.click(within(dialog).getByRole("button", { name: /existing worker/i }));
    const comboboxes = within(dialog).getAllByRole("combobox");
    await user.click(comboboxes[comboboxes.length - 1]);
    await user.click(await screen.findByRole("option", { name: /ahmad/i }));
    await user.click(within(dialog).getByRole("button", { name: /create labour entry/i }));

    await waitFor(() => {
      expect(capturedBody).toMatchObject({ workerId: "worker1" });
    });
    const body = capturedBody as unknown as Record<string, unknown>;
    expect(body.workerName).toBeUndefined();
    expect(body.trade).toBeUndefined();
    expect(body.rateAmount).toBeUndefined();
  });

  it("maps a body.rateAmount 422 error from the backend onto the rate field", async () => {
    // Fill workerName/trade so client-side Zod passes and the request actually
    // reaches the network; the backend is the one rejecting rateAmount here
    // (e.g. a value Zod's format check let through but the server still rejects).
    mockBase();
    server.use(
      http.post(`${baseUrl}/labour-entries`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 422,
            title: "Unprocessable Entity",
            errors: [{ location: "body.rateAmount", message: "rate is required for ad-hoc labour" }],
          },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ResourcesView projectId="proj1" />);
    const dialog = await openLabourDialog(user);

    await user.type(dialog.querySelector('input[name="workerName"]') as HTMLElement, "Day Labourer");
    await user.type(dialog.querySelector('input[name="trade"]') as HTMLElement, "General");
    await user.click(within(dialog).getByRole("button", { name: /create labour entry/i }));

    expect(await within(dialog).findByText(/rate is required for ad-hoc labour/i)).toBeInTheDocument();
  });
});
