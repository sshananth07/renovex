"use client";

import { cloneElement, useId, useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { Package, Pencil, Plus, Users, Wrench } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchField } from "@/components/layout/SearchField";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { applyFieldErrors } from "@/lib/api/applyFieldErrors";
import type { ApiError } from "@/lib/api/errors";
import { formatDate } from "@/lib/formatting/date";
import { formatMoney, majorToMinor } from "@/lib/formatting/money";
import { useWorkItemList } from "@/features/work-items/queries";
import { api, useOperationMutation } from "../mutations";
import { useLabourEntries, useMaterials, useWorkers } from "../queries";
import type { Material, Worker } from "../api";

const materialSchema = z.object({ name: z.string().trim().min(1), unit: z.string().trim().min(1), category: z.string(), specification: z.string(), price: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount") });

const RATE_TYPE_OPTIONS = [
  { label: "Hourly", value: "hourly" },
  { label: "Daily", value: "daily" },
  { label: "Fixed project", value: "fixed_project" },
  { label: "Per unit", value: "per_unit" },
  { label: "Per square meter", value: "per_square_meter" },
] as const;
const RATE_TYPE_VALUES = RATE_TYPE_OPTIONS.map((option) => option.value) as [
  (typeof RATE_TYPE_OPTIONS)[number]["value"],
  ...(typeof RATE_TYPE_OPTIONS)[number]["value"][],
];

const workerSchema = z.object({ name: z.string().trim().min(1), trade: z.string(), rateType: z.enum(RATE_TYPE_VALUES), rate: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount"), contactEmail: z.string(), contactPhone: z.string() });
const labourCommonSchema = { workItemId: z.string().min(1), quantityValue: z.string().regex(/^\d+(?:\.\d+)?$/), quantityUnit: z.string().min(1), notes: z.string() };
const labourSchema = z.discriminatedUnion("mode", [
  z.object({
    mode: z.literal("existing"),
    ...labourCommonSchema,
    workerId: z.string().min(1, "Select a worker"),
    rateOverride: z.string().refine((v) => v === "" || majorToMinor(v) !== null, "Enter a valid amount"),
  }),
  z.object({
    mode: z.literal("adhoc"),
    ...labourCommonSchema,
    workerName: z.string().trim().min(1, "Worker name is required for ad-hoc labour"),
    trade: z.string().trim().min(1, "Trade is required for ad-hoc labour"),
    rate: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount"),
  }),
]);
type MaterialValues = z.infer<typeof materialSchema>;
type WorkerValues = z.infer<typeof workerSchema>;
type LabourValues = z.infer<typeof labourSchema>;

function Field({ label, error, children }: { label: string; error?: string; children: React.ReactElement<{ id?: string }> }) {
  const id = useId();
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      {cloneElement(children, { id })}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}

export function ResourcesView({ projectId }: { projectId: string }) {
  const [search, setSearch] = useState("");
  const [materialOpen, setMaterialOpen] = useState(false);
  const [workerOpen, setWorkerOpen] = useState(false);
  const [labourOpen, setLabourOpen] = useState(false);
  const [editingMaterial, setEditingMaterial] = useState<Material | null>(null);
  const [editingWorker, setEditingWorker] = useState<Worker | null>(null);
  const materials = useMaterials();
  const workers = useWorkers();
  const labour = useLabourEntries(projectId);
  const workItems = useWorkItemList(projectId, { page: 1, pageSize: 100 });
  const materialMutation = useOperationMutation(async (values: MaterialValues) => {
    const body = { name: values.name, unit: values.unit, category: values.category, specification: values.specification, referencePriceAmount: majorToMinor(values.price)!, referencePriceCurrency: "MYR" };
    return editingMaterial ? api.updateMaterial(editingMaterial.id, body) : api.createMaterial(body);
  }, ["materials"]);
  const workerMutation = useOperationMutation(async (values: WorkerValues) => {
    const body = { name: values.name, trade: values.trade, rateType: values.rateType, defaultRateAmount: majorToMinor(values.rate)!, currency: "MYR", contactEmail: values.contactEmail, contactPhone: values.contactPhone };
    return editingWorker ? api.updateWorker(editingWorker.id, body) : api.createWorker(body);
  }, ["workers"]);
  const labourMutation = useOperationMutation((values: LabourValues) => values.mode === "existing"
    ? api.createLabourEntry({ projectId, workItemId: values.workItemId, workerId: values.workerId, quantityValue: values.quantityValue, quantityUnit: values.quantityUnit, rateAmount: values.rateOverride ? majorToMinor(values.rateOverride)! : undefined, currency: "MYR", notes: values.notes || undefined })
    : api.createLabourEntry({ projectId, workItemId: values.workItemId, workerName: values.workerName, trade: values.trade, quantityValue: values.quantityValue, quantityUnit: values.quantityUnit, rateAmount: majorToMinor(values.rate)!, currency: "MYR", notes: values.notes || undefined }), ["projects", projectId, "labour"]);
  const materialForm = useForm<MaterialValues>({ resolver: zodResolver(materialSchema), defaultValues: { name: "", unit: "unit", category: "", specification: "", price: "0.00" } });
  const workerForm = useForm<WorkerValues>({ resolver: zodResolver(workerSchema), defaultValues: { name: "", trade: "", rateType: "daily", rate: "0.00", contactEmail: "", contactPhone: "" } });
  const labourForm = useForm<LabourValues>({ resolver: zodResolver(labourSchema), defaultValues: { mode: "adhoc", workItemId: "", workerName: "", trade: "", quantityValue: "1", quantityUnit: "day", rate: "0.00", notes: "" } });
  // Driven by plain useState, not labourForm.watch("mode") — watch's
  // subscription-based re-render can lag one tick behind reset() in some
  // render orderings, letting the tab buttons show stale active state while
  // the actually-rendered fields (also gated on labourMode) are already
  // correct, or vice versa. This state and the reset() call that changes it
  // are always set together, synchronously, in the same click handler below.
  const [labourMode, setLabourMode] = useState<LabourValues["mode"]>("adhoc");
  const labourErrors = labourForm.formState.errors as Record<string, { message?: string } | undefined>;
  const labourFieldError = (field: string) => labourErrors[field]?.message;
  const q = search.toLowerCase();
  const filteredMaterials = useMemo(() => (materials.data ?? []).filter((item) => `${item.name} ${item.category ?? ""}`.toLowerCase().includes(q)), [materials.data, q]);
  const filteredWorkers = useMemo(() => (workers.data ?? []).filter((item) => `${item.name} ${item.trade ?? ""}`.toLowerCase().includes(q)), [workers.data, q]);

  function openMaterial(item?: Material) {
    setEditingMaterial(item ?? null);
    materialForm.reset(item ? { name: item.name, unit: item.unit, category: item.category ?? "", specification: item.specification ?? "", price: (item.referencePriceAmount / 100).toFixed(2) } : { name: "", unit: "unit", category: "", specification: "", price: "0.00" });
    setMaterialOpen(true);
  }
  function openWorker(item?: Worker) {
    setEditingWorker(item ?? null);
    workerForm.reset(item ? { name: item.name, trade: item.trade ?? "", rateType: item.rateType as WorkerValues["rateType"], rate: (item.defaultRate.amount / 100).toFixed(2), contactEmail: item.contactEmail ?? "", contactPhone: item.contactPhone ?? "" } : { name: "", trade: "", rateType: "daily", rate: "0.00", contactEmail: "", contactPhone: "" });
    setWorkerOpen(true);
  }
  const submitMaterial = materialForm.handleSubmit((values) => materialMutation.mutate(values, { onSuccess: () => setMaterialOpen(false) }));
  const submitWorker = workerForm.handleSubmit((values) => workerMutation.mutate(values, { onSuccess: () => setWorkerOpen(false) }));
  const [labourFormError, setLabourFormError] = useState<string>();
  const labourFields = ["workItemId", "workerId", "workerName", "trade", "quantityValue", "quantityUnit", "rate", "rateOverride", "notes"];
  const labourFieldAliases = { rateAmount: labourMode === "existing" ? "rateOverride" : "rate" };
  const submitLabour = labourForm.handleSubmit((values) => {
    setLabourFormError(undefined);
    labourMutation.mutate(values, {
      onSuccess: () => { setLabourOpen(false); labourForm.reset(); },
      onError: (error) => setLabourFormError(applyFieldErrors(error as unknown as ApiError, labourForm.setError, labourFields, labourFieldAliases)),
    });
  });
  const activeError = materials.error ?? workers.error ?? labour.error ?? workItems.error ?? materialMutation.error ?? workerMutation.error;

  return <div className="flex flex-col gap-5">
    <PageHeader title="Resources" description="Reusable materials, workers and project labour records" />
    {activeError && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">The resource action could not be completed. Your input is preserved; review the refreshed authoritative records before retrying.</div>}
    <Tabs defaultValue="materials">
      <div className="surface-card flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between">
        <TabsList><TabsTrigger value="materials"><Package />Materials</TabsTrigger><TabsTrigger value="workers"><Users />Workers</TabsTrigger><TabsTrigger value="labour"><Wrench />Labour</TabsTrigger></TabsList>
        <SearchField value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search resources…" className="w-full sm:max-w-xs" />
      </div>
      <TabsContent value="materials" className="mt-4">
        <section className="surface-card overflow-hidden"><div className="flex items-center justify-between border-b p-4"><div><h2 className="font-heading font-semibold">Material catalog</h2><p className="text-xs text-muted-foreground">Reference prices are informational and never rewrite historical costs.</p></div><Button onClick={() => openMaterial()}><Plus />Add material</Button></div>
        <div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Material</th><th>Category</th><th>Unit</th><th>Reference price</th><th>As of</th><th className="w-14" /></tr></thead><tbody>{filteredMaterials.map((item) => <tr key={item.id} className="border-b last:border-0"><td className="p-3 font-medium">{item.name}<span className="block text-xs font-normal text-muted-foreground">{item.specification}</span></td><td>{item.category || "—"}</td><td>{item.unit}</td><td className="font-mono">{formatMoney(item.referencePriceAmount, item.referencePriceCurrency)}</td><td>{formatDate(item.referencePriceAsOf)}</td><td><Button size="icon-sm" variant="ghost" onClick={() => openMaterial(item)} aria-label={`Edit ${item.name}`}><Pencil /></Button></td></tr>)}</tbody></table></div>{!materials.isLoading && filteredMaterials.length === 0 && <p className="p-10 text-center text-sm text-muted-foreground">No materials found.</p>}</section>
      </TabsContent>
      <TabsContent value="workers" className="mt-4">
        <section className="surface-card overflow-hidden"><div className="flex items-center justify-between border-b p-4"><div><h2 className="font-heading font-semibold">Worker directory</h2><p className="text-xs text-muted-foreground">Default rates prefill new labour entries; history remains unchanged.</p></div><Button onClick={() => openWorker()}><Plus />Add worker</Button></div>
        <div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Worker</th><th>Trade</th><th>Rate type</th><th>Default rate</th><th>Contact</th><th className="w-14" /></tr></thead><tbody>{filteredWorkers.map((item) => <tr key={item.id} className="border-b last:border-0"><td className="p-3 font-medium">{item.name}</td><td>{item.trade || "—"}</td><td className="capitalize">{item.rateType}</td><td className="font-mono">{formatMoney(item.defaultRate.amount, item.defaultRate.currency)}</td><td>{item.contactPhone || item.contactEmail || "—"}</td><td><Button size="icon-sm" variant="ghost" onClick={() => openWorker(item)} aria-label={`Edit ${item.name}`}><Pencil /></Button></td></tr>)}</tbody></table></div>{!workers.isLoading && filteredWorkers.length === 0 && <p className="p-10 text-center text-sm text-muted-foreground">No workers found.</p>}</section>
      </TabsContent>
      <TabsContent value="labour" className="mt-4">
        <section className="surface-card overflow-hidden"><div className="flex items-center justify-between border-b p-4"><div><h2 className="font-heading font-semibold">Project labour</h2><p className="text-xs text-muted-foreground">Authoritative labour entries and their resulting cost.</p></div><Button onClick={() => setLabourOpen(true)}><Plus />Log labour</Button></div>
        <div className="table-scroll"><table className="w-full min-w-180 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Worker</th><th>Trade</th><th>Quantity</th><th>Rate</th><th>Cost</th><th>Date</th></tr></thead><tbody>{(labour.data ?? []).map((item) => <tr key={item.id} className="border-b last:border-0"><td className="p-3 font-medium">{item.workerName}</td><td>{item.trade || "—"}</td><td className="font-mono">{item.quantityValue} {item.quantityUnit}</td><td className="font-mono">{formatMoney(item.rate.amount, item.rate.currency)}</td><td className="font-mono font-semibold">{formatMoney(item.cost.amount, item.cost.currency)}</td><td>{formatDate(item.date)}</td></tr>)}</tbody></table></div>{!labour.isLoading && (labour.data ?? []).length === 0 && <p className="p-10 text-center text-sm text-muted-foreground">No labour logged for this project.</p>}</section>
      </TabsContent>
    </Tabs>

    <Dialog open={materialOpen} onOpenChange={setMaterialOpen}><DialogContent><DialogHeader><DialogTitle>{editingMaterial ? "Edit material" : "Add material"}</DialogTitle></DialogHeader><form onSubmit={submitMaterial} className="grid gap-4"><Field label="Name" error={materialForm.formState.errors.name?.message}><Input {...materialForm.register("name")} /></Field><div className="grid gap-4 sm:grid-cols-2"><Field label="Unit"><Input {...materialForm.register("unit")} /></Field><Field label="Reference price (MYR)" error={materialForm.formState.errors.price?.message}><Input inputMode="decimal" {...materialForm.register("price")} /></Field></div><Field label="Category"><Input {...materialForm.register("category")} /></Field><Field label="Specification"><Textarea {...materialForm.register("specification")} /></Field><Button type="submit" disabled={materialMutation.isPending}>Save material</Button></form></DialogContent></Dialog>
    <Dialog open={workerOpen} onOpenChange={setWorkerOpen}><DialogContent><DialogHeader><DialogTitle>{editingWorker ? "Edit worker" : "Add worker"}</DialogTitle></DialogHeader><form onSubmit={submitWorker} className="grid gap-4"><Field label="Name"><Input {...workerForm.register("name")} /></Field><Field label="Trade"><Input {...workerForm.register("trade")} /></Field><div className="grid gap-4 sm:grid-cols-2"><Field label="Rate type"><Select value={workerForm.watch("rateType")} onValueChange={(v) => v && workerForm.setValue("rateType", v as WorkerValues["rateType"])}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{RATE_TYPE_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select></Field><Field label="Default rate (MYR)"><Input inputMode="decimal" {...workerForm.register("rate")} /></Field></div><div className="grid gap-4 sm:grid-cols-2"><Field label="Phone"><Input {...workerForm.register("contactPhone")} /></Field><Field label="Email"><Input type="email" {...workerForm.register("contactEmail")} /></Field></div><Button type="submit" disabled={workerMutation.isPending}>Save worker</Button></form></DialogContent></Dialog>
    <Dialog open={labourOpen} onOpenChange={(open) => { setLabourOpen(open); setLabourFormError(undefined); if (!open) { setLabourMode("adhoc"); labourForm.reset({ mode: "adhoc", workItemId: "", workerName: "", trade: "", quantityValue: "1", quantityUnit: "day", rate: "0.00", notes: "" }); } }}><DialogContent><DialogHeader><DialogTitle>Log labour</DialogTitle></DialogHeader><form onSubmit={submitLabour} className="grid gap-4">
      <div className="grid grid-cols-2 gap-2 rounded-lg bg-muted p-1">
        <Button type="button" variant={labourMode === "existing" ? "default" : "ghost"} size="sm" onClick={() => { setLabourFormError(undefined); setLabourMode("existing"); labourForm.reset({ mode: "existing", workItemId: labourForm.getValues("workItemId"), workerId: "", rateOverride: "", quantityValue: labourForm.getValues("quantityValue"), quantityUnit: labourForm.getValues("quantityUnit"), notes: labourForm.getValues("notes") }); }}>Existing worker</Button>
        <Button type="button" variant={labourMode === "adhoc" ? "default" : "ghost"} size="sm" onClick={() => { setLabourFormError(undefined); setLabourMode("adhoc"); labourForm.reset({ mode: "adhoc", workItemId: labourForm.getValues("workItemId"), workerName: "", trade: "", rate: "0.00", quantityValue: labourForm.getValues("quantityValue"), quantityUnit: labourForm.getValues("quantityUnit"), notes: labourForm.getValues("notes") }); }}>Ad-hoc labour</Button>
      </div>
      {labourFormError && <p className="text-sm text-destructive">{labourFormError}</p>}
      <Field label="Work item"><Select value={labourForm.watch("workItemId")} onValueChange={(v) => v && labourForm.setValue("workItemId", v)}><SelectTrigger><SelectValue placeholder="Select work item" /></SelectTrigger><SelectContent>{(workItems.data?.items ?? []).map((item) => <SelectItem key={item.id} value={item.id}>{item.description}</SelectItem>)}</SelectContent></Select></Field>
      {labourMode === "existing" ? <>
        <Field label="Worker" error={labourFieldError("workerId")}><Select value={labourForm.watch("workerId") ?? ""} onValueChange={(id) => { if (!id) return; const worker = (workers.data ?? []).find((w) => w.id === id); labourForm.setValue("workerId", id); if (worker) labourForm.setValue("quantityUnit", worker.rateType); }}><SelectTrigger><SelectValue placeholder="Select worker" /></SelectTrigger><SelectContent>{(workers.data ?? []).map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}</SelectContent></Select></Field>
        <div className="grid gap-4 sm:grid-cols-3"><Field label="Quantity"><Input {...labourForm.register("quantityValue")} /></Field><Field label="Unit"><Input {...labourForm.register("quantityUnit")} /></Field><Field label="Rate override (MYR, optional)" error={labourFieldError("rateOverride")}><Input placeholder="Uses worker default" inputMode="decimal" {...labourForm.register("rateOverride")} /></Field></div>
      </> : <>
        <Field label="Worker name" error={labourFieldError("workerName")}><Input {...labourForm.register("workerName")} /></Field>
        <Field label="Trade" error={labourFieldError("trade")}><Input {...labourForm.register("trade")} /></Field>
        <div className="grid gap-4 sm:grid-cols-3"><Field label="Quantity"><Input {...labourForm.register("quantityValue")} /></Field><Field label="Unit"><Input {...labourForm.register("quantityUnit")} /></Field><Field label="Rate (MYR)" error={labourFieldError("rate")}><Input inputMode="decimal" {...labourForm.register("rate")} /></Field></div>
      </>}
      <Field label="Notes"><Textarea {...labourForm.register("notes")} /></Field>
      <Button type="submit" disabled={labourMutation.isPending}>Create labour entry</Button>
    </form></DialogContent></Dialog>
  </div>;
}
