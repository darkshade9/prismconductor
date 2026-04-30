import { defineConfig } from "@playwright/test";

export default defineConfig({
  // testDir is relative to this config (tests/playwright.config.ts), so the
  // spec at tests/e2e/startup.spec.ts is one segment down.
  testDir: "./e2e",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: { trace: "retain-on-failure" },
  webServer: {
    command: "wails dev -loglevel error",
    url: "http://localhost:34115",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    // wails dev needs to run from the repo root, which is one level up from
    // this config file. Without this override, playwright would launch wails
    // from tests/ and the build would fail on missing wails.json.
    cwd: "..",
    stdout: "pipe",
    stderr: "pipe",
  },
});
