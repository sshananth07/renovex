import { expect, test } from "@playwright/test";
import { uniqueJourneyData } from "./support/data";
import { createClient, createProject, openProject, projectNavigation, registerContractor } from "./support/flows";

test("mobile shell composes navigation, tabs, dialog, drawer, and contained Work Item table", async ({ page }) => {
  test.setTimeout(120_000);
  const data = uniqueJourneyData("Mobile");
  await registerContractor(page, data);

  await page.getByRole("button", { name: "Open menu" }).click();
  const mobileNav = page.getByRole("dialog");
  await expect(mobileNav.getByRole("link", { name: "Clients", exact: true })).toBeVisible();
  await mobileNav.getByRole("link", { name: "Clients", exact: true }).click();
  await expect(page).toHaveURL(/\/clients$/);
  await createClient(page, data, true);

  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("dialog").getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page).toHaveURL(/\/projects$/);
  await createProject(page, data);
  await openProject(page, data);
  const tabs = projectNavigation(page);
  await expect(tabs.getByRole("link", { name: "Spaces", exact: true })).toBeVisible();

  await tabs.getByRole("link", { name: "Spaces", exact: true }).click();
  await page.getByRole("button", { name: "Add space" }).click();
  const spaceDialog = page.getByRole("dialog");
  await spaceDialog.getByRole("button", { name: "Save" }).click();
  await expect(spaceDialog.getByRole("alert")).toHaveText("Name is required");
  await spaceDialog.getByLabel("Name").fill(data.spaceName);
  await spaceDialog.getByLabel("Type").fill("Kitchen");
  const dialogBox = await spaceDialog.boundingBox();
  expect(dialogBox).not.toBeNull();
  expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(412);
  await spaceDialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("heading", { name: data.spaceName })).toBeVisible();

  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  await page.getByRole("button", { name: "Add work item" }).click();
  const drawer = page.getByRole("dialog");
  await expect(drawer.getByRole("heading", { name: "Add work item" })).toBeVisible();
  await expect(drawer.getByLabel("Space")).toHaveText(/No space assigned/);
  await expect.poll(async () => Math.round((await drawer.boundingBox())?.x ?? -1)).toBe(0);
  const drawerBox = await drawer.boundingBox();
  expect(drawerBox).not.toBeNull();
  expect(drawerBox!.x).toBeGreaterThanOrEqual(0);
  expect(drawerBox!.x + drawerBox!.width).toBeLessThanOrEqual(412);
  await drawer.getByLabel("Description").fill(data.workItemDescription);
  await drawer.getByLabel("Quantity").fill(data.quantity);
  await drawer.getByLabel("Unit").fill(data.unit);
  await drawer.getByLabel("Space").click();
  await page.getByRole("option", { name: data.spaceName }).click();
  await drawer.getByRole("button", { name: "Save" }).click();

  await expect(page.getByRole("row").filter({ hasText: data.workItemDescription })).toBeVisible();
  await expect(page.getByText("Swipe table to view all columns →")).toBeVisible();
  const tableMetrics = await page.locator('[data-slot="table-container"]').evaluate((element) => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }));
  expect(tableMetrics.scrollWidth).toBeGreaterThan(tableMetrics.clientWidth);
  const documentMetrics = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(documentMetrics.scrollWidth).toBeLessThanOrEqual(documentMetrics.clientWidth + 1);
  await expect(page.getByRole("button", { name: "Add work item" })).toBeVisible();
});
