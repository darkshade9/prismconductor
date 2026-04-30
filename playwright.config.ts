import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: { trace: "retain-on-failure" },
  webServer: {
    command: "wails dev -loglevel error",
    url: "http://localhost:34115",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    cwd: ".",
    stdout: "pipe",
    stderr: "pipe",
  },
});
