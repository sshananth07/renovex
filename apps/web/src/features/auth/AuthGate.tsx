"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "./useAuth";
import { parseSafeReturnTo } from "./returnTo";

// Client-side route boundary that protects the (app) route group. Renders
// nothing behind a loading state until AuthProvider's initial session-
// restore settles, then either renders children (authenticated) or
// redirects to /login (unauthenticated) — never both, and never redirects
// while status is still "initializing" (that would send an about-to-be-
// authenticated user to /login for a flash before the refresh resolves).
export function AuthGate({ children }: { children: React.ReactNode }) {
  const { status } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (status === "unauthenticated") {
      // The current path becomes returnTo so login can send the user back
      // to where they were, but only after passing it through
      // parseSafeReturnTo — window.location.pathname is always same-origin
      // so this is defense in depth, not a response to an untrusted input.
      const returnTo = parseSafeReturnTo(
        typeof window !== "undefined" ? window.location.pathname : null
      );
      // replace, not push: a session-loss redirect shouldn't leave the
      // gated route as a back-button target, which would just bounce the
      // user straight back here.
      router.replace(`/login?returnTo=${encodeURIComponent(returnTo)}`);
    }
  }, [status, router]);

  if (status === "initializing") {
    return <div>Loading…</div>;
  }

  if (status === "unauthenticated") {
    return null;
  }

  return <>{children}</>;
}
