"use client";

import { useMemo, useState } from "react";
import { Copy, ExternalLink, Plus, Send } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { PageHeader } from "@/components/layout/PageHeader";
import { formatMoney } from "@/lib/formatting/money";
import { useClient } from "@/features/clients/queries";
import { api, useOperationMutation } from "../mutations";
import { useEstimates, useQuotations } from "../queries";

export function QuotationsView({ projectId }: { projectId: string }) {
  const quotations = useQuotations(projectId);
  const estimates = useEstimates(projectId);
  const ordered = useMemo(() => [...(quotations.data ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt)), [quotations.data]);
  const [selectedId, setSelectedId] = useState<string>();
  const selected = ordered.find((item) => item.id === selectedId) ?? ordered[0];
  const selectedClient = useClient(selected?.clientId ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const [estimateId, setEstimateId] = useState("");
  const [termsOpen, setTermsOpen] = useState(false);
  const [terms, setTerms] = useState("");
  const [paymentSchedule, setPaymentSchedule] = useState("");
  const [notes, setNotes] = useState("");
  const [validUntil, setValidUntil] = useState("");
  const [shareUrl, setShareUrl] = useState("");
  const createMutation = useOperationMutation(() => api.createQuotation({ projectId, estimateId }), ["projects", projectId, "quotations"]);
  const termsMutation = useOperationMutation(() => api.updateQuotationTerms(selected!.id, { expectedRevision: selected!.revision, terms, paymentSchedule, notes, validUntil: validUntil || undefined }), ["projects", projectId, "quotations"]);
  const finalizeMutation = useOperationMutation(() => api.finalizeQuotation(selected!.id, selected!.revision), ["projects", projectId, "quotations"]);
  const versionMutation = useOperationMutation((sourceEstimateId: string) => api.createQuotationVersion(selected!.id, sourceEstimateId), ["projects", projectId, "quotations"]);
  const shareMutation = useOperationMutation(async () => { const grant = await api.shareQuotation(selected!.id); setShareUrl(grant.token ? `${window.location.origin}/client/quotations/${grant.token}` : grant.url ?? ""); return grant; }, ["projects", projectId, "quotations"]);
  const finalizedEstimates = (estimates.data ?? []).filter((item) => item.status === "finalized");
  const activeError = quotations.error ?? estimates.error ?? createMutation.error ?? termsMutation.error ?? finalizeMutation.error ?? versionMutation.error ?? shareMutation.error;

  function openTerms() {
    if (!selected) return;
    setTerms(selected.terms ?? ""); setPaymentSchedule(selected.paymentSchedule ?? ""); setNotes(selected.notes ?? ""); setValidUntil(selected.validUntil?.slice(0, 10) ?? ""); setTermsOpen(true);
  }

  return <div className="flex flex-col gap-5">
    <PageHeader title="Quotations" description="Customer-facing, versioned selling-price records" actions={<Button onClick={() => setCreateOpen(true)} disabled={finalizedEstimates.length === 0}><Plus />New quotation</Button>} />
    {activeError && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">The quotation action could not be completed. Your input is preserved; review the refreshed authoritative record before retrying.</div>}
    {ordered.length === 0 ? <section className="surface-card flex min-h-72 flex-col items-center justify-center p-8 text-center"><h2 className="font-heading text-lg font-semibold">No quotations yet</h2><p className="mt-1 max-w-lg text-sm text-muted-foreground">Finalize an internal Estimate, then create a customer-facing Quotation from its authoritative selling price.</p>{finalizedEstimates.length > 0 && <Button className="mt-4" onClick={() => setCreateOpen(true)}>Create quotation</Button>}</section> : <div className="grid gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <aside className="flex flex-col gap-2">{ordered.map((item) => <button key={item.id} onClick={() => setSelectedId(item.id)} className={`surface-card p-4 text-left transition-colors ${item.id === selected?.id ? "border-primary bg-accent/40" : "hover:border-primary/30"}`}><span className="flex items-center justify-between gap-2"><strong>{item.quotationNumber}</strong><Badge variant="outline" className="capitalize">{item.status}</Badge></span><span className="mt-2 block text-xs text-muted-foreground">Version {item.version}</span><span className="mt-2 block font-mono font-semibold">{formatMoney(item.total.amount, item.currency)}</span></button>)}</aside>
      {selected && <section className="surface-card overflow-hidden"><div className="flex flex-col gap-4 border-b p-5 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex items-center gap-2"><h2 className="font-heading text-xl font-semibold">{selected.quotationNumber}</h2><Badge variant="outline" className="capitalize">{selected.status}</Badge><span className="text-xs text-muted-foreground">v{selected.version}</span></div><p className="mt-1 text-sm text-muted-foreground">Revision {selected.revision} · Client {selectedClient.data?.name ?? selected.clientId}</p></div><div className="flex flex-wrap gap-2">{selected.status === "draft" ? <><Button variant="outline" onClick={openTerms}>Terms & validity</Button><Button onClick={() => finalizeMutation.mutate(undefined)} disabled={finalizeMutation.isPending}>Finalize</Button></> : <><Button variant="outline" onClick={() => { const next=finalizedEstimates[0]?.id; if(next) versionMutation.mutate(next); }}>Revise</Button><Button onClick={() => shareMutation.mutate(undefined)} disabled={shareMutation.isPending}><Send />Share</Button></>}</div></div>
        <div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Description</th><th>Quantity</th><th>Unit price</th><th>Amount</th></tr></thead><tbody>{(selected.lines ?? []).map((line) => <tr key={line.id} className="border-b last:border-0"><td className="p-3 font-medium">{line.description}</td><td className="font-mono">{line.quantity ? `${line.quantity} ${line.unit ?? ""}` : "—"}</td><td className="font-mono">{line.unitPrice ? formatMoney(line.unitPrice.amount, line.unitPrice.currency) : "—"}</td><td className="font-mono font-semibold">{formatMoney(line.amount.amount, line.amount.currency)}</td></tr>)}</tbody></table></div>
        <div className="ml-auto grid max-w-md gap-2 p-5 text-sm"><div className="flex justify-between"><span className="text-muted-foreground">Subtotal</span><span className="font-mono">{formatMoney(selected.subtotal.amount, selected.currency)}</span></div><div className="flex justify-between"><span className="text-muted-foreground">{selected.taxLabel || "Tax"}</span><span className="font-mono">{formatMoney(selected.taxAmount.amount, selected.currency)}</span></div><div className="flex justify-between border-t pt-3 text-base font-semibold"><span>Total quotation</span><span className="font-mono text-primary">{formatMoney(selected.total.amount, selected.currency)}</span></div></div>
        {(selected.terms || selected.paymentSchedule || selected.notes) && <div className="grid gap-4 border-t bg-muted/20 p-5 text-sm sm:grid-cols-3"><div><p className="eyebrow">Terms</p><p className="mt-1 whitespace-pre-wrap">{selected.terms || "—"}</p></div><div><p className="eyebrow">Payment schedule</p><p className="mt-1 whitespace-pre-wrap">{selected.paymentSchedule || "—"}</p></div><div><p className="eyebrow">Customer notes</p><p className="mt-1 whitespace-pre-wrap">{selected.notes || "—"}</p></div></div>}
      </section>}
    </div>}
    {shareUrl && <section className="surface-card flex flex-col gap-3 p-4 sm:flex-row sm:items-center"><div className="min-w-0 flex-1"><p className="eyebrow">Secure client link</p><p className="truncate text-sm">{shareUrl}</p></div><Button variant="outline" onClick={() => navigator.clipboard.writeText(shareUrl)}><Copy />Copy</Button><a className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-primary-foreground" href={shareUrl} target="_blank" rel="noreferrer"><ExternalLink className="size-4" />Open</a></section>}
    <Dialog open={createOpen} onOpenChange={setCreateOpen}><DialogContent><DialogHeader><DialogTitle>Create quotation</DialogTitle></DialogHeader><div className="grid gap-4"><Label>Finalized estimate</Label><Select value={estimateId} onValueChange={(v) => setEstimateId(v ?? "")}><SelectTrigger><SelectValue placeholder="Select estimate" /></SelectTrigger><SelectContent>{finalizedEstimates.map((item) => <SelectItem key={item.id} value={item.id}>Version {item.version} · {formatMoney(item.proposedSellingPrice.amount, item.currency)}</SelectItem>)}</SelectContent></Select><Button disabled={!estimateId || createMutation.isPending} onClick={() => createMutation.mutate(undefined, { onSuccess: () => setCreateOpen(false) })}>Create draft quotation</Button></div></DialogContent></Dialog>
    <Dialog open={termsOpen} onOpenChange={setTermsOpen}><DialogContent><DialogHeader><DialogTitle>Quotation terms</DialogTitle></DialogHeader><div className="grid gap-4"><div className="grid gap-1.5"><Label>Terms</Label><Textarea value={terms} onChange={(e) => setTerms(e.target.value)} /></div><div className="grid gap-1.5"><Label>Payment schedule</Label><Textarea value={paymentSchedule} onChange={(e) => setPaymentSchedule(e.target.value)} /></div><div className="grid gap-1.5"><Label>Customer notes</Label><Textarea value={notes} onChange={(e) => setNotes(e.target.value)} /></div><div className="grid gap-1.5"><Label>Valid until</Label><Input type="date" value={validUntil} onChange={(e) => setValidUntil(e.target.value)} /></div><Button onClick={() => termsMutation.mutate(undefined, { onSuccess: () => setTermsOpen(false) })}>Save terms</Button></div></DialogContent></Dialog>
  </div>;
}
