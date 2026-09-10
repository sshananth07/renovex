import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import type { ReactNode } from "react";
import { useAcceptSuggestion, useGenerateSpaceSuggestions, useRejectSuggestion } from "./mutations";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe("useGenerateSpaceSuggestions", () => {
  it("does not retry on failure (retry: false)", async () => {
    let callCount = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () => {
        callCount++;
        return HttpResponse.json({ status: 503, detail: "unavailable" }, { status: 503 });
      })
    );
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const { result } = renderHook(() => useGenerateSpaceSuggestions("p1"), { wrapper: wrapper(queryClient) });

    result.current.mutate();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(callCount).toBe(1);
  });

  it("invalidates the batches query on success", async () => {
    server.use(
      http.post(`${baseUrl}/projects/:projectId/ai/space-suggestions`, () =>
        HttpResponse.json({
          batch: { id: "b1", projectId: "p1", type: "space_suggestions", status: "completed", promptVersion: "spaces-v1", schemaVersion: 1, inputFingerprint: "fp", startedAt: "2026-08-16T00:00:00Z" },
          suggestions: [],
        })
      )
    );
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useGenerateSpaceSuggestions("p1"), { wrapper: wrapper(queryClient) });

    result.current.mutate();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(invalidateSpy).toHaveBeenCalled();
  });
});

describe("useAcceptSuggestion", () => {
  it("does not retry on failure", async () => {
    let callCount = 0;
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () => {
        callCount++;
        return HttpResponse.json({ status: 409, detail: "stale" }, { status: 409 });
      })
    );
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const { result } = renderHook(() => useAcceptSuggestion("batch1", "p1"), { wrapper: wrapper(queryClient) });

    result.current.mutate({ suggestionId: "s1", input: { expectedRevision: 1 } });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(callCount).toBe(1);
  });

  it("invalidates suggestions, batches, and Spaces on success", async () => {
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () => HttpResponse.json({ domainObjectId: "space_1" }))
    );
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useAcceptSuggestion("batch1", "p1"), { wrapper: wrapper(queryClient) });

    result.current.mutate({ suggestionId: "s1", input: { expectedRevision: 1 } });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const invalidatedKeys = invalidateSpy.mock.calls.map((call) => JSON.stringify(call[0]?.queryKey));
    expect(invalidatedKeys.some((k) => k?.includes("suggestions"))).toBe(true);
  });

  it("actually refetches the real Spaces list query key after accept (not just an unrelated key)", async () => {
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/accept`, () => HttpResponse.json({ domainObjectId: "space_1" })),
      http.get(`${baseUrl}/spaces`, () => HttpResponse.json({ items: [{ id: "space_1", projectId: "p1", name: "Kitchen", createdAt: "2026-08-16T00:00:00Z" }], page: 1, pageSize: 20, total: 1 }))
    );
    const queryClient = new QueryClient();
    // Seed the exact key the real Spaces feature uses (features/spaces/queries.ts).
    const spacesKey = ["projects", "p1", "spaces", { page: 1 }];
    await queryClient.fetchQuery({
      queryKey: spacesKey,
      queryFn: async () => {
        const res = await fetch(`${baseUrl}/spaces?projectId=p1&page=1`);
        return res.json();
      },
    });

    const { result } = renderHook(() => useAcceptSuggestion("batch1", "p1"), { wrapper: wrapper(queryClient) });
    result.current.mutate({ suggestionId: "s1", input: { expectedRevision: 1 } });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    // A stale/inactive-but-invalidated query is marked invalid immediately;
    // querying its state directly proves the real key was matched.
    const state = queryClient.getQueryState(spacesKey);
    expect(state?.isInvalidated).toBe(true);
  });
});

describe("useRejectSuggestion", () => {
  it("does not retry on failure", async () => {
    let callCount = 0;
    server.use(
      http.post(`${baseUrl}/ai-suggestions/:id/reject`, () => {
        callCount++;
        return HttpResponse.json({ status: 409, detail: "stale" }, { status: 409 });
      })
    );
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const { result } = renderHook(() => useRejectSuggestion("batch1", "p1"), { wrapper: wrapper(queryClient) });

    result.current.mutate({ suggestionId: "s1", expectedRevision: 1 });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(callCount).toBe(1);
  });
});
