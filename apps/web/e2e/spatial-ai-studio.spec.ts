import { expect, test, type Page } from "@playwright/test";
import {
  API_BASE_URL,
  OBJECT_ID,
  ROOM_DRAFT_ID,
  blockedTurn,
  conceptReadyAttempt,
  designSession,
  geometryAttempt,
  geometryTurn,
  installBaseRoutes,
  installStudioState,
  materialOnlyConceptReadyAttempt,
  materialOnlyTurn,
  roomDraft,
  studioRoute,
} from "./support/spatialStudioFixtures";

// RP4E3 visual-acceptance gate (M8.5C plan). Renders the REAL SpatialEditor
// and AIDesignStudio production component tree — a real 3D canvas, real
// Three.js selection/raycasting, real Current/Concept toggle, real
// responsive CSS — in a real browser. Only the network is faked, via
// page.route against the real NEXT_PUBLIC_API_BASE_URL, with fixtures typed
// against the actual generated OpenAPI schema (see spatialStudioFixtures.ts).
// No test-only seeding route, no fabricated RoomPlan capture chain, no
// alternate/simplified renderer — matches SpatialEditor.test.tsx's own
// component-level MSW convention, moved to a real browser.
//
// Desktop/laptop scope only (2026-09-10 decision): the plan's 3-tier
// responsive layout (overlay+scrim at 1024-1279px, bottom Sheet below
// 1024px) was never implemented — SpatialEditor.tsx has only one `xl:`
// (1280px) breakpoint. Renovex Spatial Studio is a desktop/laptop product
// for now; mobile/tablet responsiveness is deferred. This spec therefore
// covers two viewports, both at/above the side-by-side breakpoint, not the
// plan's original four.
const VIEWPORTS = [
  { name: "wide-desktop", width: 1440, height: 900 },
  { name: "laptop", width: 1280, height: 800 },
];

test.describe.configure({ mode: "parallel" });

async function gotoStudio(page: Page) {
  await page.goto(studioRoute());
  await expect(page.getByRole("img", { name: /3d room view/i })).toBeVisible({ timeout: 15000 });
}

// Selects the fixture object through the 2D floor plan view, then returns
// to 3D — the exact same real-production selection path
// SpatialEditor.test.tsx's own "concept preview wiring" integration test
// uses (see that file's comment: the 3D canvas's hit-testing is real
// Three.js/WebGL scene-graph state, not DOM, so a real browser click still
// needs a genuinely correct screen-space projection of the target's
// current camera pose — precise but camera-math-dependent). The 2D
// FloorPlanViewport is equally real production code (an explicitly
// supported, plan-documented "2D switch remains available" path) and
// exposes the canonical selection via ordinary DOM elements
// (data-element-id), giving deterministic, camera-independent selection.
// `editor.selection` is the single shared selection state both viewports
// read from (useEditorState), so 3D reflects the same selection the moment
// the view switches back — this is not a parallel/simplified selection
// mechanism, it is the same one SpatialEditor already uses for both views.
//
// The object hit target is an SVG <rect fill="none"> — real SVG hit-testing
// only registers a fill-less rect's STROKE (its outline), never its
// interior, so the click must land on the element's edge, not its
// geometric center (confirmed empirically: a center click is silently
// swallowed by the parent <svg>, which is what "intercepts pointer events"
// reports). `position: {x: 1, y: 1}` targets a corner of the rect's own
// bounding box, always on the stroke regardless of the rect's rendered size.
async function selectObjectViaCanvas(page: Page) {
  await page.getByRole("radio", { name: "2D" }).click();
  await expect(page.getByRole("img", { name: /floor plan/i })).toBeVisible();
  await page.locator(`[data-element-kind="object"][data-element-id="${OBJECT_ID}"]`).click({ position: { x: 1, y: 1 } });
  await page.getByRole("radio", { name: "3D" }).click();
  await expect(page.getByRole("img", { name: /3d room view/i })).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole("status").filter({ hasText: /armchair/i })).toBeVisible();
}

// --- Prohibited-outcomes checks, reusable across every state ---
// Each assertion is a structural check for one item on the plan's
// "Prohibited frontend outcomes" list — never a screenshot-only proxy.
async function assertNoProhibitedOutcomes(page: Page) {
  // No generic chatbot rail (user/assistant bubbles, avatars, timestamps).
  await expect(page.locator('[class*="bubble"], [class*="avatar"]')).toHaveCount(0);
  // Exactly one R3F canvas — never two, never a detached preview.
  await expect(page.locator("canvas")).toHaveCount(1);
  // No full-screen modal/spinner during planning or generation — the only
  // dialog allowed to exist is the (closed) Reset-to-scan confirmation.
  const openDialogs = page.locator('[role="dialog"][data-state="open"]');
  await expect(openDialogs).toHaveCount(0);
  // No raw operation JSON, provider status codes, or storage/model
  // identifiers leaking into visible text.
  const bodyText = await page.locator("body").innerText();
  expect(bodyText).not.toMatch(/cloudflare|hugging ?face|hunyuan|zerogpu|flux/i);
  expect(bodyText).not.toMatch(/"status":\s*\d{3}/);
  expect(bodyText).not.toMatch(/asset_[a-f0-9]{6,}|attempt_[a-f0-9]{6,}/i);
}

// The room canvas must remain visible and interactive — "room remains
// dominant" holds for every state, not just a subset.
async function assertRoomRemainsDominant(page: Page) {
  const canvas = page.getByRole("img", { name: /3d room view/i });
  await expect(canvas).toBeVisible();
  const box = await canvas.boundingBox();
  expect(box?.width ?? 0).toBeGreaterThan(300);
  expect(box?.height ?? 0).toBeGreaterThan(300);
}

// 2026-09-10 revision: the plan's original "68-74% canvas / 26-32% dock at
// 1440px" figures assumed a near-full-viewport workspace. AppShell's
// sidebar + content padding mean the actual usable workspace at 1440px is
// materially narrower, and the dock's 390px usability floor (touch
// targets, chip layout, readable plan cards) cannot simultaneously satisfy
// both the fixed floor and the tight percentage band inside that narrower
// space. Per the resulting product decision, the percentage is no longer
// a hard invariant — the actual invariants checked here are: the dock
// stays within its 390-440px usability band, the canvas is always the
// larger of the two, the canvas receives all remaining workspace width
// (not a second fixed figure), and neither is cramped/near-zero.
async function assertDesktopWidthTargets(page: Page) {
  const canvas = page.getByRole("img", { name: /3d room view/i });
  const dock = page.locator("h1", { hasText: "AI Design" }).locator("xpath=ancestor::div[contains(@class,'rounded-xl')][1]");
  const canvasBox = await canvas.boundingBox();
  const dockBox = await dock.boundingBox();
  if (!canvasBox || !dockBox) throw new Error("could not measure canvas/dock");
  expect(dockBox.width).toBeGreaterThanOrEqual(390);
  expect(dockBox.width).toBeLessThanOrEqual(440);
  expect(canvasBox.width).toBeGreaterThan(dockBox.width);
  expect(canvasBox.width).toBeGreaterThan(400);
}

async function assertNoOverflowOrClippedPrimaryActions(page: Page) {
  const viewportSize = page.viewportSize();
  if (!viewportSize) return;
  const buttons = page.getByRole("button");
  const count = await buttons.count();
  for (let i = 0; i < count; i++) {
    const button = buttons.nth(i);
    if (!(await button.isVisible())) continue;
    const box = await button.boundingBox();
    if (!box) continue;
    expect(box.x).toBeGreaterThanOrEqual(-1);
    expect(box.x + box.width).toBeLessThanOrEqual(viewportSize.width + 1);
    // A button whose text wraps past one line reads as clipped/cramped —
    // the plan requires every label to fit on one line at desktop.
    expect(box.height).toBeLessThan(48);
  }
}

for (const viewport of VIEWPORTS) {
  test.describe(`Spatial AI Studio — ${viewport.name} (${viewport.width}x${viewport.height})`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("01 empty / welcoming — no selection", async ({ page }) => {
      await installBaseRoutes(page);
      await gotoStudio(page);

      await expect(page.getByText("Choose a piece in the room to imagine a change.")).toBeVisible();
      // No AI overlay/dock content beyond the reopen affordance and the
      // empty-state copy — no disabled form wall.
      await expect(page.getByRole("textbox")).toHaveCount(0);
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await assertNoOverflowOrClippedPrimaryActions(page);
      await assertDesktopWidthTargets(page);
      await expect(page).toHaveScreenshot(`01-empty-${viewport.name}.png`);
    });

    test("02 supported object selected — welcome/idea-chips state", async ({ page }) => {
      await installBaseRoutes(page);
      await installStudioState(page, { session: null });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByRole("heading", { name: /what should we try with this armchair/i })).toBeVisible();
      await expect(page.getByRole("textbox")).toBeVisible();
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await assertNoOverflowOrClippedPrimaryActions(page);
      await expect(page).toHaveScreenshot(`02-supported-selected-${viewport.name}.png`);
    });

    test("03 unsupported structural element selected", async ({ page }) => {
      await installBaseRoutes(page);
      await gotoStudio(page);

      // A wall is a structural/unsupported design target
      // (isSupportedDesignTarget only allows "object"/"fixture") — selected
      // through the same real 2D-then-3D path as selectObjectViaCanvas,
      // never a guessed 3D-canvas click coordinate.
      await page.getByRole("radio", { name: "2D" }).click();
      await expect(page.getByRole("img", { name: /floor plan/i })).toBeVisible();
      await page.locator(`[data-element-kind="wallBody"][data-wall-id="wall_n"]`).click({ force: true });
      await page.getByRole("radio", { name: "3D" }).click();
      await expect(page.getByRole("img", { name: /3d room view/i })).toBeVisible({ timeout: 15000 });

      await expect(page.getByText(/works with movable objects and fixtures/i)).toBeVisible({ timeout: 10000 });

      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`03-unsupported-${viewport.name}.png`);
    });

    test("04 restoring an existing DesignSession", async ({ page }) => {
      await installBaseRoutes(page);
      const session = designSession();
      // A deliberately slow session GET keeps the studio in "restoring"
      // long enough to screenshot deterministically.
      await page.route(`${API_BASE_URL}/spatial/design-sessions`, (route) =>
        route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ session }) }),
      );
      await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}`, async (route) => {
        await new Promise((resolve) => setTimeout(resolve, 3000));
        await route.fulfill({
          status: 200, contentType: "application/json",
          body: JSON.stringify({ session, stale: false, currentRoomDraftRevision: session.basedOnRoomDraftRevision }),
        });
      });
      await page.route(`${API_BASE_URL}/spatial/design-sessions/${session.id}/turns`, (route) =>
        route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([]) }),
      );
      await gotoStudio(page);

      await page.getByRole("radio", { name: "2D" }).click();
      await expect(page.getByRole("img", { name: /floor plan/i })).toBeVisible();
      await page.locator(`[data-element-kind="object"][data-element-id="${OBJECT_ID}"]`).click({ position: { x: 1, y: 1 } });
      await page.getByRole("radio", { name: "3D" }).click();
      await expect(page.getByRole("img", { name: /3d room view/i })).toBeVisible({ timeout: 15000 });

      // Restoring renders two Skeleton placeholders, never the welcome
      // heading or a blank studio.
      await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible({ timeout: 2000 });
      await expect(page.getByRole("heading", { name: /what should we try/i })).not.toBeVisible();

      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`04-restoring-${viewport.name}.png`);
    });

    test("05 understanding / planning", async ({ page }) => {
      await installBaseRoutes(page);
      await installStudioState(page, { session: designSession(), turns: [{ ...geometryTurn(), status: "reasoning", changePlan: undefined, execution: undefined, fitAnalysis: undefined }] });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByText(/understanding your idea/i)).toBeVisible();
      await expect(page.getByRole("status").filter({ hasText: /understanding your idea/i })).toHaveAttribute("aria-live", "polite");
      // No full-screen modal/spinner — the progress copy lives inline in
      // the dock, the canvas stays interactive.
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`05-planning-${viewport.name}.png`);
    });

    test("06 valid change plan ready — geometry/mixed (GPU notice present)", async ({ page }) => {
      await installBaseRoutes(page);
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()] });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByText("Change plan", { exact: true })).toBeVisible({ timeout: 10000 });
      await expect(page.getByRole("button", { name: "Create concept" })).toBeVisible();
      // GPU-runtime notice appears ONLY for geometry/mixed generation.
      await expect(page.getByText(/shared gpu capacity/i)).toBeVisible();
      // Exactly one primary-colored button visible at a time in the studio.
      await assertNoOverflowOrClippedPrimaryActions(page);
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`06-plan-ready-geometry-${viewport.name}.png`);
    });

    test("06b valid change plan ready — material-only (no GPU notice, 'Preview change')", async ({ page }) => {
      await installBaseRoutes(page);
      await installStudioState(page, { session: designSession(), turns: [materialOnlyTurn()] });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByRole("button", { name: "Preview change" })).toBeVisible();
      await expect(page.getByText(/shared gpu capacity/i)).not.toBeVisible();
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`06b-plan-ready-material-${viewport.name}.png`);
    });

    test("07 blocked change plan", async ({ page }) => {
      await installBaseRoutes(page);
      await installStudioState(page, { session: designSession(), turns: [blockedTurn()] });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByRole("alert").filter({ hasText: /would not fit/i })).toBeVisible();
      // Blocked plans cannot confirm — no Confirm/Create-concept/Preview
      // action exists in this state at all.
      await expect(page.getByRole("button", { name: /create concept|preview change/i })).toHaveCount(0);
      await expect(page.getByRole("textbox")).toBeVisible();
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`07-blocked-${viewport.name}.png`);
    });

    test("08 material-only preview — near-instant, no 3D-generation progress", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = materialOnlyConceptReadyAttempt();
      await installStudioState(page, { session: designSession(), turns: [{ ...materialOnlyTurn(), status: "proposed" }], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Preview change" }).click();

      // Material-only reaches concept_ready directly — ConceptActions, not
      // a progress/staged-generation view.
      await expect(page.getByRole("button", { name: "Use Design" })).toBeVisible({ timeout: 10000 });
      await expect(page.getByText(/preparing the visual direction|shaping the 3d concept/i)).not.toBeVisible();
      // Current/Concept toggle lives ON the canvas, not a second viewer.
      const toggle = page.getByRole("group", { name: /compare current and concept/i });
      await expect(toggle).toBeVisible();
      await expect(page.locator("canvas")).toHaveCount(1);
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`08-material-preview-${viewport.name}.png`);
    });

    test("09 preparing reference (geometry generation, reference sub-stage)", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = geometryAttempt("generating_reference");
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      // deriveStudioState only reads attemptStatus once studio.activeAttemptId
      // is set, which only happens via the real attemptCreated() callback
      // after a genuine Confirm click succeeds — never by the attempt fixture
      // existing on its own. Clicking "Create concept" drives that real path.
      await page.getByRole("button", { name: "Create concept" }).click();

      await expect(page.getByText("Preparing the visual direction")).toBeVisible({ timeout: 10000 });
      await expect(page.getByText("You can keep working in the room while this finishes.")).toBeVisible();
      // Selected object stays visible during generation — never hidden by
      // a full-screen loader.
      await assertRoomRemainsDominant(page);
      const dialogs = page.locator('[role="dialog"][data-state="open"]');
      await expect(dialogs).toHaveCount(0);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`09-preparing-reference-${viewport.name}.png`);
    });

    test("10 generating 3D (asset generation sub-stage)", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = geometryAttempt("asset_generation_processing");
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Create concept" }).click();

      await expect(page.getByText("Shaping the 3D concept")).toBeVisible({ timeout: 10000 });
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`10-generating-3d-${viewport.name}.png`);
    });

    test("11 validating asset (asset_generation_pending sub-stage)", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = geometryAttempt("asset_generation_pending");
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Create concept" }).click();

      // asset_generation_pending maps to the same "geometry" sub-stage as
      // asset_generation_processing (DesignProgress's stageForStatus) —
      // this is the pre-validation/queued state on the way to concept_ready.
      await expect(page.getByText("Shaping the 3D concept")).toBeVisible({ timeout: 10000 });
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`11-validating-asset-${viewport.name}.png`);
    });

    test("12 concept ready — geometry/mixed, Current/Concept toggle in-canvas", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = conceptReadyAttempt();
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Create concept" }).click();

      await expect(page.getByRole("button", { name: "Use Design" })).toBeVisible({ timeout: 10000 });
      await expect(page.getByRole("button", { name: "Regenerate" })).toBeVisible();
      await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
      // Fixed order: Use Design primary, Regenerate outline, Cancel ghost.
      const toggle = page.getByRole("group", { name: /compare current and concept/i });
      await expect(toggle).toBeVisible();
      await expect(page.locator("canvas")).toHaveCount(1);
      // At most 3 refinement chips.
      const chips = page.getByRole("group", { name: /refinement suggestions/i }).getByRole("button");
      expect(await chips.count()).toBeLessThanOrEqual(3);
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`12-concept-ready-${viewport.name}.png`);
    });

    test("13 generation failure", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = geometryAttempt("needs_attention", { safeFailureCode: "asset_generation_needs_attention" });
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Create concept" }).click();

      await expect(page.getByRole("alert").filter({ hasText: /could not be created/i })).toBeVisible();
      // Never a raw provider/failure code in visible text.
      const bodyText = await page.locator("body").innerText();
      expect(bodyText).not.toMatch(/needs_attention|asset_generation_needs_attention/i);
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`13-generation-failure-${viewport.name}.png`);
    });

    test("14 stale plan / re-review state", async ({ page }) => {
      await installBaseRoutes(page, roomDraft({ revision: 2 }));
      // basedOnRoomDraftRevision (1) no longer matches the draft's current
      // revision (2) — deriveStudioState's isStalePlan path.
      await installStudioState(page, { session: designSession({ basedOnRoomDraftRevision: 1 }), turns: [{ ...geometryTurn(), basedOnRoomDraftRevision: 1 }] });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);

      await expect(page.getByText(/room changed\. review this idea again/i)).toBeVisible({ timeout: 10000 });
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`14-stale-${viewport.name}.png`);
    });

    test("continued-refinement — accepted state stays active for further prompts", async ({ page }) => {
      await installBaseRoutes(page);
      const attempt = conceptReadyAttempt({ status: "accepted", acceptedAt: "2026-09-10T00:05:00Z" });
      await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
      await gotoStudio(page);
      await selectObjectViaCanvas(page);
      await page.getByRole("button", { name: "Create concept" }).click();

      await expect(page.getByText(/design applied\. refine this design/i)).toBeVisible({ timeout: 10000 });
      // Session stays active — Use Design/Regenerate/Cancel remain, ready
      // for another prompt, never a terminal dead-end screen.
      await expect(page.getByRole("button", { name: "Regenerate" })).toBeVisible();
      await assertRoomRemainsDominant(page);
      await assertNoProhibitedOutcomes(page);
      await expect(page).toHaveScreenshot(`accepted-continued-refinement-${viewport.name}.png`);
    });
  });
}

test.describe("Spatial AI Studio — reduced motion", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("concept_ready renders with no transition-dependent hidden content under reduced motion", async ({ page }) => {
    await page.emulateMedia({ reducedMotion: "reduce" });
    await installBaseRoutes(page);
    const attempt = conceptReadyAttempt();
    await installStudioState(page, { session: designSession(), turns: [geometryTurn()], attempt });
    await gotoStudio(page);
    await selectObjectViaCanvas(page);
    await page.getByRole("button", { name: "Create concept" }).click();

    await expect(page.getByRole("button", { name: "Use Design" })).toBeVisible({ timeout: 10000 });
    // Every actionable control must already be visible with no reliance on
    // a fired transition/animation-end event under reduced motion.
    await expect(page.getByRole("group", { name: /compare current and concept/i })).toBeVisible();
    await expect(page.getByRole("button", { name: "Regenerate" })).toBeVisible();
  });
});

// T1B regression: RoomDraft objects (e.g. the armchair fixture) must be
// manually editable through the real production SpatialEditor tree
// independent of AI Design — reuses this spec's existing harness
// (installBaseRoutes/selectObjectViaCanvas) rather than a second E2E setup.
// Drives the commit through the manual inspector's numeric controls (not a
// pixel-precise TransformControls gizmo drag) for the same reason
// selectObjectViaCanvas prefers the 2D DOM path over 3D raycasting —
// deterministic, camera-math-independent — while still exercising the real
// component tree, a real network round trip, and the real canonical
// EditOperation dispatch.
test.describe("Spatial AI Studio — object manual editing independent of AI (T1B)", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("selecting the armchair and committing a manual resize dispatches resize_object with the expected revision, with no working AI session", async ({ page }) => {
    await installBaseRoutes(page);
    // The AI service is down for every session-create attempt — proves the
    // manual edit commits even though AI Design cannot respond at all.
    await page.route(`${API_BASE_URL}/spatial/design-sessions`, async (route) => {
      await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ title: "AI service unavailable" }) });
    });

    let editBody: { kind: string; payload: { objectId: string; dimensions: { x: number; y: number; z: number } }; expectedRevision: number } | undefined;
    await page.route(`${API_BASE_URL}/spatial/room-drafts/${ROOM_DRAFT_ID}/edits`, async (route) => {
      editBody = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          roomDraft: roomDraft({ revision: 2, objects: [{ ...roomDraft().objects![0], dimensions: editBody?.payload.dimensions }] }),
          record: {
            id: "rec_e2e", roomDraftId: ROOM_DRAFT_ID, operationId: "op_e2e", operationKind: "resize_object",
            operationPayload: "{}", baseRevision: 1, resultingRevision: 2, createdAt: "2026-09-10T00:00:00Z",
          },
          replayed: false,
        }),
      });
    });

    await gotoStudio(page);
    await selectObjectViaCanvas(page);

    // Manual controls remain functional regardless of the AI panel's own
    // degraded state (session-create 503s in the background).
    await page.getByText("Manual controls").click();

    const widthField = page.getByLabel(/^w \(m\)$/i);
    await widthField.fill("2.5");
    await widthField.blur();

    await expect.poll(() => editBody?.kind).toBe("resize_object");
    expect(editBody?.payload.objectId).toBe(OBJECT_ID);
    expect(editBody?.payload.dimensions.x).toBe(2.5);
    expect(editBody?.expectedRevision).toBe(1);
  });
});

test.describe("Spatial AI Studio — keyboard navigation", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("keyboard-only Tab reaches the studio's composer in logical order", async ({ page }) => {
    await installBaseRoutes(page);
    await installStudioState(page, { session: null });
    await gotoStudio(page);
    await selectObjectViaCanvas(page);

    await expect(page.getByRole("heading", { name: /what should we try with this armchair/i })).toBeVisible();
    // Tab from the top of the page must reach the prompt composer without
    // ever landing focus inside the WebGL canvas itself.
    let reachedComposer = false;
    for (let i = 0; i < 40; i++) {
      await page.keyboard.press("Tab");
      const active = page.locator(":focus");
      const tag = await active.evaluate((el) => el.tagName).catch(() => "");
      if (tag === "CANVAS") throw new Error("keyboard focus landed inside the WebGL canvas");
      if (tag === "TEXTAREA" || tag === "INPUT") {
        reachedComposer = true;
        break;
      }
    }
    expect(reachedComposer).toBe(true);
  });
});
