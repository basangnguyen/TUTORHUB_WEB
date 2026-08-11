import { defineConfig, devices } from "@playwright/test";

const baseURL = "http://127.0.0.1:5173";

export default defineConfig({
  testDir: "./e2e",
  testMatch: [
    "p4-03-prejoin.spec.ts",
    "p4-04-lobby.spec.ts",
    "p4-05-classroom-shell.spec.ts",
    "p4-05-livekit-provider.spec.ts",
  ],
  outputDir: "test-results/playwright-p405-provider",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: {
    timeout: 15_000,
  },
  forbidOnly: true,
  reporter: [["line"]],
  use: {
    actionTimeout: 15_000,
    baseURL,
    locale: "vi-VN",
    screenshot: "off",
    trace: "off",
    video: "off",
  },
  projects: [
    {
      name: "p405-provider-chromium",
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],
  webServer: {
    command: "node node_modules/vite/bin/vite.js --host 127.0.0.1 --strictPort",
    cwd: "apps/web",
    env: {
      ...process.env,
      DATABASE_MIGRATION_URL: "",
      DATABASE_POOL_URL: "",
      LIVEKIT_API_KEY: "",
      LIVEKIT_API_SECRET: "",
      LIVEKIT_URL: "",
      P4_05_BROWSER_PROVIDER_CONFIRM: "",
      P4_05_PROVIDER_CONFIRM: "",
      P4_05_PROVIDER_ROOM_NAME: "",
    },
    gracefulShutdown: {
      signal: "SIGTERM",
      timeout: 15_000,
    },
    url: baseURL,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
