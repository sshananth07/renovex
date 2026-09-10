"use client";

import { useState } from "react";
import { ListChecks, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchField } from "@/components/layout/SearchField";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { PaginatedTable, type PaginatedTableColumn } from "@/components/layout/PaginatedTable";
import { useListSearchParams } from "@/lib/url/listParams";
import { useSpaceList } from "@/features/spaces/queries";
import { useWorkItemList } from "../queries";
import { useCreateWorkItem, useUpdateWorkItem, useUpdateWorkItemStatus } from "../mutations";
import { WorkItemForm } from "./WorkItemForm";
import { WorkItemResources } from "./WorkItemResources";
import type { WorkItem } from "../api";
import type { WorkItemFormValues } from "../schemas";

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="surface-card flex min-h-72 flex-col items-center justify-center gap-3 border-dashed p-8 text-center">
      <div className="flex size-12 items-center justify-center rounded-2xl bg-accent text-accent-foreground">
        <ListChecks className="size-6" />
      </div>
      <div>
        <p className="font-heading text-base font-semibold text-foreground">No work items yet</p>
        <p className="mt-1 max-w-md text-sm text-muted-foreground">
          Scope the renovation into work items so quantities, spaces, and status are tracked per task.
        </p>
      </div>
      <Button onClick={onCreate} className="mt-1">
        <Plus className="size-4" />
        Add work item
      </Button>
    </div>
  );
}

function toFormValues(item: WorkItem): Partial<WorkItemFormValues> {
  return {
    description: item.description,
    quantityValue: item.quantityValue,
    quantityUnit: item.quantityUnit,
    workType: item.workType ?? "",
    spaceId: item.spaceId ?? "",
  };
}

// Every field on WorkItemFormValues maps onto a genuine-partial PATCH field —
// spaceId is the one tri-state exception: an empty selection in the form
// means "explicitly clear", not "leave unchanged" (there is no UI affordance
// for the omit case on edit, since the form always has a definite value once
// opened against an existing item).
function toUpdateInput(values: WorkItemFormValues) {
  return {
    description: values.description,
    quantityValue: values.quantityValue,
    quantityUnit: values.quantityUnit,
    workType: values.workType || "",
    spaceId: values.spaceId ? values.spaceId : null,
  };
}

// Work Items render as a dense operational table (architecture doc §F1.8) —
// the primary renovation scope-management surface, so density and clear
// column hierarchy (description/space/type/qty+unit/status/actions) matter
// more here than the card treatment Spaces uses.
export function WorkItemList({ projectId }: { projectId: string }) {
  const { params, setParams } = useListSearchParams();
  const [searchInput, setSearchInput] = useState(params.search ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const [editingItem, setEditingItem] = useState<WorkItem | null>(null);
  const [cancellingItem, setCancellingItem] = useState<WorkItem | null>(null);

  const { data, isLoading, error } = useWorkItemList(projectId, params);
  const { data: spacesData } = useSpaceList(projectId, { page: 1, pageSize: 100 });
  const spaces = spacesData?.items ?? [];
  const spaceNameById = new Map(spaces.map((space) => [space.id, space.name]));

  const createWorkItem = useCreateWorkItem(projectId);
  const updateWorkItem = useUpdateWorkItem(projectId, editingItem?.id ?? "");
  const updateStatus = useUpdateWorkItemStatus(projectId);

  function handleSearchChange(value: string) {
    setSearchInput(value);
    setParams({ search: value || undefined });
  }

  const items = data?.items ?? [];

  const columns: PaginatedTableColumn<WorkItem>[] = [
    { key: "description", label: "Work item", sortable: true, render: (row) => <span className="font-medium text-foreground">{row.description}</span> },
    {
      key: "spaceId",
      label: "Space",
      render: (row) => row.spaceId ? <Badge variant="outline" className="bg-muted/50 font-normal text-muted-foreground">{spaceNameById.get(row.spaceId) ?? "—"}</Badge> : <span className="text-muted-foreground">—</span>,
    },
    { key: "workType", label: "Work type", render: (row) => row.workType || "—" },
    {
      key: "quantity",
      label: "Quantity",
      render: (row) => `${row.quantityValue} ${row.quantityUnit}`,
    },
    {
      key: "status",
      label: "Status",
      render: (row) =>
        row.status === "cancelled" ? (
          <Badge variant="outline" className="bg-muted/60 text-muted-foreground">
            Cancelled
          </Badge>
        ) : (
          <Badge variant="outline" className="border-primary/20 bg-primary/5 text-primary">Planned</Badge>
        ),
    },
    {
      key: "actions",
      label: "",
      render: (row) =>
        row.status === "planned" ? (
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation();
              setCancellingItem(row);
            }}
          >
            Cancel
          </Button>
        ) : null,
    },
  ];

  const isEmptyWithNoFilter = !isLoading && !error && items.length === 0 && !params.search;

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Work Items"
        description={data ? `${data.total} scope ${data.total === 1 ? "item" : "items"} in this project` : "Operational scope for this project"}
        actions={!isEmptyWithNoFilter ? <Button onClick={() => setCreateOpen(true)}><Plus className="size-4" />Add work item</Button> : undefined}
      />
      {!isEmptyWithNoFilter && (
        <div className="surface-card flex items-center p-3">
          <SearchField
            placeholder="Search work items…"
            value={searchInput}
            onChange={(e) => handleSearchChange(e.target.value)}
            className="w-full sm:max-w-sm"
          />
        </div>
      )}

      {isEmptyWithNoFilter ? (
        <EmptyState onCreate={() => setCreateOpen(true)} />
      ) : (
        <PaginatedTable<WorkItem>
          columns={columns}
          rows={items}
          getRowKey={(row) => row.id}
          page={data?.page ?? params.page}
          pageSize={data?.pageSize ?? params.pageSize ?? 25}
          total={data?.total ?? 0}
          isLoading={isLoading}
          error={error ? "Could not load work items." : undefined}
          emptyMessage="No work items match your search."
          onPageChange={(page) => setParams({ page })}
          onSortChange={(sort) =>
            setParams({ sort, order: params.sort === sort && params.order === "asc" ? "desc" : "asc" })
          }
          onRowClick={(row) => setEditingItem(row)}
        />
      )}

      <Sheet open={createOpen} onOpenChange={setCreateOpen}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Add work item</SheetTitle>
          </SheetHeader>
          <div className="flex-1 overflow-y-auto px-4 pb-4">
            <WorkItemForm
              spaces={spaces}
              submitting={createWorkItem.isPending}
              onSubmit={(values) => {
                createWorkItem.mutate(
                  {
                    description: values.description,
                    quantityValue: values.quantityValue,
                    quantityUnit: values.quantityUnit,
                    workType: values.workType || undefined,
                    spaceId: values.spaceId || undefined,
                  },
                  { onSuccess: () => setCreateOpen(false) }
                );
              }}
            />
          </div>
        </SheetContent>
      </Sheet>

      <Sheet open={editingItem !== null} onOpenChange={(open) => !open && setEditingItem(null)}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Edit work item</SheetTitle>
          </SheetHeader>
          <div className="flex-1 overflow-y-auto px-4 pb-4">
            {editingItem && (
              <>
                {updateWorkItem.isError && (
                  <p role="alert" className="mb-3 text-sm text-destructive">
                    {(updateWorkItem.error as { detail?: string })?.detail ??
                      "Could not save this work item."}
                  </p>
                )}
                <WorkItemForm
                  spaces={spaces}
                  defaultValues={toFormValues(editingItem)}
                  submitting={updateWorkItem.isPending}
                  onSubmit={(values) => {
                    updateWorkItem.mutate(toUpdateInput(values), {
                      onSuccess: () => setEditingItem(null),
                    });
                  }}
                />
                <div className="mt-5 border-t border-border pt-4">
                  <h3 className="text-sm font-semibold">Resources</h3>
                  <div className="mt-2">
                    <WorkItemResources projectId={projectId} workItemId={editingItem.id} />
                  </div>
                </div>
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <Dialog open={cancellingItem !== null} onOpenChange={(open) => !open && setCancellingItem(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Cancel work item</DialogTitle>
            <DialogDescription>
              This marks &ldquo;{cancellingItem?.description}&rdquo; as cancelled. Cancelled work items can
              no longer be edited.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCancellingItem(null)}>
              Keep it
            </Button>
            <Button
              variant="destructive"
              disabled={updateStatus.isPending}
              onClick={() => {
                if (!cancellingItem) return;
                updateStatus.mutate(
                  { id: cancellingItem.id, status: "cancelled" },
                  { onSuccess: () => setCancellingItem(null) }
                );
              }}
            >
              Confirm
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
