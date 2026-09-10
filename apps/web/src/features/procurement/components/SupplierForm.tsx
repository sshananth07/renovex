"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { Supplier } from "../api";

const schema = z.object({ name: z.string().trim().min(1, "Name is required"), contactPerson: z.string(), email: z.string(), phone: z.string(), address: z.string(), categories: z.string(), notes: z.string() });
export type SupplierValues = z.infer<typeof schema>;

export function SupplierForm({ supplier, pending, onSubmit }: { supplier?: Supplier; pending: boolean; onSubmit: (values: SupplierValues) => void }) {
  const form = useForm<SupplierValues>({ resolver: zodResolver(schema), defaultValues: { name: supplier?.name ?? "", contactPerson: supplier?.contactPerson ?? "", email: supplier?.email ?? "", phone: supplier?.phone ?? "", address: supplier?.address ?? "", categories: supplier?.materialCategories?.join(", ") ?? "", notes: supplier?.notes ?? "" } });
  return <form className="grid gap-4" onSubmit={form.handleSubmit(onSubmit)}>{[
    ["name", "Supplier name"], ["contactPerson", "Contact person"], ["email", "Email"], ["phone", "Phone"], ["address", "Address"], ["categories", "Material categories (comma-separated)"],
  ].map(([name, label]) => { const id=`supplier-${name}`; return <div className="grid gap-1.5" key={name}><Label htmlFor={id}>{label}</Label><Input id={id} {...form.register(name as keyof SupplierValues)} />{form.formState.errors[name as keyof SupplierValues]?.message && <p className="text-xs text-destructive">{form.formState.errors[name as keyof SupplierValues]?.message}</p>}</div>; })}<div className="grid gap-1.5"><Label htmlFor="supplier-notes">Notes</Label><Textarea id="supplier-notes" {...form.register("notes")} /></div><Button type="submit" disabled={pending}>Save supplier</Button></form>;
}
