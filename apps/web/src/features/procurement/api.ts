import { apiClient } from "@/lib/api/client";
import { unwrapOrThrow } from "@/lib/api/errors";
import type { components } from "@/lib/api/generated/schema";
import { listProjects, type Project } from "@/features/projects/api";

export type Supplier = components["schemas"]["SupplierDTO"];
export type RFQ = components["schemas"]["RfqDTO"];
export type Invitation = components["schemas"]["InvitationDTO"];
export type IssuedVersion = components["schemas"]["IssuedVersionDTO"];
export type Comparison = components["schemas"]["ComparisonDTO"];
export type AwardRevision = components["schemas"]["AwardRevisionDTO"];
export type AwardOutcome = components["schemas"]["AwardOutcomeDTO"];
export type AwardDelivery = components["schemas"]["AwardDeliveryDTO"];
export type AwardDraft = components["schemas"]["AwardDraftDTO"];
export type Offering = components["schemas"]["OfferingDTO"];

export async function listSuppliers(query: { q?: string; category?: string; active?: string } = {}) { const { data, error } = await apiClient.GET("/suppliers", { params: { query } }); if (error) throw error; return data.suppliers; }
export async function getSupplier(id: string) { const { data, error } = await apiClient.GET("/suppliers/{id}", { params: { path: { id } } }); if (error) throw error; return data; }
export async function createSupplier(body: components["schemas"]["CreateSupplierInputBody"]) { return unwrapOrThrow(await apiClient.POST("/suppliers", { body })); }
export async function updateSupplier(id: string, body: components["schemas"]["PatchSupplierInputBody"]) { const { data, error } = await apiClient.PATCH("/suppliers/{id}", { params: { path: { id } }, body }); if (error) throw error; return data; }
export async function listOfferings(supplierId: string) { const { data, error } = await apiClient.GET("/supplier-offerings", { params: { query: { supplierId } } }); if (error) throw error; return data.offerings; }
export async function createOffering(body: components["schemas"]["CreateOfferingInputBody"]) { return unwrapOrThrow(await apiClient.POST("/supplier-offerings", { body })); }
export async function updateOffering(id: string, body: components["schemas"]["PatchOfferingInputBody"]) { return unwrapOrThrow(await apiClient.PATCH("/supplier-offerings/{id}", { params: { path: { id } }, body })); }
export async function setOfferingActive(id: string, expectedRevision: number, active: boolean) { return unwrapOrThrow(await apiClient.POST("/supplier-offerings/{id}/active", { params: { path: { id } }, body: { expectedRevision, active } })); }
export async function listRequirements(projectId: string) { const { data, error } = await apiClient.GET("/material-requirements", { params: { query: { projectId } } }); if (error) throw error; return data.materialRequirements; }
export async function createRequirement(body: components["schemas"]["CreateRequirementInputBody"]) { return unwrapOrThrow(await apiClient.POST("/material-requirements", { body })); }
// Idempotent: creates missing anchors AND re-syncs sourceSyncState on
// existing cost_item-sourced requirements against their current cost items in
// the same call — a rerun on unchanged cost items reports them under
// unchangedCount rather than creating duplicates.
export async function generateRequirementsFromCosts(projectId: string) { return unwrapOrThrow(await apiClient.POST("/projects/{projectId}/material-requirements/generate", { params: { path: { projectId } } })); }
export async function reviewMaterialRequirement(id: string, expectedRevision: number) { return unwrapOrThrow(await apiClient.POST("/material-requirements/{id}/review", { params: { path: { id } }, body: { expectedRevision } })); }
export async function acknowledgeUnitMismatch(id: string, expectedRevision: number) { return unwrapOrThrow(await apiClient.POST("/material-requirements/{id}/acknowledge-unit", { params: { path: { id } }, body: { expectedRevision } })); }
export async function archiveMaterialRequirement(id: string, expectedRevision: number) { return unwrapOrThrow(await apiClient.POST("/material-requirements/{id}/archive", { params: { path: { id } }, body: { expectedRevision } })); }
export async function deleteMaterialRequirement(id: string, expectedRevision: number) { unwrapOrThrow(await apiClient.DELETE("/material-requirements/{id}", { params: { path: { id } }, body: { expectedRevision } })); }
export async function getSourceDiscrepancy(id: string) { return unwrapOrThrow(await apiClient.GET("/material-requirements/{id}/source-discrepancy", { params: { path: { id } } })); }
export async function resolveSourceDiscrepancy(id: string, body: components["schemas"]["ResolveDiscrepancyInputBody"]) { return unwrapOrThrow(await apiClient.POST("/material-requirements/{id}/source-discrepancy/resolve", { params: { path: { id } }, body })); }
export async function listRFQs(projectId: string) { const { data, error } = await apiClient.GET("/rfqs", { params: { query: { projectId } } }); if (error) throw error; return data.rfqs; }
export async function createRFQ(projectId: string, body: components["schemas"]["CreateRFQInputBody"]) { return unwrapOrThrow(await apiClient.POST("/projects/{projectId}/rfqs", { params: { path: { projectId } }, body })); }
export async function patchRFQ(id: string, body: components["schemas"]["PatchRFQInputBody"]) { return unwrapOrThrow(await apiClient.PATCH("/rfqs/{id}", { params: { path: { id } }, body })); }
export async function addRFQLine(id: string, body: components["schemas"]["AddLineInputBody"]) { return unwrapOrThrow(await apiClient.POST("/rfqs/{id}/lines", { params: { path: { id } }, body })); }
export async function readyRFQ(id: string, expectedRevision: number) { return unwrapOrThrow(await apiClient.POST("/rfqs/{id}/ready", { params: { path: { id } }, body: { expectedRevision } })); }
export async function reopenRFQ(id: string, expectedRevision: number) { return unwrapOrThrow(await apiClient.POST("/rfqs/{id}/reopen", { params: { path: { id } }, body: { expectedRevision } })); }
export async function issueRFQ(rfqChainId: string) { return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/issue", { params: { path: { rfqChainId } }, body: { currency: "MYR", operationId: crypto.randomUUID() } })); }
export async function listIssuedVersions(rfqChainId: string) { const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/versions", { params: { path: { rfqChainId } } }); if (error) throw error; return data.versions ?? []; }
export async function listIssuedVersionsOrEmpty(rfqChainId: string) {
  try {
    return await listIssuedVersions(rfqChainId);
  } catch (error) {
    if (isNotFoundError(error)) return [];
    throw error;
  }
}

function isNotFoundError(error: unknown): boolean {
  return typeof error === "object" && error !== null && "status" in error && (error as { status?: number }).status === 404;
}
export async function listInvitations(rfqChainId: string) { const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/invitations", { params: { path: { rfqChainId } } }); if (error) throw error; return data.invitations; }
export async function createInvitation(rfqChainId: string, body: components["schemas"]["CreateInvitationHTTPInputBody"]) { return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/invitations", { params: { path: { rfqChainId } }, body })); }
export async function sendInvitation(rfqChainId: string, invitationId: string) { return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/invitations/{invitationId}/send", { params: { path: { rfqChainId, invitationId } }, body: { operationId: crypto.randomUUID() } })); }
export async function copyInvitationLink(rfqChainId: string, invitationId: string) { return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/invitations/{invitationId}/copy-link", { params: { path: { rfqChainId, invitationId } } })); }
export async function getComparison(rfqChainId: string, versionId: string) { const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/comparison", { params: { path: { rfqChainId, versionId } } }); if (error) throw error; return data; }
export async function getAwardDraft(rfqChainId: string, versionId: string) { const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-draft", { params: { path: { rfqChainId, versionId } } }); if (error) throw error; return data; }
// A chain with no open draft yet is a normal, common state (nothing selected
// so far, or the Award is already finalised and its draft was consumed) —
// the backend reports this as a bounded 404, matching listAwardRevisionsOrEmpty's
// same "expected miss, not a real failure" reasoning below.
export async function getAwardDraftOrEmpty(rfqChainId: string, versionId: string) {
  try {
    return await getAwardDraft(rfqChainId, versionId);
  } catch (error) {
    // React Query's queryFn contract forbids returning undefined — null is
    // the valid "no data" sentinel.
    if (isNotFoundError(error)) return null;
    throw error;
  }
}
export async function createAwardDraft(rfqChainId: string, versionId: string) { const { data, error } = await apiClient.POST("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-draft", { params: { path: { rfqChainId, versionId } } }); if (error) throw error; return data; }
export async function selectAwardLine(rfqChainId: string, versionId: string, lineId: string, body: components["schemas"]["SelectAwardLineInputBody"]) { return unwrapOrThrow(await apiClient.PUT("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-draft/lines/{lineId}/selection", { params: { path: { rfqChainId, versionId, lineId } }, body })); }
export async function unawardLine(rfqChainId: string, versionId: string, lineId: string, body: components["schemas"]["UnawardLineInputBody"]) { return unwrapOrThrow(await apiClient.PUT("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-draft/lines/{lineId}/unawarded", { params: { path: { rfqChainId, versionId, lineId } }, body })); }
export async function finalizeAward(rfqChainId: string, versionId: string) { return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions", { params: { path: { rfqChainId, versionId } }, body: { operationId: crypto.randomUUID() } })); }
export async function listAwardRevisions(rfqChainId: string, versionId: string) { return unwrapOrThrow(await apiClient.GET("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions", { params: { path: { rfqChainId, versionId } } })).revisions ?? []; }
// Before any award exists for an issued version, the backend collapses
// "no award yet" and every other award-lookup miss into the same generic 404
// ("award resource not found") to avoid disclosing which case applies to a
// caller. That collapsing is safe to treat as "no award yet" here because the
// only caller (useAwardRevisions) always supplies rfqChainId/versionId
// already sourced from loaded, verified data — never a user-guessed id — so
// a 404 in this call path can only mean the award hasn't been created yet.
export async function listAwardRevisionsOrEmpty(rfqChainId: string, versionId: string) {
  try {
    return await listAwardRevisions(rfqChainId, versionId);
  } catch (error) {
    if (isNotFoundError(error)) return [];
    throw error;
  }
}

// Outcomes are created automatically for every awarded Supplier at
// finalisation (one per awarded-line group) — this only reads them, it never
// creates one. A Supplier notification is sent against a specific outcome.
export async function listAwardOutcomes(rfqChainId: string, versionId: string, revisionId: string) {
  const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions/{revisionId}/outcomes", { params: { path: { rfqChainId, versionId, revisionId } } });
  if (error) throw error;
  return data.outcomes ?? [];
}

// One call for the whole revision, not one per Supplier — the caller maps
// deliveries back onto outcomes/suppliers itself, so opening the finalised
// award view never fans out into a delivery-attempts request per Supplier.
export async function listAwardDeliveries(rfqChainId: string, versionId: string, revisionId: string) {
  const { data, error } = await apiClient.GET("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions/{revisionId}/notifications", { params: { path: { rfqChainId, versionId, revisionId } } });
  if (error) throw error;
  return data.deliveries ?? [];
}

// Sending is idempotent per operationId — the backend resolves a same-operation
// retry to the existing record rather than sending twice (§8I).
type NotificationContext = { companyName?: string; supplierName?: string; rfqNumber?: string; rfqTitle?: string };

export async function sendAwardOutcomeNotification(rfqChainId: string, versionId: string, revisionId: string, outcomeId: string, body: { recipientIdentity: string; accessGeneration: number } & NotificationContext) {
  return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/award-revisions/{revisionId}/outcomes/{outcomeId}/notifications", { params: { path: { rfqChainId, versionId, revisionId, outcomeId } }, body: { operationId: crypto.randomUUID(), ...body } }));
}

export async function retryAwardOutcomeNotification(rfqChainId: string, versionId: string, deliveryId: string, body: { recipientIdentity: string; accessGeneration: number } & NotificationContext) {
  return unwrapOrThrow(await apiClient.POST("/rfq-chains/{rfqChainId}/issued-versions/{versionId}/notifications/{deliveryId}/retry", { params: { path: { rfqChainId, versionId, deliveryId } }, body: { operationId: crypto.randomUUID(), ...body } }));
}

export async function getProjectProcurementSummary(projectId: string) {
  const rfqs = await listRFQs(projectId);
  const details = await Promise.all((rfqs ?? []).map(async (rfq) => {
    const versions = await listIssuedVersionsOrEmpty(rfq.id);
    const issued = versions.at(-1);
    if (!issued) return { invitations: 0, offers: 0, awards: 0, reviewRequired: 0 };
    const [invitations, comparison, awards] = await Promise.all([
      listInvitations(rfq.id),
      getComparison(rfq.id, issued.id),
      listAwardRevisionsOrEmpty(rfq.id, issued.id),
    ]);
    const offers = comparison.offers ?? [];
    return {
      invitations: (invitations ?? []).length,
      offers: offers.length,
      awards: awards.length,
      reviewRequired: offers.filter((offer) => !offer.selectable).length,
    };
  }));
  return details.reduce<{ rfqs: number; invitations: number; offers: number; awards: number; reviewRequired: number }>((summary, detail) => ({
    rfqs: summary.rfqs,
    invitations: summary.invitations + detail.invitations,
    offers: summary.offers + detail.offers,
    awards: summary.awards + detail.awards,
    reviewRequired: summary.reviewRequired + detail.reviewRequired,
  }), { rfqs: rfqs?.length ?? 0, invitations: 0, offers: 0, awards: 0, reviewRequired: 0 });
}

export interface PortfolioRFQ {
  project: Project;
  rfq: RFQ;
  invitationCount: number;
  offerCount: number;
  reviewRequiredCount: number;
  awardCount: number;
}

export async function listPortfolioProcurement() {
  const pageSize = 100;
  const first = await listProjects({ page: 1, pageSize });
  const pages = Math.max(1, Math.ceil(first.total / pageSize));
  const rest = await Promise.all(Array.from({ length: pages - 1 }, (_, index) => listProjects({ page: index + 2, pageSize })));
  const projects = [first, ...rest].flatMap((page) => page.items ?? []);
  const projectRFQs = await Promise.all(projects.map(async (project) => ({ project, rfqs: await listRFQs(project.id) })));
  const rows = projectRFQs.flatMap(({ project, rfqs }) => (rfqs ?? []).map((rfq) => ({ project, rfq })));
  return Promise.all(rows.map(async ({ project, rfq }): Promise<PortfolioRFQ> => {
    const [invites, issuedVersions] = await Promise.all([listInvitations(rfq.id), listIssuedVersionsOrEmpty(rfq.id)]);
    const issued = issuedVersions.at(-1);
    if (!issued) return { project, rfq, invitationCount: invites?.length ?? 0, offerCount: 0, reviewRequiredCount: 0, awardCount: 0 };
    const [comparison, awards] = await Promise.all([getComparison(rfq.id, issued.id), listAwardRevisionsOrEmpty(rfq.id, issued.id)]);
    const offers = comparison.offers ?? [];
    return { project, rfq, invitationCount: invites?.length ?? 0, offerCount: offers.length, reviewRequiredCount: offers.filter((offer) => !offer.selectable).length, awardCount: awards.length };
  }));
}
