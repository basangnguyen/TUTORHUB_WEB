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
    label: "private-alpha isolated Render candidate",
    command: process.execPath,
    args: ["scripts/check-p519-render-candidate.mjs"],
  },
  {
    label: "private-alpha live control",
    command: process.execPath,
    args: [
      "--test",
      "scripts/check-p519-render-candidate.test.mjs",
      "scripts/p519-live-control.test.mjs",
    ],
  },
  {
    label: "private-alpha soak plan and provider preflight",
    command: pnpmCommand,
    args: [
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "p519-live-soak-plan.test.mjs",
      "p519-provider-preflight.test.mjs",
    ],
  },
  {
    label: "private-alpha static contract",
    command: process.execPath,
    args: ["scripts/check-p519-private-alpha.mjs"],
  },
  {
    label: "private-alpha report schema and fail-closed policy",
    command: process.execPath,
    args: [
      "--test",
      "scripts/p519-private-alpha-contract.test.mjs",
      "scripts/run-p519-disposable.test.mjs",
    ],
  },
  {
    label: "private-alpha inherited outage and provider-exit regression",
    command: process.execPath,
    args: ["scripts/run-p516-local.mjs"],
  },
  {
    label: "private-alpha inherited internal-canary regression",
    command: process.execPath,
    args: ["scripts/run-p518-local.mjs"],
  },
];

console.log(
  "[P5-COLLAB-19] local checks validate schema and regression only; they do not execute the provider-observed 60-minute soak.",
);

for (const step of steps) {
  console.log(`[P5-COLLAB-19] ${step.label}`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    env: environment,
    stdio: "inherit",
    windowsHide: true,
    shell: process.platform === "win32" && step.command === pnpmCommand,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

console.log("[P5-COLLAB-19] local candidate gates: PASS");
