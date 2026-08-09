import { spawn } from "node:child_process";
import { access } from "node:fs/promises";
import { createServer } from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

const appRoot = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
const baseUrl = "http://127.0.0.1:4176";

async function firstExistingPath(candidates) {
  for (const candidate of candidates) {
    try {
      await access(candidate);
      return candidate;
    } catch {
      // Continue to the workspace-root fallback.
    }
  }
  throw new Error(`Không tìm thấy executable: ${candidates.join(", ")}`);
}

async function assertPreviewPortAvailable() {
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.unref();
    server.once("error", (error) => {
      reject(
        new Error(
          `Port 4176 đang được dùng; dừng server cũ trước khi chạy E2E (${error.code ?? "unknown"}).`,
        ),
      );
    });
    server.listen(4176, "127.0.0.1", () => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
  });
}

async function fetchPreview() {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 1_000);
  try {
    return await fetch(`${baseUrl}/`, {
      cache: "no-store",
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeout);
  }
}

async function waitForPreview(preview, output) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (preview.exitCode !== null) {
      throw new Error(`Vite preview thoát sớm.\n${output.join("")}`);
    }
    try {
      const response = await fetchPreview();
      if (response.ok) {
        return;
      }
    } catch {
      // Preview is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(
    `Vite preview không sẵn sàng sau 15 giây.\n${output.join("")}`,
  );
}

async function waitForExit(child, timeoutMs) {
  if (child.exitCode !== null) {
    return;
  }
  await Promise.race([
    new Promise((resolve) => child.once("exit", resolve)),
    new Promise((resolve) => setTimeout(resolve, timeoutMs)),
  ]);
}

const viteCli = await firstExistingPath([
  path.join(appRoot, "node_modules", "vite", "bin", "vite.js"),
  path.join(appRoot, "..", "..", "node_modules", "vite", "bin", "vite.js"),
]);
const playwrightCli = await firstExistingPath([
  path.join(appRoot, "node_modules", "@playwright", "test", "cli.js"),
  path.join(
    appRoot,
    "..",
    "..",
    "node_modules",
    "@playwright",
    "test",
    "cli.js",
  ),
]);

await assertPreviewPortAvailable();

const previewOutput = [];
const preview = spawn(
  process.execPath,
  [viteCli, "preview", "--host", "127.0.0.1", "--port", "4176", "--strictPort"],
  {
    cwd: appRoot,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  },
);
preview.stdout.on("data", (chunk) => previewOutput.push(String(chunk)));
preview.stderr.on("data", (chunk) => previewOutput.push(String(chunk)));

let exitCode;
try {
  await waitForPreview(preview, previewOutput);
  exitCode = await new Promise((resolve, reject) => {
    const tests = spawn(
      process.execPath,
      [playwrightCli, "test", "--config", "playwright.config.ts"],
      {
        cwd: appRoot,
        env: { ...process.env, MEDIA_UX_E2E_REUSE_SERVER: "1" },
        stdio: "inherit",
        windowsHide: true,
      },
    );
    tests.once("error", reject);
    tests.once("exit", (code) => resolve(code ?? 1));
  });
} finally {
  preview.kill();
  await waitForExit(preview, 2_000);
  if (preview.exitCode === null) {
    preview.kill("SIGKILL");
    await waitForExit(preview, 2_000);
  }
}

process.exitCode = exitCode;
