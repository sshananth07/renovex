import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000";
const webPort = new URL(baseURL).port || "3000";

/**
 * See https://playwright.dev/docs/test-configuration.
 *
 * These composed journeys require the Go backend running locally
 * (AI_PROVIDER=mock) with APP_ALLOWED_ORIGINS including this baseURL.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: "html",
  use: {
    baseURL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /.*\.desktop\.spec\.ts/,
    },
    {
      name: "mobile-chromium",
      use: { ...devices["Pixel 7"] },
      testMatch: /.*\.mobile\.spec\.ts/,
    },
    {
      // RP4E3 visual-acceptance gate: network-mocked (page.route), so it
      // needs no real Go backend — every test sets its own desktop/laptop
      // viewport explicitly (test.use per describe block), unlike the
      // device-preset-driven projects above.
      name: "spatial-studio",
      use: { ...devices["Desktop Chrome"] },
      testMatch: /spatial-ai-studio\.spec\.ts/,
      // A small tolerance for WebGL/Three.js anti-aliasing jitter on
      // wireframe edges between runs (confirmed via diff inspection: only
      // outline/grid-line pixels shift by ~1px, never panel text or
      // structural content) — never used to mask the selected object,
      // concept toggle, or AI panel, which every test asserts on
      // structurally (role/text queries) independent of the screenshot.
      expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.02 } },
    },
  ],

  webServer: {
    command: `npm run dev -- --port ${webPort}`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
  },
});
