import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { ProjectBriefForm } from "./ProjectBriefForm";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("ProjectBriefForm", () => {
  it("saves the brief and calls scope-brief PATCH", async () => {
    let capturedBody: unknown = null;
    server.use(
      http.patch(`${baseUrl}/projects/:projectId/scope-brief`, async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({
          id: "p1", clientId: "c1", name: "x", status: "lead",
          scopeBrief: "Full renovation of a condo.", createdAt: "2026-08-16T00:00:00Z",
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectBriefForm projectId="p1" initialBrief="" />);

    await user.type(screen.getByLabelText(/project brief/i), "Full renovation of a condo.");
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(capturedBody).toEqual({ scopeBrief: "Full renovation of a condo." }));
  });

  it("rejects a brief over 5000 characters", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ProjectBriefForm projectId="p1" initialBrief="" />);

    const textarea = screen.getByLabelText(/project brief/i);
    // fireEvent-style paste is far faster than user.type for 5001 chars.
    await user.click(textarea);
    await user.paste("a".repeat(5001));
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/5000/i)).toBeInTheDocument();
  });

  it("disables the save button while the mutation is pending", async () => {
    server.use(
      http.patch(`${baseUrl}/projects/:projectId/scope-brief`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return HttpResponse.json({
          id: "p1", clientId: "c1", name: "x", status: "lead", scopeBrief: "x", createdAt: "2026-08-16T00:00:00Z",
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectBriefForm projectId="p1" initialBrief="" />);

    await user.type(screen.getByLabelText(/project brief/i), "Full renovation.");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByRole("button", { name: /saving/i })).toBeDisabled();
  });

  it("maps a backend field error through the existing error-display helper", async () => {
    server.use(
      http.patch(`${baseUrl}/projects/:projectId/scope-brief`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Unprocessable Entity", status: 422, detail: "scope brief exceeds maximum length" },
          { status: 422 }
        )
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<ProjectBriefForm projectId="p1" initialBrief="" />);

    await user.type(screen.getByLabelText(/project brief/i), "Full renovation.");
    await user.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/exceeds maximum length/i)).toBeInTheDocument();
  });
});
