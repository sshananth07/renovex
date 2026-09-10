"use client";

import Link from "next/link";
import { ArrowRight, CalendarDays, CircleDollarSign, FileText, Home, LayoutGrid, ListChecks, MapPin, ShoppingCart, Users } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatDate } from "@/lib/formatting/date";
import { useProject } from "../queries";
import { useClient } from "@/features/clients/queries";
import { useProjectProperty } from "@/features/properties/queries";
import { useProjectSpacesTotal } from "@/features/spaces/queries";
import { useProjectWorkItemsTotal } from "@/features/work-items/queries";
import { deriveSetupProgress } from "../setupProgress";
import { projectStatusLabel } from "../statusPresentation";
import { SetupProgress } from "./SetupProgress";
import { ProjectStatusDialog } from "./ProjectStatusDialog";
import { useCostItems, useEstimates, useQuotationShareStatus, useQuotations } from "@/features/operations/queries";
import { useProjectProcurementSummary } from "@/features/procurement/queries";
import { formatMoney } from "@/lib/formatting/money";
import { Sparkles } from "lucide-react";

export function ProjectOverview({ projectId }: { projectId: string }) {
  const { data: project, isLoading: projectLoading } = useProject(projectId);
  const { data: client } = useClient(project?.clientId ?? "");
  const { data: property, isLoading: propertyLoading } = useProjectProperty(projectId);
  const { data: spaces, isLoading: spacesLoading } = useProjectSpacesTotal(projectId);
  const { data: workItems, isLoading: workItemsLoading } = useProjectWorkItemsTotal(projectId);
  const { data: costs } = useCostItems(projectId);
  const { data: estimates } = useEstimates(projectId);
  const { data: quotations } = useQuotations(projectId);
  const { data: procurement } = useProjectProcurementSummary(projectId);
  const latestQuotation = [...(quotations ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0];
  const quotationGrant = useQuotationShareStatus(latestQuotation?.status === "finalized" ? latestQuotation.id : "");
  const resourcesLoading = propertyLoading || spacesLoading || workItemsLoading;

  if (projectLoading || !project) {
    return (
      <div className="flex flex-col gap-5">
        <Skeleton className="h-40 w-full rounded-xl" />
        <div className="grid gap-3 sm:grid-cols-3"><Skeleton className="h-28" /><Skeleton className="h-28" /><Skeleton className="h-28" /></div>
      </div>
    );
  }

  const progress = deriveSetupProgress({
    hasProperty: Boolean(property),
    spacesTotal: spaces?.total ?? 0,
    workItemsTotal: workItems?.total ?? 0,
  });
  const totals = (costs ?? []).reduce((result, item) => ({ estimated: result.estimated + (item.estimated?.amount ?? 0), committed: result.committed + (item.committed?.amount ?? 0), actual: result.actual + (item.actual?.amount ?? 0), paid: result.paid + (item.paid?.amount ?? 0) }), { estimated: 0, committed: 0, actual: 0, paid: 0 });
  const latestEstimate = [...(estimates ?? [])].sort((a, b) => b.version - a.version)[0];
  return (
    <div className="flex flex-col gap-5">
      <section className="surface-card flex flex-col gap-5 p-5 sm:p-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <div className="mb-2 flex flex-wrap items-center gap-3">
              <ProjectStatusDialog projectId={project.id} currentStatus={project.status} />
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <CalendarDays className="size-3.5" /> Created {formatDate(project.createdAt)}
              </span>
            </div>
            <h1 className="font-heading text-2xl font-semibold tracking-[-0.025em] text-foreground">{project.name}</h1>
            <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-sm text-muted-foreground">
              {client && (
                <Link href={`/clients/${client.id}`} className="flex items-center gap-1.5 font-medium text-foreground hover:text-primary">
                  <Users className="size-4 text-primary" /> {client.name}
                </Link>
              )}
              {property?.address && <span className="flex items-center gap-1.5"><MapPin className="size-4 text-primary" />{property.address}</span>}
            </div>
          </div>
          <Link href={`/projects/${projectId}/work-items`} className="inline-flex h-9 items-center justify-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-primary-foreground shadow-sm hover:bg-primary/90">
            Open work items <ArrowRight className="size-4" />
          </Link>
        </div>
      </section>

      {resourcesLoading ? (
        <div className="grid gap-3 sm:grid-cols-3"><Skeleton className="h-28" /><Skeleton className="h-28" /><Skeleton className="h-28" /></div>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            {[
              { label: "Property", value: property ? "Configured" : "Not set", icon: Home, href: `/projects/${projectId}/property` },
              { label: "Spaces", value: String(spaces?.total ?? 0), icon: LayoutGrid, href: `/projects/${projectId}/spaces` },
              { label: "Work items", value: String(workItems?.total ?? 0), icon: ListChecks, href: `/projects/${projectId}/work-items` },
            ].map((metric) => {
              const Icon = metric.icon;
              return (
                <Link key={metric.label} href={metric.href} className="surface-card group flex items-center justify-between p-5 transition-colors hover:border-primary/30">
                  <div><p className="eyebrow">{metric.label}</p><p className="mt-2 text-xl font-semibold tracking-tight">{metric.value}</p></div>
                  <span className="flex size-10 items-center justify-center rounded-xl bg-accent text-accent-foreground transition-colors group-hover:bg-primary group-hover:text-primary-foreground"><Icon className="size-5" /></span>
                </Link>
              );
            })}
          </div>

          <Link
            href={`/projects/${projectId}/ai-setup`}
            className="surface-card group flex items-center justify-between gap-4 p-5 transition-colors hover:border-primary/30"
          >
            <div className="flex items-center gap-3">
              <span className="flex size-10 items-center justify-center rounded-xl bg-accent text-accent-foreground transition-colors group-hover:bg-primary group-hover:text-primary-foreground">
                <Sparkles className="size-5" />
              </span>
              <div>
                <p className="text-sm font-semibold">AI Project Setup</p>
                <p className="text-xs text-muted-foreground">Turn your renovation brief into structured scope.</p>
              </div>
            </div>
            <span className="shrink-0 text-xs font-semibold text-primary">Continue AI Setup →</span>
          </Link>

          <div className="grid gap-4 lg:grid-cols-[0.9fr_1.1fr]">
            <SetupProgress projectId={projectId} progress={progress} />
            <section className="surface-card p-5">
              <h2 className="font-heading text-base font-semibold">Project details</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">Authoritative project foundation data</p>
              <dl className="mt-5 grid gap-5 sm:grid-cols-2">
                <div><dt className="eyebrow">Project status</dt><dd className="mt-1 text-sm font-medium">{projectStatusLabel(project.status)}</dd></div>
                <div><dt className="eyebrow">Created</dt><dd className="mt-1 text-sm">{formatDate(project.createdAt)}</dd></div>
                <div className="sm:col-span-2"><dt className="eyebrow">Property</dt><dd className="mt-1 text-sm">{property?.address ?? "No property configured"}</dd></div>
              </dl>
              <div className="mt-6 flex flex-wrap gap-2 border-t border-border pt-4">
                <Link href={`/projects/${projectId}/property`} className="text-xs font-semibold text-primary hover:underline">Property →</Link>
                <Link href={`/projects/${projectId}/spaces`} className="text-xs font-semibold text-primary hover:underline">Spaces →</Link>
                <Link href={`/projects/${projectId}/work-items`} className="text-xs font-semibold text-primary hover:underline">Work Items →</Link>
              </div>
            </section>
          </div>
          <section className="surface-card overflow-hidden">
            <div className="border-b p-5"><h2 className="font-heading text-base font-semibold">Commercial & procurement</h2><p className="mt-0.5 text-xs text-muted-foreground">Live state from Costs, Estimates, Quotations and RFQs</p></div>
            <div className="grid gap-px bg-border sm:grid-cols-2 xl:grid-cols-4">{Object.entries(totals).map(([label, amount]) => <Link href={`/projects/${projectId}/costs`} key={label} className="bg-card p-5 hover:bg-muted/30"><p className="eyebrow">{label}</p><p className="mt-2 font-mono text-lg font-semibold">{formatMoney(amount)}</p></Link>)}</div>
            <div className="grid gap-px border-t bg-border md:grid-cols-3">
              <Link href={`/projects/${projectId}/estimates`} className="flex items-center gap-3 bg-card p-5 hover:bg-muted/30"><FileText className="size-5 text-primary" /><div><p className="eyebrow">Estimate</p><p className="mt-1 text-sm font-semibold">{latestEstimate ? `Version ${latestEstimate.version} · ${latestEstimate.status}` : "Not created"}</p></div></Link>
              <Link href={`/projects/${projectId}/quotations`} className="flex items-center gap-3 bg-card p-5 hover:bg-muted/30"><CircleDollarSign className="size-5 text-primary" /><div><p className="eyebrow">Quotation</p><p className="mt-1 text-sm font-semibold">{latestQuotation ? `${latestQuotation.quotationNumber} · ${latestQuotation.status}` : "Not created"}</p>{quotationGrant.data?.decision && <p className="mt-1 text-xs capitalize text-muted-foreground">Client: {quotationGrant.data.decision.status.replaceAll("_", " ")}</p>}</div></Link>
              <Link href={`/projects/${projectId}/procurement`} className="flex items-center gap-3 bg-card p-5 hover:bg-muted/30"><ShoppingCart className="size-5 text-primary" /><div><p className="eyebrow">Procurement</p><p className="mt-1 text-sm font-semibold">{procurement?.rfqs ?? 0} RFQs · {procurement?.offers ?? 0} offers · {procurement?.awards ?? 0} awards</p>{Boolean(procurement?.reviewRequired) && <p className="mt-1 text-xs text-amber-700">{procurement?.reviewRequired} offer{procurement?.reviewRequired === 1 ? "" : "s"} require review</p>}</div></Link>
            </div>
          </section>
        </>
      )}
    </div>
  );
}
