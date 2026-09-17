import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { basename, extname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  containsP520SecretMaterial,
  evaluateP520HoldPoint,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const INPUT_ROOT = resolve(ROOT, "tmp", "p5-collab-20");
const MAX_INPUT_BYTES = 64 * 1024;

function digest(value) {
  return createHash("sha256").update(value).digest("hex");
}

export function resolveP520JsonInput(inputPath) {
  if (typeof inputPath !== "string" || inputPath.trim() === "") {
    throw new Error("p520_input_path_required");
  }
  const absolutePath = resolve(ROOT, inputPath);
  const relativePath = relative(INPUT_ROOT, absolutePath);
  if (
    relativePath === "" ||
    relativePath.startsWith("..") ||
    isAbsolute(relativePath) ||
    extname(absolutePath).toLowerCase() !== ".json" ||
    basename(absolutePath).toLowerCase().startsWith(".env")
  ) {
    throw new Error("p520_input_path_outside_private_tmp");
  }
  return absolutePath;
}

function parseBoundedJson(path) {
  const raw = readFileSync(path);
  if (raw.byteLength === 0 || raw.byteLength > MAX_INPUT_BYTES) {
    throw new Error("p520_input_size_invalid");
  }
  try {
    return { raw, value: JSON.parse(raw.toString("utf8")) };
  } catch {
    throw new Error("p520_input_json_invalid");
  }
}

export function evaluateP520DryRun({
  observation,
  observationSha256,
  packet,
  packetSha256,
}) {
  const plan = evaluateP520RampExitPlan(packet);
  if (!plan.ok) {
    return {
      outcome: "blocked",
      proposedMode: "off",
      packetSha256,
      observationSha256,
      reasons: plan.errors.slice(),
    };
  }
  if (!plan.liveRampAllowed) {
    return {
      outcome: "blocked",
      proposedMode: "off",
      packetSha256,
      observationSha256,
      reasons: ["p520_live_authorization_required"],
    };
  }
  if (containsP520SecretMaterial(observation)) {
    return {
      outcome: "blocked",
      proposedMode: "off",
      packetSha256,
      observationSha256,
      reasons: ["p520_observation_contains_secret_material"],
    };
  }
  const decision = evaluateP520HoldPoint(observation);
  return {
    outcome: "dry-run-pass",
    proposedMode: decision.mode,
    packetSha256,
    observationSha256,
    reasons: decision.reasons,
    target: {
      candidateSha: packet.target.candidateSha,
      environment: packet.target.environment,
      targetFingerprintSha256: packet.target.targetFingerprintSha256,
      tenantAllowlistSha256: packet.target.tenantAllowlistSha256,
      tenantCount: packet.target.tenantCount,
    },
  };
}

export function parseP520DryRunArgs(args) {
  const values = new Map();
  for (let index = 0; index < args.length; index += 1) {
    const key = args[index];
    if (key === "--execute" || key === "--live") {
      throw new Error("p520_live_execution_not_implemented");
    }
    if (!new Set(["--packet", "--observation"]).has(key)) {
      throw new Error("p520_dry_run_usage_invalid");
    }
    const value = args[index + 1];
    if (!value || value.startsWith("--") || values.has(key)) {
      throw new Error("p520_dry_run_usage_invalid");
    }
    values.set(key, value);
    index += 1;
  }
  if (!values.has("--packet") || !values.has("--observation")) {
    throw new Error("p520_dry_run_usage_invalid");
  }
  return {
    observationPath: resolveP520JsonInput(values.get("--observation")),
    packetPath: resolveP520JsonInput(values.get("--packet")),
  };
}

export function runP520DryRunCli(args = process.argv.slice(2)) {
  const paths = parseP520DryRunArgs(args);
  const packet = parseBoundedJson(paths.packetPath);
  const observation = parseBoundedJson(paths.observationPath);
  const receipt = evaluateP520DryRun({
    observation: observation.value,
    observationSha256: digest(observation.raw),
    packet: packet.value,
    packetSha256: digest(packet.raw),
  });
  process.stdout.write(`${JSON.stringify(receipt)}\n`);
  return receipt.outcome === "dry-run-pass" ? 0 : 2;
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  try {
    process.exitCode = runP520DryRunCli();
  } catch {
    process.stderr.write(
      `${JSON.stringify({
        outcome: "blocked",
        proposedMode: "off",
        reason: "p520_dry_run_failed",
      })}\n`,
    );
    process.exitCode = 1;
  }
}
