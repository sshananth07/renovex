"use client";

// Result-oriented presentation of a FINALISED Award. Once an Award is
// finalised its selections are immutable (M8 §8E) — this component never
// renders a Select/Unselect control. Its job is to answer, in a few
// seconds: who won, what they won, how much it costs, and what to do next.
//
// The pre-finalisation comparison/selection workflow is untouched and lives
// in ProjectProcurement.tsx's own ComparisonPanel — this file only covers
// what a contractor sees AFTER an Award exists.

import { useMemo, useState } from "react";
import { CheckCircle2, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatMoney } from "@/lib/formatting/money";
import { useAwardDeliveries, useAwardOutcomes } from "../queries";
import { useSendAwardOutcomeNotification, useRetryAwardOutcomeNotification } from "../mutations";
import type { AwardDelivery, AwardOutcome, AwardRevision, Invitation } from "../api";

function formatFinalisedAt(iso: string) {
  return new Date(iso).toLocaleString("en-MY", { day: "numeric", month: "short", year: "numeric", hour: "numeric", minute: "2-digit" });
}

// One supplier's contribution derived from the revision itself — no
// recomputation of money/quantity, every figure is backend-authoritative.
type SupplierAward = {
  supplierId: string;
  supplierName: string;
  invitationId: string;
  itemNames: string[];
  total: { amount: number; currency: string };
};

function deriveSupplierAwards(revision: AwardRevision): SupplierAward[] {
  const itemsBySupplier = new Map<string, string[]>();
  for (const line of revision.awardedLines ?? []) {
    const items = itemsBySupplier.get(line.supplierId) ?? [];
    if (line.materialName) items.push(line.materialName);
    itemsBySupplier.set(line.supplierId, items);
  }
  return (revision.supplierSummaries ?? []).map((summary) => ({
    supplierId: summary.supplierId,
    supplierName: summary.supplierName || "Unnamed supplier",
    invitationId: summary.invitationId,
    itemNames: itemsBySupplier.get(summary.supplierId) ?? [],
    total: summary.supplierTotal,
  }));
}

export function FinalisedAwardSummary({ revision, supplierAwards }: { revision: AwardRevision; supplierAwards: SupplierAward[] }) {
  const lineCount = revision.awardedLines?.length ?? 0;
  return <section className="surface-card p-5 sm:p-6">
    <div className="flex items-center gap-2 text-emerald-700"><CheckCircle2 className="size-5" /><h2 className="font-heading text-xl font-semibold">Award Finalised</h2></div>
    <p className="mt-3 font-mono text-3xl font-semibold">{formatMoney(revision.grandAwardTotal.amount, revision.grandAwardTotal.currency)}</p>
    <p className="mt-1 text-sm text-muted-foreground">{lineCount} material line{lineCount === 1 ? "" : "s"} · {supplierAwards.length} supplier{supplierAwards.length === 1 ? "" : "s"}</p>
    <p className="mt-1 text-xs text-muted-foreground">Finalised {formatFinalisedAt(revision.finalisedAt)}</p>
  </section>;
}

// Contractor-facing notification vocabulary — technical failure codes and
// delivery-attempt metadata stay behind the details view, never surfaced
// here directly (per the redesign's language rules).
function notificationLabel(delivery: AwardDelivery | undefined): string {
  if (!delivery) return "No notification yet";
  switch (delivery.status) {
    case "sent": return "Sent";
    case "delivered": return "Delivered";
    case "failed": return "Delivery failed";
    default: return "Ready to send";
  }
}

function SupplierNotification({
  outcome, delivery, invitation, rfqChainId, versionId, revisionId, supplierName, rfqNumber, rfqTitle,
}: {
  outcome: AwardOutcome; delivery: AwardDelivery | undefined; invitation: Invitation | undefined;
  rfqChainId: string; versionId: string; revisionId: string;
  supplierName: string; rfqNumber?: string; rfqTitle?: string;
}) {
  const send = useSendAwardOutcomeNotification(rfqChainId, versionId, revisionId);
  const retry = useRetryAwardOutcomeNotification(rfqChainId, versionId);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const label = notificationLabel(delivery);
  const pending = send.isPending || retry.isPending;

  if (!invitation) {
    // No invitation on hand yet (still loading, or an edge case) — show the
    // status only, no action that would send with incomplete recipient info.
    return <div className="mt-3 border-t pt-3"><p className="text-xs text-muted-foreground">Notification</p><p className="text-sm font-medium">{label}</p></div>;
  }

  function doSend() {
    if (!invitation) return;
    send.mutate({ outcomeId: outcome.id, recipientIdentity: invitation.recipientEmail, accessGeneration: invitation.accessGeneration, supplierName, rfqNumber, rfqTitle });
  }
  function doRetry() {
    if (!delivery || !invitation) return;
    retry.mutate({ deliveryId: delivery.id, recipientIdentity: invitation.recipientEmail, accessGeneration: invitation.accessGeneration, supplierName, rfqNumber, rfqTitle });
  }

  return <div className="mt-3 border-t pt-3">
    <p className="text-xs text-muted-foreground">Notification</p>
    <div className="mt-1 flex items-center justify-between gap-2">
      <p className="text-sm font-medium">{label}</p>
      {!delivery && <Button size="sm" variant="outline" disabled={pending} onClick={doSend}>{pending ? "Sending…" : "Review & notify"}</Button>}
      {delivery && delivery.status === "failed" && <Button size="sm" variant="outline" disabled={pending} onClick={doRetry}>{pending ? "Sending…" : "Resend"}</Button>}
      {delivery && delivery.status !== "failed" && <Button size="sm" variant="ghost" onClick={() => setDetailsOpen((open) => !open)}>{detailsOpen ? "Hide details" : "View delivery details"}</Button>}
    </div>
    {detailsOpen && delivery && <div className="mt-2 rounded-lg bg-muted/40 p-3 text-xs text-muted-foreground">
      <p>Sent to {delivery.recipientIdentity}</p>
      {delivery.sentAt && <p>At {formatFinalisedAt(delivery.sentAt)}</p>}
      {delivery.failureCode && <p>Failure code: {delivery.failureCode}</p>}
    </div>}
  </div>;
}

export function SupplierAwardCard({
  award, outcome, delivery, invitation, rfqChainId, versionId, revisionId, rfqNumber, rfqTitle,
}: {
  award: SupplierAward; outcome: AwardOutcome | undefined; delivery: AwardDelivery | undefined; invitation: Invitation | undefined;
  rfqChainId: string; versionId: string; revisionId: string; rfqNumber?: string; rfqTitle?: string;
}) {
  return <article className="surface-card p-4">
    <p className="font-semibold">{award.supplierName}</p>
    <p className="mt-1 text-sm text-muted-foreground">{award.itemNames.length} item{award.itemNames.length === 1 ? "" : "s"} awarded</p>
    <p className="mt-2 font-mono text-lg font-semibold">{formatMoney(award.total.amount, award.total.currency)}</p>
    {award.itemNames.length > 0 && <p className="mt-2 text-xs text-muted-foreground">{award.itemNames.join(", ")}</p>}
    {outcome && <SupplierNotification outcome={outcome} delivery={delivery} invitation={invitation} rfqChainId={rfqChainId} versionId={versionId} revisionId={revisionId} supplierName={award.supplierName} rfqNumber={rfqNumber} rfqTitle={rfqTitle} />}
  </article>;
}

export function AwardedItemsTable({ revision }: { revision: AwardRevision }) {
  const lines = revision.awardedLines ?? [];
  return <section className="surface-card overflow-hidden">
    <div className="border-b p-4"><h3 className="font-heading font-semibold">Awarded items</h3></div>
    <div className="grid divide-y sm:hidden">
      {lines.map((line) => <div key={line.issuedRfqLineId} className="p-4">
        <p className="font-medium">{line.materialName ?? "Scope line"}</p>
        <p className="text-xs text-muted-foreground">{line.quantityValue ? `${line.quantityValue} ${line.quantityUnit ?? ""}` : "—"}</p>
        <p className="mt-1 text-sm">Awarded to <span className="font-medium">{line.supplierName || "Unnamed supplier"}</span></p>
        <p className="mt-1 font-mono text-sm text-muted-foreground">{formatMoney(line.unitPriceExcludingTax.amount, line.unitPriceExcludingTax.currency)} / {line.quantityUnit ?? "unit"}</p>
        <p className="font-mono font-semibold">{formatMoney(line.lineSubtotal.amount, line.lineSubtotal.currency)}</p>
      </div>)}
    </div>
    <table className="hidden w-full text-sm sm:table"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Material</th><th>Quantity</th><th>Awarded supplier</th><th>Unit price</th><th>Line total</th></tr></thead>
      <tbody>{lines.map((line) => <tr key={line.issuedRfqLineId} className="border-b last:border-0">
        <td className="p-3 font-medium">{line.materialName ?? "Scope line"}</td>
        <td className="font-mono">{line.quantityValue ? `${line.quantityValue} ${line.quantityUnit ?? ""}` : "—"}</td>
        <td>{line.supplierName || "Unnamed supplier"}</td>
        <td className="font-mono">{formatMoney(line.unitPriceExcludingTax.amount, line.unitPriceExcludingTax.currency)}/{line.quantityUnit ?? "unit"}</td>
        <td className="font-mono font-semibold">{formatMoney(line.lineSubtotal.amount, line.lineSubtotal.currency)}</td>
      </tr>)}</tbody>
    </table>
    {(revision.unawardedLines ?? []).length > 0 && <p className="border-t p-3 text-xs text-muted-foreground">{revision.unawardedLines?.length} scope line{revision.unawardedLines?.length === 1 ? " remains" : "s remain"} unawarded.</p>}
  </section>;
}

// Read-only replay of the original comparison, collapsed by default. It is
// the same information the pre-finalisation ComparisonPanel showed, minus
// every interactive control — the caller supplies the already-rendered
// content so this file does not duplicate ComparisonPanel's own markup.
export function OriginalOfferComparison({ children }: { children: React.ReactNode }) {
  return <details className="group surface-card overflow-hidden">
    <summary className="cursor-pointer list-none p-4 font-heading font-semibold [&::-webkit-details-marker]:hidden">
      <span className="inline-flex items-center gap-1"><ChevronRight className="size-4 transition-transform group-open:rotate-90" />View original offer comparison</span>
    </summary>
    <div className="border-t p-4">
      <p className="mb-3 text-xs text-muted-foreground">Read-only — this Award is finalised and its selections can no longer be changed.</p>
      {children}
    </div>
  </details>;
}

export function AwardHistory({ revisions }: { revisions: AwardRevision[] }) {
  const [expanded, setExpanded] = useState<string>();
  const ordered = useMemo(() => [...revisions].reverse(), [revisions]);
  return <section className="surface-card overflow-hidden">
    <div className="border-b p-4"><h3 className="font-heading font-semibold">Award history</h3></div>
    <div className="divide-y">{ordered.map((revision) => {
      const supplierAwards = deriveSupplierAwards(revision);
      const isOpen = expanded === revision.id;
      return <div key={revision.id} className="p-4">
        <button type="button" className="flex w-full items-center justify-between gap-3 text-left" onClick={() => setExpanded(isOpen ? undefined : revision.id)}>
          <div><p className="text-sm font-medium">Revision {revision.revisionNumber} · Finalised</p><p className="text-xs text-muted-foreground">{formatFinalisedAt(revision.finalisedAt)} · {formatMoney(revision.grandAwardTotal.amount, revision.grandAwardTotal.currency)}</p></div>
          <span className="text-xs font-medium text-primary">{isOpen ? "Hide" : "View"}</span>
        </button>
        {isOpen && <div className="mt-3 grid gap-2 sm:grid-cols-2">
          {supplierAwards.map((award) => <div key={`${revision.id}-${award.supplierId}`} className="rounded-lg border bg-muted/20 p-3">
            <p className="font-medium">{award.supplierName}</p>
            <p className="text-xs text-muted-foreground">{award.itemNames.length} awarded line{award.itemNames.length === 1 ? "" : "s"}</p>
            <p className="font-mono text-sm font-semibold">{formatMoney(award.total.amount, award.total.currency)}</p>
          </div>)}
          {(revision.changeReason || revision.supersedesRevisionId) && <details className="sm:col-span-2 mt-1"><summary className="cursor-pointer text-xs text-muted-foreground">Technical details</summary>
            <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
              {revision.changeReason && <p>Change reason: {revision.changeReason}</p>}
              <p>Award revision ID: {revision.id}</p>
              <p>Issued RFQ version ID: {revision.issuedRfqVersionId}</p>
              {revision.supersedesRevisionId && <p>Supersedes revision ID: {revision.supersedesRevisionId}</p>}
            </div>
          </details>}
        </div>}
      </div>;
    })}</div>
  </section>;
}

export function FinalisedAwardView({
  revision, invitations, rfqChainId, versionId, comparison, rfqNumber, rfqTitle,
}: {
  revision: AwardRevision; invitations: Invitation[]; rfqChainId: string; versionId: string; comparison: React.ReactNode;
  rfqNumber?: string; rfqTitle?: string;
}) {
  const supplierAwards = useMemo(() => deriveSupplierAwards(revision), [revision]);
  const outcomes = useAwardOutcomes(rfqChainId, versionId, revision.id);
  const deliveries = useAwardDeliveries(rfqChainId, versionId, revision.id);
  const outcomeBySupplier = useMemo(() => new Map((outcomes.data ?? []).map((outcome) => [outcome.supplierId, outcome])), [outcomes.data]);
  // Latest delivery per outcome — a supplier may have more than one attempt
  // (retry), the most recent one is what the card should reflect.
  const latestDeliveryByOutcome = useMemo(() => {
    const byOutcome = new Map<string, AwardDelivery>();
    for (const delivery of deliveries.data ?? []) {
      const existing = byOutcome.get(delivery.awardOutcomeId);
      if (!existing || new Date(delivery.createdAt) > new Date(existing.createdAt)) byOutcome.set(delivery.awardOutcomeId, delivery);
    }
    return byOutcome;
  }, [deliveries.data]);
  const invitationBySupplier = useMemo(() => new Map(invitations.map((invitation) => [invitation.supplierId, invitation])), [invitations]);

  return <div className="flex flex-col gap-4">
    <FinalisedAwardSummary revision={revision} supplierAwards={supplierAwards} />
    <section>
      <h3 className="mb-3 font-heading font-semibold">Supplier awards</h3>
      <div className="grid gap-3 sm:grid-cols-2">{supplierAwards.map((award) => {
        const outcome = outcomeBySupplier.get(award.supplierId);
        return <SupplierAwardCard
          key={award.supplierId}
          award={award}
          outcome={outcome}
          delivery={outcome ? latestDeliveryByOutcome.get(outcome.id) : undefined}
          invitation={invitationBySupplier.get(award.supplierId)}
          rfqChainId={rfqChainId}
          versionId={versionId}
          revisionId={revision.id}
          rfqNumber={rfqNumber}
          rfqTitle={rfqTitle}
        />;
      })}</div>
    </section>
    <AwardedItemsTable revision={revision} />
    <OriginalOfferComparison>{comparison}</OriginalOfferComparison>
  </div>;
}
