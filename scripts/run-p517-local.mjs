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
const environment = {
  ...process.env,
  GOCACHE: resolve(root, ".tmp-gocache"),
};

const commands = [
  {
    label: "static deployment force-off guard",
    command: process.execPath,
    args: ["scripts/check-p517-force-off.mjs"],
  },
  {
    label: "Core API force-off, privacy and tenant override gates",
    command: "go",
    args: [
      "test",
      "-count=1",
      "./services/core-api/cmd/api",
      "./services/core-api/internal/httpapi",
      "./services/core-api/internal/modules/featurecontrol",
    ],
  },
  {
    label: "web force-off and error-state contract",
    command: pnpm,
    args: [
      ...pnpmPrefix,
      "--dir",
      "apps/web",
      "exec",
      "vitest",
      "run",
      "src/features/collaboration/ClassroomWhiteboardTool.test.tsx",
      "--maxWorkers=1",
    ],
  },
  {
    label: "failure, outage and provider-exit regression",
    command: process.execPath,
    args: ["scripts/run-p516-local.mjs"],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-17] ${step.label}\n`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    env: { ...environment, ...step.env },
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-17] local force-off gates PASS\n");
