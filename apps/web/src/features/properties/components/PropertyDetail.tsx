"use client";

import { useState } from "react";
import { Building2, Home, MapPin, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/layout/PageHeader";
import { PropertyForm } from "./PropertyForm";
import { useProjectProperty } from "../queries";
import { useCreateProperty, useUpdateProperty } from "../mutations";
import type { PropertyFormValues } from "../schemas";

export function PropertyDetail({ projectId }: { projectId: string }) {
  const { data: property, isLoading } = useProjectProperty(projectId);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState(false);
  const createProperty = useCreateProperty(projectId);
  const updateProperty = useUpdateProperty(projectId, property?.id ?? "");

  if (isLoading) {
    return <div className="flex flex-col gap-5"><Skeleton className="h-16 w-72" /><Skeleton className="h-72 w-full rounded-xl" /></div>;
  }

  if (!property && !creating) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader title="Property" description="Project site details and address" />
        <div className="surface-card flex min-h-72 flex-col items-center justify-center gap-3 border-dashed p-8 text-center">
          <span className="flex size-12 items-center justify-center rounded-2xl bg-accent text-accent-foreground"><Home className="size-6" /></span>
          <div><p className="font-heading text-base font-semibold">No property yet</p><p className="mt-1 max-w-md text-sm text-muted-foreground">Add the site being renovated so Spaces and Work Items have a clear project context.</p></div>
          <Button onClick={() => setCreating(true)} className="mt-1">Add property</Button>
        </div>
      </div>
    );
  }

  if (!property && creating) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader title="Add property" description="Record the authoritative project site details" />
        <section className="surface-card max-w-2xl p-5 sm:p-6">
          <PropertyForm submitting={createProperty.isPending} onSubmit={(values: PropertyFormValues) => {
            createProperty.mutate({ projectId, ...values }, { onSuccess: () => setCreating(false) });
          }} />
        </section>
      </div>
    );
  }

  if (property && editing) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader title="Edit property" description="Update the project site details" />
        <section className="surface-card max-w-2xl p-5 sm:p-6">
          <PropertyForm
            defaultValues={{ address: property.address, propertyType: property.propertyType ?? "", notes: property.notes ?? "" }}
            submitting={updateProperty.isPending}
            onSubmit={(values: PropertyFormValues) => updateProperty.mutate(values, { onSuccess: () => setEditing(false) })}
          />
        </section>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Property"
        description="Project site details and address"
        actions={<Button variant="outline" onClick={() => setEditing(true)}><Pencil className="size-4" />Edit property</Button>}
      />
      <div className="grid gap-4 lg:grid-cols-[1.35fr_0.65fr]">
        <section className="surface-card overflow-hidden">
          <div className="flex min-h-40 flex-col items-center justify-center gap-3 bg-muted/60 p-8 text-center">
            <span className="flex size-14 items-center justify-center rounded-2xl bg-card text-primary shadow-sm"><Building2 className="size-7" /></span>
            <p className="font-heading text-lg font-semibold">{property!.propertyType ? `${property!.propertyType} property` : "Project property"}</p>
          </div>
          <div className="p-5 sm:p-6">
            <p className="eyebrow">Full address</p>
            <div className="mt-2 flex items-start gap-2 text-base font-medium"><MapPin className="mt-0.5 size-4 shrink-0 text-primary" /><p>{property!.address}</p></div>
            <div className="mt-5 border-t border-border pt-5"><p className="eyebrow">Property type</p><p className="mt-1.5 text-sm">{property!.propertyType || "Not specified"}</p></div>
          </div>
        </section>
        <section className="surface-card p-5 sm:p-6">
          <h2 className="font-heading text-base font-semibold">Notes</h2>
          <p className="mt-4 text-sm leading-6 whitespace-pre-wrap text-foreground">{property!.notes || "No property notes have been added."}</p>
        </section>
      </div>
    </div>
  );
}
