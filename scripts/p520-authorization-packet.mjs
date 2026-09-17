import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import {
  basename,
  dirname,
  extname,
  isAbsolute,
  relative,
  resolve,
} from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  P520_RAMP_EXIT_CONTRACT,
  createP520PreparationPlan,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const PRIVATE_TMP_ROOT = resolve(ROOT, "tmp", "p5-collab-20");
const P519_BINDING_FILE = resolve(
  ROOT,
  "tmp",
  "p5-collab-19",
  "run-binding.json",
);
const P519_DEPLOY_FILE = resolve(
  ROOT,
  "tmp",
  "p5-collab-19",
  "render-deploy.json",
);
const MAX_INPUT_BYTES = 64 * 1024;
const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const DEPLOY_PATTERN = /^dep-[a-z0-9]+$/u;

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
  inheritedBaseline = null,
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
      inheritedBaseline,
      proposedTarget: {
        environment: P520_RAMP_EXIT_CONTRACT.allowedEnvironment,
        candidateSha: preparedFromCommitSha,
        inheritedTargetFingerprintSha256:
          inheritedBaseline?.targetFingerprintSha256 ?? null,
        inheritedDeployId: inheritedBaseline?.deployId ?? null,
        tenantAllowlistSha256: null,
        tenantCount: null,
        requiredTenantCount: P520_RAMP_EXIT_CONTRACT.initialRampTenantCount,
        perTenantQuotas: {
          ...P520_RAMP_EXIT_CONTRACT.initialPerTenantQuotaCeilings,
        },
        holdDurationSeconds: P520_RAMP_EXIT_CONTRACT.minimumHoldSeconds,
      },
      authorizationFieldsRequired: [
        "target.tenantAllowlistSha256",
        "target.tenantCount",
        "authorization.approvedBy",
        "authorization.approvedAt",
        "reviews.*.evidenceRef",
        "reviews.*.reviewedAt",
      ],
    },
  };
}

export function createP520AuthorizationPacketSummary(packet) {
  const evaluation = evaluateP520RampExitPlan(packet);
  return {
    schemaVersion: packet?.schemaVersion,
    status: packet?.status,
    preparedFromCommitSha: packet?.preparation?.preparedFromCommitSha,
    proposedCandidateSha:
      packet?.preparation?.proposedTarget?.candidateSha ?? null,
    inheritedTargetFingerprintAvailable:
      typeof packet?.preparation?.proposedTarget
        ?.inheritedTargetFingerprintSha256 === "string",
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

function parseBoundedJson(path) {
  const raw = readFileSync(path);
  if (raw.byteLength === 0 || raw.byteLength > MAX_INPUT_BYTES) {
    throw new Error("p520_inherited_artifact_size_invalid");
  }
  try {
    return JSON.parse(raw.toString("utf8"));
  } catch {
    throw new Error("p520_inherited_artifact_json_invalid");
  }
}

export function validateP520InheritedBaseline(bindingDocument, deployState) {
  const binding = bindingDocument?.binding;
  const controlDeployId = deployState?.control?.deployId;
  const runtimeDeployId = deployState?.runtime?.deployId;
  const combinedDeployId = `${controlDeployId}.${runtimeDeployId}`;
  if (
    binding?.schemaVersion !== "p5-collab-19-run-binding-v1" ||
    !SHA256_PATTERN.test(binding?.targetFingerprint ?? "") ||
    binding?.commitSha !== P520_RAMP_EXIT_CONTRACT.priorGate.candidateSha ||
    deployState?.commitSha !== binding.commitSha ||
    !DEPLOY_PATTERN.test(controlDeployId ?? "") ||
    !DEPLOY_PATTERN.test(runtimeDeployId ?? "") ||
    binding?.deployId !== combinedDeployId ||
    deployState?.deployId !== combinedDeployId ||
    deployState?.runId !== binding.runId
  ) {
    throw new Error("p520_inherited_baseline_mismatch");
  }
  return {
    sourceTask: "P5-COLLAB-19",
    candidateSha: binding.commitSha,
    targetFingerprintSha256: binding.targetFingerprint,
    deployId: combinedDeployId,
    controlDeployId,
    runtimeDeployId,
    runId: binding.runId,
    bindingGeneratedAt: binding.generatedAt,
    deployGeneratedAt: deployState.generatedAt,
  };
}

export function loadP520InheritedBaseline() {
  return validateP520InheritedBaseline(
    parseBoundedJson(P519_BINDING_FILE),
    parseBoundedJson(P519_DEPLOY_FILE),
  );
}

export function resolveP520PacketOutput(outputPath) {
  if (typeof outputPath !== "string" || outputPath.trim() === "") {
    throw new Error("p520_packet_output_required");
  }
  const absolutePath = resolve(ROOT, outputPath);
  const relativePath = relative(PRIVATE_TMP_ROOT, absolutePath);
  if (
    relativePath === "" ||
    relativePath.startsWith("..") ||
    isAbsolute(relativePath) ||
    extname(absolutePath).toLowerCase() !== ".json" ||
    basename(absolutePath).toLowerCase().startsWith(".env")
  ) {
    throw new Error("p520_packet_output_outside_private_tmp");
  }
  return absolutePath;
}

export function assertP520PacketOutputAvailable(path, exists = existsSync) {
  if (exists(path)) throw new Error("p520_packet_output_exists");
  return path;
}

function writePrivatePacket(path, packet) {
  assertP520PacketOutputAvailable(path);
  mkdirSync(dirname(path), { recursive: true });
  const realWorkspace = realpathSync(ROOT);
  const realPrivateRoot = realpathSync(PRIVATE_TMP_ROOT);
  const realOutputDirectory = realpathSync(dirname(path));
  const privateRootFromWorkspace = relative(realWorkspace, realPrivateRoot);
  const outputFromPrivateRoot = relative(realPrivateRoot, realOutputDirectory);
  if (
    privateRootFromWorkspace.startsWith("..") ||
    isAbsolute(privateRootFromWorkspace) ||
    outputFromPrivateRoot.startsWith("..") ||
    isAbsolute(outputFromPrivateRoot)
  ) {
    throw new Error("p520_packet_output_realpath_invalid");
  }
  const temporaryPath = `${path}.${process.pid}.tmp`;
  writeFileSync(temporaryPath, `${JSON.stringify(packet, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
    flag: "wx",
  });
  renameSync(temporaryPath, path);
}

export function runP520AuthorizationPacketCli(args = process.argv.slice(2)) {
  const stdout = args.length === 1 && args[0] === "--stdout";
  const output =
    args.length === 2 && args[0] === "--output"
      ? resolveP520PacketOutput(args[1])
      : null;
  if (!stdout && output === null) {
    throw new Error("p520_packet_usage_invalid");
  }
  const packet = createP520AuthorizationPacket({
    inheritedBaseline: loadP520InheritedBaseline(),
  });
  const evaluation = evaluateP520RampExitPlan(packet);
  if (!evaluation.ok || evaluation.liveRampAllowed) {
    throw new Error("p520_preparation_packet_invalid");
  }
  if (stdout) {
    process.stdout.write(`${JSON.stringify(packet, null, 2)}\n`);
  } else {
    writePrivatePacket(output, packet);
    const summary = createP520AuthorizationPacketSummary(packet);
    process.stdout.write(
      `${JSON.stringify({
        ...summary,
        file: relative(ROOT, output).replaceAll("\\", "/"),
        outcome: "pass",
      })}\n`,
    );
  }
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
