import { expect, test } from "@playwright/test";
import { uniqueJourneyData } from "./support/data";
import {
  createClient,
  createProject,
  openProject,
  projectNavigation,
  registerContractor,
} from "./support/flows";

// Deterministic E2E against the REAL Python FastAPI service with
// AI_PROVIDER=mock (design doc plan Task 17). Requires locally:
//   ai-service on :8090 (AI_PROVIDER=mock, INTERNAL_API_TOKEN=<test token>)
//   Go API with AI_SERVICE_URL=http://localhost:8090, AI_INTERNAL_TOKEN=<same token>
// The mock provider's suggest_resources always proposes, for the first
// Work Item it's given: material "Tile Adhesive" (candidateMaterialId = the
// first material candidate Go supplies, alphabetically first by name),
// material "Tile Spacers" (no candidate), trade "Tiler", equipment "Tile
// Cutter" (ai-service/app/providers/mock.py).
test("AI Scope & Resource preview: brief through Space/WorkItem/Resource acceptance with no financial mutation", async ({ page }) => {
  test.setTimeout(180_000);
  // Accept/reject actions round-trip through the real Go API and Mongo;
  // the default 5s expect timeout is occasionally too tight under load.
  const acceptTimeout = { timeout: 15_000 };
  const data = uniqueJourneyData("AI");

  await registerContractor(page, data);
  await createClient(page, data);
  await createProject(page, data);
  const projectId = await openProject(page, data);

  const tabs = projectNavigation(page);

  // A Material catalog entry so the resource-suggestion step can exercise
  // "Use Existing" against a real, deterministic candidate — its name
  // ("Adhesive Compound") sorts alphabetically before any other Material in
  // this fresh company, so it is guaranteed to be materialCandidates[0].
  await tabs.getByRole("link", { name: "Resources", exact: true }).click();
  await page.getByRole("button", { name: "Add material" }).click();
  const materialDialog = page.getByRole("dialog");
  await materialDialog.getByLabel("Name").fill("Adhesive Compound");
  await materialDialog.getByLabel("Unit").fill("bag");
  await materialDialog.getByRole("button", { name: "Save material" }).click();
  await expect(materialDialog).toBeHidden();
  await expect(page.getByRole("cell", { name: "Adhesive Compound", exact: true })).toBeVisible();

  // Financial-truth baseline: this fresh Project has no CostItems yet.
  await tabs.getByRole("link", { name: "Costs", exact: true }).click();
  await expect(page.getByText("No cost items yet")).toBeVisible();

  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByRole("link", { name: "AI Project Setup" }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/ai-setup$`));

  // --- Brief ---
  await page.getByLabel("Project brief").fill(
    "Full renovation of a 3-bedroom condominium. Redo the kitchen and two bathrooms, replace flooring throughout, and repaint the whole unit."
  );
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("button", { name: "Suggest Spaces" })).toBeVisible();

  // --- Space suggestions: accept Kitchen, edit Guest Bedroom -> Kids Bedroom, reject the extra Space ---
  // The mock provider (ai-service/app/providers/mock.py) always proposes,
  // in this fixed order for a fresh company: Kitchen, Master Bathroom,
  // Living Area, Guest Bedroom. Each card's rationale text disappears once
  // accepted/edited/rejected, so cards are addressed by position (nth)
  // rather than by re-querying a hasText filter whose match text just
  // vanished from the DOM.
  await page.getByRole("button", { name: "Suggest Spaces" }).click();
  const spacesSection = page.getByRole("heading", { name: "Suggested Spaces" }).locator("xpath=ancestor::section[1]");
  const spaceCards = spacesSection.locator(".flex.flex-col.gap-1\\.5").filter({ has: page.locator(".surface-card") });
  await expect(spaceCards).toHaveCount(4);

  await spaceCards.nth(0).getByRole("button", { name: "Accept" }).click();
  await expect(spaceCards.nth(0)).toContainText("Added to project", acceptTimeout);
  await expect(spaceCards.nth(0)).toContainText("Kitchen");

  await spaceCards.nth(1).getByRole("button", { name: "Accept" }).click();
  await expect(spaceCards.nth(1)).toContainText("Added to project", acceptTimeout);
  await expect(spaceCards.nth(1)).toContainText("Master Bathroom");

  await spaceCards.nth(3).getByRole("button", { name: "Edit" }).click();
  await spaceCards.nth(3).getByLabel("Name").fill("Kids Bedroom");
  await spaceCards.nth(3).getByRole("button", { name: "Accept edits" }).click();
  // "Kids Bedroom" + "Added to project" together is a unique combination
  // within this section by this point, so assert via a fresh locator rather
  // than re-querying the nth(3) handle — index-based locators can
  // transiently observe a stale render frame during the accept mutation's
  // re-render.
  const kidsBedroomAccepted = spacesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Kids Bedroom" });
  await expect(kidsBedroomAccepted).toContainText("Added to project", acceptTimeout);

  const livingAreaCard = spacesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Living Area" });
  await livingAreaCard.getByRole("button", { name: "Reject" }).click();
  await expect(livingAreaCard).toContainText("Rejected", acceptTimeout);

  // Real Space UI reflects the accepted Spaces.
  await tabs.getByRole("link", { name: "Spaces", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Kitchen" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Master Bathroom" })).toBeVisible();
  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByRole("link", { name: "AI Project Setup" }).click();

  // --- Work Item suggestions: explicit, supporting, possible-missing, project-wide ---
  // The mock provider proposes, for the first (by createdAt) confirmed
  // Space (Kitchen, accepted first above): explicit_scope "Replace flooring
  // in Kitchen", supporting_scope "Remove existing fixtures in Kitchen",
  // possible_missing_scope "Make good wall surfaces in Kitchen", then one
  // project-wide "Site protection for common areas". Each card's
  // description text survives acceptance (unlike the Space cards' rationale
  // text), so cards are addressed by their unique description rather than
  // position — avoids the same render-timing race the Space section hit.
  await page.getByRole("button", { name: "Generate Work Items" }).click();
  const workItemsSection = page.getByRole("heading", { name: "Suggested Work Items" }).locator("xpath=ancestor::section[1]");
  const workItemCards = workItemsSection.locator(".flex.flex-col.gap-1\\.5").filter({ has: page.locator(".surface-card") });
  await expect(workItemCards).toHaveCount(4);

  const explicitCard = workItemsSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Replace flooring in Kitchen" });
  await expect(explicitCard).toContainText("Requested scope");
  await explicitCard.getByLabel("Quantity").fill("25");
  await explicitCard.getByLabel("Unit").fill("m2");
  await explicitCard.getByRole("button", { name: "Accept" }).click();
  await expect(explicitCard).toContainText("Added to project", acceptTimeout);

  const supportingCard = workItemsSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Remove existing fixtures in Kitchen" });
  await expect(supportingCard).toContainText("Supporting scope");
  await supportingCard.getByLabel("Quantity").fill("25");
  await supportingCard.getByLabel("Unit").fill("m2");
  await supportingCard.getByRole("button", { name: "Accept" }).click();
  await expect(supportingCard).toContainText("Added to project", acceptTimeout);

  const possibleMissingCard = workItemsSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Make good wall surfaces in Kitchen" });
  await expect(possibleMissingCard).toContainText("Possible missing scope");
  await possibleMissingCard.getByLabel("Quantity").fill("25");
  await possibleMissingCard.getByLabel("Unit").fill("m2");
  await possibleMissingCard.getByRole("button", { name: "Accept" }).click();
  await expect(possibleMissingCard).toContainText("Added to project", acceptTimeout);

  const projectWideCard = workItemsSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Site protection for common areas" });
  await projectWideCard.getByLabel("Quantity").fill("1");
  await projectWideCard.getByLabel("Unit").fill("lot");
  await projectWideCard.getByRole("button", { name: "Accept" }).click();
  await expect(projectWideCard).toContainText("Added to project", acceptTimeout);

  // Real WorkItem UI reflects the accepted items.
  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  await expect(page.getByRole("row").filter({ hasText: "25 m2" }).first()).toBeVisible();
  await tabs.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByRole("link", { name: "AI Project Setup" }).click();

  // --- Resource suggestions: Use Existing, Create & Add, Trade, Equipment ---
  // The mock provider proposes, for the first confirmed Work Item, in fixed
  // order: material "Tile Adhesive" (candidateMaterialId = the
  // alphabetically-first Material candidate — "Adhesive Compound", created
  // above), material "Tile Spacers" (no candidate), trade "Tiler", equipment
  // "Tile Cutter". Each suggestion's own name is unique and survives
  // acceptance, so cards are addressed by name rather than position.
  await page.getByRole("button", { name: "Suggest Resources" }).click();
  const resourcesSection = page.getByRole("heading", { name: "Suggested Resources" }).locator("xpath=ancestor::section[1]");
  const resourceCards = resourcesSection.locator(".flex.flex-col.gap-1\\.5").filter({ has: page.locator(".surface-card") });
  await expect(resourceCards).toHaveCount(4);

  const tileAdhesiveCard = resourcesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Tile Adhesive" });
  await expect(tileAdhesiveCard).toContainText("Possible catalog match: Adhesive Compound");
  await tileAdhesiveCard.getByRole("button", { name: "Use Existing" }).click();
  await expect(tileAdhesiveCard).toContainText("Added to project", acceptTimeout);

  const tileSpacersCard = resourcesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Tile Spacers" });
  await expect(tileSpacersCard).toContainText("No catalog match");
  await tileSpacersCard.getByRole("button", { name: "Create & Add" }).click();
  await tileSpacersCard.getByLabel("Unit").fill("bag");
  await tileSpacersCard.getByLabel("Reference price").fill("8.50");
  await tileSpacersCard.getByRole("button", { name: "Add", exact: true }).click();
  await expect(tileSpacersCard).toContainText("Added to project", acceptTimeout);

  const tilerCard = resourcesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Tiler" });
  await tilerCard.getByRole("button", { name: "Accept" }).click();
  await expect(tilerCard).toContainText("Added to project", acceptTimeout);

  const tileCutterCard = resourcesSection.locator(".flex.flex-col.gap-1\\.5", { hasText: "Tile Cutter" });
  await tileCutterCard.getByRole("button", { name: "Accept" }).click();
  await expect(tileCutterCard).toContainText("Added to project", acceptTimeout);

  // Approved resources are visible as real Work Item planning data (design
  // doc §30) — open the same Work Item the resources were attached to.
  await tabs.getByRole("link", { name: "Work Items", exact: true }).click();
  await page.getByRole("row").filter({ hasText: "25 m2" }).first().click();
  const workItemDialog = page.getByRole("dialog");
  await expect(workItemDialog.getByRole("heading", { name: "Resources" })).toBeVisible();
  // The WorkResourceRequirement stores the AI-suggested resource name
  // ("Tile Adhesive"), not the linked Material catalog name — materialId is
  // the actual link, name is what the resource was called at acceptance
  // time (workresources/service.go CreateFromAISuggestion).
  await expect(workItemDialog.getByText("Tile Adhesive")).toBeVisible();
  await expect(workItemDialog.getByText("Tile Spacers")).toBeVisible();
  await expect(workItemDialog.getByText("Tiler")).toBeVisible();
  await expect(workItemDialog.getByText("Tile Cutter")).toBeVisible();
  await page.keyboard.press("Escape");

  // Financial-truth assertion: AI Space/WorkItem/Resource approval never
  // creates a CostItem (design doc §11.2, "AI suggests, the system
  // calculates, the contractor approves").
  await tabs.getByRole("link", { name: "Costs", exact: true }).click();
  await expect(page.getByText("No cost items yet")).toBeVisible();
});
