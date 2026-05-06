import { test, expect, ConsoleMessage } from "@playwright/test";

const BASE_URL = process.env.PRISMCONDUCTOR_DEV_URL ?? "http://localhost:34115";
const SETTLE_MS = Number(process.env.PRISMCONDUCTOR_SMOKE_SETTLE_MS ?? 5000);
const CONVERGE_MS = 1000;

test("event buffer is exposed and captures synthetic events within 1s", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (msg: ConsoleMessage) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });

  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  // React must mount before we can test anything.
  await page.waitForFunction(
    () => {
      const root = document.getElementById("root");
      return !!root && root.children.length > 0;
    },
    { timeout: SETTLE_MS },
  );

  // The eventBus module must have initialised window.__pcEventBuffer.
  const bufferReady = await page.evaluate(() => {
    return (
      typeof window.__pcEventBuffer !== "undefined" &&
      Array.isArray(window.__pcEventBuffer.receives) &&
      typeof window.__pcEventBuffer.emit === "function"
    );
  });
  expect(bufferReady, "window.__pcEventBuffer must be initialised by eventBus.ts").toBe(true);

  // Capture the receive-count before the synthetic emission.
  const before = await page.evaluate(() => window.__pcEventBuffer.receives.length);

  // Emit a synthetic session.state "completed" via the debug channel, bypassing
  // Wails IPC entirely. This verifies the full handler → buffer pipeline: if the
  // registered EventsOnWrapped handler runs and the buffer records the event, the
  // UI would have received and processed it within the same tick.
  await page.evaluate(() => {
    window.__pcEventBuffer.emit("session.state", {
      id: "e2e-test-session",
      workspace_id: "ws-e2e",
      issue_number: 9999,
      state: "completed",
      mode: "execute",
    });
  });

  // The buffer must reflect the new entry within CONVERGE_MS.
  await page.waitForFunction(
    (count: number) => window.__pcEventBuffer.receives.length > count,
    before,
    { timeout: CONVERGE_MS },
  );

  const latest = await page.evaluate(() => window.__pcEventBuffer.receives[0]);
  expect(latest.name).toBe("session.state");
  expect((latest.payload as { state: string }).state).toBe("completed");

  // No new console errors should have occurred during the test.
  expect.soft(consoleErrors, `console.error: ${consoleErrors.join(" | ")}`).toEqual([]);
});
