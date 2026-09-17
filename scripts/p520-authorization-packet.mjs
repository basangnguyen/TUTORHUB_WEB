import { execFileSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  createP520PreparationPlan,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const SHA_PATTERN = /^[a-f0-9]{40}$/u;

function currentCommitSha() {
  const sha = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: ROOT,
    encoding: "utf8",
    windowsHide: true,
  }).trim();
  if (!SHA_PATTERN.test(sha)) throw new Error("p520_source_commit_invalid");
  return sha;
}

export function createP520AuthorizationPacket({
  generatedAt = new Date().toISOString(),
  preparedFromCommitSha = currentCommitSha(),
} = {}) {
  if (!SHA_PATTERN.test(preparedFromCommitSha)) {
    throw new Error("p520_source_commit_invalid");
  }
  if (!Number.isFinite(Date.parse(generatedAt))) {
    throw new Error("p520_generated_at_invalid");
  }
  return {
    ...createP520PreparationPlan(),
    preparation: {
      generatedAt,
      preparedFromCommitSha,
      secretMaterialAllowed: false,
      executionMode: "dry-run-only",
    },
  };
}

export function createP520AuthorizationPacketSummary(packet) {
  const evaluation = evaluateP520RampExitPlan(packet);
  return {
    schemaVersion: packet?.schemaVersion,
    status: packet?.status,
    preparedFromCommitSha: packet?.preparation?.preparedFromCommitSha,
    liveRampAllowed: evaluation.liveRampAllowed,
    providerMutationAuthorized:
      packet?.posture?.providerMutationAuthorized === true,
    productionAuthorized: packet?.posture?.productionAuthorized === true,
    sharedStagingAuthorized: packet?.posture?.sharedStagingAuthorized === true,
    targetBound:
      typeof packet?.target?.targetFingerprintSha256 === "string" &&
      typeof packet?.target?.tenantAllowlistSha256 === "string",
    valid: evaluation.ok,
  };
}

export function runP520AuthorizationPacketCli(args = process.argv.slice(2)) {
  if (args.length > 1 || (args.length === 1 && args[0] !== "--stdout")) {
    throw new Error("p520_packet_usage_invalid");
  }
  const packet = createP520AuthorizationPacket();
  const evaluation = evaluateP520RampExitPlan(packet);
  if (!evaluation.ok || evaluation.liveRampAllowed) {
    throw new Error("p520_preparation_packet_invalid");
  }
  process.stdout.write(`${JSON.stringify(packet, null, 2)}\n`);
  return 0;
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  try {
    process.exitCode = runP520AuthorizationPacketCli();
  } catch {
    process.stderr.write(
      `${JSON.stringify({
        outcome: "blocked",
        reason: "p520_authorization_packet_generation_failed",
      })}\n`,
    );
    process.exitCode = 1;
  }
}
