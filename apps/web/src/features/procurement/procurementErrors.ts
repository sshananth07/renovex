import type { ApiError } from "@/lib/api/errors";

export type ProcurementErrorKind =
  | "network"
  | "resolution-operation-conflict"
  | "requirement-not-eligible"
  | "unit-mismatch-unresolved"
  | "source-discrepancy-unresolved"
  | "already-claimed"
  | "stale-revision"
  | "already-issued"
  | "validation"
  | "conflict"
  | "unknown";

export type TranslatedProcurementError = { kind: ProcurementErrorKind; message: string };

// The form/workflow-level fallback for procurement backend errors, applied
// AFTER field-scoped Huma errors have already been mapped via
// applyFieldErrors/setError — never instead of that mapping. Side-effect-free:
// never invalidates/refetches, and never claims a refetch happened. The
// backend exposes no stable machine-readable error codes today (every
// mapping is huma.Error4xx("string literal")), so this string-matching is
// deliberately isolated here so it can move to structured codes later
// without touching call sites.
export function translateProcurementError(error: ApiError): TranslatedProcurementError {
  if (error.kind !== "api") {
    return { kind: "network", message: "The request could not be completed. Check your connection and try again." };
  }
  const detail = error.detail ?? "";

  // Specific matches first, most specific to least — so a future, more
  // precise backend message is never swallowed by the broad "not eligible"
  // catch-all below.
  if (detail.includes("resolution operation id belongs to a different resolution")) {
    return {
      kind: "resolution-operation-conflict",
      message: "This source-change resolution no longer matches the current requirement. Close this review and start again.",
    };
  }
  if (detail.includes("unit mismatch is unresolved")) {
    return { kind: "unit-mismatch-unresolved", message: "Acknowledge the unit mismatch before adding this requirement to the RFQ." };
  }
  if (detail.includes("source discrepancy is unresolved")) {
    return { kind: "source-discrepancy-unresolved", message: "Review the source change before adding this requirement to the RFQ." };
  }
  // Two different endpoints report a claim conflict with different wording:
  // internal/rfqs (add-line path) says "already claimed by another rfq
  // chain"; internal/materialrequirements (discrepancy-resolution path)
  // says "claimed by an active RFQ chain". Both matched here.
  if (detail.includes("already claimed by another rfq chain") || detail.includes("claimed by an active RFQ chain")) {
    return { kind: "already-claimed", message: "This requirement is already assigned to another RFQ." };
  }
  if (detail.includes("changed since it was read")) {
    return { kind: "stale-revision", message: "This record changed since you opened it. Review the latest data before retrying." };
  }
  // A reopen against an already-issued chain is a permanent domain rule, not
  // a transient staleness conflict — retrying with fresher data can never
  // succeed, so this must not fall into the generic "record may have
  // changed" conflict message below.
  if (detail.includes("rfq chain has already been issued")) {
    return { kind: "already-issued", message: "This RFQ has already been issued and can no longer be reopened to draft." };
  }
  // Broad eligibility catch-all — last among the specific matches, since
  // "not eligible" is the least specific known message.
  if (detail.includes("requirement is not eligible") || detail.includes("not eligible for an RFQ")) {
    return {
      kind: "requirement-not-eligible",
      message: "This material requirement is not currently eligible for an RFQ. Review its current status before retrying.",
    };
  }
  if (error.status === 422) return { kind: "validation", message: error.detail ?? "This action could not be completed." };
  if (error.status === 409) return { kind: "conflict", message: "The record may have changed. Review the current state before retrying." };
  return { kind: "unknown", message: error.detail ?? "The action could not be completed." };
}
