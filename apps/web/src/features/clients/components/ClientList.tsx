"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchField } from "@/components/layout/SearchField";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { PaginatedTable, type PaginatedTableColumn } from "@/components/layout/PaginatedTable";
import { useListSearchParams } from "@/lib/url/listParams";
import { formatDate } from "@/lib/formatting/date";
import { useClientList } from "../queries";
import { useCreateClient } from "../mutations";
import { ClientForm } from "./ClientForm";
import type { Client } from "../api";

const columns: PaginatedTableColumn<Client>[] = [
  {
    key: "name",
    label: "Client",
    sortable: true,
    render: (row) => (
      <div className="flex items-center gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent text-xs font-semibold text-accent-foreground">
          {row.name.slice(0, 2).toUpperCase()}
        </span>
        <span className="font-medium text-foreground">{row.name}</span>
      </div>
    ),
  },
  {
    key: "contact",
    label: "Contact",
    render: (row) => (
      <span className="flex flex-col">
        <span>{row.email || "—"}</span>
        {row.phone && <span className="text-xs text-muted-foreground">{row.phone}</span>}
      </span>
    ),
  },
  {
    key: "address",
    label: "Location",
    render: (row) => <span className="block max-w-56 truncate text-muted-foreground">{row.address || "—"}</span>,
  },
  {
    key: "createdAt",
    label: "Created",
    sortable: true,
    render: (row) => formatDate(row.createdAt),
  },
];

// Wires URL-derived list state (useListSearchParams) into a TanStack Query
// fetch, then hands the result to the shared, presentation-only
// PaginatedTable. The search input is debounced locally so every keystroke
// doesn't rewrite the URL, but the committed value that lands in the URL
// (and therefore the query key) is what search actually filters by.
export function ClientList() {
  const router = useRouter();
  const { params, setParams } = useListSearchParams();
  const [searchInput, setSearchInput] = useState(params.search ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const { data, isLoading, error } = useClientList(params);
  const createClient = useCreateClient();

  function handleSearchChange(value: string) {
    setSearchInput(value);
    setParams({ search: value || undefined });
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Clients"
        description={data ? `${data.total} client ${data.total === 1 ? "account" : "accounts"}` : "Contractor client accounts"}
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            Add client
          </Button>
        }
      />
      <div className="surface-card flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchField
          placeholder="Search clients…"
          value={searchInput}
          onChange={(e) => handleSearchChange(e.target.value)}
          className="w-full sm:max-w-sm"
        />
        <span className="text-xs text-muted-foreground">Search by client name or contact details</span>
      </div>
      <PaginatedTable<Client>
        columns={columns}
        rows={data?.items ?? []}
        getRowKey={(row) => row.id}
        page={data?.page ?? params.page}
        pageSize={data?.pageSize ?? params.pageSize ?? 25}
        total={data?.total ?? 0}
        isLoading={isLoading}
        error={error ? "Could not load clients." : undefined}
        emptyMessage="No clients yet."
        onPageChange={(page) => setParams({ page })}
        onSortChange={(sort) =>
          setParams({ sort, order: params.sort === sort && params.order === "asc" ? "desc" : "asc" })
        }
        onRowClick={(row) => router.push(`/clients/${row.id}`)}
      />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add client</DialogTitle>
          </DialogHeader>
          <ClientForm
            submitting={createClient.isPending}
            onSubmit={(values) => {
              createClient.mutate(
                {
                  name: values.name,
                  email: values.email,
                  phone: values.phone,
                  address: values.address,
                  billingAddress: values.billingAddress,
                  notes: values.notes,
                },
                { onSuccess: () => setCreateOpen(false) }
              );
            }}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
