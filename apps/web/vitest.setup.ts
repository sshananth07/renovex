import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll } from "vitest";
import { cleanup } from "@testing-library/react";
import { server } from "./src/test/server";
import { resetSingleFlightRefreshForTests } from "./src/features/auth/singleFlightRefresh";

// jsdom does not implement ResizeObserver. Components that measure their own
// layout (e.g. FloorPlanViewport's fit-to-room framing) need SOME
// implementation present or they throw on mount; a no-op is sufficient since
// jsdom also doesn't produce real layout/resize events for it to report.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver;

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  cleanup();
  resetSingleFlightRefreshForTests();
});
afterAll(() => server.close());
