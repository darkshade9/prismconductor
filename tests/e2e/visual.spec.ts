/**
 * Visual smoke spec — captures six canonical full-page screenshots of the
 * app UI so PR reviewers can see UI changes without a local build.
 *
 * Prerequisites (handled by the CI workflow):
 *   1. wails dev is already running at PRISMCONDUCTOR_DEV_URL (default
 *      http://localhost:34115) with PRISMCONDUCTOR_DATA_DIR pointing at a
 *      temp dir that contains a copy of tests/fixtures/seed.db as
 *      conductor.db.
 *   2. Screenshots are written to tests/screenshots/ and uploaded as a
 *      CI artifact by smoke.yml.
 *
 * To run locally after starting wails dev with the fixture:
 *   FIXTURE_DATA_DIR=<tmp-dir-with-seed-db> npx playwright test e2e/visual.spec.ts
 */

import { test, expect } from "@playwright/test";
import { fileURLToPath } from "url";
import path from "path";

const BASE_URL = process.env.PRISMCONDUCTOR_DEV_URL ?? "http://localhost:34115";
const SETTLE_MS = Number(process.env.PRISMCONDUCTOR_SMOKE_SETTLE_MS ?? 3000);

// Resolve the screenshots dir relative to this spec file, ESM-safe.
const SHOT_DIR = path.resolve(
  fileURLToPath(import.meta.url),
  "..",   // e2e/
  "..",   // tests/
  "screenshots",
);

/**
 * Wait for the board to be visible and card data to have settled.
 * The board renders immediately; actual card rows appear once the backend
 * responds — we wait for the WorkspaceSwitcher row to appear as a proxy.
 */
async function waitForBoard(page: import("@playwright/test").Page) {
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(
    () => {
      const root = document.getElementById("root");
      return !!root && root.children.length > 0;
    },
    { timeout: SETTLE_MS * 2 },
  );
  // Wait for the WorkspaceSwitcher to render (contains the "Workspace:" label).
  await expect(page.getByText("Workspace:")).toBeVisible({ timeout: SETTLE_MS });
  // Wait for the workspace list to load from the backend. The "Workspace:" label
  // is a static span that appears before ListWorkspaces() responds; workspace
  // chip buttons (data-testid="workspace-chip") only appear after the IPC call
  // completes. Give the backend up to 20s — in CI under xvfb-run the IPC
  // connection can take several seconds to establish.
  await page
    .locator('[data-testid="workspace-chip"]')
    .or(page.getByText("no workspaces yet, add one in Settings"))
    .first()
    .waitFor({ state: "visible", timeout: 20_000 });
  // Short soak so async card fetches finish.
  await page.waitForTimeout(500);
}

// Disable CSS transitions / animations globally so screenshots are
// pixel-stable across repeated runs and OS/font differences are minimised.
async function disableAnimations(page: import("@playwright/test").Page) {
  await page.addStyleTag({
    content: `
      *, *::before, *::after {
        animation-duration: 0s !important;
        animation-delay: 0s !important;
        transition-duration: 0s !important;
        transition-delay: 0s !important;
      }
      * { cursor: none !important; }
    `,
  });
}

test.describe("visual captures", () => {
  // Use a consistent viewport for every capture.
  test.use({ viewport: { width: 1280, height: 800 } });

  test("1 — board: all workspaces overview", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // With two workspaces in the fixture, no auto-select occurs.
    // The board starts in "All workspaces" mode — shows all seeded cards.
    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "board-all-workspaces.png"),
    });
  });

  test("2 — board: empty workspace selected", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // Click the "Empty Project" workspace chip.
    await page.getByRole("button", { name: "Empty Project" }).click();
    await page.waitForTimeout(500);

    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "board-empty.png"),
    });
  });

  test("3 — board: seeded workspace with cards across columns", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // Click the "Seeded Project" workspace chip to filter to that workspace.
    await page.getByRole("button", { name: "Seeded Project" }).click();
    await page.waitForTimeout(500);

    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "board-populated.png"),
    });
  });

  test("4 — plan modal: ready-to-execute plan", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // Select seeded workspace first so the PLAN-column card is visible.
    await page.getByRole("button", { name: "Seeded Project" }).click();
    await page.waitForTimeout(500);

    // Click the plan-ready card (issue #102 — "Refactor database schema").
    await page.getByText("Refactor database schema").first().click();

    // Wait for the plan modal to appear.
    await page.waitForTimeout(1000);

    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "plan-modal.png"),
    });
  });

  test("5 — settings panel: workspaces tab", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // Open settings via the header button. Use exact:true to avoid matching the
    // PoolsHeader div whose aria-label="Open pool settings" also contains "Settings".
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.waitForTimeout(500);

    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "settings-workspaces.png"),
    });
  });

  test("6 — session drawer: transcript panel", async ({ page }) => {
    await waitForBoard(page);
    await disableAnimations(page);

    // Open the session drawer via the header "Drawer" button.
    await page.getByRole("button", { name: "Drawer" }).click();
    await page.waitForTimeout(500);

    await page.screenshot({
      fullPage: true,
      path: path.join(SHOT_DIR, "session-drawer.png"),
    });
  });
});
