"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

interface Tab {
  label: string;
  href: string;
}

function buildTabs(projectId: string): Tab[] {
  const base = `/projects/${projectId}`;
  return [
    { label: "Overview", href: base },
    { label: "Property", href: `${base}/property` },
    { label: "Spaces", href: `${base}/spaces` },
    { label: "Work Items", href: `${base}/work-items` },
    { label: "Resources", href: `${base}/resources` },
    { label: "Costs", href: `${base}/costs` },
    { label: "Estimates", href: `${base}/estimates` },
    { label: "Quotations", href: `${base}/quotations` },
    { label: "Procurement", href: `${base}/procurement` },
  ];
}

// Route-based workspace tabs (architecture doc §11): visually matches
// shadcn Tabs' "line" variant (underline-style active indicator) but is
// built from real <Link>s, not the Tabs primitive, since each tab is its
// own page/route rather than a client-side panel switch. On mobile this
// scrolls horizontally rather than wrapping (architecture doc §12).
export function ProjectTabs({ projectId }: { projectId: string }) {
  const pathname = usePathname();
  const tabs = buildTabs(projectId);

  return (
    <nav
      aria-label="Project workspace"
      className="flex gap-1 overflow-x-auto rounded-xl border border-border bg-card p-1 shadow-[0_1px_2px_oklch(0.2_0.02_45/3%)]"
    >
      {tabs.map((tab) => {
        const isActive =
          tab.href === `/projects/${projectId}`
            ? pathname === tab.href
            : pathname?.startsWith(tab.href);
        return (
          <Link
            key={tab.href}
            href={tab.href}
            data-active={isActive || undefined}
            className={cn(
              "relative shrink-0 rounded-lg px-3 py-2 text-xs font-semibold whitespace-nowrap transition-colors duration-150 sm:text-sm",
              isActive
                ? "bg-accent text-accent-foreground shadow-sm"
                : "text-muted-foreground hover:bg-muted/70 hover:text-foreground"
            )}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
