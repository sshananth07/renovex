"use client";

import { useState } from "react";
import Link from "next/link";
import { LayoutGrid, Pencil, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { PageHeader } from "@/components/layout/PageHeader";
import { SearchField } from "@/components/layout/SearchField";
import { useListSearchParams } from "@/lib/url/listParams";
import { useSpaceList } from "../queries";
import { useCreateSpace, useUpdateSpace } from "../mutations";
import { SpaceForm } from "./SpaceForm";
import type { Space } from "../api";
import type { SpaceFormValues } from "../schemas";

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="surface-card flex min-h-72 flex-col items-center justify-center gap-3 border-dashed p-8 text-center">
      <span className="flex size-12 items-center justify-center rounded-2xl bg-accent text-accent-foreground"><LayoutGrid className="size-6" /></span>
      <div><p className="font-heading text-base font-semibold">No spaces yet</p><p className="mt-1 max-w-md text-sm text-muted-foreground">Break the property into rooms and areas so Work Items can be organized by where the work happens.</p></div>
      <Button onClick={onCreate} className="mt-1"><Plus className="size-4" />Add space</Button>
    </div>
  );
}

function SpaceCardSkeleton() {
  return <div className="surface-card flex min-h-44 flex-col gap-3 p-5"><Skeleton className="size-10 rounded-xl" /><Skeleton className="h-5 w-2/3" /><Skeleton className="h-4 w-1/3" /><Skeleton className="h-4 w-full" /></div>;
}

export function SpaceList({ projectId }: { projectId: string }) {
  const { params, setParams } = useListSearchParams();
  const [searchInput, setSearchInput] = useState(params.search ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const [editingSpace, setEditingSpace] = useState<Space | null>(null);
  const { data, isLoading, error } = useSpaceList(projectId, params);
  const createSpace = useCreateSpace(projectId);
  const updateSpace = useUpdateSpace(projectId, editingSpace?.id ?? "");
  const spaces = data?.items ?? [];
  const isEmptyWithNoFilter = !isLoading && !error && spaces.length === 0 && !params.search;

  function handleSearchChange(value: string) {
    setSearchInput(value);
    setParams({ search: value || undefined });
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Spaces"
        description={data ? `${data.total} ${data.total === 1 ? "space" : "spaces"} defined in this project` : "Rooms and work areas in this project"}
        actions={!isEmptyWithNoFilter ? <Button onClick={() => setCreateOpen(true)}><Plus className="size-4" />Add space</Button> : undefined}
      />

      {!isEmptyWithNoFilter && (
        <div className="surface-card flex items-center p-3">
          <SearchField placeholder="Search spaces…" value={searchInput} onChange={(event) => handleSearchChange(event.target.value)} className="w-full sm:max-w-sm" />
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 6 }).map((_, index) => <SpaceCardSkeleton key={index} />)}</div>
      ) : error ? (
        <div className="surface-card border-destructive/20 bg-destructive/5 py-14 text-center text-sm text-destructive">Could not load spaces.</div>
      ) : spaces.length === 0 ? (
        <EmptyState onCreate={() => setCreateOpen(true)} />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {spaces.map((space) => (
            <article key={space.id} className="surface-card group relative flex min-h-44 flex-col p-5 transition-all hover:-translate-y-0.5 hover:border-primary/25 hover:shadow-md">
              <div className="flex items-start justify-between gap-3">
                <span className="flex size-10 items-center justify-center rounded-xl bg-accent text-accent-foreground"><LayoutGrid className="size-5" /></span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Edit ${space.name}`}
                  className="relative z-10 text-muted-foreground hover:text-primary"
                  onClick={() => setEditingSpace(space)}
                >
                  <Pencil className="size-3.5" />
                </Button>
              </div>
              <Link href={`/projects/${projectId}/spaces/${space.id}`} className="mt-4 flex flex-1 flex-col after:absolute after:inset-0">
                <h2 className="font-heading text-base font-semibold text-foreground">{space.name}</h2>
                {space.description && <p className="mt-1 line-clamp-2 text-sm leading-5 text-muted-foreground">{space.description}</p>}
              </Link>
              <div className="mt-auto border-t border-border/70 pt-3">
                {space.type ? <Badge variant="outline" className="bg-muted/50 text-muted-foreground">{space.type}</Badge> : <span className="text-xs text-muted-foreground">No type specified</span>}
              </div>
            </article>
          ))}
        </div>
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>Add space</DialogTitle></DialogHeader>
          <SpaceForm submitting={createSpace.isPending} onSubmit={(values: SpaceFormValues) => createSpace.mutate(values, { onSuccess: () => setCreateOpen(false) })} />
        </DialogContent>
      </Dialog>

      <Dialog open={editingSpace !== null} onOpenChange={(open) => !open && setEditingSpace(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>Edit space</DialogTitle></DialogHeader>
          {editingSpace && (
            <SpaceForm
              defaultValues={{ name: editingSpace.name, type: editingSpace.type ?? "", description: editingSpace.description ?? "" }}
              submitting={updateSpace.isPending}
              onSubmit={(values: SpaceFormValues) => updateSpace.mutate(
                { name: values.name, type: values.type ?? "", description: values.description ?? "" },
                { onSuccess: () => setEditingSpace(null) }
              )}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
