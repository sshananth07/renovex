import { setAccessToken } from "@/lib/api/accessToken";

let inFlight: Promise<string> | null = null;

async function performRefresh(): Promise<string> {
  const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
  const response = await fetch(`${baseUrl}/auth/refresh`, {
    method: "POST",
    credentials: "include",
  });

  if (!response.ok) {
    setAccessToken(null);
    throw new Error(`refresh failed with status ${response.status}`);
  }

  const body = (await response.json()) as { accessToken: string };
  setAccessToken(body.accessToken);
  return body.accessToken;
}

// Coordinates concurrent 401-triggered refresh attempts (and the initial
// session-restore call) into exactly one POST /auth/refresh in flight at a
// time. Uses a raw fetch, not apiClient, so the refresh call itself can
// never recursively trigger the 401 -> refresh retry path in client.ts.
export function ensureFreshToken(): Promise<string> {
  if (!inFlight) {
    inFlight = performRefresh().finally(() => {
      inFlight = null;
    });
  }
  return inFlight;
}

// Test-only: clears any in-flight promise left over from a previous test
// (e.g. one that deliberately never resolved its mocked /auth/refresh) so
// the next test's ensureFreshToken() call starts a genuinely fresh request
// instead of awaiting a stale one. Never called from application code.
export function resetSingleFlightRefreshForTests(): void {
  inFlight = null;
}
