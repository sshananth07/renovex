"use client";

import { useCallback, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { setAccessToken } from "@/lib/api/client";
import { ensureFreshToken } from "./singleFlightRefresh";
import { AuthContext, type AuthStatus } from "./useAuth";
import * as authApi from "./api";
import type { MeOutput } from "./api";

// Owns the entire authentication/session lifecycle for the app: the
// in-memory access token, the current-user projection, and the
// initializing/authenticated/unauthenticated status. Per the approved
// session contract, this is the ONLY place /auth/me is read — it is
// intentionally never mirrored into a TanStack Query cache, since it's
// session state, not server-resource state (mixing the two would create
// two independently-invalidated sources of truth for the same identity).
// Must render inside QueryClientProvider (see app/providers.tsx) because
// logout() needs useQueryClient() to clear cached resource data.
export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("initializing");
  const [user, setUser] = useState<MeOutput | null>(null);
  const queryClient = useQueryClient();

  const loadMe = useCallback(async () => {
    const me = await authApi.getMe();
    setUser(me);
    setStatus("authenticated");
  }, []);

  // Session bootstrap on mount: there is no access token yet (page load
  // always starts with an empty in-memory holder), so the only way to know
  // whether the user has a valid session is to attempt a refresh. Staying in
  // "initializing" until this resolves is what stops AuthGate from
  // redirecting to /login before we've had a chance to restore the session.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await ensureFreshToken();
        if (cancelled) return;
        await loadMe();
      } catch {
        if (cancelled) return;
        setStatus("unauthenticated");
      }
    })();
    return () => {
      cancelled = true;
    };
    // Runs once on mount to restore the session; loadMe is stable (useCallback, no deps).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    await authApi.login(email, password);
    await loadMe();
  }, [loadMe]);

  const register = useCallback(
    async (input: { email: string; password: string; companyName: string }) => {
      await authApi.register(input);
      await loadMe();
    },
    [loadMe]
  );

  const logout = useCallback(async () => {
    try {
      await authApi.logout();
    } catch {
      // Best-effort: still clear local state even if the network call fails.
    }
    setAccessToken(null);
    setUser(null);
    setStatus("unauthenticated");
    // Drops all cached Clients/Projects/etc. data so nothing from this
    // session is visible if a different user signs in on the same device.
    queryClient.clear();
  }, [queryClient]);

  const refetchMe = useCallback(async () => {
    await loadMe();
  }, [loadMe]);

  return (
    <AuthContext.Provider value={{ status, user, login, register, logout, refetchMe }}>
      {children}
    </AuthContext.Provider>
  );
}
