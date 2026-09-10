"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { CheckCircle2, XCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatMoney } from "@/lib/formatting/money";
import {
  acknowledgeSupplierOutcome,
  getSupplierOutcome,
  getSupplierSession,
  type SupplierOutcome,
} from "../api";
import { PortalShell } from "./PortalShell";

// The disclaimer's exact wording is a product requirement (M8 §8J): an
// Award is a sourcing decision only, never a Purchase Order or committed
// cost, and acknowledging receipt must not read as accepting either.
const ACKNOWLEDGEMENT_DISCLAIMER =
  "Acknowledgement confirms receipt of this sourcing outcome only. " +
  "It does not constitute acceptance of a Purchase Order or contractual commitment.";

// This page is a pure post-auth destination — the Supplier Access session is
// the security boundary, and it must already exist by the time this page
// loads. It never runs its own token/OTP bootstrap: the email link always
// resolves through /supplier-access/open first (token -> OTP if needed ->
// session), which then navigates here via its own validated `returnTo`. If
// this page is ever reached without a session (a bookmark, a shared link,
// direct navigation), it sends the visitor back through that same gateway,
// carrying itself as the returnTo so they land back here afterward.
export function SupplierOutcomePage({
  invitationId,
  outcomeId,
}: {
  invitationId: string;
  outcomeId: string;
}) {
  const router = useRouter();
  const [sessionState, setSessionState] = useState<"checking" | "ok" | "missing">("checking");
  const [outcome, setOutcome] = useState<SupplierOutcome>();
  const [loadError, setLoadError] = useState("");
  const [acknowledgedAt, setAcknowledgedAt] = useState<string>();
  const [acknowledging, setAcknowledging] = useState(false);
  const [acknowledgeError, setAcknowledgeError] = useState("");

  const loadOutcome = useCallback(async () => {
    try {
      const data = await getSupplierOutcome(invitationId, outcomeId);
      setOutcome(data);
    } catch {
      setLoadError("This outcome is not available for this invitation.");
    }
  }, [invitationId, outcomeId]);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        await getSupplierSession();
        if (alive) setSessionState("ok");
      } catch {
        if (alive) setSessionState("missing");
      }
    })();
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    if (sessionState !== "ok") return;
    (async () => {
      await loadOutcome();
    })();
  }, [sessionState, loadOutcome]);

  useEffect(() => {
    if (sessionState !== "missing") return;
    const returnTo = encodeURIComponent(
      `/supplier-access/invitations/${invitationId}/outcomes/${outcomeId}`,
    );
    router.replace(`/supplier-access/open?returnTo=${returnTo}`);
  }, [sessionState, invitationId, outcomeId, router]);

  async function acknowledge() {
    setAcknowledging(true);
    setAcknowledgeError("");
    try {
      const result = await acknowledgeSupplierOutcome(invitationId, outcomeId);
      setAcknowledgedAt(result.acknowledgedAt);
    } catch {
      setAcknowledgeError("Acknowledgement could not be recorded. Please try again.");
    } finally {
      setAcknowledging(false);
    }
  }

  if (sessionState !== "ok" || (!outcome && !loadError)) {
    return <PortalShell audience="RFQ Outcome"><div className="surface-card h-64 animate-pulse" /></PortalShell>;
  }

  if (loadError && !outcome) {
    return (
      <PortalShell audience="RFQ Outcome">
        <div className="surface-card p-10 text-center">
          <h1 className="font-heading text-xl font-semibold">Outcome unavailable</h1>
          <p className="mt-2 text-sm text-muted-foreground">{loadError}</p>
        </div>
      </PortalShell>
    );
  }

  if (!outcome) return null;

  // Status is derived directly from the immutable projection's own Result —
  // never inferred from line counts, since the projection deliberately
  // carries no denominator or unawarded-lines data (§8H privacy design).
  const selected = outcome.result === "selected";
  const acknowledged = acknowledgedAt !== undefined;

  return (
    <PortalShell audience="RFQ Outcome">
      <div className="mx-auto flex max-w-2xl flex-col gap-5">
        <section className="surface-card p-6">
          <p className="eyebrow">{outcome.rfqNumber}</p>
          <h1 className="mt-2 font-heading text-2xl font-semibold">{outcome.rfqTitle || "RFQ outcome"}</h1>
          <div className="mt-4">
            {selected ? (
              <Badge className="gap-1.5 bg-emerald-600 text-white hover:bg-emerald-600">
                <CheckCircle2 className="size-3.5" /> Selected
              </Badge>
            ) : (
              <Badge variant="outline" className="gap-1.5">
                <XCircle className="size-3.5" /> Not selected
              </Badge>
            )}
          </div>
          <p className="mt-3 text-sm text-muted-foreground">
            {selected
              ? "Your quotation was selected for the following items."
              : "Your quotation was not selected for this request."}
          </p>
        </section>

        {selected && (
          <section className="surface-card overflow-hidden">
            <div className="border-b p-4">
              <h2 className="font-heading font-semibold">Awarded items</h2>
            </div>
            <div className="table-scroll">
              <table className="w-full text-sm">
                <tbody>
                  {outcome.awardedLines?.map((line) => (
                    <tr key={line.issuedRfqLineId} className="border-b p-4 last:border-0">
                      <td className="p-4">
                        <p className="font-medium">{line.materialName || "Awarded item"}</p>
                        <p className="text-xs text-muted-foreground">
                          {line.quantityValue} {line.quantityUnit} × {formatMoney(line.unitPriceMinorUnits, outcome.currency)}
                        </p>
                      </td>
                      <td className="p-4 text-right font-mono font-semibold">
                        {formatMoney(line.lineSubtotalMinorUnits, outcome.currency)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex items-center justify-between border-t bg-muted/40 p-4">
              <span className="font-heading font-semibold">Awarded total</span>
              <span className="font-mono text-lg font-semibold">
                {formatMoney(outcome.awardTotalMinorUnits, outcome.currency)}
              </span>
            </div>
          </section>
        )}

        {outcome.contractorMessage && (
          <section className="surface-card p-5">
            <p className="eyebrow">Message from the contractor</p>
            <p className="mt-2 text-sm">{outcome.contractorMessage}</p>
          </section>
        )}

        <section className="surface-card p-5">
          <p className="text-sm text-muted-foreground">
            This records the sourcing decision only. It is not a Purchase Order or contractual commitment.
          </p>
          {acknowledgeError && (
            <p className="mt-3 rounded-lg bg-destructive/5 p-3 text-sm text-destructive">{acknowledgeError}</p>
          )}
          <div className="mt-4">
            {acknowledged ? (
              <div className="flex items-center gap-2 text-sm text-emerald-700">
                <CheckCircle2 className="size-4" />
                Acknowledged {new Date(acknowledgedAt).toLocaleString("en-MY")}
              </div>
            ) : (
              <Button disabled={acknowledging} onClick={acknowledge}>
                Acknowledge receipt
              </Button>
            )}
          </div>
          <p className="mt-3 text-xs text-muted-foreground">{ACKNOWLEDGEMENT_DISCLAIMER}</p>
        </section>
      </div>
    </PortalShell>
  );
}
