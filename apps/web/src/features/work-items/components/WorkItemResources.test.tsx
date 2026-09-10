import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { renderWithProviders } from "@/test/renderWithProviders";
import { WorkItemResources } from "./WorkItemResources";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

describe("WorkItemResources", () => {
  it("shows nothing to indicate a section when there are no requirements for this Work Item", async () => {
    server.use(
      http.get(`${baseUrl}/work-resource-requirements`, () => HttpResponse.json({ requirements: [] }))
    );
    renderWithProviders(<WorkItemResources projectId="p1" workItemId="wi1" />);
    expect(await screen.findByText(/no resources yet/i)).toBeInTheDocument();
  });

  it("groups this Work Item's confirmed requirements by resource type", async () => {
    server.use(
      http.get(`${baseUrl}/work-resource-requirements`, () =>
        HttpResponse.json({
          requirements: [
            { id: "r1", projectId: "p1", workItemId: "wi1", resourceType: "material", materialId: "m1", name: "Tile Adhesive", status: "confirmed", source: "ai_suggestion", createdAt: "2026-08-16T00:00:00Z" },
            { id: "r2", projectId: "p1", workItemId: "wi1", resourceType: "trade", name: "Tiler", status: "confirmed", source: "manual", createdAt: "2026-08-16T00:00:00Z" },
            { id: "r3", projectId: "p1", workItemId: "wi2", resourceType: "equipment", name: "Tile Cutter", status: "confirmed", source: "ai_suggestion", createdAt: "2026-08-16T00:00:00Z" },
          ],
        })
      )
    );
    renderWithProviders(<WorkItemResources projectId="p1" workItemId="wi1" />);

    expect(await screen.findByText("Tile Adhesive")).toBeInTheDocument();
    expect(screen.getByText("Tiler")).toBeInTheDocument();
    expect(screen.queryByText("Tile Cutter")).not.toBeInTheDocument();
  });
});
