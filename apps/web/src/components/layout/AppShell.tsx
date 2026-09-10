"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { HardHat } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { TopBar } from "./TopBar";
import { navItems } from "./navItems";
import { useAuth } from "@/features/auth/useAuth";

// Shared between the desktop sidebar and the mobile drawer so both surfaces
// render the exact same nav vocabulary (architecture doc §18: consistent
// affordances across the app). onNavigate closes the mobile Sheet after a
// link is clicked — the desktop sidebar passes nothing, since it's always
// visible and never needs to "close".
function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  return (
    <nav aria-label="Main" className="flex flex-col gap-0.5">
      {navItems.map((item) => {
        const active = pathname?.startsWith(item.href);
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            data-active={active || undefined}
            className={cn(
              "relative flex h-10 items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors duration-150",
              active
                ? "bg-sidebar-accent text-sidebar-accent-foreground before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-sidebar-primary"
                : "text-sidebar-foreground/70 hover:bg-white/5 hover:text-sidebar-foreground"
            )}
          >
            <Icon className="size-4" />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}

function Brand() {
  return (
    <div className="flex h-11 items-center gap-3 px-1">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-sidebar-primary text-sidebar-primary-foreground shadow-sm">
        <HardHat className="size-5" />
      </span>
      <span className="flex flex-col">
        <span className="font-heading text-sm font-semibold text-sidebar-foreground">Renovex</span>
        <span className="text-[0.6875rem] text-sidebar-foreground/45">Contractor Platform</span>
      </span>
    </div>
  );
}

function AccountSummary() {
  const { user } = useAuth();
  const initials = user?.email?.slice(0, 2).toUpperCase() ?? "?";
  return (
    <div className="mt-auto border-t border-sidebar-border pt-4">
      <div className="flex items-center gap-3 px-1">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-sidebar-primary text-xs font-semibold text-sidebar-primary-foreground">
          {initials}
        </span>
        <span className="min-w-0">
          <span className="block truncate text-xs font-medium text-sidebar-foreground">
            {user?.role ? `${user.role.charAt(0).toUpperCase()}${user.role.slice(1)} account` : "Account"}
          </span>
          <span className="block text-[0.6875rem] capitalize text-sidebar-foreground/45">{user?.role}</span>
        </span>
      </div>
    </div>
  );
}

// The authenticated Contractor app's structural frame (architecture doc §12).
// Desktop (lg: and above): a persistent sidebar renders alongside the
// content, so the <aside> is just always in the layout ("hidden ... lg:flex"
// — never a JS-driven toggle). Mobile/tablet: the sidebar is removed from
// the layout entirely and its content is duplicated into a Sheet-based
// drawer, opened from the TopBar's menu button. This is a structural
// (CSS breakpoint) split, not a fluid one, per the product register.
//
// Page content is capped at a max-width and centered (matching the density
// a real operational tool needs without sprawling full-bleed on wide
// desktop monitors) — every route renders inside this shell so the
// constraint lives here once, not per-page.
//
// The Spatial Studio route is a deliberate, narrow exception: it is a
// design-workspace/editor (closer to CAD/Figma-style tooling than a
// dashboard page), where maximizing canvas space is the point, not a
// stylistic preference. Detected by pathname (this component already calls
// usePathname for nav-active-state) rather than threading a prop through
// every layout in the (app) route group — every other route keeps the
// standard constrained width unconditionally.
const FULL_WIDTH_ROUTE_PATTERN = /\/spatial\/[^/]+$/;

export function AppShell({ children }: { children: React.ReactNode }) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const pathname = usePathname();
  const isFullWidthRoute = FULL_WIDTH_ROUTE_PATTERN.test(pathname ?? "");

  return (
    <div className="flex min-h-screen bg-background">
      <aside className="sticky top-0 hidden h-screen w-58 shrink-0 flex-col gap-5 self-start border-r border-sidebar-border bg-sidebar p-4 lg:flex">
        <Brand />
        <div className="mt-2 flex flex-col gap-2">
          <p className="px-3 text-[0.625rem] font-semibold tracking-[0.13em] text-sidebar-foreground/35 uppercase">Workspace</p>
          <NavLinks />
        </div>
        <AccountSummary />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar onOpenMenu={() => setMobileNavOpen(true)} />
        <main className="flex-1">
          <div className={cn("mx-auto w-full px-4 py-5 sm:px-6 sm:py-6 lg:px-7 xl:px-8", isFullWidthRoute ? "max-w-none" : "max-w-[1280px]")}>
            {children}
          </div>
        </main>
      </div>

      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen}>
        <SheetContent side="left" className="w-72 border-sidebar-border bg-sidebar text-sidebar-foreground">
          <SheetHeader>
            <SheetTitle className="sr-only">Menu</SheetTitle>
          </SheetHeader>
          <div className="px-4"><Brand /></div>
          <div className="px-4">
            <NavLinks onNavigate={() => setMobileNavOpen(false)} />
          </div>
          <div className="px-4 pb-4"><AccountSummary /></div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
