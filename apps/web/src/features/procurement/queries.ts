import { useQuery } from "@tanstack/react-query";
import * as api from "./api";
import { procurementKeys } from "./queryKeys";
export const useSuppliers = (query: { q?: string; category?: string; active?: string } = {}) => useQuery({ queryKey: ["suppliers", query], queryFn: () => api.listSuppliers(query) });
export const useSupplier = (id: string) => useQuery({ queryKey: ["suppliers", id], queryFn: () => api.getSupplier(id), enabled: Boolean(id) });
export const useOfferings = (supplierId: string) => useQuery({ queryKey: ["suppliers", supplierId, "offerings"], queryFn: () => api.listOfferings(supplierId), enabled: Boolean(supplierId) });
export const useRequirements = (projectId: string) => useQuery({ queryKey: procurementKeys.requirements(projectId), queryFn: () => api.listRequirements(projectId), enabled: Boolean(projectId) });
export const useRFQs = (projectId: string) => useQuery({ queryKey: procurementKeys.rfqs(projectId), queryFn: () => api.listRFQs(projectId), enabled: Boolean(projectId) });
// listIssuedVersionsOrEmpty normalizes the expected 404 (no issuance chain
// yet for a never-issued RFQ) into an empty array, so `data === []` is the
// natural "no issued versions yet" state and `error` reflects only real
// failures.
// A draft RFQ structurally has no issuance chain yet (it cannot have been
// issued and reopened to draft), so the caller passes enabled=false for
// draft RFQs to skip the request entirely rather than rely on the 404
// fallback in listIssuedVersionsOrEmpty.
export const useIssuedVersions = (chainId: string, enabled = true) => useQuery({ queryKey: procurementKeys.rfqVersions(chainId), queryFn: () => api.listIssuedVersionsOrEmpty(chainId), enabled: Boolean(chainId) && enabled });
export const useSourceDiscrepancy = (requirementId: string, enabled: boolean) => useQuery({ queryKey: procurementKeys.sourceDiscrepancy(requirementId), queryFn: () => api.getSourceDiscrepancy(requirementId), enabled: enabled && Boolean(requirementId) });
export const useInvitations = (chainId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "invitations"], queryFn: () => api.listInvitations(chainId), enabled: Boolean(chainId) });
export const useComparison = (chainId: string, versionId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "versions", versionId, "comparison"], queryFn: () => api.getComparison(chainId, versionId), enabled: Boolean(chainId && versionId) });
// listAwardRevisionsOrEmpty normalizes the expected 404 (no award created yet
// for this issued version) into an empty array, matching the
// listIssuedVersionsOrEmpty pattern, so a missing award never contributes to
// the page-level query-error banner.
export const useAwardRevisions = (chainId: string, versionId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "versions", versionId, "awards"], queryFn: () => api.listAwardRevisionsOrEmpty(chainId, versionId), enabled: Boolean(chainId && versionId) });
// getAwardDraftOrEmpty normalizes the expected 404 (nothing selected yet, or
// the Award is already finalised and its draft was consumed) into
// `undefined`, matching useAwardRevisions/useIssuedVersions — a missing open
// draft is a normal state, never a page-level query-error.
export const useAwardDraft = (chainId: string, versionId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "versions", versionId, "award-draft"], queryFn: () => api.getAwardDraftOrEmpty(chainId, versionId), enabled: Boolean(chainId && versionId) });
// One outcomes fetch and one deliveries fetch per finalised revision, not one
// per Supplier — the finalised award view maps both onto its Supplier cards
// itself (design note: avoid an N-suppliers x N-calls notification pattern).
export const useAwardOutcomes = (chainId: string, versionId: string, revisionId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "versions", versionId, "awards", revisionId, "outcomes"], queryFn: () => api.listAwardOutcomes(chainId, versionId, revisionId), enabled: Boolean(chainId && versionId && revisionId) });
export const useAwardDeliveries = (chainId: string, versionId: string, revisionId: string) => useQuery({ queryKey: ["rfq-chains", chainId, "versions", versionId, "awards", revisionId, "notifications"], queryFn: () => api.listAwardDeliveries(chainId, versionId, revisionId), enabled: Boolean(chainId && versionId && revisionId) });
export const useProjectProcurementSummary = (projectId: string) => useQuery({ queryKey: ["projects", projectId, "procurement-summary"], queryFn: () => api.getProjectProcurementSummary(projectId), enabled: Boolean(projectId) });
