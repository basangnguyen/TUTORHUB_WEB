import { defineConfig, devices } from "@playwright/test";

const mode = process.env.E2E_MODE?.trim() || "local";
if (mode !== "local" && mode !== "staging") {
  throw new Error("E2E_MODE must be local or staging.");
}

function stagingBaseURL() {
  const raw = process.env.E2E_BASE_URL?.trim();
  if (!raw) {
    throw new Error("E2E_BASE_URL is required when E2E_MODE=staging.");
  }
  const parsed = new URL(raw);
  if (
    parsed.protocol !== "https:" ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash ||
    parsed.pathname !== "/"
  ) {
    throw new Error(
      "Staging E2E_BASE_URL must be an HTTPS origin without credentials, query, or fragment.",
    );
  }
  return parsed.origin;
}

const baseURL = mode === "staging" ? stagingBaseURL() : "http://127.0.0.1:5173";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "test-results/playwright",
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
    baseURL,
    locale: "vi-VN",
    screenshot: "off",
    trace: "off",
    video: "off",
  },
  projects: [
    {
      name: mode === "local" ? "local-chromium" : "staging-chromium",
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],
  webServer:
    mode === "local"
      ? {
          command: "node scripts/e2e-local.mjs serve",
          url: `${baseURL}/sign-in`,
          reuseExistingServer: false,
          timeout: 180_000,
          stdout: "pipe",
          stderr: "pipe",
        }
      : undefined,
});
