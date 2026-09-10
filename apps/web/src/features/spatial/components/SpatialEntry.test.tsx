import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { SpatialEntry } from "./SpatialEntry";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function mockAuth() {
  server.use(http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })));
}

describe("SpatialEntry", () => {
  it("shows a no-scan-yet entry state with a scan CTA when there are no captures", async () => {
    mockAuth();
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json([])),
      http.get(`${baseUrl}/spatial/space-state`, () => HttpResponse.json({ spaceId: "space_x" }))
    );
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    expect(await screen.findByText(/no spatial scan yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /scan with renovex capture/i })).toBeInTheDocument();
  });

  it("shows the current room scan and previous scans when captures exist", async () => {
    mockAuth();
    const captures = [
      {
        id: "c1",
        projectId: "p1",
        spaceId: "space_x",
        status: "confirmed",
        roomVersionId: "v1",
        provider: "roomplan",
        captureNumber: 2,
        createdAt: "2026-01-15T00:00:00Z",
        updatedAt: "2026-01-15T00:05:00Z",
      },
      {
        id: "c2",
        projectId: "p1",
        spaceId: "space_x",
        status: "draft",
        provider: "roomplan",
        captureNumber: 1,
        createdAt: "2026-01-10T00:00:00Z",
        updatedAt: "2026-01-10T00:00:00Z",
      },
    ];
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json(captures)),
      http.get(`${baseUrl}/spatial/space-state`, () =>
        HttpResponse.json({ spaceId: "space_x", currentRoomVersionId: "v1" })
      )
    );
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    expect(await screen.findByText(/current room scan/i)).toBeInTheDocument();
    expect(screen.getByText(/previous scans/i)).toBeInTheDocument();
    expect(screen.getByText("Current")).toBeInTheDocument();
    expect(screen.getByText("Scan #2")).toBeInTheDocument();
    expect(screen.getByText("Scan #1")).toBeInTheDocument();
  });

  it("links both the current and a previous scan to their floor plan when a roomDraftId exists", async () => {
    mockAuth();
    const captures = [
      {
        id: "c1",
        projectId: "p1",
        spaceId: "space_x",
        status: "confirmed",
        roomVersionId: "v1",
        roomDraftId: "draft_current",
        provider: "roomplan",
        captureNumber: 2,
        createdAt: "2026-01-15T00:00:00Z",
        updatedAt: "2026-01-15T00:05:00Z",
      },
      {
        id: "c2",
        projectId: "p1",
        spaceId: "space_x",
        status: "draft",
        roomDraftId: "draft_previous",
        provider: "roomplan",
        captureNumber: 1,
        createdAt: "2026-01-10T00:00:00Z",
        updatedAt: "2026-01-10T00:00:00Z",
      },
    ];
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json(captures)),
      http.get(`${baseUrl}/spatial/space-state`, () =>
        HttpResponse.json({ spaceId: "space_x", currentRoomVersionId: "v1" })
      )
    );
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    await screen.findByText("Scan #1");
    const links = screen.getAllByRole("link", { name: /open floor plan/i });
    expect(links).toHaveLength(2);
    expect(links.some((l) => l.getAttribute("href") === "/projects/p1/spaces/space_x/spatial/draft_current")).toBe(
      true
    );
    expect(links.some((l) => l.getAttribute("href") === "/projects/p1/spaces/space_x/spatial/draft_previous")).toBe(
      true
    );
  });

  it("shows a disabled-style Open floor plan affordance (not a link) for a scan with no roomDraftId yet", async () => {
    mockAuth();
    const captures = [
      {
        id: "c1",
        projectId: "p1",
        spaceId: "space_x",
        status: "confirmed",
        roomVersionId: "v1",
        roomDraftId: "draft_current",
        provider: "roomplan",
        captureNumber: 2,
        createdAt: "2026-01-15T00:00:00Z",
        updatedAt: "2026-01-15T00:05:00Z",
      },
      {
        id: "c2",
        projectId: "p1",
        spaceId: "space_x",
        status: "draft",
        provider: "roomplan",
        captureNumber: 1,
        createdAt: "2026-01-10T00:00:00Z",
        updatedAt: "2026-01-10T00:00:00Z",
      },
    ];
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json(captures)),
      http.get(`${baseUrl}/spatial/space-state`, () =>
        HttpResponse.json({ spaceId: "space_x", currentRoomVersionId: "v1" })
      )
    );
    const user = userEvent.setup();
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    await screen.findByText("Scan #1");
    const openFloorPlanControls = screen.getAllByText(/open floor plan/i);
    expect(openFloorPlanControls).toHaveLength(2);

    // The current scan (has roomDraftId) is a real navigable link.
    expect(screen.getAllByRole("link", { name: /open floor plan/i })).toHaveLength(1);
    // The previous scan (no roomDraftId) is a button, not a link — clicking
    // it must not throw or navigate anywhere.
    const buttons = screen.getAllByRole("button", { name: /open floor plan/i });
    expect(buttons).toHaveLength(1);
    await user.click(buttons[0]);
  });

  it("shows a Continue Review affordance for a capture awaiting review", async () => {
    mockAuth();
    const captures = [
      {
        id: "c1",
        projectId: "p1",
        spaceId: "space_x",
        status: "review",
        provider: "roomplan",
        captureNumber: 1,
        createdAt: "2026-01-15T00:00:00Z",
        updatedAt: "2026-01-15T00:05:00Z",
      },
    ];
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json(captures)),
      http.get(`${baseUrl}/spatial/space-state`, () => HttpResponse.json({ spaceId: "space_x" }))
    );
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    expect(await screen.findByText("Continue Review")).toBeInTheDocument();
    expect(screen.getByText("Awaiting review")).toBeInTheDocument();
  });

  it("starts a new capture when the scan CTA is clicked from the empty state", async () => {
    mockAuth();
    let startCalled = false;
    server.use(
      http.get(`${baseUrl}/spatial/captures`, () => HttpResponse.json([])),
      http.get(`${baseUrl}/spatial/space-state`, () => HttpResponse.json({ spaceId: "space_x" })),
      http.post(`${baseUrl}/spatial/captures`, () => {
        startCalled = true;
        return HttpResponse.json({
          id: "c1", projectId: "p1", spaceId: "space_x", status: "draft",
          provider: "roomplan", captureNumber: 1,
          createdAt: "2026-01-15T00:00:00Z", updatedAt: "2026-01-15T00:00:00Z",
        });
      })
    );
    const user = userEvent.setup();
    renderWithProviders(<SpatialEntry projectId="p1" spaceId="space_x" />);

    const button = await screen.findByRole("button", { name: /scan with renovex capture/i });
    await user.click(button);

    expect(startCalled).toBe(true);
  });
});
