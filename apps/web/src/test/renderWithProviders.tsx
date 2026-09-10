import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderOptions } from "@testing-library/react";
import type { ReactElement } from "react";
import { createQueryClient } from "@/lib/query/queryClient";
import { AuthProvider } from "@/features/auth/AuthProvider";

interface RenderWithProvidersOptions extends Omit<RenderOptions, "wrapper"> {
  queryClient?: QueryClient;
}

// The same nesting order as the real Providers component (§5a):
// QueryClientProvider outside AuthProvider, so AuthProvider's useQueryClient()
// call (used on logout) always has a client to read. Every caller gets its
// own isolated QueryClient by default (no shared singleton), so cached query
// data never leaks between tests.
export function renderWithProviders(
  ui: ReactElement,
  { queryClient = createQueryClient(), ...options }: RenderWithProvidersOptions = {}
) {
  function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthProvider>{children}</AuthProvider>
      </QueryClientProvider>
    );
  }

  return {
    queryClient,
    ...render(ui, { wrapper: Wrapper, ...options }),
  };
}
