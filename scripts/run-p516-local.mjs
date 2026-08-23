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

const commands = [
  {
    label: "production authority, outage, rotation and provider-exit matrix",
    command: pnpm,
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/failureProviderExit.p516.test.ts",
      "src/controlPlane.test.ts",
      "src/runtimeReadinessCoordinator.test.ts",
      "src/artifactLifecycle.p513.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "control, Neon and B2 outage regression",
    command: process.execPath,
    args: [
      "--test",
      "scripts/p5-collab-01-gate-f3-control.test.mjs",
      "scripts/p5-collab-01-gate-f3-neon-outage.test.mjs",
      "scripts/p5-collab-01-gate-f3-b2-outage.test.mjs",
    ],
  },
  {
    label: "portable export and reconnect client regression",
    command: pnpm,
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/collaboration-client",
      "exec",
      "vitest",
      "run",
      "src/browserSession.test.ts",
      "src/portableScene.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "Core API mode, grant and recovery authority regression",
    command: "go",
    args: [
      "test",
      "-count=1",
      "./services/core-api/internal/modules/collaboration",
    ],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-16] ${step.label}\n`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    env: process.env,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-16] local gates PASS\n");
