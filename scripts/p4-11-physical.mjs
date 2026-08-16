import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const children = [];
let stopping = false;
const mode = process.env.P4_11_PHYSICAL_MODE?.trim() || "standard";
if (mode !== "standard" && mode !== "outage") {
  console.error("P4-11 physical harness mode must be standard or outage.");
  process.exit(2);
}

function start(command, args, cwd = root, env = process.env) {
  const child = spawn(command, args, {
    cwd,
    env,
    shell: false,
    stdio: "inherit",
    windowsHide: true,
  });
  children.push(child);
  child.once("error", () => stop(1));
  child.once("exit", (code) => {
    if (!stopping) stop(code === 0 ? 0 : 1);
  });
}

function stop(exitCode = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) {
    if (child.exitCode === null && child.signalCode === null) child.kill();
  }
  setTimeout(() => process.exit(exitCode), 500).unref();
}

process.once("SIGINT", () => stop(0));
process.once("SIGTERM", () => stop(0));

console.log("P4-11 physical harness local-only:");
console.log(
  "  Chrome / Teacher: http://127.0.0.1:5173/p4-11-physical.html?role=teacher",
);
console.log(
  "  Edge / Student:   http://127.0.0.1:5173/p4-11-physical.html?role=student",
);
console.log("Nhan Ctrl+C sau khi cleanup_zero=true. Khong in credential.");

const goArguments = [
  "run",
  "./services/core-api/cmd/p411-physical-harness",
  "--env-file",
  mode === "outage"
    ? ".env.p4-11-livekit-outage.local"
    : ".env.p4-11-livekit.local",
];
if (mode === "outage") {
  goArguments.push("--recovery-env-file", ".env.p4-11-livekit.local");
  console.log("  Mode: isolated outage with bounded recovery cleanup.");
}

start("go", goArguments, root, {
  ...process.env,
  GOCACHE: path.join(root, ".tmp-gocache"),
});
start(
  process.execPath,
  [
    path.join(root, "apps", "web", "node_modules", "vite", "bin", "vite.js"),
    "--host",
    "127.0.0.1",
    "--strictPort",
  ],
  path.join(root, "apps", "web"),
);
