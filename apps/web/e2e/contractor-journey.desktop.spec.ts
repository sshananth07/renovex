import { expect, test } from "@playwright/test";
import { uniqueJourneyData } from "./support/data";
import {
  createClient,
  createProject,
  expectSetupProgress,
  logout,
  openProject,
  projectNavigation,
  registerContractor,
} from "./support/flows";

test("contractor composes Client through cancelled Work Item with persisted 0/3 to 3/3 setup", async ({ page }) => {
  test.setTimeout(120_000);
  const data = uniqueJourneyData("Desktop");

  await registerContractor(page, data);
  await createClient(page, data, true);
  await page.reload();
  await expect(page.getByRole("row").filter({ hasText: data.clientName })).toBeVisible();

  await createProject(page, data);
  const projectId = await openProject(page, data);
  await expectSetupProgress(page, 0);
  await page.reload();
  await expectSetupProgress(page, 0);

  const tabs = projectNavigation(page);
  await tabs.getByRole("link", { name: "Property", exact: true }).click();
  await page.getByRole("button", { name: "Add property" }).click();
  await page.getByLabel("Address").fill(data.propertyAddress);
  await page.getByLabel("Property type").fill("Landed");
  await page.getByLabel("Notes").fill("F1.10 persisted property");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(data.propertyAddress)).toBeVisible();
  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}$`));
  await page.reload();
  await expectSetupProgress(page, 1);

  await tabs.getByRole("link", { name: "Spaces", exact: true }).click();
  await page.getByRole("button", { name: "Add space" }).click();
  const spaceDialog = page.getByRole("dialog");
  await spaceDialog.getByLabel("Name").fill(data.spaceName);
  await spaceDialog.getByLabel("Type").fill("Kitchen");
  await spaceDialog.getByLabel("Description").fill("F1.10 operational space");
  await spaceDialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("heading", { name: data.spaceName })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading", { name: data.spaceName })).toBeVisible();
  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}$`));
  await page.reload();
  await expectSetupProgress(page, 2);

  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  await page.getByRole("button", { name: "Add work item" }).click();
  const createDrawer = page.getByRole("dialog");
  await createDrawer.getByLabel("Description").fill(data.workItemDescription);
  await createDrawer.getByLabel("Quantity").fill(data.quantity);
  await createDrawer.getByLabel("Unit").fill(data.unit);
  await createDrawer.getByLabel("Work type").fill("Carpentry");
  await createDrawer.getByLabel("Space").click();
  await page.getByRole("option", { name: data.spaceName }).click();
  await createDrawer.getByRole("button", { name: "Save" }).click();

  let row = page.getByRole("row").filter({ hasText: data.workItemDescription });
  await expect(row).toContainText(`${data.quantity} ${data.unit}`);
  await expect(row).toContainText(data.spaceName);
  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}$`));
  await page.reload();
  await expectSetupProgress(page, 3);

  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  row = page.getByRole("row").filter({ hasText: data.workItemDescription });
  await row.click();
  let editDrawer = page.getByRole("dialog");
  await editDrawer.getByLabel("Space").click();
  await page.getByRole("option", { name: "No space assigned" }).click();
  await editDrawer.getByRole("button", { name: "Save" }).click();
  await expect(editDrawer).toBeHidden();

  row = page.getByRole("row").filter({ hasText: data.workItemDescription });
  await row.click();
  editDrawer = page.getByRole("dialog");
  await editDrawer.getByLabel("Description").fill(data.editedWorkItemDescription);
  await editDrawer.getByLabel("Space").click();
  await page.getByRole("option", { name: data.spaceName }).click();
  await editDrawer.getByRole("button", { name: "Save" }).click();
  row = page.getByRole("row").filter({ hasText: data.editedWorkItemDescription });
  await expect(row).toContainText(data.spaceName);
  await expect(row).toContainText(`${data.quantity} ${data.unit}`);

  await row.getByRole("button", { name: "Cancel" }).click();
  const cancelDialog = page.getByRole("dialog");
  await expect(cancelDialog).toContainText("can no longer be edited");
  await cancelDialog.getByRole("button", { name: "Confirm" }).click();
  row = page.getByRole("row").filter({ hasText: data.editedWorkItemDescription });
  await expect(row).toContainText("Cancelled");

  await row.click();
  editDrawer = page.getByRole("dialog");
  await editDrawer.getByLabel("Description").fill("Forbidden cancelled edit");
  await editDrawer.getByRole("button", { name: "Save" }).click();
  await expect(editDrawer.getByRole("alert")).toContainText(/cancelled/i);
  await page.keyboard.press("Escape");
  await page.reload();
  row = page.getByRole("row").filter({ hasText: data.editedWorkItemDescription });
  await expect(row).toContainText("Cancelled");
  await expect(row).toContainText(`${data.quantity} ${data.unit}`);

  await tabs.getByRole("link", { name: "Spaces", exact: true }).click();
  await expect(page.getByRole("heading", { name: data.spaceName })).toBeVisible();
  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/work-items`));
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/spaces`));
  await page.goForward();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/work-items`));

  const protectedUrl = page.url();
  await logout(page, data);
  await page.goto(protectedUrl);
  await expect(page).toHaveURL(new RegExp(`/login\\?returnTo=${encodeURIComponent(`/projects/${projectId}/work-items`)}`));
});
