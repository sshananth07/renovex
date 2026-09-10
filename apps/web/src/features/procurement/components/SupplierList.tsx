"use client";

import { useState } from "react";
import Link from "next/link";
import { Building2, Plus } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchField } from "@/components/layout/SearchField";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { createSupplier } from "../api";
import { useSuppliers } from "../queries";
import { SupplierForm, type SupplierValues } from "./SupplierForm";

export function SupplierList() {
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [active, setActive] = useState("All statuses");
  const [open, setOpen] = useState(false);
  const suppliers = useSuppliers({
    q: search || undefined,
    category: category || undefined,
    active: active === "All statuses" ? undefined : active === "Active" ? "true" : "false",
  });
  const queryClient = useQueryClient();
  const create = useMutation({
    mutationFn: (values: SupplierValues) => createSupplier({
      name: values.name,
      contactPerson: values.contactPerson || undefined,
      email: values.email || undefined,
      phone: values.phone || undefined,
      address: values.address || undefined,
      materialCategories: values.categories.split(",").map((value) => value.trim()).filter(Boolean),
      notes: values.notes || undefined,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["suppliers"] });
      setOpen(false);
    },
  });

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Suppliers" description="Reusable contractor supplier directory" actions={<Button onClick={() => setOpen(true)}><Plus />Add supplier</Button>} />
      <div className="surface-card grid gap-2 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,0.45fr)_11rem]">
        <SearchField value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search suppliers…" className="w-full" />
        <Input value={category} onChange={(event) => setCategory(event.target.value)} placeholder="Filter by category" />
        <Select value={active} onValueChange={(value) => setActive(value ?? "All statuses")}>
          <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="All statuses">All statuses</SelectItem><SelectItem value="Active">Active</SelectItem><SelectItem value="Inactive">Inactive</SelectItem></SelectContent>
        </Select>
      </div>
      {(suppliers.error || create.error) && <div className="rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">The Supplier action could not be completed. Your input is preserved; retry after reviewing the current directory.</div>}
      <section className="surface-card overflow-hidden">
        <div className="table-scroll">
          <table className="w-full min-w-200 text-sm">
            <thead><tr className="border-b bg-muted/40 text-left"><th className="p-3">Supplier</th><th>Contact</th><th>Categories</th><th>Status</th><th className="w-20" /></tr></thead>
            <tbody>{(suppliers.data ?? []).map((supplier) => <tr className="border-b last:border-0" key={supplier.id}><td className="p-3"><Link href={`/suppliers/${supplier.id}`} className="font-semibold hover:text-primary">{supplier.name}</Link><span className="block text-xs text-muted-foreground">{supplier.address}</span></td><td>{supplier.contactPerson || "—"}<span className="block text-xs text-muted-foreground">{supplier.email || supplier.phone}</span></td><td><div className="flex flex-wrap gap-1">{supplier.materialCategories?.map((item) => <Badge variant="outline" key={item}>{item}</Badge>)}</div></td><td><Badge variant="outline" className={supplier.active ? "border-emerald-200 bg-emerald-50 text-emerald-700" : ""}>{supplier.active ? "Active" : "Inactive"}</Badge></td><td><Link className="text-sm font-semibold text-primary hover:underline" href={`/suppliers/${supplier.id}`}>Open</Link></td></tr>)}</tbody>
          </table>
        </div>
        {!suppliers.isLoading && !suppliers.error && (suppliers.data ?? []).length === 0 && <div className="flex flex-col items-center p-12 text-center"><Building2 className="size-9 text-primary" /><h2 className="mt-3 font-heading font-semibold">No suppliers found</h2><p className="mt-1 text-sm text-muted-foreground">Adjust the filters or create a directory entry for RFQ invitations.</p></div>}
      </section>
      <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>Add supplier</DialogTitle></DialogHeader><SupplierForm pending={create.isPending} onSubmit={(values) => create.mutate(values)} /></DialogContent></Dialog>
    </div>
  );
}
