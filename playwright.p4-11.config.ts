import { defineConfig, devices } from "@playwright/test";

const browserProjects = [
  {
    name: "chromium-source-supplement",
    use: devices["Desktop Chrome"],
  },
  {
    name: "firefox-source-supplement",
    use: devices["Desktop Firefox"],
  },
  {
    name: "webkit-source-supplement",
    testMatch: /p4-03-.*\.spec\.ts/,
    use: devices["Desktop Safari"],
  },
];

export default defineConfig({
  testDir: "./e2e",
  testMatch: /p4-0(?:3|[5-7])-.*\.spec\.ts/,
  testIgnore: /p4-05-livekit-provider\.spec\.ts/,
  outputDir: "test-results/playwright-p4-11-browser-supplement",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: {
    timeout: 15_000,
  },
  forbidOnly: Boolean(process.env.CI),
  reporter: [["line"]],
  use: {
    actionTimeout: 15_000,
    baseURL: "http://127.0.0.1:5173",
    locale: "vi-VN",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "off",
  },
  projects: browserProjects,
  webServer: {
    command:
      "node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5173 --strictPort",
    cwd: "apps/web",
    url: "http://127.0.0.1:5173/sign-in",
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
