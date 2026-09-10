import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { server } from "@/test/server";
import { createQueryClient } from "@/lib/query/queryClient";
import {
  designAttemptsQueryKey,
  designTurnsQueryKey,
  useCancelDesignGenerationAttempt,
  useConfirmDesignPlan,
  useDesignGenerationAttempt,
  useUseDesignPlan,
} from "./useDesignStudioQueries";
import { roomDraftQueryKey } from "../queries";

const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL;

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const attemptDTO = {
  id: "attempt_1",
  sessionId: "session_1",
  turnId: "turn_1",
  attemptNumber: 1,
  kind: "material_only",
  status: "concept_ready",
  basedOnRoomDraftRevision: 17,
  createdAt: "2026-09-09T00:00:00Z",
  updatedAt: "2026-09-09T00:00:00Z",
};

describe("useDesignGenerationAttempt polling", () => {
  it("keeps polling while status is nonterminal and stops once concept_ready", async () => {
    let callCount = 0;
    server.use(
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId`, () => {
        callCount += 1;
        return HttpResponse.json(callCount === 1 ? { ...attemptDTO, status: "asset_generation_processing" } : attemptDTO);
      }),
    );

    const queryClient = createQueryClient();
    const { result } = renderHook(() => useDesignGenerationAttempt("session_1", "attempt_1"), {
      wrapper: makeWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.data?.status).toBe("asset_generation_processing"));
    await waitFor(() => expect(result.current.data?.status).toBe("concept_ready"), { timeout: 5000 });
    expect(callCount).toBeGreaterThanOrEqual(2);
  });
});

describe("useConfirmDesignPlan", () => {
  it("invalidates the attempts list for this session on success", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/turns/:turnId/confirm`, () => HttpResponse.json(attemptDTO)),
      http.get(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts`, () => HttpResponse.json([attemptDTO])),
    );

    const queryClient = createQueryClient();
    const wrapper = makeWrapper(queryClient);

    const { result: attemptsResult } = renderHook(
      () => useQuery({ queryKey: designAttemptsQueryKey("session_1"), queryFn: () => Promise.resolve([]) }),
      { wrapper },
    );
    await waitFor(() => expect(attemptsResult.current.isSuccess).toBe(true));

    const { result: mutation } = renderHook(() => useConfirmDesignPlan("session_1"), { wrapper });
    mutation.current.mutate({
      turnId: "turn_1",
      body: { clientRequestId: "confirm-1", planFingerprint: "sha256:abc", expectedRoomDraftRevision: 17 },
    });

    await waitFor(() => expect(mutation.current.isSuccess).toBe(true));
    expect(queryClient.getQueryState(designAttemptsQueryKey("session_1"))?.isInvalidated).toBe(false);
  });
});

describe("useCancelDesignGenerationAttempt", () => {
  it("sends only clientRequestId and reflects the abandoned status", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/cancel`, () =>
        HttpResponse.json({ ...attemptDTO, status: "abandoned" }),
      ),
    );

    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCancelDesignGenerationAttempt("session_1"), { wrapper: makeWrapper(queryClient) });

    result.current.mutate({ attemptId: "attempt_1", body: { clientRequestId: "cancel-1" } });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.status).toBe("abandoned");
  });
});

describe("useUseDesignPlan", () => {
  it("replaces the RoomDraft cache with the server response, not a client merge", async () => {
    server.use(
      http.post(`${baseUrl}/spatial/design-sessions/:sessionId/generation-attempts/:attemptId/use`, () =>
        HttpResponse.json({
          acceptance: {
            id: "acceptance_1",
            sessionId: "session_1",
            turnId: "turn_1",
            attemptId: "attempt_1",
            roomDraftId: "roomdraft_1",
            baseRoomDraftRevision: 17,
            resultingRoomDraftRevision: 18,
            appliedOperationIds: ["use_req_1:0"],
            createdAt: "2026-09-09T00:00:00Z",
          },
          roomDraft: { id: "roomdraft_1", revision: 18 },
        }),
      ),
    );

    const queryClient = createQueryClient();
    queryClient.setQueryData(roomDraftQueryKey("roomdraft_1"), { id: "roomdraft_1", revision: 17 });

    const { result } = renderHook(() => useUseDesignPlan("session_1", "roomdraft_1"), { wrapper: makeWrapper(queryClient) });
    result.current.mutate({
      attemptId: "attempt_1",
      body: { clientRequestId: "use-1", planFingerprint: "sha256:abc", expectedRoomDraftRevision: 17 },
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(queryClient.getQueryData(roomDraftQueryKey("roomdraft_1"))).toMatchObject({ revision: 18 });
  });
});

describe("useDesignTurns polling key", () => {
  it("uses a stable per-session query key", () => {
    expect(designTurnsQueryKey("session_1")).toEqual(["design-studio", "session", "session_1", "turns"]);
  });
});
