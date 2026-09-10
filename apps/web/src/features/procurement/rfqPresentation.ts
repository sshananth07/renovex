import type { components } from "@/lib/api/generated/schema";

type RFQ = components["schemas"]["RfqDTO"];

export type RFQReadinessBlocker = "not-draft" | "no-scope" | "missing-delivery-address";

export type RFQReadinessState = {
  canAttemptMarkReady: boolean;
  blockers: RFQReadinessBlocker[];
  message?: string;
};

// Covers exactly the three preconditions locally knowable from the RFQ
// projection (status, line count, delivery address). Deliberately does not
// re-derive per-line eligibility (each line's underlying requirement's
// claim/sync/unit-mismatch state) — the backend re-validates that
// authoritatively at transition time regardless. "Mark ready" being enabled
// here means only that the obvious preconditions are met, not that the
// backend will necessarily accept the transition.
export function deriveRFQReadinessState(rfq: RFQ): RFQReadinessState {
  if (rfq.status !== "draft") {
    return { canAttemptMarkReady: false, blockers: ["not-draft"] };
  }
  if ((rfq.lines?.length ?? 0) === 0) {
    return {
      canAttemptMarkReady: false,
      blockers: ["no-scope"],
      message: "Add at least one reviewed material requirement to this RFQ first.",
    };
  }
  if (!rfq.deliveryAddress?.trim()) {
    return {
      canAttemptMarkReady: false,
      blockers: ["missing-delivery-address"],
      message: "Add a delivery address before marking this RFQ ready.",
    };
  }
  return { canAttemptMarkReady: true, blockers: [] };
}
