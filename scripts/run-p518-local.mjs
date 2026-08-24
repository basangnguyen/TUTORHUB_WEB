import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const pnpmCommand = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
const environment = {
  ...process.env,
  GOCACHE: resolve(root, ".tmp-gocache"),
};

const steps = [
  {
    label: "internal-canary static guard",
    command: "node",
    args: ["scripts/check-p518-internal-canary.mjs"],
  },
  {
    label: "P5-COLLAB-17 force-off and failure regression",
    command: "node",
    args: ["scripts/run-p517-local.mjs"],
  },
  {
    label: "Core API canary allowlist configuration gate",
    command: "go",
    args: ["test", "-count=1", "./services/core-api/internal/config"],
  },
  {
    label: "Core API canary allowlist enforcement gate",
    command: "go",
    args: [
      "test",
      "-count=1",
      "./services/core-api/internal/modules/featurecontrol",
    ],
  },
  {
    label: "Core API internal-canary collaboration lifecycle gate",
    command: "go",
    args: [
      "test",
      "-count=1",
      "./services/core-api/internal/modules/collaboration",
    ],
  },
  {
    label: "Core API canary guardrail wiring gate",
    command: "go",
    args: ["test", "-count=1", "./services/core-api/cmd/api"],
  },
  {
    label: "Whiteboard runtime exact quota boundary gate",
    command: pnpmCommand,
    args: [
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/runtimePolicy.test.ts",
    ],
  },
  {
    label: "MediaSpace capability and whiteboard UI gate",
    command: pnpmCommand,
    args: [
      "--dir",
      "apps/web",
      "exec",
      "vitest",
      "run",
      "src/pages/MediaSpacePages.test.tsx",
      "src/features/collaboration/ClassroomWhiteboardTool.test.tsx",
      "--maxWorkers=1",
    ],
  },
];

for (const step of steps) {
  console.log(`[P5-COLLAB-18] ${step.label}`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    env: environment,
    stdio: "inherit",
    shell: process.platform === "win32" && step.command === pnpmCommand,
  });
  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

console.log("[P5-COLLAB-18] local internal-canary gate: PASS");
