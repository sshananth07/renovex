"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { CheckCircle2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { formatMoney, majorToMinor } from "@/lib/formatting/money";
import {
  acknowledgeSupplierChargeGroup,
  acknowledgeSupplierDelivery,
  acknowledgeSupplierTax,
  copyForwardSupplierOffer,
  createSupplierDraft,
  declineSupplierLine,
  getSupplierInvitation,
  getSupplierSession,
  listSupplierOfferVersions,
  quoteSupplierLine,
  resetSupplierLine,
  setSupplierOfferValidity,
  submitSupplierOffer,
  type SupplierDraft,
  type SupplierInvitation,
} from "../api";
import { useSupplierAccessEntry } from "../useSupplierAccessEntry";
import { safeSupplierReturnTo } from "@/lib/url/safeSupplierReturnTo";
import { PortalShell } from "./PortalShell";
import { SupplierVerificationForm } from "./SupplierVerificationForm";

export function SupplierRFQPortal({ token, returnTo }: { token?: string; returnTo?: string }) {
  const router = useRouter();
  const [invitationId, setInvitationId] = useState("");
  const [invitation, setInvitation] = useState<SupplierInvitation>();
  const [draft, setDraft] = useState<SupplierDraft>();
  const [prices, setPrices] = useState<Record<string, string>>({});
  const [lineDetails, setLineDetails] = useState<Record<string, {
    brand: string;
    sku: string;
    productDescription: string;
    leadTime: string;
    supplierLineNotes: string;
    commercialExceptions: string;
  }>>({});
  const [versions, setVersions] = useState<Awaited<ReturnType<typeof listSupplierOfferVersions>>>([]);
  const [validUntil, setValidUntil] = useState("");
  const [savingLine, setSavingLine] = useState("");
  const [error, setError] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [code, setCode] = useState("");
  const { state: entry, verifyCode, resendCode, verifyError } = useSupplierAccessEntry(token);

  const applyDraft = useCallback((next: SupplierDraft) => {
    setDraft(next);
    setValidUntil(next.offerValidUntil?.slice(0, 16) ?? "");
    setPrices(Object.fromEntries(
      (next.lines ?? [])
        .filter((line) => line.unitPriceMinor !== undefined)
        .map((line) => [line.id, (line.unitPriceMinor! / 100).toFixed(2)]),
    ));
    setLineDetails(Object.fromEntries((next.lines ?? []).map((line) => [line.id, {
      brand: line.brand ?? "",
      sku: line.sku ?? "",
      productDescription: line.productDescription ?? "",
      leadTime: line.leadTime ?? "",
      supplierLineNotes: line.supplierLineNotes ?? "",
      commercialExceptions: line.commercialExceptions ?? "",
    }])));
  }, []);

  const bootstrapPortal = useCallback(async () => {
    const session = await getSupplierSession();
    // createSupplierDraft (CreateOrGetActiveDraft on the backend) is what
    // creates the offer chain aggregate (EnsureOfferChain) on a genuine
    // first-ever visit. listSupplierOfferVersions looks that same chain up
    // by scope and 404s (ErrOfferChainNotFound) if it doesn't exist yet —
    // so it must run strictly after the draft call resolves, never
    // concurrently with it, or it can race ahead of the chain being
    // created and see a false "not found" on a first-time visit.
    // getSupplierInvitation has no such dependency and can stay parallel.
    const [nextInvitation, nextDraft] = await Promise.all([
      getSupplierInvitation(session.invitationId),
      createSupplierDraft(session.invitationId),
    ]);
    const priorVersions = await listSupplierOfferVersions(session.invitationId);
    setInvitationId(session.invitationId);
    setInvitation(nextInvitation);
    setVersions(priorVersions);
    applyDraft(nextDraft);
  }, [applyDraft]);

  useEffect(() => {
    if (entry.status !== "session-established") return;

    // A safe returnTo means this session was established specifically to
    // reach another Supplier Access destination (e.g. an award outcome) —
    // the RFQ portal itself is not what the visitor asked for, so it must
    // never load here first. An unsafe/absent returnTo falls through to the
    // normal RFQ portal bootstrap unchanged.
    const safeDestination = safeSupplierReturnTo(returnTo);
    if (safeDestination) {
      router.replace(safeDestination);
      return;
    }

    let alive = true;
    (async () => {
      try {
        await bootstrapPortal();
      } catch {
        if (alive) setError("Verification succeeded, but the offer portal could not be loaded. Please reload the page.");
      }
    })();
    return () => { alive = false; };
  }, [entry.status, bootstrapPortal, returnTo, router]);

  const rfqLines = useMemo(
    () => new Map(invitation?.currentRfqVersion.lines.map((line) => [line.id, line])),
    [invitation],
  );

  const refreshDraftAfterConflict = useCallback(async () => {
    if (!invitationId) return;
    try {
      const current = await createSupplierDraft(invitationId);
      setDraft(current);
      setValidUntil(current.offerValidUntil?.slice(0, 16) ?? "");
    } catch {
      // Keep the Supplier's entered values visible even when the refresh also fails.
    }
  }, [invitationId]);

  async function saveLine(lineId: string) {
    if (!draft) return;
    const amount = majorToMinor(prices[lineId] ?? "");
    if (amount === null) {
      setError("Enter a valid price with no more than two decimal places.");
      return;
    }
    setSavingLine(lineId);
    setError("");
    try {
      setDraft(await quoteSupplierLine(invitationId, lineId, draft, {
        unitPriceMinor: amount,
        ...lineDetails[lineId],
      }));
    } catch {
      await refreshDraftAfterConflict();
      setError("The offer changed or the line could not be saved. Review the current draft before retrying.");
    } finally {
      setSavingLine("");
    }
  }

  async function declineLine(lineId: string, responseStatus: "no_bid" | "unavailable") {
    if (!draft) return;
    setError("");
    try {
      setDraft(await declineSupplierLine(
        invitationId,
        lineId,
        draft,
        responseStatus,
        lineDetails[lineId]?.supplierLineNotes || undefined,
      ));
    } catch {
      await refreshDraftAfterConflict();
      setError("The line response could not be updated. Review the current draft before retrying.");
    }
  }

  async function resetLine(lineId: string) {
    if (!draft) return;
    setError("");
    try {
      setDraft(await resetSupplierLine(invitationId, lineId, draft));
    } catch {
      await refreshDraftAfterConflict();
      setError("The line response could not be reset. Review the current draft before retrying.");
    }
  }

  async function reviseDraft(action: (id: string, value: SupplierDraft) => Promise<SupplierDraft>) {
    if (!draft) return;
    setError("");
    try {
      applyDraft(await action(invitationId, draft));
    } catch {
      await refreshDraftAfterConflict();
      setError("The offer changed or this review action is no longer available. Review the current draft before retrying.");
    }
  }

  async function saveValidity() {
    if (!draft || !validUntil) return;
    setError("");
    try {
      setDraft(await setSupplierOfferValidity(
        invitationId,
        draft,
        new Date(validUntil).toISOString(),
      ));
    } catch {
      await refreshDraftAfterConflict();
      setError("Validity could not be saved. Review the current draft before retrying.");
    }
  }

  async function submit() {
    if (!draft) return;
    setError("");
    try {
      await submitSupplierOffer(invitationId, draft);
      setSubmitted(true);
    } catch {
      await refreshDraftAfterConflict();
      setError("The offer is incomplete, requires review, or changed before submission. Review every line and validity date.");
    }
  }

  if (entry.status === "loading" || (entry.status === "session-established" && !draft && !error)) {
    return <PortalShell audience="Supplier RFQ Portal"><div className="surface-card h-80 animate-pulse" /></PortalShell>;
  }
  if (entry.status === "verification-required" && !draft) {
    return (
      <PortalShell audience="Supplier RFQ Portal">
        <SupplierVerificationForm
          code={code}
          onCodeChange={setCode}
          onVerify={() => verifyCode(code)}
          onResend={resendCode}
          error={verifyError}
        />
      </PortalShell>
    );
  }
  if ((entry.status === "error" || error) && !draft) {
    return (
      <PortalShell audience="Supplier RFQ Portal">
        <div className="surface-card p-10 text-center">
          <h1 className="font-heading text-xl font-semibold">Invitation unavailable</h1>
          <p className="mt-2 text-sm text-muted-foreground">{entry.status === "error" ? entry.message : error}</p>
        </div>
      </PortalShell>
    );
  }
  if (!draft || !invitation) return null;
  if (submitted) {
    return (
      <PortalShell audience="Supplier RFQ Portal">
        <div className="surface-card flex flex-col items-center p-12 text-center">
          <CheckCircle2 className="size-12 text-emerald-600" />
          <h1 className="mt-4 font-heading text-2xl font-semibold">Offer submitted</h1>
          <p className="mt-2 text-sm text-muted-foreground">Your immutable offer version was submitted to the contractor.</p>
        </div>
      </PortalShell>
    );
  }

  return (
    <PortalShell audience="Supplier RFQ Portal">
      <div className="flex flex-col gap-5">
        <section className="surface-card p-5 sm:p-6">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <p className="eyebrow">{invitation.currentRfqVersion.rfqNumber}</p>
              <h1 className="mt-2 font-heading text-2xl font-semibold">{invitation.currentRfqVersion.title}</h1>
              <p className="mt-1 text-sm text-muted-foreground">Version {invitation.currentRfqVersion.versionNumber} · Offer revision {draft.revision}</p>
              <p className="mt-2 text-xs text-muted-foreground">Deliver to {invitation.currentRfqVersion.deliveryAddress}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2"><Badge variant="outline" className="w-fit capitalize">{draft.status}</Badge>{versions.length > 0 && (draft.lines ?? []).every((line) => line.responseStatus === "unanswered") && <Button variant="outline" size="sm" onClick={() => reviseDraft(copyForwardSupplierOffer)}>Revise prior offer</Button>}</div>
          </div>
        </section>
        {error && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">{error}</div>}
        <section className="surface-card overflow-hidden">
          <div className="border-b p-4">
            <h2 className="font-heading font-semibold">Requested lines</h2>
            <p className="text-xs text-muted-foreground">Only this invitation and your own response are visible.</p>
          </div>
          <div className="table-scroll">
            <table className="w-full min-w-200 text-sm">
              <thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Line</th><th>Quantity</th><th>Status</th><th>Unit price ({draft.currency})</th><th>Response detail</th><th>Line subtotal</th><th /></tr></thead>
              <tbody>
                {(draft.lines ?? []).map((line, index) => {
                  const requested = rfqLines.get(line.rfqLineId);
                  return (
                    <tr key={line.id} className="border-b last:border-0">
                      <td className="p-3 font-medium">{requested?.materialName || `RFQ line ${index + 1}`}<span className="block max-w-sm text-xs font-normal text-muted-foreground">{requested?.specification || line.productDescription || line.sku || "Add your quoted price"}</span></td>
                      <td className="font-mono">{requested ? `${requested.quantityValue} ${requested.quantityUnit}` : "—"}</td>
                      <td><Badge variant="outline" className="capitalize">{line.responseStatus}</Badge>{line.reviewRequired && <span className="ml-2 text-xs text-amber-700">Review required</span>}</td>
                      <td><Input className="w-36 font-mono" inputMode="decimal" value={prices[line.id] ?? ""} onChange={(event) => setPrices((current) => ({ ...current, [line.id]: event.target.value }))} /></td>
                      <td><div className="grid min-w-64 grid-cols-2 gap-2"><Input placeholder="Brand" value={lineDetails[line.id]?.brand ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], brand: event.target.value } }))} /><Input placeholder="SKU / alternative" value={lineDetails[line.id]?.sku ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], sku: event.target.value } }))} /><Input placeholder="Lead time / availability" value={lineDetails[line.id]?.leadTime ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], leadTime: event.target.value } }))} /><Input placeholder="Product description" value={lineDetails[line.id]?.productDescription ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], productDescription: event.target.value } }))} /><Input className="col-span-2" placeholder="Supplier notes" value={lineDetails[line.id]?.supplierLineNotes ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], supplierLineNotes: event.target.value } }))} /><Input className="col-span-2" placeholder="Commercial exceptions" value={lineDetails[line.id]?.commercialExceptions ?? ""} onChange={(event) => setLineDetails((current) => ({ ...current, [line.id]: { ...current[line.id], commercialExceptions: event.target.value } }))} /></div></td>
                      <td className="font-mono">{line.lineSubtotalMinor !== undefined ? formatMoney(line.lineSubtotalMinor, draft.currency) : "—"}</td>
                      <td><div className="flex min-w-36 flex-wrap gap-1"><Button variant="outline" size="sm" disabled={savingLine === line.id} onClick={() => saveLine(line.id)}>Save price</Button><Button variant="ghost" size="sm" onClick={() => declineLine(line.id, "unavailable")}>Unavailable</Button><Button variant="ghost" size="sm" onClick={() => declineLine(line.id, "no_bid")}>No bid</Button>{line.responseStatus !== "unanswered" && <Button variant="ghost" size="sm" onClick={() => resetLine(line.id)}>Reset</Button>}</div></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
        {(draft.offerTaxReviewRequired || draft.deliveryChargeReviewRequired || (draft.chargeGroups ?? []).some((group) => group.reviewRequired)) && <section className="surface-card p-5"><p className="eyebrow">Review acknowledgements</p><h2 className="mt-1 font-heading font-semibold">Copied commercial terms require confirmation</h2><div className="mt-4 flex flex-wrap gap-2">{draft.offerTaxReviewRequired && <Button variant="outline" onClick={() => reviseDraft(acknowledgeSupplierTax)}>Acknowledge tax</Button>}{draft.deliveryChargeReviewRequired && <Button variant="outline" onClick={() => reviseDraft(acknowledgeSupplierDelivery)}>Acknowledge delivery charge</Button>}{(draft.chargeGroups ?? []).filter((group) => group.reviewRequired).map((group) => <Button key={group.id} variant="outline" onClick={() => reviseDraft((id, value) => acknowledgeSupplierChargeGroup(id, group.id, value))}>Acknowledge {group.name}</Button>)}</div></section>}
        {versions.length > 0 && <section className="surface-card overflow-hidden"><div className="border-b p-4"><h2 className="font-heading font-semibold">Prior submissions</h2><p className="text-xs text-muted-foreground">Your immutable offer history for this invitation only.</p></div><div className="table-scroll"><table className="w-full min-w-150 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Version</th><th>Submitted</th><th>Valid until</th><th>Status</th><th>Total</th></tr></thead><tbody>{versions.map((version) => <tr key={version.id} className="border-b last:border-0"><td className="p-3 font-semibold">v{version.versionNumber}</td><td>{new Date(version.submittedAt).toLocaleString("en-MY")}</td><td>{new Date(version.offerValidUntil).toLocaleDateString("en-MY")}</td><td><Badge variant="outline" className="capitalize">{version.publicStatus.replaceAll("_", " ")}</Badge></td><td className="font-mono font-semibold">{formatMoney(version.grandTotal.amountMinor, version.grandTotal.currency)}</td></tr>)}</tbody></table></div></section>}
        <section className="surface-card p-5">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="grid flex-1 gap-1.5"><Label htmlFor="offer-valid-until">Offer valid until</Label><Input id="offer-valid-until" type="datetime-local" value={validUntil} onChange={(event) => setValidUntil(event.target.value)} /></div>
            <Button variant="outline" onClick={saveValidity}>Save validity</Button>
            <Button onClick={submit}>Submit offer</Button>
          </div>
          <p className="mt-3 text-xs text-muted-foreground">Submission creates an immutable version. Any review-required pricing must be resolved under the backend contract before submission.</p>
        </section>
      </div>
    </PortalShell>
  );
}
