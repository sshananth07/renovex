"use client";

import { LogOut, Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAuth } from "@/features/auth/useAuth";

function initials(email: string | undefined): string {
  if (!email) return "?";
  return email.slice(0, 2).toUpperCase();
}

// Compact top bar (architecture doc §12): company context, menu trigger,
// and the account menu (including logout — the only sign-out affordance in
// the app) on every breakpoint. The menu button is only visually present
// below lg: (CSS-hidden, not conditionally rendered) so it stays in the
// DOM/tab order consistently rather than mounting/unmounting on resize.
// user comes from AuthProvider directly (useAuth()), never a TanStack
// Query hook — see the auth-session contract's current-user ownership rule.
export function TopBar({ onOpenMenu }: { onOpenMenu: () => void }) {
  const { user, logout } = useAuth();

  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center border-b border-border/80 bg-card/95 px-4 backdrop-blur sm:px-6 lg:px-7 xl:px-8">
      <div className="mx-auto flex w-full max-w-[1280px] items-center gap-3">
        <Button
          variant="ghost"
          size="icon-sm"
          className="lg:hidden"
          aria-label="Open menu"
          onClick={onOpenMenu}
        >
          <Menu className="size-5" />
        </Button>
        <div className="min-w-0">
          <span className="block truncate text-sm font-semibold text-foreground">{user?.companyName}</span>
          <span className="hidden text-[0.6875rem] text-muted-foreground sm:block">Contractor workspace</span>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                type="button"
                aria-label={initials(user?.email)}
                className="ml-auto flex size-8 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground shadow-sm transition-colors hover:bg-primary/90"
              >
                {initials(user?.email)}
              </button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium text-foreground">
                    {user?.email}
                  </span>
                  <span className="text-xs text-muted-foreground capitalize">
                    {user?.role}
                  </span>
                </div>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => logout()} variant="destructive">
              <LogOut className="size-4" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
