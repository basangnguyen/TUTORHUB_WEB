import { defineConfig, devices } from "@playwright/test";

/**
 * AUTH-02B native sign-up/password E2E. It only targets the loopback stack:
 * the `devinbox` OTP sender and `/api/v1/dev/auth-inbox` exist solely for
 * APP_ENV=test on 127.0.0.1, so staging mode is intentionally unsupported.
 */
const mode = process.env.E2E_MODE?.trim() || "local";
if (mode !== "local") {
  throw new Error("playwright.auth.config.ts only supports E2E_MODE=local.");
}

const baseURL = "http://127.0.0.1:5173";

export default defineConfig({
  testDir: "./e2e",
  testMatch: ["**/auth-02-native-*.spec.ts"],
  outputDir: "test-results/playwright-auth",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: {
    timeout: 15_000,
  },
  forbidOnly: Boolean(process.env.CI),
  reporter: process.env.CI ? [["github"], ["line"]] : [["line"]],
  use: {
    actionTimeout: 15_000,
    baseURL,
    locale: "vi-VN",
    // Traces/screenshots could capture typed passwords and OTP codes.
    screenshot: "off",
    trace: "off",
    video: "off",
  },
  projects: [
    {
      name: "local-chromium-native-auth",
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],
  webServer: {
    command: "node scripts/e2e-local.mjs serve --native-auth",
    gracefulShutdown: {
      signal: "SIGTERM",
      timeout: 15_000,
    },
    url: `${baseURL}/sign-in`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
