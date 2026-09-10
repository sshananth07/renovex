import { expect, type Page } from "@playwright/test";
import type { JourneyData } from "./data";

export async function registerContractor(page: Page, data: JourneyData) {
  await page.goto("/register");
  await page.getByLabel("Company name").fill(data.companyName);
  await page.getByLabel("Email").fill(data.email);
  await page.getByLabel("Password").fill(data.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.getByText(data.companyName, { exact: true })).toBeVisible();
}

export async function login(page: Page, data: JourneyData) {
  await page.getByLabel("Email").fill(data.email);
  await page.getByLabel("Password").fill(data.password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

export async function logout(page: Page, data: JourneyData) {
  await page.getByRole("button", { name: data.email.slice(0, 2).toUpperCase() }).click();
  await page.getByRole("menuitem", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/login\?returnTo=/);
}

export async function createClient(page: Page, data: JourneyData, checkValidation = false) {
  if (new URL(page.url()).pathname !== "/clients") {
    await page.getByRole("link", { name: "Clients", exact: true }).click();
  }
  await expect(page.getByRole("heading", { name: "Clients", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Add client" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Add client" })).toBeVisible();
  if (checkValidation) {
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(dialog.getByRole("alert")).toHaveText("Name is required");
  }
  await dialog.getByLabel("Name").fill(data.clientName);
  await dialog.getByLabel("Email").fill(data.clientEmail);
  await dialog.getByLabel("Phone").fill("+60 12-345 6789");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("row").filter({ hasText: data.clientName })).toBeVisible();
}

export async function createProject(page: Page, data: JourneyData) {
  if (new URL(page.url()).pathname !== "/projects") {
    await page.getByRole("link", { name: "Projects", exact: true }).click();
  }
  await expect(page.getByRole("heading", { name: "Projects", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Add project" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Client").click();
  await page.getByRole("option", { name: data.clientName }).click();
  await dialog.getByLabel("Name").fill(data.projectName);
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("row").filter({ hasText: data.projectName })).toBeVisible();
}

export async function openProject(page: Page, data: JourneyData): Promise<string> {
  await page.getByRole("row").filter({ hasText: data.projectName }).click();
  await expect(page).toHaveURL(/\/projects\/[^/?]+$/);
  const projectId = new URL(page.url()).pathname.split("/").at(-1);
  if (!projectId) throw new Error("Project route did not contain an id");
  return projectId;
}

export function projectNavigation(page: Page) {
  return page.getByRole("navigation", { name: "Project workspace" });
}

export async function expectSetupProgress(page: Page, completed: number) {
  const setup = page.getByRole("region", { name: "Project setup" });
  await expect(setup.getByText(`${completed} of 3 complete`)).toBeVisible();
}
