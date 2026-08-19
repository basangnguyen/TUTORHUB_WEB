import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  testMatch: ["excalidraw-gate-a.spec.ts", "excalidraw-gate-e.spec.ts"],
  globalSetup: "./e2e/excalidrawGlobalSetup.ts",
  outputDir: "../../test-results/whiteboard-spike-excalidraw",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  expect: { timeout: 60_000 },
  reporter: [["line"]],
  use: {
    baseURL: "http://127.0.0.1:4180",
    locale: "vi-VN",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
});
