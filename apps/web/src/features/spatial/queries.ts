import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/features/auth/useAuth";
import type { ApiError } from "@/lib/api/errors";
import { getRoomDraft, getSpatialSpaceState, getVisualAssetAccess, listSpatialCaptures } from "./api";

export function roomDraftQueryKey(roomDraftId: string) {
  return ["spatial", "room-draft", roomDraftId] as const;
}

// AssetID is company-scoped server-side ((companyId, assetId, version) is
// the backend's actual compound key) — the query key MUST include the
// caller's own company, or two companies' sessions sharing one browser
// process (two tabs, a company switch) could alias one company's cached
// access grant onto another company's identical-looking assetId.
export function visualAssetAccessQueryKey(companyId: string, assetId: string, version: number) {
  return ["spatial", "visual-asset-access", companyId, assetId, version] as const;
}

function isAuthOrNotFoundError(error: unknown): boolean {
  const apiError = error as ApiError;
  return apiError?.kind === "api" && (apiError.status === 401 || apiError.status === 403 || apiError.status === 404);
}

// Mints (or returns the already-cached) short-lived read-access capability
// for one authorized visual asset version. Per RP4D: a successful grant is
// never considered stale merely because time passed (staleTime/gcTime:
// Infinity — the underlying asset version is immutable), and no periodic
// refetch is ever scheduled, since Three.js loader caches key on the URL
// itself and rotating it would fragment one asset into multiple cache
// entries. Never retries 401/403/404 (authorization failures are terminal,
// not transient); allows a couple of retries for network/5xx.
export function useVisualAssetAccess(assetId: string, version: number) {
  const { user } = useAuth();
  const companyId = user?.companyId;

  return useQuery({
    queryKey: visualAssetAccessQueryKey(companyId ?? "", assetId, version),
    queryFn: () => getVisualAssetAccess(assetId, version),
    enabled: Boolean(companyId) && Boolean(assetId) && Number.isInteger(version) && version > 0,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
    refetchInterval: false,
    retry: (failureCount, error) => {
      if (isAuthOrNotFoundError(error)) return false;
      return failureCount < 2;
    },
  });
}

export function useSpatialSpaceState(projectId: string, spaceId: string) {
  return useQuery({
    queryKey: ["spaces", spaceId, "spatial-state"],
    queryFn: () => getSpatialSpaceState(projectId, spaceId),
    enabled: Boolean(projectId) && Boolean(spaceId),
  });
}

export function useSpatialCaptureList(spaceId: string) {
  return useQuery({
    queryKey: ["spaces", spaceId, "spatial-captures"],
    queryFn: () => listSpatialCaptures(spaceId),
    enabled: Boolean(spaceId),
  });
}

export function useRoomDraft(roomDraftId: string | undefined) {
  return useQuery({
    queryKey: roomDraftQueryKey(roomDraftId ?? ""),
    queryFn: () => getRoomDraft(roomDraftId as string),
    enabled: Boolean(roomDraftId),
  });
}
