"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowLeft, ChevronRight, Mail, MapPin, Pencil, Phone, Plus, StickyNote, UserRound } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { ClientForm } from "./ClientForm";
import { useClient } from "../queries";
import { useUpdateClient } from "../mutations";
import type { ClientFormValues } from "../schemas";
import type { UpdateClientInput } from "../api";
import { useClientProjects } from "@/features/projects/queries";
import { useCreateProject } from "@/features/projects/mutations";
import { ProjectForm } from "@/features/projects/components/ProjectForm";
import { StatusBadge } from "@/features/projects/components/StatusBadge";
import type { Project } from "@/features/projects/api";
import { useQuotations, useQuotationShareStatus } from "@/features/operations/queries";

const PATCHABLE_FIELDS = [
  "name",
  "email",
  "phone",
  "address",
  "billingAddress",
  "notes",
] as const;

function buildPatch(
  original: Record<string, string | undefined>,
  submitted: ClientFormValues
): UpdateClientInput {
  const patch: UpdateClientInput = {};
  for (const field of PATCHABLE_FIELDS) {
    const before = original[field] ?? "";
    const after = submitted[field] ?? "";
    if (before !== after) patch[field] = after;
  }
  return patch;
}

interface BackendProblem {
  title?: string;
  detail?: string;
  errors?: Array<{ message?: string }>;
}

function errorMessage(error: unknown, fallback: string) {
  const problem = error as BackendProblem | null;
  return problem?.detail
    ?? problem?.errors?.find((item) => item.message)?.message
    ?? problem?.title
    ?? fallback;
}

function initials(name: string) {
  return name.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join("").toUpperCase();
}

export function ClientDetail({ clientId }: { clientId: string }) {
  const [editOpen, setEditOpen] = useState(false);
  const [projectOpen, setProjectOpen] = useState(false);
  const { data: client, isLoading, error } = useClient(clientId);
  const updateClient = useUpdateClient(clientId);
  const projects = useClientProjects(clientId);
  const createProject = useCreateProject();

  if (isLoading) {
    return <div className="grid gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]"><Skeleton className="h-96" /><Skeleton className="h-72" /></div>;
  }
  if (error || !client) {
    return <div className="surface-card p-10 text-center"><h1 className="font-heading text-xl font-semibold">Client unavailable</h1><p className="mt-2 text-sm text-muted-foreground">This Client account could not be loaded.</p></div>;
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <Link href="/clients" className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg px-2 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground">
            <ArrowLeft className="size-4" /> Back
          </Link>
          <h1 className="truncate font-heading text-2xl font-semibold tracking-[-0.025em]">{client.name}</h1>
        </div>
        <Button
          className="w-full sm:w-auto"
          onClick={() => {
            updateClient.reset();
            setEditOpen(true);
          }}
        >
          <Pencil className="size-4" /> Edit Client
        </Button>
      </header>

      <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)] lg:items-start">
        <section className="surface-card min-w-0 p-5">
          <div className="flex items-center gap-3 border-b pb-4">
            <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary text-sm font-semibold text-primary-foreground">
              {initials(client.name)}
            </span>
            <div className="min-w-0">
              <p className="eyebrow">Client</p>
              <h2 className="mt-1 truncate font-heading text-lg font-semibold">{client.name}</h2>
            </div>
          </div>
          <dl className="mt-4 grid gap-4 text-sm">
            <SummaryField icon={Mail} label="Email" value={client.email || "Not provided"} />
            <SummaryField icon={Phone} label="Phone" value={client.phone || "Not provided"} />
            <SummaryField icon={MapPin} label="Address" value={client.address || "Not provided"} />
            <SummaryField icon={StickyNote} label="Notes" value={client.notes || "No notes provided"} multiline />
          </dl>
        </section>

        <section className="surface-card min-w-0 overflow-hidden">
          <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="font-heading text-base font-semibold">Projects ({projects.data?.total ?? 0})</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">Project and customer-quotation context for this Client.</p>
            </div>
            <Button
              className="w-full sm:w-auto"
              onClick={() => {
                createProject.reset();
                setProjectOpen(true);
              }}
            >
              <Plus className="size-4" /> New Project
            </Button>
          </div>
          <div className="grid gap-2 p-3 sm:p-4">
            {projects.isLoading && <><Skeleton className="h-20" /><Skeleton className="h-20" /></>}
            {projects.error && <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive">Associated Projects could not be loaded.</p>}
            {!projects.isLoading && !projects.error && (projects.data?.items ?? []).map((project) => <ClientProjectRow project={project} key={project.id} />)}
            {!projects.isLoading && !projects.error && (projects.data?.items ?? []).length === 0 && (
              <div className="rounded-xl border border-dashed p-8 text-center"><p className="font-medium">No projects yet</p><p className="mt-1 text-sm text-muted-foreground">Create the first renovation Project for this Client.</p></div>
            )}
          </div>
        </section>
      </div>

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit Client</DialogTitle>
            <DialogDescription>Update contact, billing, and project notes.</DialogDescription>
          </DialogHeader>
          {updateClient.error && <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{errorMessage(updateClient.error, "The Client could not be updated.")}</p>}
          <ClientForm
            defaultValues={{
              name: client.name,
              email: client.email ?? "",
              phone: client.phone ?? "",
              address: client.address ?? "",
              billingAddress: client.billingAddress ?? "",
              notes: client.notes ?? "",
            }}
            submitting={updateClient.isPending}
            onCancel={() => setEditOpen(false)}
            onSubmit={(values) => updateClient.mutate(buildPatch(client, values), { onSuccess: () => setEditOpen(false) })}
          />
        </DialogContent>
      </Dialog>

      <Dialog open={projectOpen} onOpenChange={setProjectOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New Project</DialogTitle>
            <DialogDescription>Create a Project for {client.name}.</DialogDescription>
          </DialogHeader>
          {createProject.error && <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{errorMessage(createProject.error, "The Project could not be created.")}</p>}
          <ProjectForm
            clients={[client]}
            defaultClientId={client.id}
            submitting={createProject.isPending}
            onSubmit={(values) => createProject.mutate(values, { onSuccess: () => setProjectOpen(false) })}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}

function SummaryField({ icon: Icon, label, value, multiline = false }: { icon: typeof UserRound; label: string; value: string; multiline?: boolean }) {
  return (
    <div className="flex min-w-0 items-start gap-3">
      <Icon className="mt-0.5 size-4 shrink-0 text-primary" />
      <div className="min-w-0">
        <dt className="eyebrow">{label}</dt>
        <dd className={`mt-1 text-sm ${multiline ? "whitespace-pre-wrap" : "break-words"}`}>{value}</dd>
      </div>
    </div>
  );
}

function ClientProjectRow({ project }: { project: Project }) {
  const quotations = useQuotations(project.id);
  const latest = [...(quotations.data ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0];
  const grant = useQuotationShareStatus(latest?.status === "finalized" ? latest.id : "");
  const response = grant.data?.decision?.status?.replaceAll("_", " ")
    ?? (grant.data ? grant.data.effectiveStatus.replaceAll("_", " ") : latest ? "Not shared" : "Awaiting quotation");

  return (
    <article className="grid min-w-0 gap-3 rounded-xl border border-border/80 bg-card p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="min-w-0 break-words font-heading text-sm font-semibold sm:truncate">{project.name}</h3>
          <StatusBadge status={project.status} />
        </div>
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            Quotation: {latest ? <Link className="font-medium text-primary hover:underline" href={`/projects/${project.id}/quotations`}>{latest.quotationNumber} · {latest.status.replaceAll("_", " ")}</Link> : "Not created"}
          </span>
          <span className="capitalize">Client response: {response}</span>
        </div>
      </div>
      <Link href={`/projects/${project.id}`} className="inline-flex h-8 items-center justify-center gap-1 rounded-lg px-2 text-sm font-semibold text-primary hover:bg-accent sm:justify-self-end">
        Open Project <ChevronRight className="size-4" />
      </Link>
    </article>
  );
}
