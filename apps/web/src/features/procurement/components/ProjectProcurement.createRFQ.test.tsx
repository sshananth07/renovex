import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectProcurement } from "./ProjectProcurement";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function mockBase() {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs: [] })),
    http.get(`${baseUrl}/material-requirements`, () => HttpResponse.json({ materialRequirements: [] })),
    http.get(`${baseUrl}/materials`, () => HttpResponse.json({ materials: [] })),
    http.get(`${baseUrl}/suppliers`, () => HttpResponse.json({ suppliers: [] }))
  );
}

const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

async function openCreateRFQDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: /create rfq/i }));
  return screen.findByRole("dialog");
}

describe("ProjectProcurement create RFQ form", () => {
  it("converts a datetime-local responseDeadline into a full RFC3339 timestamp, not a bare date", async () => {
    mockBase();
    let capturedBody: Record<string, unknown> | null = null;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/rfqs`, async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: "rfq1", rfqNumber: "RFQ-1", status: "draft", revision: 1 });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRFQDialog(user);

    await user.type(dialog.querySelector('input[name="responseDeadline"]') as HTMLElement, "2026-08-20T17:00");
    await user.click(within(dialog).getByRole("button", { name: /create draft rfq/i }));

    await waitFor(() => {
      expect(capturedBody).not.toBeNull();
    });
    const body = capturedBody as unknown as Record<string, unknown>;
    expect(typeof body.responseDeadline).toBe("string");
    expect(body.responseDeadline as string).toMatch(rfc3339);
    expect(body.responseDeadline).not.toBe("2026-08-20");
    expect(body.responseDeadline).not.toBe("2026-08-20T17:00");
  });

  it("cannot submit without a response deadline", async () => {
    mockBase();
    let requestMade = false;
    server.use(http.post(`${baseUrl}/projects/:projectId/rfqs`, () => { requestMade = true; return HttpResponse.json({ id: "rfq1" }); }));
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRFQDialog(user);

    await user.click(within(dialog).getByRole("button", { name: /create draft rfq/i }));

    expect(await within(dialog).findByText(/response deadline is required/i)).toBeInTheDocument();
    expect(requestMade).toBe(false);
  });

  it("maps a body.responseDeadline 422 error from the backend onto the field", async () => {
    mockBase();
    server.use(
      http.post(`${baseUrl}/projects/:projectId/rfqs`, () =>
        HttpResponse.json(
          {
            type: "about:blank",
            status: 422,
            title: "Unprocessable Entity",
            detail: "responseDeadline must be an RFC3339 timestamp",
            errors: [{ location: "body.responseDeadline", message: "responseDeadline must be an RFC3339 timestamp" }],
          },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectProcurement projectId="proj1" />);
    const dialog = await openCreateRFQDialog(user);

    await user.type(dialog.querySelector('input[name="responseDeadline"]') as HTMLElement, "2026-08-20T17:00");
    await user.click(within(dialog).getByRole("button", { name: /create draft rfq/i }));

    expect(await within(dialog).findByText(/responseDeadline must be an RFC3339 timestamp/i)).toBeInTheDocument();
  });
});
