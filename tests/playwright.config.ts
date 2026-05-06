import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const smokeDataDir = mkdtempSync(join(tmpdir(), "prismconductor-smoke-"));

export default defineConfig({
  // testDir is relative to this config (tests/playwright.config.ts), so the
  // spec at tests/e2e/startup.spec.ts is one segment down.
  testDir: "./e2e",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: { trace: "retain-on-failure" },
  // No webServer block on CI: the workflow starts wails dev under xvfb-run
  // explicitly and kills it afterward. Playwright's webServer cleanup
  // doesn't reliably propagate SIGTERM through xvfb-run's process tree on
  // GitHub runners, leaving the job hanging until the 15-minute kill.
  // Locally we still launch wails dev via webServer for ergonomics.
  webServer: process.env.CI
    ? undefined
    : {
        command: "wails dev -loglevel error",
        url: "http://localhost:34115",
        reuseExistingServer: true,
        timeout: 240_000,
        cwd: "..",
        stdout: "pipe",
        stderr: "pipe",
        env: { ...process.env, PRISMCONDUCTOR_DATA_DIR: smokeDataDir },
      },
});
