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
import { useProjectList } from "../queries";
import { useCreateProject } from "../mutations";
import { useClientList } from "@/features/clients/queries";
import { ProjectForm } from "./ProjectForm";
import { StatusBadge } from "./StatusBadge";
import type { Project } from "../api";

export function ProjectList() {
  const router = useRouter();
  const { params, setParams } = useListSearchParams();
  const [searchInput, setSearchInput] = useState(params.search ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const { data, isLoading, error } = useProjectList(params);
  const { data: clientsData } = useClientList({ page: 1, pageSize: 100 });
  const createProject = useCreateProject();
  const clientNameById = new Map((clientsData?.items ?? []).map((client) => [client.id, client.name]));

  const columns: PaginatedTableColumn<Project>[] = [
    {
      key: "name",
      label: "Project",
      sortable: true,
      render: (row) => <span className="font-medium text-foreground">{row.name}</span>,
    },
    {
      key: "clientId",
      label: "Client",
      render: (row) => clientNameById.get(row.clientId) ?? "—",
    },
    {
      key: "status",
      label: "Status",
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: "createdAt",
      label: "Created",
      sortable: true,
      render: (row) => formatDate(row.createdAt),
    },
  ];

  function handleSearchChange(value: string) {
    setSearchInput(value);
    setParams({ search: value || undefined });
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Projects"
        description={data ? `${data.total} renovation ${data.total === 1 ? "project" : "projects"}` : "Active renovation portfolio"}
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            Add project
          </Button>
        }
      />
      <div className="surface-card flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchField
          placeholder="Search projects…"
          value={searchInput}
          onChange={(e) => handleSearchChange(e.target.value)}
          className="w-full sm:max-w-md"
        />
        <span className="text-xs text-muted-foreground">Search project names</span>
      </div>
      <PaginatedTable<Project>
        columns={columns}
        rows={data?.items ?? []}
        getRowKey={(row) => row.id}
        page={data?.page ?? params.page}
        pageSize={data?.pageSize ?? params.pageSize ?? 25}
        total={data?.total ?? 0}
        isLoading={isLoading}
        error={error ? "Could not load projects." : undefined}
        emptyMessage="No projects yet."
        onPageChange={(page) => setParams({ page })}
        onSortChange={(sort) =>
          setParams({ sort, order: params.sort === sort && params.order === "asc" ? "desc" : "asc" })
        }
        onRowClick={(row) => router.push(`/projects/${row.id}`)}
      />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add project</DialogTitle>
          </DialogHeader>
          <ProjectForm
            clients={clientsData?.items ?? []}
            submitting={createProject.isPending}
            onSubmit={(values) => {
              createProject.mutate(values, { onSuccess: () => setCreateOpen(false) });
            }}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
