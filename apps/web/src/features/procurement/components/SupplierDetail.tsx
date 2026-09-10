"use client";

import { useState } from "react";
import Link from "next/link";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { ArrowLeft, FolderKanban, Mail, Pencil, Phone, Plus } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { applyFieldErrors } from "@/lib/api/applyFieldErrors";
import type { ApiError } from "@/lib/api/errors";
import { formatMoney, majorToMinor, minorToMajorString } from "@/lib/formatting/money";
import { createOffering, updateSupplier, type Offering } from "../api";
import { useUpdateOffering, useSetOfferingActive } from "../mutations";
import { useOfferings, useSupplier } from "../queries";
import { SupplierForm, type SupplierValues } from "./SupplierForm";

const todayDateInput = () => new Date().toISOString().slice(0, 10);

const offeringSchema = z.object({
  productName: z.string().trim().min(1, "Product name is required"),
  category: z.string(),
  unit: z.string(),
  price: z.string().refine((v) => v === "" || majorToMinor(v) !== null, "Enter a valid amount"),
  priceAsOf: z.string(),
}).superRefine((values, ctx) => {
  if (values.price === "") return;
  if (values.unit.trim() === "") {
    ctx.addIssue({ code: "custom", path: ["unit"], message: "Unit is required when an indicative price is provided" });
  }
  if (values.priceAsOf === "") {
    ctx.addIssue({ code: "custom", path: ["priceAsOf"], message: "Price as-of date is required when an indicative price is provided" });
  }
});
type OfferingValues = z.infer<typeof offeringSchema>;

function toOfferingFormValues(offering: Offering): OfferingValues {
  return {
    productName: offering.productName,
    category: offering.category ?? "",
    unit: offering.unit ?? "",
    price: offering.indicativePrice ? minorToMajorString(offering.indicativePrice.amount) : "",
    priceAsOf: offering.indicativePriceAsOf ? offering.indicativePriceAsOf.slice(0, 10) : todayDateInput(),
  };
}

export function SupplierDetail({ supplierId }: { supplierId: string }) {
  const supplier = useSupplier(supplierId);
  const offerings = useOfferings(supplierId);
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [offeringOpen, setOfferingOpen] = useState(false);
  const [editingOffering, setEditingOffering] = useState<Offering | null>(null);
  const offeringDefaults: OfferingValues = { productName: "", category: "", unit: "", price: "", priceAsOf: todayDateInput() };
  const offeringForm = useForm<OfferingValues>({ resolver: zodResolver(offeringSchema), defaultValues: offeringDefaults });
  const editOfferingForm = useForm<OfferingValues>({ resolver: zodResolver(offeringSchema), defaultValues: offeringDefaults });
  const update = useMutation({ mutationFn: (values: SupplierValues) => updateSupplier(supplierId, { expectedRevision: supplier.data!.revision, name: values.name, contactPerson: values.contactPerson, email: values.email, phone: values.phone, address: values.address, materialCategories: values.categories.split(",").map((v) => v.trim()).filter(Boolean), notes: values.notes }), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId] }); setEditOpen(false); } });
  const [offeringFormError, setOfferingFormError] = useState<string>();
  const [editOfferingFormError, setEditOfferingFormError] = useState<string>();
  const offeringFields = ["productName", "category", "unit", "price", "priceAsOf"];
  const offeringFieldAliases = { indicativePriceAmount: "price", indicativePriceCurrency: "price", indicativePriceAsOf: "priceAsOf" };
  const createProduct = useMutation({
    mutationFn: (values: OfferingValues) => createOffering({
      supplierId,
      productName: values.productName,
      category: values.category || undefined,
      unit: values.unit || undefined,
      indicativePriceAmount: values.price ? majorToMinor(values.price) ?? undefined : undefined,
      indicativePriceCurrency: values.price ? "MYR" : undefined,
      indicativePriceAsOf: values.price ? new Date(values.priceAsOf).toISOString() : undefined,
    }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["suppliers", supplierId, "offerings"] }); setOfferingOpen(false); offeringForm.reset(offeringDefaults); },
    onError: (error) => setOfferingFormError(applyFieldErrors(error as unknown as ApiError, offeringForm.setError, offeringFields, offeringFieldAliases)),
  });
  const updateOffering = useUpdateOffering(supplierId);
  const setActive = useSetOfferingActive(supplierId);
  const submitOffering = offeringForm.handleSubmit((values) => { setOfferingFormError(undefined); createProduct.mutate(values); });
  const offeringPriceEntered = offeringForm.watch("price") !== "";

  function openOfferingEditor(entry: Offering) {
    setEditingOffering(entry);
    editOfferingForm.reset(toOfferingFormValues(entry));
    setEditOfferingFormError(undefined);
  }

  const submitEditOffering = editOfferingForm.handleSubmit((values) => {
    if (!editingOffering) return;
    setEditOfferingFormError(undefined);
    updateOffering.mutate(
      {
        offeringId: editingOffering.id,
        body: {
          expectedRevision: editingOffering.revision,
          productName: values.productName,
          category: values.category || undefined,
          unit: values.unit || undefined,
          indicativePriceAmount: values.price ? majorToMinor(values.price) ?? undefined : undefined,
          indicativePriceCurrency: values.price ? "MYR" : undefined,
          indicativePriceAsOf: values.price ? new Date(values.priceAsOf).toISOString() : undefined,
          clearIndicativePrice: values.price === "",
        },
      },
      {
        onSuccess: (updated) => setEditingOffering(updated),
        onError: (error) => setEditOfferingFormError(applyFieldErrors(error as unknown as ApiError, editOfferingForm.setError, offeringFields, offeringFieldAliases)),
      }
    );
  });

  function toggleAvailability() {
    if (!editingOffering) return;
    setActive.mutate(
      { offeringId: editingOffering.id, expectedRevision: editingOffering.revision, active: !editingOffering.active },
      { onSuccess: (updated) => setEditingOffering(updated) }
    );
  }
  if (supplier.isLoading) return <div className="surface-card h-64 animate-pulse" />;
  if (supplier.error || !supplier.data) return <div className="surface-card p-10 text-center"><h1 className="font-heading text-xl font-semibold">Supplier unavailable</h1><p className="mt-2 text-sm text-muted-foreground">This Supplier record could not be loaded.</p></div>;
  const item = supplier.data;
  return <div className="flex flex-col gap-5"><Link href="/suppliers" className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />Suppliers</Link><section className="surface-card p-5 sm:p-6"><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex items-center gap-2"><h1 className="font-heading text-2xl font-semibold">{item.name}</h1><Badge variant="outline">{item.active ? "Active" : "Inactive"}</Badge></div><p className="mt-2 text-sm text-muted-foreground">{item.contactPerson || "No contact person"}</p><div className="mt-3 flex flex-wrap gap-4 text-sm">{item.email && <a href={`mailto:${item.email}`} className="flex items-center gap-1.5 text-primary"><Mail className="size-4" />{item.email}</a>}{item.phone && <a href={`tel:${item.phone}`} className="flex items-center gap-1.5 text-primary"><Phone className="size-4" />{item.phone}</a>}</div></div><div className="flex flex-wrap gap-2"><Link href="/projects" className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-primary-foreground hover:bg-primary/90" title="Choose a Project, then open its Procurement tab"><FolderKanban className="size-4" />Use in RFQ</Link><Button variant="outline" onClick={() => setEditOpen(true)}><Pencil />Edit supplier</Button></div></div><div className="mt-5 grid gap-4 border-t pt-5 sm:grid-cols-3"><div><p className="eyebrow">Address</p><p className="mt-1 text-sm">{item.address || "—"}</p></div><div><p className="eyebrow">Capabilities</p><div className="mt-2 flex flex-wrap gap-1">{item.materialCategories?.length ? item.materialCategories.map((entry) => <Badge variant="outline" key={entry}>{entry}</Badge>) : <span className="text-sm text-muted-foreground">None recorded</span>}</div></div><div><p className="eyebrow">Notes</p><p className="mt-1 text-sm whitespace-pre-wrap">{item.notes || "—"}</p></div></div></section>{(supplier.error || offerings.error || update.error) && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">The Supplier action could not be completed. Your input is preserved for review.</div>}<section className="surface-card overflow-hidden"><div className="flex items-center justify-between border-b p-4"><div><h2 className="font-heading font-semibold">Offerings</h2><p className="text-xs text-muted-foreground">Products and capabilities recorded for this supplier.</p></div><Button onClick={() => setOfferingOpen(true)}><Plus />Add offering</Button></div><div className="table-scroll"><table className="w-full min-w-160 text-sm"><thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Product</th><th>Category</th><th>Unit</th><th>Indicative price</th><th>Availability</th></tr></thead><tbody>{(offerings.data ?? []).map((entry) => <tr key={entry.id} tabIndex={0} role="button" aria-label={`Edit offering ${entry.productName}`} className="cursor-pointer border-b last:border-0 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => openOfferingEditor(entry)} onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openOfferingEditor(entry); } }}><td className="p-3 font-medium">{entry.productName}<span className="block text-xs text-muted-foreground">{entry.brand}</span></td><td>{entry.category || "—"}</td><td>{entry.unit || "—"}</td><td className="font-mono">{entry.indicativePrice ? formatMoney(entry.indicativePrice.amount, entry.indicativePrice.currency) : "—"}</td><td>{entry.effectivelyAvailable ? "Available" : "Unavailable"}</td></tr>)}</tbody></table></div>{!offerings.isLoading && !offerings.error && (offerings.data ?? []).length === 0 && <p className="p-10 text-center text-sm text-muted-foreground">No offerings recorded.</p>}</section><Dialog open={editOpen} onOpenChange={setEditOpen}><DialogContent><DialogHeader><DialogTitle>Edit supplier</DialogTitle></DialogHeader><SupplierForm supplier={item} pending={update.isPending} onSubmit={(v) => update.mutate(v)} /></DialogContent></Dialog><Dialog open={offeringOpen} onOpenChange={(open) => { setOfferingOpen(open); setOfferingFormError(undefined); if (!open) offeringForm.reset(offeringDefaults); }}><DialogContent><DialogHeader><DialogTitle>Add offering</DialogTitle></DialogHeader><form onSubmit={submitOffering} className="grid gap-4">
    {offeringFormError && <p className="text-sm text-destructive">{offeringFormError}</p>}
    <div className="grid gap-1.5"><Label>Product name</Label><Input {...offeringForm.register("productName")} />{offeringForm.formState.errors.productName?.message && <p className="text-xs text-destructive">{offeringForm.formState.errors.productName.message}</p>}</div>
    <div className="grid gap-1.5"><Label>Category</Label><Input {...offeringForm.register("category")} /></div>
    <div className="grid gap-1.5"><Label>Unit{offeringPriceEntered ? " (required with a price)" : ""}</Label><Input {...offeringForm.register("unit")} />{offeringForm.formState.errors.unit?.message && <p className="text-xs text-destructive">{offeringForm.formState.errors.unit.message}</p>}</div>
    <div className="grid gap-1.5"><Label>Indicative price (MYR)</Label><Input inputMode="decimal" placeholder="Leave blank if unknown" {...offeringForm.register("price")} />{offeringForm.formState.errors.price?.message && <p className="text-xs text-destructive">{offeringForm.formState.errors.price.message}</p>}</div>
    {offeringPriceEntered && <div className="grid gap-1.5"><Label>Price as of</Label><Input type="date" {...offeringForm.register("priceAsOf")} /><p className="text-xs text-muted-foreground">Date this price was observed or applied.</p>{offeringForm.formState.errors.priceAsOf?.message && <p className="text-xs text-destructive">{offeringForm.formState.errors.priceAsOf.message}</p>}</div>}
    <p className="text-xs text-muted-foreground">Indicative pricing is reference information, not an authoritative project cost.</p>
    <Button type="submit" disabled={createProduct.isPending}>Save offering</Button>
  </form></DialogContent></Dialog>
  <Sheet open={editingOffering !== null} onOpenChange={(open) => { if (!open) setEditingOffering(null); }}>
    <SheetContent>
      <SheetHeader><SheetTitle>Edit offering</SheetTitle></SheetHeader>
      <div className="flex-1 overflow-y-auto px-4 pb-4">
        {editingOffering && <>
          <div className="mb-4 flex items-center justify-between rounded-lg border p-3">
            <div>
              <p className="text-sm font-medium">Availability</p>
              <p className="text-xs text-muted-foreground">{editingOffering.active ? "Available for new RFQ invitations" : "Marked unavailable"}</p>
            </div>
            <Button type="button" variant="outline" size="sm" disabled={setActive.isPending} onClick={toggleAvailability}>
              {setActive.isPending ? "Saving…" : editingOffering.active ? "Mark unavailable" : "Mark available"}
            </Button>
          </div>
          <form onSubmit={submitEditOffering} className="grid gap-4">
            {editOfferingFormError && <p className="text-sm text-destructive">{editOfferingFormError}</p>}
            <Label className="grid gap-1.5">Product name<Input {...editOfferingForm.register("productName")} />{editOfferingForm.formState.errors.productName?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.productName.message}</p>}</Label>
            <Label className="grid gap-1.5">Category<Input {...editOfferingForm.register("category")} /></Label>
            <Label className="grid gap-1.5">Unit<Input {...editOfferingForm.register("unit")} />{editOfferingForm.formState.errors.unit?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.unit.message}</p>}</Label>
            <Label className="grid gap-1.5">Indicative price (MYR)<Input inputMode="decimal" placeholder="Leave blank if unknown…" {...editOfferingForm.register("price")} />{editOfferingForm.formState.errors.price?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.price.message}</p>}</Label>
            {editOfferingForm.watch("price") !== "" && <Label className="grid gap-1.5">Price as of<Input type="date" {...editOfferingForm.register("priceAsOf")} />{editOfferingForm.formState.errors.priceAsOf?.message && <p className="text-xs font-normal text-destructive">{editOfferingForm.formState.errors.priceAsOf.message}</p>}</Label>}
            <div className="flex gap-2"><Button type="submit" disabled={updateOffering.isPending}>{updateOffering.isPending ? "Saving…" : "Save"}</Button><Button type="button" variant="outline" onClick={() => setEditingOffering(null)}>Cancel</Button></div>
          </form>
        </>}
      </div>
    </SheetContent>
  </Sheet>
  </div>;
}
