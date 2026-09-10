import type { HttpHandler } from "msw";

// Per-feature default MSW handlers are added incrementally starting F1.1.
// This array is the single source `server.ts` composes into `setupServer`.
export const handlers: HttpHandler[] = [];
