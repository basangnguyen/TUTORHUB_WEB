import AxeBuilder from "@axe-core/playwright";
import { chromium } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { preview } from "vite";

const browsers = [
  { channel: "chrome", label: "Google Chrome" },
  { channel: "msedge", label: "Microsoft Edge" },
];
const requiredTools = [
  { name: "Hand (panning tool) — H", role: "radio" },
  { name: "Selection", role: "radio" },
  { name: "Rectangle", role: "radio" },
  { name: "Arrow", role: "radio" },
  { name: "Text", role: "radio" },
  { name: "Zoom out", role: "button" },
  { name: "Reset zoom", role: "button" },
  { name: "Zoom in", role: "button" },
  { name: "Undo", role: "button" },
  { name: "Redo", role: "button" },
];
const outputDirectory = resolve(
  process.cwd(),
  "../../test-results/p5-collab-01-gate-e-physical",
);
const previewServer = await preview({
  configFile: resolve(process.cwd(), "vite.excalidraw.config.ts"),
  logLevel: "silent",
  preview: { host: "127.0.0.1", port: 4190, strictPort: false },
  root: process.cwd(),
});
const baseUrl = previewServer.resolvedUrls?.local[0];
invariant(baseUrl, "physical_preview_url_missing");

await mkdir(outputDirectory, { recursive: true });
const evidence = [];

try {
  for (const browserTarget of browsers) {
    const browser = await chromium.launch({
      channel: browserTarget.channel,
      headless: false,
    });
    try {
      const context = await browser.newContext({
        locale: "vi-VN",
        reducedMotion: "no-preference",
        viewport: { height: 720, width: 1280 },
      });
      const page = await context.newPage();
      await page.goto(`${baseUrl}excalidraw.html?fixture=500`, {
        waitUntil: "networkidle",
      });

      await page.getByRole("button", { name: "Mở bảng Excalidraw" }).click();
      await page
        .getByRole("status")
        .filter({ hasText: "Excalidraw sẵn sàng với 500 đối tượng." })
        .waitFor();
      await page.waitForFunction(() =>
        globalThis.document.activeElement?.classList.contains("board-frame"),
      );

      await page.getByRole("button", { name: "Mở menu bảng vẽ" }).waitFor();
      for (const tool of requiredTools) {
        const locator = page.getByRole(tool.role, { name: tool.name });
        invariant(
          (await locator.count()) === 1,
          `${browserTarget.channel}_tool_name_missing:${tool.name}`,
        );
      }

      await page.getByRole("button", { name: "Đọc nội dung bảng" }).click();
      const semanticHeading = page.getByRole("heading", {
        name: "Nội dung bảng có thể truy cập",
      });
      await semanticHeading.waitFor();
      invariant(
        await semanticHeading.evaluate((element) => element.matches(":focus")),
        `${browserTarget.channel}_semantic_focus_missing`,
      );
      await page.getByRole("button", { name: "Trang sau" }).click();
      await page
        .getByTestId("semantic-page-status")
        .filter({ hasText: "trang 2 trên 10" })
        .waitFor();
      await page
        .getByRole("button", { name: "Chuyển tiêu điểm tới canvas" })
        .click();
      await page.waitForFunction(() =>
        globalThis.document.activeElement?.classList.contains("board-frame"),
      );

      const defaultAxe = await new AxeBuilder({ page }).analyze();
      invariant(
        defaultAxe.violations.length === 0,
        `${browserTarget.channel}_axe_default:${describeViolations(
          defaultAxe.violations,
        )}`,
      );

      await page.setViewportSize({ height: 720, width: 640 });
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
        `${browserTarget.channel}_horizontal_overflow`,
      );
      const constrainedAxe = await new AxeBuilder({ page }).analyze();
      invariant(
        constrainedAxe.violations.length === 0,
        `${browserTarget.channel}_axe_constrained:${describeViolations(
          constrainedAxe.violations,
        )}`,
      );

      await page.setViewportSize({ height: 720, width: 1280 });
      await page.emulateMedia({
        forcedColors: "none",
        reducedMotion: "no-preference",
      });
      await page.getByRole("button", { name: "Đóng bảng Excalidraw" }).click();
      const reopenButton = page.getByRole("button", {
        name: "Mở bảng Excalidraw",
      });
      invariant(
        await reopenButton.evaluate((element) => element.matches(":focus")),
        `${browserTarget.channel}_close_focus_recovery_missing`,
      );

      const screenshotPath = resolve(
        outputDirectory,
        `${browserTarget.channel}-gate-e.png`,
      );
      await page.screenshot({ fullPage: false, path: screenshotPath });
      evidence.push({
        axeDefaultViolations: defaultAxe.violations.length,
        axeForcedColorsViolations: constrainedAxe.violations.length,
        browser: browserTarget.label,
        channel: browserTarget.channel,
        cleanupFocus: "Mở bảng Excalidraw",
        semanticPage: "2/10",
        status: "PASS",
        toolCount: requiredTools.length + 1,
        version: browser.version(),
      });
      await context.close();
    } finally {
      await browser.close();
    }
  }
} finally {
  if ("closeAllConnections" in previewServer.httpServer) {
    previewServer.httpServer.closeAllConnections();
  }
  await new Promise((resolveClose, rejectClose) => {
    previewServer.httpServer.close((error) => {
      if (error) rejectClose(error);
      else resolveClose();
    });
  });
}

const result = {
  browsers: evidence,
  generatedAt: new Date().toISOString(),
  scope:
    "Installed browser role/name/focus/semantic/reflow/forced-colors automation; not a substitute for owner NVDA speech confirmation.",
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
