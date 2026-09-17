import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));

const steps = [
  {
    label: "inherited force-off guard",
    command: process.execPath,
    args: ["scripts/check-p517-force-off.mjs"],
  },
  {
    label: "inherited bounded-canary guard",
    command: process.execPath,
    args: ["scripts/check-p518-internal-canary.mjs"],
  },
  {
    label: "ramp authorization and automatic hold-point policy",
    command: process.execPath,
    args: ["--test", "scripts/p520-ramp-exit-contract.test.mjs"],
  },
];

console.log(
  "[P5-COLLAB-20] preparation checks only; no provider mutation, ramp, rollback, production, or shared staging action is executed.",
);

for (const step of steps) {
  console.log(`[P5-COLLAB-20] ${step.label}`);
  const result = spawnSync(step.command, step.args, {
    cwd: root,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

console.log(
  "[P5-COLLAB-20] preparation contract: PASS; live ramp remains authorization-gated.",
);
