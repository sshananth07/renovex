import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as api from "./api";
import { procurementKeys } from "./queryKeys";
import type { components } from "@/lib/api/generated/schema";

type ResolveDiscrepancyBody = components["schemas"]["ResolveDiscrepancyInputBody"];

// Every mutation here awaits its query invalidation (which, with React
// Query's default refetchType, also awaits the refetch of currently-mounted
// observers) INSIDE mutationFn, before the promise resolves — so
// `mutation.isPending` does not flip to false until the fresh authoritative
// record is already in the cache. This matters because the next legal
// action (e.g. Add to RFQ after Review) depends on a revision that only
// exists after the refetch completes; a fire-and-forget `onSuccess`
// invalidation would let a card render its old action, or a second submit
// race ahead of the refetch.

export function useReviewRequirement(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ requirementId, expectedRevision }: { requirementId: string; expectedRevision: number }) => {
      const result = await api.reviewMaterialRequirement(requirementId, expectedRevision);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) });
      return result;
    },
  });
}

export function useArchiveRequirement(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ requirementId, expectedRevision }: { requirementId: string; expectedRevision: number }) => {
      const result = await api.archiveMaterialRequirement(requirementId, expectedRevision);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) });
      return result;
    },
  });
}

// Unlike archive, this is irreversible — the requirement is gone, not
// merely marked terminal.
export function useDeleteRequirement(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ requirementId, expectedRevision }: { requirementId: string; expectedRevision: number }) => {
      await api.deleteMaterialRequirement(requirementId, expectedRevision);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) });
    },
  });
}

export function useAcknowledgeUnitMismatch(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ requirementId, expectedRevision }: { requirementId: string; expectedRevision: number }) => {
      const result = await api.acknowledgeUnitMismatch(requirementId, expectedRevision);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) });
      return result;
    },
  });
}

export function useResolveSourceDiscrepancy(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ requirementId, body }: { requirementId: string; body: ResolveDiscrepancyBody }) => {
      const result = await api.resolveSourceDiscrepancy(requirementId, body);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) }),
        queryClient.invalidateQueries({ queryKey: procurementKeys.sourceDiscrepancy(requirementId) }),
      ]);
      return result;
    },
  });
}

// Serialized per selected RFQ by the caller (ProjectProcurement owns a
// single shared mutation instance per selected RFQ id, so `isPending`
// naturally disables every Add-to-RFQ action targeting that RFQ while one
// is in flight — see design spec §11). Combines the requirement's own fresh
// revision (from the presentation action) with the currently-selected RFQ's
// latest revision at call time; RequirementPrimaryAction never carries an
// RFQ revision itself.
export function useAddRequirementToRFQ(projectId: string, rfqId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      materialRequirementId,
      expectedRequirementRevision,
      expectedRfqRevision,
    }: {
      materialRequirementId: string;
      expectedRequirementRevision: number;
      expectedRfqRevision: number;
    }) => {
      const result = await api.addRFQLine(rfqId, {
        materialRequirementId,
        expectedRequirementRevision,
        expectedRfqRevision,
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: procurementKeys.requirements(projectId) }),
        queryClient.invalidateQueries({ queryKey: procurementKeys.rfqs(projectId) }),
      ]);
      return result;
    },
  });
}

export function useUpdateOffering(supplierId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ offeringId, body }: { offeringId: string; body: Parameters<typeof api.updateOffering>[1] }) => {
      const result = await api.updateOffering(offeringId, body);
      await queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId, "offerings"] });
      return result;
    },
  });
}

export function useSetOfferingActive(supplierId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ offeringId, expectedRevision, active }: { offeringId: string; expectedRevision: number; active: boolean }) => {
      const result = await api.setOfferingActive(offeringId, expectedRevision, active);
      await queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId, "offerings"] });
      return result;
    },
  });
}

export function useUpdateRFQ(projectId: string, rfqId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: components["schemas"]["PatchRFQInputBody"]) => {
      const result = await api.patchRFQ(rfqId, body);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.rfqs(projectId) });
      return result;
    },
  });
}

export function useMarkRFQReady(projectId: string, rfqId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ expectedRevision }: { expectedRevision: number }) => {
      const result = await api.readyRFQ(rfqId, expectedRevision);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.rfqs(projectId) });
      return result;
    },
  });
}

// RFQ creation is not idempotent (a retry allocates a new RFQ number and
// creates a second empty draft) — retry is explicitly disabled, and the
// caller must also disable the trigger button while pending.
export function useCreateRFQ(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    retry: false,
    mutationFn: async (body: components["schemas"]["CreateRFQInputBody"]) => {
      const result = await api.createRFQ(projectId, body);
      await queryClient.invalidateQueries({ queryKey: procurementKeys.rfqs(projectId) });
      return result;
    },
  });
}

// Never invoked automatically on Award finalisation — an explicit contractor
// action only (§8I). Sending is idempotent per operationId (generated by
// api.sendAwardOutcomeNotification), so a retried click resolves to the
// existing delivery record rather than notifying the Supplier twice.
type NotificationContext = { companyName?: string; supplierName?: string; rfqNumber?: string; rfqTitle?: string };

export function useSendAwardOutcomeNotification(rfqChainId: string, versionId: string, revisionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ outcomeId, recipientIdentity, accessGeneration, ...context }: { outcomeId: string; recipientIdentity: string; accessGeneration: number } & NotificationContext) => {
      const result = await api.sendAwardOutcomeNotification(rfqChainId, versionId, revisionId, outcomeId, { recipientIdentity, accessGeneration, ...context });
      await queryClient.invalidateQueries({ queryKey: ["rfq-chains", rfqChainId, "versions", versionId, "awards", revisionId, "notifications"] });
      return result;
    },
  });
}

export function useRetryAwardOutcomeNotification(rfqChainId: string, versionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ deliveryId, recipientIdentity, accessGeneration, ...context }: { deliveryId: string; recipientIdentity: string; accessGeneration: number } & NotificationContext) => {
      return api.retryAwardOutcomeNotification(rfqChainId, versionId, deliveryId, { recipientIdentity, accessGeneration, ...context });
    },
    onSuccess: () => {
      // The revisionId a given delivery belongs to isn't known by this hook's
      // caller-independent signature, so invalidate every notifications query
      // under this issued version — cheap: at most one Award revision per
      // issued version has deliveries in practice.
      queryClient.invalidateQueries({ queryKey: ["rfq-chains", rfqChainId, "versions", versionId, "awards"], predicate: (query) => query.queryKey.at(-1) === "notifications" });
    },
  });
}

