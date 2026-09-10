"use client";

import Link from "next/link";
import { ArrowLeft, LayoutGrid } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useSpace } from "../queries";
import { SpatialEntry } from "@/features/spatial/components/SpatialEntry";

export function SpaceDetail({ projectId, spaceId }: { projectId: string; spaceId: string }) {
  const { data: space, isLoading, error } = useSpace(spaceId);

  if (isLoading) {
    return <div className="grid gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]"><Skeleton className="h-40" /><Skeleton className="h-72" /></div>;
  }
  if (error || !space) {
    return <div className="surface-card p-10 text-center"><h1 className="font-heading text-xl font-semibold">Space unavailable</h1><p className="mt-2 text-sm text-muted-foreground">This Space could not be loaded.</p></div>;
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <Link
            href={`/projects/${projectId}/spaces`}
            className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg px-2 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <ArrowLeft className="size-4" /> Back
          </Link>
          <h1 className="truncate font-heading text-2xl font-semibold tracking-[-0.025em]">{space.name}</h1>
        </div>
      </header>

      <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)] lg:items-start">
        <section className="surface-card min-w-0 p-5">
          <div className="flex items-center gap-3 border-b pb-4">
            <span className="flex size-10 items-center justify-center rounded-xl bg-accent text-accent-foreground"><LayoutGrid className="size-5" /></span>
            <div className="min-w-0">
              <p className="truncate font-heading text-sm font-semibold">{space.name}</p>
              {space.type ? <Badge variant="outline" className="mt-1 bg-muted/50 text-muted-foreground">{space.type}</Badge> : <p className="text-xs text-muted-foreground">No type specified</p>}
            </div>
          </div>
          {space.description && <p className="mt-4 text-sm leading-6 text-muted-foreground">{space.description}</p>}
        </section>

        <SpatialEntry projectId={projectId} spaceId={spaceId} />
      </div>
    </div>
  );
}
