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
    label: "canonical convergence and actor-local history soak",
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/collaboration-client",
      "exec",
      "vitest",
      "run",
      "src/canonicalAuthority.p512.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "real WebSocket reconnect soak",
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/runtimeServer.test.ts",
      "-t",
      "P5-COLLAB-12",
      "--maxWorkers=1",
    ],
  },
  {
    label: "split-brain, session race, and checkpoint invariants",
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/providerAuthorityGuard.test.ts",
      "src/runtimeSessionRegistry.test.ts",
      "src/checkpointCompaction.test.ts",
      "--maxWorkers=1",
    ],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-12] ${step.label}\n`);
  const result = spawnSync(pnpm, step.args, {
    cwd: root,
    env: process.env,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-12] PASS\n");
