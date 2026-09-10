import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PropertyDetail } from "@/features/properties/components/PropertyDetail";
import { SpaceList } from "@/features/spaces/components/SpaceList";
import { WorkItemList } from "@/features/work-items/components/WorkItemList";
import { renderWithProviders } from "@/test/renderWithProviders";
import { server } from "@/test/server";
import { ProjectOverview } from "./ProjectOverview";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;
const projectId = "progress-project";

vi.mock("@/lib/url/listParams", () => ({
  useListSearchParams: () => ({ params: { page: 1, pageSize: 25 }, setParams: vi.fn() }),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

interface BackendState {
  property: Record<string, unknown> | null;
  spaces: Array<Record<string, unknown>>;
  workItems: Array<Record<string, unknown>>;
}

function installMutableBackend(state: BackendState) {
  server.use(
    http.post(`${baseUrl}/auth/refresh`, () => new HttpResponse(null, { status: 401 })),
    http.get(`${baseUrl}/projects/:id`, () =>
      HttpResponse.json({
        id: projectId,
        clientId: "progress-client",
        name: "Progress Renovation",
        status: "in_progress",
        createdAt: "2026-08-08T00:00:00Z",
      })
    ),
    http.get(`${baseUrl}/clients/:id`, () =>
      HttpResponse.json({
        id: "progress-client",
        name: "Progress Client",
        createdAt: "2026-08-08T00:00:00Z",
      })
    ),
    http.get(`${baseUrl}/properties`, () =>
      HttpResponse.json({ properties: state.property ? [state.property] : [] })
    ),
    http.post(`${baseUrl}/properties`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      state.property = {
        id: "progress-property",
        ...body,
        createdAt: "2026-08-08T00:01:00Z",
      };
      return HttpResponse.json(state.property);
    }),
    http.get(`${baseUrl}/spaces`, () =>
      HttpResponse.json({
        items: state.spaces,
        page: 1,
        pageSize: 25,
        total: state.spaces.length,
      })
    ),
    http.post(`${baseUrl}/spaces`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      const space = {
        id: "progress-space",
        ...body,
        createdAt: "2026-08-08T00:02:00Z",
      };
      state.spaces.push(space);
      return HttpResponse.json(space);
    }),
    http.get(`${baseUrl}/work-items`, () =>
      HttpResponse.json({
        items: state.workItems,
        page: 1,
        pageSize: 25,
        total: state.workItems.length,
      })
    ),
    http.post(`${baseUrl}/work-items`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      const workItem = {
        id: "progress-work-item",
        status: "planned",
        source: "manual",
        verificationStatus: "confirmed",
        ...body,
        createdAt: "2026-08-08T00:03:00Z",
      };
      state.workItems.push(workItem);
      return HttpResponse.json(workItem);
    })
  );
}

async function expectProgress(completed: number) {
  expect(await screen.findByText(`${completed} of 3 complete`)).toBeInTheDocument();

  const expected = {
    Property: completed >= 1 ? "Complete" : "Set up",
    Spaces: completed >= 2 ? "Complete" : "Set up",
    "Work Items": completed >= 3 ? "Complete" : "Set up",
  };

  for (const [label, status] of Object.entries(expected)) {
    const setup = screen.getByRole("region", { name: /project setup/i });
    const link = within(setup).getByRole("link", { name: new RegExp(label, "i") });
    expect(within(link).getByText(new RegExp(status, "i"))).toBeInTheDocument();
  }
}

describe("Project setup progression", () => {
  let state: BackendState;

  beforeEach(() => {
    state = { property: null, spaces: [], workItems: [] };
    installMutableBackend(state);
  });

  it("re-derives 0/3 through 3/3 from persisted resources after every reload", async () => {
    const user = userEvent.setup();

    let view = renderWithProviders(<ProjectOverview projectId={projectId} />);
    await expectProgress(0);
    view.unmount();

    view = renderWithProviders(<PropertyDetail projectId={projectId} />);
    await user.click(await screen.findByRole("button", { name: /add property/i }));
    await user.type(screen.getByLabelText(/address/i), "8 Jalan Progress");
    await user.click(screen.getByRole("button", { name: /save/i }));
    expect(await screen.findByText("8 Jalan Progress")).toBeInTheDocument();
    view.unmount();

    view = renderWithProviders(<ProjectOverview projectId={projectId} />);
    await expectProgress(1);
    view.unmount();

    view = renderWithProviders(<SpaceList projectId={projectId} />);
    fireEvent.click(await screen.findByRole("button", { name: /add space/i }));
    const spaceDialog = await screen.findByRole("dialog");
    await user.type(within(spaceDialog).getByLabelText(/name/i), "Kitchen");
    await user.click(within(spaceDialog).getByRole("button", { name: /save/i }));
    await waitFor(() => expect(state.spaces).toHaveLength(1));
    view.unmount();

    view = renderWithProviders(<ProjectOverview projectId={projectId} />);
    await expectProgress(2);
    view.unmount();

    view = renderWithProviders(<WorkItemList projectId={projectId} />);
    (await screen.findByRole("button", { name: /add work item/i })).click();
    const workItemDrawer = await screen.findByRole("dialog");
    await user.type(within(workItemDrawer).getByLabelText(/description/i), "Install cabinets");
    await user.type(within(workItemDrawer).getByLabelText(/quantity/i), "12.5");
    await user.type(within(workItemDrawer).getByLabelText(/unit/i), "sqft");
    await user.click(within(workItemDrawer).getByRole("button", { name: /save/i }));
    await waitFor(() => expect(state.workItems).toHaveLength(1));
    view.unmount();

    // A fresh provider on every overview render models a hard reload: no
    // prior QueryClient survives, so progress can only come from the API.
    renderWithProviders(<ProjectOverview projectId={projectId} />);
    await expectProgress(3);
  });
});
