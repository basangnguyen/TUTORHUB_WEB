import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const goCache = resolve(root, ".tmp-gocache");
mkdirSync(goCache, { recursive: true });

const commands = [
  {
    label: "credential and revoke broker",
    command: "go",
    args: [
      "test",
      "-count=1",
      "-run",
      "^(TestP511|TestMemoryGrantBroker|TestService.*Grant)",
      "./services/core-api/internal/modules/collaboration",
    ],
  },
  {
    label: "WebSocket authority and abuse bounds",
    command:
      process.platform === "win32"
        ? (process.env.ComSpec ?? "C:\\Windows\\System32\\cmd.exe")
        : "pnpm",
    args: [
      ...(process.platform === "win32" ? ["/d", "/s", "/c", "pnpm.cmd"] : []),
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/runtimeServer.test.ts",
      "src/rawWebSocketIngressGate.test.ts",
      "src/rawHocuspocusAwareness.test.ts",
      "src/runtimePolicy.test.ts",
      "src/runtimeDocumentBudget.test.ts",
      "--maxWorkers=1",
    ],
  },
];

for (const step of commands) {
  process.stdout.write(`[P5-COLLAB-11] ${step.label}\n`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    env: { ...process.env, GOCACHE: goCache },
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

process.stdout.write("[P5-COLLAB-11] PASS\n");
