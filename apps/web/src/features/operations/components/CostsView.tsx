"use client";

import { useEffect, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { CircleDollarSign, Plus } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { applyFieldErrors } from "@/lib/api/applyFieldErrors";
import type { ApiError } from "@/lib/api/errors";
import { formatMoney, majorToMinor, minorToMajorString } from "@/lib/formatting/money";
import { useWorkItemList } from "@/features/work-items/queries";
import { api, useOperationMutation } from "../mutations";
import { useCostItems, useMaterials } from "../queries";
import type { CostItem } from "../api";

const CATEGORY_OPTIONS = [
  { label: "Material", value: "material" },
  { label: "Subcontractor", value: "subcontractor" },
  { label: "Equipment", value: "equipment" },
  { label: "Transport", value: "transport" },
  { label: "Permit", value: "permit" },
  { label: "Professional Fee", value: "professional_fee" },
  { label: "Utility", value: "utility" },
  { label: "Miscellaneous", value: "miscellaneous" },
] as const;
const CATEGORY_VALUES = CATEGORY_OPTIONS.map((option) => option.value) as [
  (typeof CATEGORY_OPTIONS)[number]["value"],
  ...(typeof CATEGORY_OPTIONS)[number]["value"][],
];

// materialId/quantityValue/quantityUnit/unitPrice are all-or-nothing on the
// backend (CreateCostItemInputBody) and materialId requires category=material
// — mirrored here so the contractor sees the constraint before submitting,
// not just after a 422 round-trip.
const schema = z.object({
  description: z.string().trim().min(1),
  category: z.enum(CATEGORY_VALUES),
  workItemId: z.string(),
  estimated: z.string().refine((v) => majorToMinor(v) !== null),
  notes: z.string(),
  materialId: z.string(),
  quantityValue: z.string(),
  quantityUnit: z.string(),
  unitPrice: z.string(),
}).superRefine((values, ctx) => {
  const materialFields = [values.materialId, values.quantityValue, values.quantityUnit, values.unitPrice];
  const anyFilled = materialFields.some((v) => v.trim() !== "");
  const allFilled = materialFields.every((v) => v.trim() !== "");
  if (anyFilled && !allFilled) {
    ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["materialId"], message: "Material, quantity, unit, and unit price must all be filled in together, or all left blank." });
  }
  if (values.unitPrice.trim() && majorToMinor(values.unitPrice) === null) {
    ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["unitPrice"], message: "Enter a valid price with no more than two decimal places." });
  }
});
const stageAmountSchema = z.object({ amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount") });
const recordActualSchema = z.object({ amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount") });
const correctionSchema = z.object({
  amount: z.string().refine((v) => majorToMinor(v) !== null, "Enter a valid amount"),
  reason: z.string().trim().min(1, "A reason is required to correct a previously recorded amount"),
});
type Values = z.infer<typeof schema>;
type StageAmountValues = z.infer<typeof stageAmountSchema>;
type RecordActualValues = z.infer<typeof recordActualSchema>;
type CorrectionValues = z.infer<typeof correctionSchema>;

export function CostsView({ projectId }: { projectId: string }) {
  const [createOpen, setCreateOpen] = useState(false);
  const [detailItem, setDetailItem] = useState<CostItem | null>(null);
  const costs = useCostItems(projectId);
  const workItems = useWorkItemList(projectId, { page: 1, pageSize: 100 });
  const materials = useMaterials();
  const createDefaults: Values = { description: "", category: "material", workItemId: "", estimated: "0.00", notes: "", materialId: "", quantityValue: "", quantityUnit: "", unitPrice: "" };
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: createDefaults });
  const editForm = useForm<StageAmountValues>({ resolver: zodResolver(stageAmountSchema), defaultValues: { amount: "0.00" } });
  const recordForm = useForm<RecordActualValues>({ resolver: zodResolver(recordActualSchema), defaultValues: { amount: "0.00" } });
  const correctForm = useForm<CorrectionValues>({ resolver: zodResolver(correctionSchema), defaultValues: { amount: "0.00", reason: "" } });
  const [editingStage, setEditingStage] = useState<"estimated" | "committed" | null>(null);
  const [recordingActual, setRecordingActual] = useState(false);
  const [correctingActual, setCorrectingActual] = useState(false);
  const [confirmingCorrection, setConfirmingCorrection] = useState(false);
  const [editFormError, setEditFormError] = useState<string>();
  const [recordFormError, setRecordFormError] = useState<string>();
  const [correctFormError, setCorrectFormError] = useState<string>();
  const createMutation = useOperationMutation((values: Values) => api.createCostItem({
    projectId,
    description: values.description,
    category: values.category,
    currency: "MYR",
    workItemId: values.workItemId || undefined,
    estimatedAmount: majorToMinor(values.estimated)!,
    notes: values.notes || undefined,
    materialId: values.materialId || undefined,
    quantityValue: values.quantityValue || undefined,
    quantityUnit: values.quantityUnit || undefined,
    unitPriceAmount: values.unitPrice ? majorToMinor(values.unitPrice)! : undefined,
  }), ["projects", projectId, "costs"]);
  const editStageMutation = useOperationMutation(
    (values: StageAmountValues) => api.updateCostLifecycle(detailItem!.id, { stage: editingStage!, amount: majorToMinor(values.amount)!, expectedRevision: detailItem!.revision }),
    ["projects", projectId, "costs"]
  );
  const recordActualMutation = useOperationMutation(
    (values: RecordActualValues) => api.recordCostItemActual(detailItem!.id, { amount: majorToMinor(values.amount)!, expectedRevision: detailItem!.revision }),
    ["projects", projectId, "costs"]
  );
  const correctActualMutation = useOperationMutation(
    (values: CorrectionValues) => api.correctCostItemActual(detailItem!.id, { amount: majorToMinor(values.amount)!, expectedRevision: detailItem!.revision, reason: values.reason }),
    ["projects", projectId, "costs"]
  );
  const rows = costs.data ?? [];
  const total = (key: "estimated" | "committed" | "actual" | "paid") => rows.reduce((sum, item) => sum + (item[key]?.amount ?? 0), 0);
  const [createFormError, setCreateFormError] = useState<string>();
  const costFields = ["description", "category", "workItemId", "estimated", "notes", "materialId", "quantityValue", "quantityUnit", "unitPrice"];
  const selectedCategory = form.watch("category");
  const submit = form.handleSubmit((values) => {
    setCreateFormError(undefined);
    createMutation.mutate(values, {
      onSuccess: () => { setCreateOpen(false); form.reset(createDefaults); },
      onError: (error) => setCreateFormError(applyFieldErrors(error as unknown as ApiError, form.setError, costFields)),
    });
  });
  const activeError = costs.error ?? workItems.error;

  function openCostDetail(item: CostItem) {
    setDetailItem(item);
  }

  useEffect(() => {
    if (!detailItem) return;
    const fresh = (costs.data ?? []).find((row) => row.id === detailItem.id);
    if (fresh && fresh.revision !== detailItem.revision) setDetailItem(fresh);
  }, [costs.data, detailItem]);

  return <div className="flex flex-col gap-5">
    <PageHeader title="Costs" description="Authoritative project cost position and universal ledger" actions={<Button onClick={() => setCreateOpen(true)}><Plus />Add cost item</Button>} />
    {activeError && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">The cost action could not be completed. Your input is preserved; review the refreshed ledger before retrying.</div>}
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{(["estimated", "committed", "actual", "paid"] as const).map((key) => <section key={key} className="surface-card p-5"><p className="eyebrow">{key}</p><p className="mt-3 font-mono text-xl font-semibold">{formatMoney(total(key))}</p><div className="mt-4 h-1.5 rounded-full bg-muted"><div className={`h-full rounded-full ${key === "paid" ? "bg-emerald-500" : "bg-primary"}`} style={{ width: `${Math.min(100, total("estimated") ? total(key) / total("estimated") * 100 : 0)}%` }} /></div></section>)}</div>
    <section className="surface-card overflow-hidden"><div className="flex items-center gap-3 border-b p-4"><span className="flex size-9 items-center justify-center rounded-xl bg-accent text-primary"><CircleDollarSign className="size-5" /></span><div><h2 className="font-heading font-semibold">Cost ledger</h2><p className="text-xs text-muted-foreground">All lifecycle values remain visible at the same time.</p></div></div>
      <div className="table-scroll"><table className="w-full min-w-240 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Cost item</th><th>Category</th><th>Estimated</th><th>Committed</th><th>Actual</th><th>Paid</th><th>Quantity</th></tr></thead><tbody>{rows.map((item) => <tr
        key={item.id}
        tabIndex={0}
        role="button"
        aria-label={`View cost item ${item.description}`}
        className="cursor-pointer border-b last:border-0 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => openCostDetail(item)}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openCostDetail(item); } }}
      ><td className="p-3 font-medium">{item.description}<span className="block text-xs font-normal text-muted-foreground">{item.notes}</span></td><td className="capitalize">{item.category.replaceAll("_", " ")}</td><td className="font-mono">{formatMoney(item.estimated?.amount, item.currency)}</td><td className="font-mono">{formatMoney(item.committed?.amount, item.currency)}</td><td className="font-mono">{formatMoney(item.actual?.amount, item.currency)}</td><td className="font-mono text-emerald-700">{formatMoney(item.paid?.amount, item.currency)}</td><td className="font-mono">{item.quantityValue ? `${item.quantityValue} ${item.quantityUnit ?? ""}` : "—"}</td></tr>)}</tbody></table></div>
      {!costs.isLoading && !costs.error && rows.length === 0 && <div className="p-12 text-center"><p className="font-heading font-semibold">No cost items yet</p><p className="mt-1 text-sm text-muted-foreground">Add the first authoritative project cost.</p></div>}
    </section>
    <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); setCreateFormError(undefined); }}><DialogContent><DialogHeader><DialogTitle>Add cost item</DialogTitle></DialogHeader><form onSubmit={submit} className="grid gap-4">{createFormError && <p className="text-sm text-destructive">{createFormError}</p>}<div className="grid gap-1.5"><Label>Description</Label><Input {...form.register("description")} />{form.formState.errors.description?.message && <p className="text-xs text-destructive">{form.formState.errors.description.message}</p>}</div><div className="grid gap-4 sm:grid-cols-2"><div className="grid gap-1.5"><Label>Category</Label><Select value={form.watch("category")} onValueChange={(v) => v && form.setValue("category", v as Values["category"])}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{CATEGORY_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select>{form.formState.errors.category?.message && <p className="text-xs text-destructive">{form.formState.errors.category.message}</p>}</div><div className="grid gap-1.5"><Label>Estimated (MYR)</Label><Input inputMode="decimal" {...form.register("estimated")} /></div></div><div className="grid gap-1.5"><Label>Work item (optional)</Label><Select value={form.watch("workItemId")} onValueChange={(v) => form.setValue("workItemId", v ?? "")}><SelectTrigger><SelectValue placeholder="Project-level cost" /></SelectTrigger><SelectContent>{(workItems.data?.items ?? []).map((item) => <SelectItem key={item.id} value={item.id}>{item.description}</SelectItem>)}</SelectContent></Select></div>
      {selectedCategory === "material" && <div className="grid gap-3 rounded-lg border p-3"><div className="grid gap-1.5"><Label>Material (optional)</Label><Select value={form.watch("materialId")} onValueChange={(v) => { form.setValue("materialId", v ?? ""); const material = (materials.data ?? []).find((item) => item.id === v); if (material) form.setValue("quantityUnit", material.unit); }}><SelectTrigger><SelectValue placeholder="No material linkage" /></SelectTrigger><SelectContent>{(materials.data ?? []).map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}</SelectContent></Select>{form.formState.errors.materialId?.message && <p className="text-xs text-destructive">{form.formState.errors.materialId.message}</p>}</div>
        <div className="grid grid-cols-3 gap-3"><div className="grid gap-1.5"><Label htmlFor="cost-item-quantity">Quantity</Label><Input id="cost-item-quantity" inputMode="decimal" {...form.register("quantityValue")} /></div><div className="grid gap-1.5"><Label htmlFor="cost-item-unit">Unit</Label><Input id="cost-item-unit" {...form.register("quantityUnit")} /></div><div className="grid gap-1.5"><Label htmlFor="cost-item-unit-price">Unit price (MYR)</Label><Input id="cost-item-unit-price" inputMode="decimal" {...form.register("unitPrice")} />{form.formState.errors.unitPrice?.message && <p className="text-xs text-destructive">{form.formState.errors.unitPrice.message}</p>}</div></div>
        <p className="text-xs text-muted-foreground">Linking a material and quantity here lets this cost item be used to generate a procurement requirement. A Work item is also required for that — set one above.</p>
      </div>}
      <div className="grid gap-1.5"><Label>Notes</Label><Textarea {...form.register("notes")} /></div><Button type="submit" disabled={createMutation.isPending}>Create cost item</Button></form></DialogContent></Dialog>
    <Sheet open={detailItem !== null} onOpenChange={(open) => { if (!open) { setDetailItem(null); setEditingStage(null); setRecordingActual(false); setCorrectingActual(false); } }}>
      <SheetContent>
        <SheetHeader><SheetTitle>{detailItem?.description}</SheetTitle></SheetHeader>
        {detailItem && <div className="flex-1 overflow-y-auto px-4 pb-4">
          <p className="text-xs text-muted-foreground capitalize">{detailItem.category.replaceAll("_", " ")}{detailItem.notes ? ` · ${detailItem.notes}` : ""}</p>

          <div className="mt-4 grid gap-3">
            <div className="flex items-center justify-between rounded-lg border p-3">
              <div><p className="text-xs text-muted-foreground">Estimated</p><p className="font-mono text-sm">{formatMoney(detailItem.estimated?.amount, detailItem.currency)}</p></div>
              <Button variant="outline" size="sm" onClick={() => { setEditingStage("estimated"); editForm.reset({ amount: detailItem.estimated ? minorToMajorString(detailItem.estimated.amount) : "0.00" }); setEditFormError(undefined); }}>Edit</Button>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <div><p className="text-xs text-muted-foreground">Committed</p><p className="font-mono text-sm">{formatMoney(detailItem.committed?.amount, detailItem.currency)}</p></div>
              <Button variant="outline" size="sm" onClick={() => { setEditingStage("committed"); editForm.reset({ amount: detailItem.committed ? minorToMajorString(detailItem.committed.amount) : "0.00" }); setEditFormError(undefined); }}>Edit</Button>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3">
              <div><p className="text-xs text-muted-foreground">Actual</p><p className="font-mono text-sm">{formatMoney(detailItem.actual?.amount, detailItem.currency)}</p></div>
              {detailItem.actual == null
                ? <Button variant="outline" size="sm" onClick={() => { setRecordingActual(true); recordForm.reset({ amount: "0.00" }); setRecordFormError(undefined); }}>Record actual</Button>
                : <Button variant="outline" size="sm" onClick={() => { setCorrectingActual(true); correctForm.reset({ amount: minorToMajorString(detailItem.actual!.amount), reason: "" }); setCorrectFormError(undefined); }}>Correct</Button>}
            </div>
            <div className="flex items-center justify-between rounded-lg border border-dashed p-3">
              <div><p className="text-xs text-muted-foreground">Paid</p><p className="font-mono text-sm text-emerald-700">{formatMoney(detailItem.paid?.amount, detailItem.currency)}</p></div>
              <span className="text-xs text-muted-foreground">View only — no adjustment mechanism yet</span>
            </div>
          </div>

          {detailItem.actualCorrections && detailItem.actualCorrections.length > 0 && <div className="mt-4 rounded-lg border p-3">
            <p className="text-xs font-medium text-muted-foreground">Correction history</p>
            <ul className="mt-1 grid gap-1 text-xs text-muted-foreground">{detailItem.actualCorrections.map((c, i) => <li key={i}>{formatMoney(c.previousAmount.amount, c.previousAmount.currency)} → {formatMoney(c.newAmount.amount, c.newAmount.currency)} — {c.reason}</li>)}</ul>
          </div>}

          {editingStage && <form onSubmit={editForm.handleSubmit((values) => { setEditFormError(undefined); editStageMutation.mutate(values, { onSuccess: () => setEditingStage(null), onError: (error) => setEditFormError(applyFieldErrors(error as unknown as ApiError, editForm.setError, ["amount"])) }); })} className="mt-4 grid gap-3 rounded-lg border p-3">
            <p className="text-sm font-medium capitalize">Edit {editingStage}</p>
            {editFormError && <p className="text-sm text-destructive">{editFormError}</p>}
            <Label className="grid gap-1.5">Amount (MYR)<Input inputMode="decimal" {...editForm.register("amount")} />{editForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{editForm.formState.errors.amount.message}</p>}</Label>
            <div className="flex gap-2"><Button type="submit" disabled={editStageMutation.isPending}>{editStageMutation.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setEditingStage(null)}>Cancel</Button></div>
          </form>}

          {recordingActual && <form onSubmit={recordForm.handleSubmit((values) => { setRecordFormError(undefined); recordActualMutation.mutate(values, { onSuccess: () => setRecordingActual(false), onError: (error) => setRecordFormError(applyFieldErrors(error as unknown as ApiError, recordForm.setError, ["amount"])) }); })} className="mt-4 grid gap-3 rounded-lg border p-3">
            <p className="text-sm font-medium">Record Actual</p>
            {recordFormError && <p className="text-sm text-destructive">{recordFormError}</p>}
            <Label className="grid gap-1.5">Amount (MYR)<Input inputMode="decimal" {...recordForm.register("amount")} />{recordForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{recordForm.formState.errors.amount.message}</p>}</Label>
            <div className="flex gap-2"><Button type="submit" disabled={recordActualMutation.isPending}>{recordActualMutation.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setRecordingActual(false)}>Cancel</Button></div>
          </form>}

          {correctingActual && <form onSubmit={(e) => { e.preventDefault(); correctForm.handleSubmit(() => setConfirmingCorrection(true))(); }} className="mt-4 grid gap-3 rounded-lg border p-3">
            <p className="text-sm font-medium">Correct Actual</p>
            <p className="text-xs text-muted-foreground">The previous value is preserved in this cost item&rsquo;s correction history — it is never silently discarded.</p>
            {correctFormError && <p className="text-sm text-destructive">{correctFormError}</p>}
            <Label className="grid gap-1.5">Corrected amount (MYR)<Input inputMode="decimal" {...correctForm.register("amount")} />{correctForm.formState.errors.amount?.message && <p className="text-xs font-normal text-destructive">{correctForm.formState.errors.amount.message}</p>}</Label>
            <Label className="grid gap-1.5">Reason<Textarea {...correctForm.register("reason")} />{correctForm.formState.errors.reason?.message && <p className="text-xs font-normal text-destructive">{correctForm.formState.errors.reason.message}</p>}</Label>
            <div className="flex gap-2"><Button type="submit">Review correction</Button><Button type="button" variant="outline" onClick={() => setCorrectingActual(false)}>Cancel</Button></div>
          </form>}
        </div>}

        <Dialog open={confirmingCorrection} onOpenChange={setConfirmingCorrection}>
          <DialogContent>
            <DialogHeader><DialogTitle>Confirm correction</DialogTitle></DialogHeader>
            <p className="text-sm text-muted-foreground">
              This changes the recorded Actual amount from {formatMoney(detailItem?.actual?.amount, detailItem?.currency)} to {formatMoney(majorToMinor(correctForm.getValues("amount")) ?? undefined, detailItem?.currency)}.
              The previous value is preserved in this cost item&rsquo;s correction history, not discarded.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setConfirmingCorrection(false)}>Cancel</Button>
              <Button
                disabled={correctActualMutation.isPending}
                onClick={() => {
                  setCorrectFormError(undefined);
                  correctActualMutation.mutate(correctForm.getValues(), {
                    onSuccess: () => { setConfirmingCorrection(false); setCorrectingActual(false); },
                    onError: (error) => { setConfirmingCorrection(false); setCorrectFormError(applyFieldErrors(error as unknown as ApiError, correctForm.setError, ["amount", "reason"])); },
                  });
                }}
              >
                {correctActualMutation.isPending ? "Saving…" : "Confirm correction"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </SheetContent>
    </Sheet>
  </div>;
}
