import createClient from "openapi-fetch";
import { z } from "zod";
import type { components, paths } from "@/lib/api/generated/schema";

export type ClientQuotation = components["schemas"]["ClientQuotationDTO"];
export type SupplierDraft = components["schemas"]["DraftDTO"];

const portalClient = createClient<paths>({
  baseUrl: process.env.NEXT_PUBLIC_API_BASE_URL ?? "",
  credentials: "include",
  fetch: (request) => globalThis.fetch(request),
});

const supplierInvitationSchema = z.object({
  invitationId: z.string().min(1),
  status: z.string(),
  expiresAt: z.string(),
  currentRfqVersion: z.object({
    id: z.string(),
    rfqNumber: z.string(),
    versionNumber: z.number(),
    currency: z.string(),
    title: z.string(),
    deliveryAddress: z.string(),
    requiredByDate: z.string().nullable().optional(),
    responseDeadline: z.string(),
    supplierInstructions: z.string(),
    lines: z.array(z.object({
      id: z.string(),
      materialName: z.string(),
      specification: z.string(),
      quantityValue: z.string(),
      quantityUnit: z.string(),
      requiredByDate: z.string().nullable().optional(),
      procurementNotes: z.string(),
      sortOrder: z.number(),
    })),
  }),
  responseWindow: z.object({
    status: z.string(),
    deadline: z.string(),
    canRespond: z.boolean(),
  }),
});

export type SupplierInvitation = z.infer<typeof supplierInvitationSchema>;

export async function getClientQuotation(token: string) {
  const { data, error } = await portalClient.GET("/client/quotations/{token}", {
    params: { path: { token } },
  });
  if (error) throw error;
  return data;
}

export async function decideClientQuotation(
  token: string,
  decision: "accept" | "reject" | "request-changes",
  body: components["schemas"]["ClientDecisionRequestInputBody"],
) {
  const path = decision === "accept"
    ? "/client/quotations/{token}/accept" as const
    : decision === "reject"
      ? "/client/quotations/{token}/reject" as const
      : "/client/quotations/{token}/request-changes" as const;
  const { data, error } = await portalClient.POST(path, {
    params: { path: { token } },
    body,
  });
  if (error) throw error;
  return data;
}

function csrfToken() {
  return document.cookie
    .split("; ")
    .find((part) => part.startsWith("supplier_csrf="))
    ?.split("=")
    .slice(1)
    .join("=") ?? "";
}

export async function openSupplierAccess(token: string) {
  const { data, error } = await portalClient.GET("/supplier-access/open", {
    params: { query: { token } },
  });
  if (error) throw error;
  return data;
}

export async function createSupplierChallenge() {
  const { data, error } = await portalClient.POST("/supplier-access/challenges", {
    body: { operationId: crypto.randomUUID() },
  });
  if (error) throw error;
  return data;
}

export async function verifySupplierChallenge(challengeId: string, code: string) {
  const { data, error } = await portalClient.POST("/supplier-access/challenges/verify", {
    body: { challengeId, code, operationId: crypto.randomUUID() },
  });
  if (error) throw error;
  return data;
}

export async function resendSupplierChallenge(challengeId: string) {
  const { data, error } = await portalClient.POST("/supplier-access/challenges/resend", {
    body: { challengeId, operationId: crypto.randomUUID() },
  });
  if (error) throw error;
  return data;
}

export async function getSupplierSession() {
  const { data, error } = await portalClient.GET("/supplier-access/session");
  if (error) throw error;
  const invitationId = data?.invitationId;
  if (!invitationId) throw new Error("Supplier session has no invitation");
  return { invitationId };
}

export async function getSupplierInvitation(invitationId: string) {
  const { data, error } = await portalClient.GET(
    "/supplier-access/invitations/{invitationId}",
    { params: { path: { invitationId } } },
  );
  if (error) throw error;
  return supplierInvitationSchema.parse(data);
}

export async function createSupplierDraft(invitationId: string) {
  const { data, error } = await portalClient.POST(
    "/supplier-access/invitations/{invitationId}/offer",
    { params: { path: { invitationId }, header: { "X-CSRF-Token": csrfToken() } } },
  );
  if (error) throw error;
  return data;
}

export async function quoteSupplierLine(
  invitationId: string,
  lineId: string,
  draft: SupplierDraft,
  values: Omit<components["schemas"]["Supplier-access-offer-quote-lineRequest"], "draftId" | "expectedRevision">,
) {
  const { data, error } = await portalClient.PUT(
    "/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/quote",
    {
      params: {
        path: { invitationId, lineId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: { draftId: draft.id, expectedRevision: draft.revision, ...values },
    },
  );
  if (error) throw error;
  return data;
}

export async function declineSupplierLine(
  invitationId: string,
  lineId: string,
  draft: SupplierDraft,
  responseStatus: "no_bid" | "unavailable",
  supplierLineNotes?: string,
) {
  const { data, error } = await portalClient.PUT(
    "/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/decline",
    {
      params: {
        path: { invitationId, lineId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: {
        draftId: draft.id,
        expectedRevision: draft.revision,
        responseStatus,
        supplierLineNotes,
      },
    },
  );
  if (error) throw error;
  return data;
}

export async function resetSupplierLine(
  invitationId: string,
  lineId: string,
  draft: SupplierDraft,
) {
  const { data, error } = await portalClient.POST(
    "/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/reset",
    {
      params: {
        path: { invitationId, lineId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: { expectedRevision: draft.revision },
    },
  );
  if (error) throw error;
  return data;
}

async function reviseSupplierDraft(
  path:
    | "/supplier-access/invitations/{invitationId}/offer/copy-forward"
    | "/supplier-access/invitations/{invitationId}/offer/tax/acknowledge"
    | "/supplier-access/invitations/{invitationId}/offer/delivery-charge/acknowledge",
  invitationId: string,
  draft: SupplierDraft,
) {
  const { data, error } = await portalClient.POST(path, {
    params: {
      path: { invitationId },
      header: { "X-CSRF-Token": csrfToken() },
    },
    body: { expectedRevision: draft.revision },
  });
  if (error) throw error;
  return data;
}

export function copyForwardSupplierOffer(invitationId: string, draft: SupplierDraft) {
  return reviseSupplierDraft(
    "/supplier-access/invitations/{invitationId}/offer/copy-forward",
    invitationId,
    draft,
  );
}

export function acknowledgeSupplierTax(invitationId: string, draft: SupplierDraft) {
  return reviseSupplierDraft(
    "/supplier-access/invitations/{invitationId}/offer/tax/acknowledge",
    invitationId,
    draft,
  );
}

export function acknowledgeSupplierDelivery(invitationId: string, draft: SupplierDraft) {
  return reviseSupplierDraft(
    "/supplier-access/invitations/{invitationId}/offer/delivery-charge/acknowledge",
    invitationId,
    draft,
  );
}

export async function acknowledgeSupplierChargeGroup(
  invitationId: string,
  chargeGroupId: string,
  draft: SupplierDraft,
) {
  const { data, error } = await portalClient.POST(
    "/supplier-access/invitations/{invitationId}/offer/charge-groups/{chargeGroupId}/acknowledge",
    {
      params: {
        path: { invitationId, chargeGroupId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: { expectedRevision: draft.revision },
    },
  );
  if (error) throw error;
  return data;
}

const offerHistorySchema = z.object({
  versions: z.array(z.object({
    id: z.string(),
    versionNumber: z.number(),
    currency: z.string(),
    submittedAt: z.string(),
    offerValidUntil: z.string(),
    publicStatus: z.string(),
    isSuperseded: z.boolean(),
    canWithdraw: z.boolean(),
    grandTotal: z.object({ amountMinor: z.number(), currency: z.string() }),
  })),
  nextCursor: z.number().nullable().optional(),
});

export async function listSupplierOfferVersions(invitationId: string) {
  const { data, error } = await portalClient.GET(
    "/supplier-access/invitations/{invitationId}/offer/versions",
    { params: { path: { invitationId }, query: { pageSize: 100 } } },
  );
  if (error) throw error;
  return offerHistorySchema.parse(data).versions;
}

export async function setSupplierOfferValidity(
  invitationId: string,
  draft: SupplierDraft,
  offerValidUntil: string,
) {
  const { data, error } = await portalClient.PUT(
    "/supplier-access/invitations/{invitationId}/offer/validity",
    {
      params: {
        path: { invitationId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: { draftId: draft.id, expectedRevision: draft.revision, offerValidUntil },
    },
  );
  if (error) throw error;
  return data;
}

export type SupplierOutcome = components["schemas"]["SupplierOutcomeDTO"];

export async function getSupplierOutcome(invitationId: string, outcomeId: string) {
  const { data, error } = await portalClient.GET(
    "/supplier-access/outcomes/{outcomeId}",
    { params: { path: { outcomeId }, query: { invitationId } } },
  );
  if (error) throw error;
  return data;
}

export async function acknowledgeSupplierOutcome(invitationId: string, outcomeId: string) {
  const { data, error } = await portalClient.POST(
    "/supplier-access/outcomes/{outcomeId}/acknowledgements",
    {
      params: {
        path: { outcomeId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: { invitationId, operationId: crypto.randomUUID() },
    },
  );
  if (error) throw error;
  return data;
}

export async function submitSupplierOffer(invitationId: string, draft: SupplierDraft) {
  const { data, error } = await portalClient.POST(
    "/supplier-access/invitations/{invitationId}/offer/submissions",
    {
      params: {
        path: { invitationId },
        header: { "X-CSRF-Token": csrfToken() },
      },
      body: {
        draftId: draft.id,
        expectedRevision: draft.revision,
        operationId: crypto.randomUUID(),
      },
    },
  );
  if (error) throw error;
  return data;
}
