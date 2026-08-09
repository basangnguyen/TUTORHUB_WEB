import { defineConfig, devices } from "@playwright/test";

const reuseExistingServer = process.env.MEDIA_UX_E2E_REUSE_SERVER === "1";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "../../test-results/media-ux-spike",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 45_000,
  expect: {
    timeout: 10_000,
  },
  reporter: [["line"]],
  use: {
    baseURL: "http://127.0.0.1:4176",
    locale: "vi-VN",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
  webServer: {
    command:
      "node node_modules/vite/bin/vite.js preview --host 127.0.0.1 --port 4176 --strictPort",
    url: "http://127.0.0.1:4176",
    reuseExistingServer,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
