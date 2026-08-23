import AxeBuilder from "@axe-core/playwright";
import { chromium } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { createServer } from "vite";

const browsers = [
  { channel: "chrome", label: "Google Chrome" },
  { channel: "msedge", label: "Microsoft Edge" },
];
const headless = process.env.P5_COLLAB_15_HEADLESS === "1";
const outputDirectory = resolve(
  process.cwd(),
  "../../test-results/p5-collab-15-browser-matrix",
);
const server = await createServer({
  logLevel: "silent",
  root: process.cwd(),
  server: { host: "127.0.0.1", port: 5195, strictPort: false },
});
await server.listen();
const baseUrl = server.resolvedUrls?.local[0];
invariant(baseUrl, "p515_local_harness_url_missing");
await mkdir(outputDirectory, { recursive: true });
const evidence = [];

try {
  for (const target of browsers) {
    const browser = await chromium.launch({
      channel: target.channel,
      headless,
    });
    try {
      const context = await browser.newContext({
        locale: "en-US",
        reducedMotion: "no-preference",
        viewport: { height: 800, width: 1280 },
      });
      const page = await context.newPage();
      await page.goto(`${baseUrl}p5-15-physical.html`, {
        waitUntil: "networkidle",
      });

      const harnessAxe = await new AxeBuilder({ page }).analyze();
      invariant(
        harnessAxe.violations.length === 0,
        `${target.channel}_axe_harness:${describeViolations(harnessAxe.violations)}`,
      );

      const trigger = page.getByRole("button", {
        name: "Open classroom whiteboard",
      });
      await trigger.focus();
      await trigger.press("Enter");
      const dialog = page.getByRole("dialog", { name: "Classroom whiteboard" });
      await dialog.waitFor();
      invariant(
        (await page.getByRole("radio", { name: "Selection" }).count()) === 1,
        `${target.channel}_selection_tool_missing`,
      );
      for (const toolName of ["Rectangle", "Arrow", "Text"]) {
        invariant(
          (await page.getByRole("radio", { name: toolName }).count()) === 1,
          `${target.channel}_${toolName.toLowerCase()}_tool_missing`,
        );
      }

      await page
        .getByRole("button", { name: "Read whiteboard as text" })
        .click();
      const semanticHeading = page.getByRole("heading", {
        name: "Whiteboard text representation",
      });
      invariant(
        await semanticHeading.evaluate((element) => element.matches(":focus")),
        `${target.channel}_semantic_focus_missing`,
      );
      invariant(
        (await page.locator(".whiteboard-semantic-list > li").count()) === 50,
        `${target.channel}_semantic_page_size_invalid`,
      );
      await page.getByRole("button", { name: "Next page" }).click();
      await page
        .getByTestId("whiteboard-semantic-page")
        .filter({ hasText: "Page 2/2 · 55 elements" })
        .waitFor();
      invariant(
        (await page.locator(".whiteboard-semantic-list > li").count()) === 5,
        `${target.channel}_semantic_second_page_invalid`,
      );
      await page
        .getByRole("button", { name: "Move focus to drawing canvas" })
        .click();
      const canvasRegion = page.getByRole("region", {
        name: "Interactive whiteboard canvas",
      });
      invariant(
        await canvasRegion.evaluate((element) => element.matches(":focus")),
        `${target.channel}_canvas_focus_missing`,
      );

      await page.getByRole("button", { name: "Simulate reconnect" }).click();
      await page
        .getByRole("status")
        .filter({ hasText: "Restoring the whiteboard connection" })
        .waitFor();
      await page.getByRole("button", { name: "Restore connection" }).click();
      await page
        .getByRole("status")
        .filter({ hasText: "Whiteboard connected" })
        .waitFor();
      await page
        .getByRole("button", { name: "Simulate connection failure" })
        .click();
      await page
        .getByRole("status")
        .filter({ hasText: "The whiteboard connection failed" })
        .waitFor();
      await page.getByRole("button", { name: "Restore connection" }).click();

      const defaultAxe = await new AxeBuilder({ page }).analyze();
      invariant(
        defaultAxe.violations.length === 0,
        `${target.channel}_axe_default:${describeViolations(defaultAxe.violations)}`,
      );

      await page.setViewportSize({ height: 800, width: 640 });
      await page.emulateMedia({
        forcedColors: "active",
        reducedMotion: "reduce",
      });
      invariant(
        await page.evaluate(
          () =>
            globalThis.document.documentElement.scrollWidth <=
            globalThis.window.innerWidth + 1,
        ),
        `${target.channel}_horizontal_overflow`,
      );
      const constrainedAxe = await new AxeBuilder({ page }).analyze();
      invariant(
        constrainedAxe.violations.length === 0,
        `${target.channel}_axe_constrained:${describeViolations(constrainedAxe.violations)}`,
      );

      const screenshotPath = resolve(
        outputDirectory,
        `${target.channel}-production-whiteboard.png`,
      );
      await page.screenshot({ fullPage: false, path: screenshotPath });
      await page
        .getByRole("button", {
          name: "Close classroom whiteboard",
          exact: true,
        })
        .last()
        .click();
      invariant(
        await trigger.evaluate((element) => element.matches(":focus")),
        `${target.channel}_drawer_focus_recovery_missing`,
      );

      evidence.push({
        axeConstrainedViolations: constrainedAxe.violations.length,
        axeDefaultViolations: defaultAxe.violations.length,
        axeHarnessViolations: harnessAxe.violations.length,
        browser: target.label,
        channel: target.channel,
        execution: headless ? "headless-supplement" : "installed-headful",
        focusRecovery: "Open classroom whiteboard",
        semanticPage: "2/2 (50 items per page)",
        status: "PASS",
        version: browser.version(),
      });
      await context.close();
    } finally {
      await browser.close();
    }
  }
} finally {
  await server.close();
}

const result = {
  browsers: evidence,
  generatedAt: new Date().toISOString(),
  matrix: {
    "Chrome / Windows": "PASS (installed browser automation)",
    "Edge / Windows": "PASS (installed browser automation)",
    "Firefox / Windows": "UNAVAILABLE (outside private-alpha pilot matrix)",
    "Safari / macOS": "UNAVAILABLE (no physical device in this pilot)",
    "Mobile browsers": "UNAVAILABLE (desktop classroom scope)",
  },
  scope:
    "Production Drawer and CanonicalExcalidrawCanvas role/name/focus/semantic/reflow/forced-colors automation. This is not a substitute for physical 200% browser zoom or owner NVDA speech confirmation.",
};
await writeFile(
  resolve(outputDirectory, "evidence.json"),
  `${JSON.stringify(result, null, 2)}\n`,
  "utf8",
);
process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);

function invariant(condition, message) {
  if (!condition) throw new Error(message);
}

function describeViolations(violations) {
  return violations
    .map(
      (violation) =>
        `${violation.id}[${violation.nodes
          .flatMap((node) => node.target)
          .join("|")}]`,
    )
    .join(",");
}
