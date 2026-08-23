import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const pnpm =
  process.platform === "win32"
    ? (process.env.ComSpec ?? "C:\\Windows\\System32\\cmd.exe")
    : "pnpm";
const pnpmPrefix =
  process.platform === "win32" ? ["/d", "/s", "/c", "pnpm.cmd"] : [];
const packageVitest = (packageDirectory) =>
  resolve(root, packageDirectory, "node_modules", "vitest", "vitest.mjs");

const commands = [
  {
    label: "real WebSocket 2/10/50 and 500/2,000-shape profiles",
    command: process.execPath,
    cwd: resolve(root, "apps/whiteboard-spike"),
    args: [
      packageVitest("apps/whiteboard-spike"),
      "run",
      "server/excalidrawLoadHarness.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "production runtime compaction, backpressure and cleanup soak",
    command: process.execPath,
    cwd: resolve(root, "services/whiteboard-runtime"),
    args: [
      packageVitest("services/whiteboard-runtime"),
      "run",
      "src/performanceProfile.p514.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "web production build",
    command: pnpm,
    args: [...pnpmPrefix, "--filter", "@tutorhub/web", "build"],
  },
  {
    label: "lazy whiteboard bundle guard unit tests",
    command: process.execPath,
    args: ["--test", "scripts/check-whiteboard-performance-bundle.test.mjs"],
  },
  {
    label: "lazy whiteboard production bundle budget",
    command: process.execPath,
    args: ["scripts/check-whiteboard-performance-bundle.mjs"],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-14] ${step.label}\n`);
  const result = spawnSync(step.command, step.args, {
    cwd: step.cwd ?? root,
    env: process.env,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-14] local gates PASS\n");
