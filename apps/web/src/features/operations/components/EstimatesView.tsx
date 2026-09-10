"use client";

import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { PageHeader } from "@/components/layout/PageHeader";
import { formatBasisPoints, formatMoney, majorToMinor } from "@/lib/formatting/money";
import { api, useOperationMutation } from "../mutations";
import { useEstimates } from "../queries";

function message(error: unknown) {
  if (error && typeof error === "object" && "detail" in error) return String((error as { detail?: string }).detail ?? "Request failed");
  return error ? "The request could not be completed." : "";
}

export function EstimatesView({ projectId }: { projectId: string }) {
  const estimates = useEstimates(projectId);
  const ordered = useMemo(() => [...(estimates.data ?? [])].sort((a, b) => b.version - a.version), [estimates.data]);
  const [selectedId, setSelectedId] = useState<string>();
  const selected = ordered.find((item) => item.id === selectedId) ?? ordered[0];
  const [createOpen, setCreateOpen] = useState(false);
  const [pricingMode, setPricingMode] = useState<"markup" | "margin">("markup");
  const [ratePercent, setRatePercent] = useState("20");
  const pricingRate = majorToMinor(ratePercent);
  const createMutation = useOperationMutation(() => api.createEstimate({ projectId, pricingMode, pricingRate: pricingRate! }), ["projects", projectId, "estimates"]);
  const pricingMutation = useOperationMutation(() => api.updateEstimatePricing(selected!.id, { pricingMode, pricingRate: pricingRate!, expectedRevision: selected!.revision }), ["projects", projectId, "estimates"]);
  const refreshMutation = useOperationMutation(() => api.refreshEstimate(selected!.id, selected!.revision), ["projects", projectId, "estimates"]);
  const finalizeMutation = useOperationMutation(() => api.finalizeEstimate(selected!.id, selected!.revision), ["projects", projectId, "estimates"]);
  const versionMutation = useOperationMutation(() => api.createEstimateVersion(selected!.id, {}), ["projects", projectId, "estimates"]);
  const activeError = createMutation.error ?? pricingMutation.error ?? refreshMutation.error ?? finalizeMutation.error ?? versionMutation.error;

  if (!estimates.isLoading && !selected) return <div className="flex flex-col gap-5"><PageHeader title="Estimates" description="Internal contractor pricing snapshots" actions={<Button onClick={() => setCreateOpen(true)}>Create estimate</Button>} /><section className="surface-card flex min-h-72 flex-col items-center justify-center p-8 text-center"><h2 className="font-heading text-lg font-semibold">No estimate yet</h2><p className="mt-1 max-w-lg text-sm text-muted-foreground">Create a private estimate from the project’s current cost items. The backend calculates all financial totals.</p><Button className="mt-4" onClick={() => setCreateOpen(true)}>Create version 1</Button></section><CreateDialog open={createOpen} setOpen={setCreateOpen} mode={pricingMode} setMode={setPricingMode} rate={ratePercent} setRate={setRatePercent} valid={pricingRate !== null} onSubmit={() => createMutation.mutate(undefined, { onSuccess: () => setCreateOpen(false) })} /></div>;

  return <div className="flex flex-col gap-5">
    <PageHeader title="Estimates" description="Private, versioned internal pricing records" actions={selected?.status === "finalized" ? <Button onClick={() => versionMutation.mutate(undefined)} disabled={versionMutation.isPending}>Create next version</Button> : undefined} />
    <div className="surface-card flex gap-2 overflow-x-auto p-2">{ordered.map((item) => <button key={item.id} onClick={() => setSelectedId(item.id)} className={`shrink-0 rounded-lg px-4 py-2 text-left text-sm ${item.id === selected?.id ? "bg-accent text-accent-foreground" : "hover:bg-muted"}`}><span className="font-semibold">Version {item.version}</span><span className="ml-2 text-xs capitalize opacity-70">{item.status}</span></button>)}</div>
    {selected && <>
      <section className="surface-card p-5"><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex items-center gap-2"><h2 className="font-heading text-xl font-semibold">Estimate v{selected.version}</h2><Badge variant="outline" className="capitalize">{selected.status}</Badge></div><p className="mt-1 text-sm text-muted-foreground">Revision {selected.revision} · {selected.lines?.length ?? 0} snapshotted cost lines · {selected.excludedCostItemCount} excluded</p></div>{selected.status === "draft" && <div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => refreshMutation.mutate(undefined)} disabled={refreshMutation.isPending}>Refresh costs</Button><Button onClick={() => finalizeMutation.mutate(undefined)} disabled={finalizeMutation.isPending}>Finalize estimate</Button></div>}</div>
      {activeError && <div className="mt-4 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{message(activeError)} Input has been preserved; review the refreshed authoritative record before retrying.</div>}</section>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{[
        ["Cost subtotal", formatMoney(selected.costSubtotal.amount, selected.currency)],
        ["Selling price", formatMoney(selected.proposedSellingPrice.amount, selected.currency)],
        ["Gross profit", formatMoney(selected.projectedGrossProfit.amount, selected.currency)],
        ["Gross margin", formatBasisPoints(selected.projectedGrossMarginBps)],
      ].map(([label, value]) => <section className="surface-card p-5" key={label}><p className="eyebrow">{label}</p><p className="mt-3 font-mono text-lg font-semibold">{value}</p></section>)}</div>
      {selected.status === "draft" && <section className="surface-card p-4"><div className="flex flex-col gap-3 sm:flex-row sm:items-end"><div className="grid flex-1 gap-1.5"><Label>Pricing mode</Label><Select value={pricingMode} onValueChange={(v) => v && setPricingMode(v as "markup" | "margin")}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="markup">Markup</SelectItem><SelectItem value="margin">Margin</SelectItem></SelectContent></Select></div><div className="grid flex-1 gap-1.5"><Label>Rate (%)</Label><Input value={ratePercent} onChange={(e) => setRatePercent(e.target.value)} inputMode="decimal" /></div><Button onClick={() => pricingMutation.mutate(undefined)} disabled={pricingMutation.isPending || pricingRate === null}>Recalculate pricing</Button></div></section>}
      <section className="surface-card overflow-hidden"><div className="border-b p-4"><h2 className="font-heading font-semibold">Estimate composition</h2><p className="text-xs text-muted-foreground">Immutable source snapshots after finalization.</p></div><div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Description</th><th>Category</th><th>Snapshot amount</th></tr></thead><tbody>{(selected.lines ?? []).map((line) => <tr key={line.sourceCostItemId} className="border-b last:border-0"><td className="p-3 font-medium">{line.description}</td><td className="capitalize">{line.category.replaceAll("_", " ")}</td><td className="font-mono font-semibold">{formatMoney(line.snapshottedAmount.amount, line.snapshottedAmount.currency)}</td></tr>)}</tbody></table></div>{(selected.lines ?? []).length === 0 && <p className="p-10 text-center text-sm text-muted-foreground">No eligible cost lines were included.</p>}</section>
    </>}
  </div>;
}

function CreateDialog({ open, setOpen, mode, setMode, rate, setRate, valid, onSubmit }: { open: boolean; setOpen: (v: boolean) => void; mode: "markup" | "margin"; setMode: (v: "markup" | "margin") => void; rate: string; setRate: (v: string) => void; valid: boolean; onSubmit: () => void }) {
  return <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>Create estimate</DialogTitle></DialogHeader><div className="grid gap-4"><div className="grid gap-1.5"><Label>Pricing mode</Label><Select value={mode} onValueChange={(v) => v && setMode(v as "markup" | "margin")}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="markup">Markup</SelectItem><SelectItem value="margin">Margin</SelectItem></SelectContent></Select></div><div className="grid gap-1.5"><Label>Rate (%)</Label><Input value={rate} onChange={(e) => setRate(e.target.value)} />{!valid && <p className="text-xs text-destructive">Enter a percentage with no more than two decimal places.</p>}</div><Button disabled={!valid} onClick={onSubmit}>Create from current costs</Button></div></DialogContent></Dialog>;
}
