import { defineConfig, devices } from "@playwright/test";
import { randomBytes } from "node:crypto";

/**
 * AUTH-02B/03B/04B native sign-up, password, email-code login, recovery,
 * step-up re-authentication and device/session management E2E, plus AUTH-05B
 * Google federation against a loopback fake issuer (never Google itself),
 * AUTH-06B Facebook federation against a third fake that serves Meta's fixed
 * Login paths (never Meta itself), and AUTH-07B Microsoft federation against
 * a fourth fake Microsoft identity platform authority (never Microsoft itself),
 * and AUTH-08B passkeys through a CDP virtual authenticator. It only targets
 * the loopback stack: the `devinbox` OTP sender and `/api/v1/dev/auth-inbox`
 * exist solely for APP_ENV=test on 127.0.0.1, so staging mode is
 * intentionally unsupported.
 *
 * WebAuthn rejects an IP address as relying-party ID and only allows plain
 * HTTP on `localhost`, so this stack's browser-facing origin is
 * http://localhost:5173 (`scripts/e2e-local.mjs` publishes the same origins).
 * The servers still bind 127.0.0.1: readiness uses that address, and Chromium
 * maps `localhost` straight to 127.0.0.1 so no request waits on an IPv6
 * (::1) attempt first.
 */
const mode = process.env.E2E_MODE?.trim() || "local";
if (mode !== "local") {
  throw new Error("playwright.auth.config.ts only supports E2E_MODE=local.");
}

const baseURL = "http://localhost:5173";
const boundWebOrigin = "http://127.0.0.1:5173";

// One random Facebook app secret per run: the stack's API verifies Meta-style
// `signed_request` callbacks with it and the spec signs them. Playwright loads
// this config in every worker; `??=` keeps the runner's value, which workers
// and the web server inherit through the environment.
process.env.E2E_FACEBOOK_CLIENT_SECRET ??=
  randomBytes(32).toString("base64url");

export default defineConfig({
  testDir: "./e2e",
  testMatch: [
    "**/auth-0[23]-native-*.spec.ts",
    "**/auth-04-*.spec.ts",
    "**/auth-05-*.spec.ts",
    "**/auth-06-*.spec.ts",
    "**/auth-07-*.spec.ts",
    "**/auth-08-*.spec.ts",
    "**/auth-09-*.spec.ts",
  ],
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
        launchOptions: {
          args: ["--host-resolver-rules=MAP localhost 127.0.0.1"],
        },
      },
    },
  ],
  webServer: {
    command: "node scripts/e2e-local.mjs serve --native-auth",
    gracefulShutdown: {
      signal: "SIGTERM",
      timeout: 15_000,
    },
    url: `${boundWebOrigin}/sign-in`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
