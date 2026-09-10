"use client";

import Link from "next/link";
import { Check, Home, LayoutGrid, ListChecks } from "lucide-react";
import { cn } from "@/lib/utils";
import type { SetupProgress as SetupProgressData } from "../setupProgress";

interface Step {
  key: "property" | "spaces" | "workItems";
  label: string;
  description: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
}

function buildSteps(projectId: string): Step[] {
  const base = `/projects/${projectId}`;
  return [
    { key: "property", label: "Property", description: "Where the work happens", href: `${base}/property`, icon: Home },
    { key: "spaces", label: "Spaces", description: "Rooms and areas to renovate", href: `${base}/spaces`, icon: LayoutGrid },
    { key: "workItems", label: "Work Items", description: "Scope of work per space", href: `${base}/work-items`, icon: ListChecks },
  ];
}

export function SetupProgress({ projectId, progress }: { projectId: string; progress: SetupProgressData }) {
  const steps = buildSteps(projectId);
  const completedCount = steps.filter((step) => progress[step.key] === "complete").length;
  const fillPercent = (completedCount / steps.length) * 100;

  return (
    <section aria-label="Project setup" className="surface-card flex flex-col gap-4 p-5">
      <div className="flex items-baseline justify-between gap-4">
        <div>
          <h2 className="font-heading text-base font-semibold text-foreground">Project setup</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">Complete the operational project foundation</p>
        </div>
        <span className="shrink-0 text-xs font-medium text-muted-foreground">
          {completedCount} of {steps.length} complete
        </span>
      </div>

      <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
        <div className="h-full rounded-full bg-primary transition-[width] duration-300 ease-out" style={{ width: `${fillPercent}%` }} />
      </div>

      <ol className="flex flex-col divide-y divide-border/70">
        {steps.map((step) => {
          const isComplete = progress[step.key] === "complete";
          const Icon = step.icon;
          return (
            <li key={step.key}>
              <Link href={step.href} aria-label={step.label} className="group flex items-center gap-3 py-3">
                <span
                  className={cn(
                    "flex size-8 shrink-0 items-center justify-center rounded-full border transition-colors",
                    isComplete
                      ? "border-primary/20 bg-primary/10 text-primary"
                      : "border-border bg-background text-muted-foreground group-hover:border-primary/40 group-hover:text-primary"
                  )}
                >
                  {isComplete ? <Check className="size-4" /> : <Icon className="size-4" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-foreground">{step.label}</span>
                  <span className="block truncate text-xs text-muted-foreground">{step.description}</span>
                </span>
                <span className={cn("shrink-0 text-xs font-medium", isComplete ? "text-primary" : "text-muted-foreground")}>
                  {isComplete ? "Complete" : "Set up →"}
                </span>
              </Link>
            </li>
          );
        })}
      </ol>
    </section>
  );
}
