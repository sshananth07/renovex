import { expect, test } from "@playwright/test";
import { uniqueJourneyData } from "./support/data";
import { createClient, createProject, login, logout, openProject, registerContractor } from "./support/flows";

test("restores the real session, honors safe returnTo, and clears it on logout", async ({ page }) => {
  const data = uniqueJourneyData("Session");
  await registerContractor(page, data);
  await page.reload();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.getByText(data.companyName, { exact: true })).toBeVisible();
  await logout(page, data);

  await page.goto("/login?returnTo=https%3A%2F%2Fevil.example%2Fsteal");
  await login(page, data);
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.goto("/clients");
  await page.reload();
  await expect(page.getByRole("heading", { name: "Clients", exact: true })).toBeVisible();
  await logout(page, data);
  await page.goto("/clients");
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fclients$/);
  await login(page, data);
  await expect(page).toHaveURL(/\/clients$/);
});

test("two concurrent business 401s trigger one real refresh and both retries succeed", async ({ page }) => {
  test.setTimeout(90_000);
  const data = uniqueJourneyData("Refresh");
  await registerContractor(page, data);
  await createClient(page, data);
  await createProject(page, data);

  let arrivals = 0;
  let release!: () => void;
  const bothArrived = new Promise<void>((resolve) => { release = resolve; });
  const intercepted = new Set<string>();
  let refreshRequests = 0;
  const successfulRetries = new Set<string>();

  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/auth/refresh") refreshRequests += 1;
  });
  page.on("response", (response) => {
    const path = new URL(response.url()).pathname;
    if ((path === "/properties" || path === "/spaces") && response.status() === 200) {
      successfulRetries.add(path);
    }
  });

  for (const path of ["/properties", "/spaces"]) {
    await page.route(`**${path}**`, async (route) => {
      const requestPath = new URL(route.request().url()).pathname;
      if (requestPath !== path || intercepted.has(path)) {
        await route.continue();
        return;
      }
      intercepted.add(path);
      arrivals += 1;
      if (arrivals === 2) release();
      await bothArrived;
      await route.fulfill({
        status: 401,
        contentType: "application/problem+json",
        headers: {
          "access-control-allow-origin": new URL(page.url()).origin,
          "access-control-allow-credentials": "true",
        },
        body: JSON.stringify({ title: "Unauthorized", status: 401 }),
      });
    });
  }

  await openProject(page, data);
  await expect(page.getByRole("region", { name: "Project setup" })).toBeVisible();
  await expect.poll(() => refreshRequests).toBe(1);
  await expect.poll(() => [...successfulRetries].sort()).toEqual(["/properties", "/spaces"]);
  const browserStorage = await page.evaluate(() => [
    ...Object.entries(localStorage),
    ...Object.entries(sessionStorage),
  ]);
  expect(browserStorage.filter(([key, value]) => /access.?token|bearer/i.test(`${key}:${value}`))).toEqual([]);
});
