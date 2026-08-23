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
    label: "immutable round-trip, quarantine, outage recovery and stale fence",
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/artifactLifecycle.p513.test.ts",
      "--maxWorkers=1",
    ],
  },
  {
    label: "production envelope, object-store and worker regression",
    args: [
      ...pnpmPrefix,
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/artifactEnvelope.test.ts",
      "src/artifactObjectStore.test.ts",
      "src/artifactWorker.test.ts",
      "--maxWorkers=1",
    ],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-13] ${step.label}\n`);
  const result = spawnSync(pnpm, step.args, {
    cwd: root,
    env: process.env,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-13] local gates PASS\n");
