import { test, expect, ConsoleMessage } from "@playwright/test";

const BASE_URL = process.env.PRISMCONDUCTOR_DEV_URL ?? "http://localhost:34115";
const SETTLE_MS = Number(process.env.PRISMCONDUCTOR_SMOKE_SETTLE_MS ?? 5000);

test("app boots, React mounts, no console errors", async ({ page }) => {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];

  page.on("console", (msg: ConsoleMessage) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("pageerror", (err) => pageErrors.push(err.message));

  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  // React must mount something into #root within SETTLE_MS
  await page.waitForFunction(
    () => {
      const root = document.getElementById("root");
      return !!root && root.children.length > 0;
    },
    { timeout: SETTLE_MS },
  );

  // Soak the rest of the window to catch infinite-render loops + late throws
  await page.waitForTimeout(SETTLE_MS);

  expect.soft(consoleErrors, `console.error: ${consoleErrors.join(" | ")}`).toEqual([]);
  expect.soft(pageErrors, `pageerror: ${pageErrors.join(" | ")}`).toEqual([]);
});
