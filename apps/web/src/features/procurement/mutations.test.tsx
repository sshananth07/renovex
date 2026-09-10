import { renderHook, waitFor } from "@testing-library/react";
import { act } from "react";
import { describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { createQueryClient } from "@/lib/query/queryClient";
import { procurementKeys } from "./queryKeys";
import * as api from "./api";
import {
  useAcknowledgeUnitMismatch,
  useAddRequirementToRFQ,
  useCreateRFQ,
  useMarkRFQReady,
  useResolveSourceDiscrepancy,
  useReviewRequirement,
} from "./mutations";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const requirement = {
  id: "req-1",
  projectId: "proj-1",
  materialId: "mat-1",
  materialName: "Cement",
  catalogUnit: "bag",
  requiredQuantity: { value: "35", unit: "bag" },
  status: "reviewed",
  sourceType: "manual",
  sourceSyncState: "clean",
  unitMismatch: false,
  unitMismatchAcknowledged: false,
  revision: 4,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

// Mounts both the mutation and a live observer for the query key(s) it's
// expected to invalidate, so invalidateQueries actually triggers a refetch
// (React Query only refetches currently-observed queries) — proving the
// mutation genuinely awaits that refetch before its own promise resolves,
// not just that invalidateQueries was called.
function useReviewRequirementWithObserver(projectId: string) {
  const requirements = useQuery({
    queryKey: procurementKeys.requirements(projectId),
    queryFn: () => api.listRequirements(projectId),
  });
  const mutation = useReviewRequirement(projectId);
  return { requirements, mutation };
}

describe("procurement mutations await invalidation before settling", () => {
  it("useReviewRequirement's promise does not resolve until the requirements refetch completes", async () => {
    let refetchCount = 0;
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/review`, () => HttpResponse.json({ ...requirement, revision: 5 })),
      http.get(`${baseUrl}/material-requirements`, () => {
        refetchCount += 1;
        return HttpResponse.json({ materialRequirements: [{ ...requirement, revision: refetchCount > 1 ? 5 : 4 }] });
      })
    );
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useReviewRequirementWithObserver("proj-1"), {
      wrapper: makeWrapper(queryClient),
    });
    await waitFor(() => expect(result.current.requirements.isSuccess).toBe(true));
    expect(refetchCount).toBe(1);

    await act(async () => {
      await result.current.mutation.mutateAsync({ requirementId: "req-1", expectedRevision: 4 });
    });

    // The refetch triggered by invalidateQueries must have already
    // completed by the time mutateAsync resolves.
    expect(refetchCount).toBe(2);
    await waitFor(() => expect(result.current.requirements.data?.[0]?.revision).toBe(5));
    expect(result.current.mutation.isPending).toBe(false);
  });

  it("useAcknowledgeUnitMismatch resolves after invalidating the requirements list", async () => {
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/acknowledge-unit`, () => HttpResponse.json(requirement)),
      http.get(`${baseUrl}/material-requirements`, () => HttpResponse.json({ materialRequirements: [requirement] }))
    );
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useAcknowledgeUnitMismatch("proj-1"), { wrapper: makeWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ requirementId: "req-1", expectedRevision: 4 });
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.isPending).toBe(false);
  });

  it("useResolveSourceDiscrepancy resolves after invalidating requirements and the discrepancy query", async () => {
    server.use(
      http.post(`${baseUrl}/material-requirements/:id/source-discrepancy/resolve`, () => HttpResponse.json(requirement)),
      http.get(`${baseUrl}/material-requirements`, () => HttpResponse.json({ materialRequirements: [requirement] })),
      http.get(`${baseUrl}/material-requirements/:id/source-discrepancy`, () =>
        HttpResponse.json({
          requirementRevision: 4,
          syncState: "clean",
          anchorStatus: "reviewed",
          accepted: { quantity: { value: "35", unit: "bag" }, costItemIds: [], fingerprint: "fp" },
          proposed: { quantity: { value: "35", unit: "bag" }, costItemIds: [], fingerprint: "fp" },
          availableActions: [],
        })
      )
    );
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useResolveSourceDiscrepancy("proj-1"), { wrapper: makeWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({
        requirementId: "req-1",
        body: { action: "keep_current", expectedRevision: 4, expectedProposedFingerprint: "fp-1" },
      });
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.isPending).toBe(false);
  });

  it("useAddRequirementToRFQ's promise does not resolve until both requirements and RFQ refetches complete", async () => {
    let requirementsRefetchCount = 0;
    let rfqsRefetchCount = 0;
    server.use(
      http.post(`${baseUrl}/rfqs/:id/lines`, () => HttpResponse.json({ id: "rfq-1", status: "draft", revision: 5, lines: [] })),
      http.get(`${baseUrl}/material-requirements`, () => {
        requirementsRefetchCount += 1;
        return HttpResponse.json({ materialRequirements: [requirement] });
      }),
      http.get(`${baseUrl}/rfqs`, () => {
        rfqsRefetchCount += 1;
        return HttpResponse.json({ rfqs: [{ id: "rfq-1", status: "draft", revision: rfqsRefetchCount > 1 ? 5 : 4, lines: [] }] });
      })
    );
    const queryClient = createQueryClient();
    function useObservers() {
      const requirements = useQuery({ queryKey: procurementKeys.requirements("proj-1"), queryFn: () => api.listRequirements("proj-1") });
      const rfqs = useQuery({ queryKey: procurementKeys.rfqs("proj-1"), queryFn: () => api.listRFQs("proj-1") });
      const mutation = useAddRequirementToRFQ("proj-1", "rfq-1");
      return { requirements, rfqs, mutation };
    }
    const { result } = renderHook(() => useObservers(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(result.current.requirements.isSuccess && result.current.rfqs.isSuccess).toBe(true));
    expect(requirementsRefetchCount).toBe(1);
    expect(rfqsRefetchCount).toBe(1);

    await act(async () => {
      await result.current.mutation.mutateAsync({
        materialRequirementId: "req-1",
        expectedRequirementRevision: 4,
        expectedRfqRevision: 4,
      });
    });

    expect(requirementsRefetchCount).toBe(2);
    expect(rfqsRefetchCount).toBe(2);
    await waitFor(() => expect(result.current.rfqs.data?.[0]?.revision).toBe(5));
    expect(result.current.mutation.isPending).toBe(false);
  });

  it("useMarkRFQReady resolves after invalidating the RFQ list", async () => {
    server.use(
      http.post(`${baseUrl}/rfqs/:id/ready`, () => HttpResponse.json({ id: "rfq-1", status: "ready", revision: 2, lines: [] })),
      http.get(`${baseUrl}/rfqs`, () => HttpResponse.json({ rfqs: [] }))
    );
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useMarkRFQReady("proj-1", "rfq-1"), { wrapper: makeWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ expectedRevision: 1 });
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.isPending).toBe(false);
  });

  it("useCreateRFQ does not retry after a failure", async () => {
    let attempts = 0;
    server.use(
      http.post(`${baseUrl}/projects/:projectId/rfqs`, () => {
        attempts += 1;
        return HttpResponse.json({ type: "about:blank", status: 500, title: "Internal Server Error" }, { status: 500 });
      })
    );
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCreateRFQ("proj-1"), { wrapper: makeWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ title: "Test RFQ" }).catch(() => {});
    });

    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(attempts).toBe(1);
  });
});
