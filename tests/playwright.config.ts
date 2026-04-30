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
    // On CI (headless Linux) wrap wails dev in xvfb-run so the GTK
    // window-init succeeds without a real X display. Locally the user's
    // own Xorg/Quartz handles it natively. Detection via CI=true (set by
    // GitHub Actions and the same env Playwright already keys on).
    command: process.env.CI
      ? "xvfb-run --auto-servernum --server-args='-screen 0 1280x800x24' wails dev -loglevel error"
      : "wails dev -loglevel error",
    url: "http://localhost:34115",
    reuseExistingServer: !process.env.CI,
    // First-time build of the Go + frontend bundle on a cold CI runner
    // can take a while (npm install + go build + bindings). 240s gives
    // headroom; locally the rebuild is much faster so this is mostly a
    // CI-only ceiling.
    timeout: 240_000,
    // wails dev needs to run from the repo root, which is one level up from
    // this config file. Without this override, playwright would launch wails
    // from tests/ and the build would fail on missing wails.json.
    cwd: "..",
    stdout: "pipe",
    stderr: "pipe",
    // Force-shutdown the dev server when tests finish. Without this,
    // xvfb-run's process tree on CI doesn't relay SIGTERM to wails, so
    // playwright hangs after the smoke test passes and the job runs out
    // the 15-minute job timeout. SIGKILL is brutal but acceptable for a
    // dev process we don't need to flush state from.
    gracefulShutdown: { signal: "SIGKILL", timeout: 5_000 },
  },
});
