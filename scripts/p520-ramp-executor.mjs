import { createHash } from "node:crypto";

import { P519_RENDER_SERVICES } from "./p519-render-sync.mjs";
import {
  P520_RAMP_EXIT_CONTRACT,
  containsP520SecretMaterial,
  evaluateP520HoldPoint,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";
import { canonicalizeP520TenantManifest } from "./p520-tenant-allowlist.mjs";

const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const DEPLOY_PATTERN = /^dep-[a-z0-9]+$/u;
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const MODE_RANK = Object.freeze({ off: 0, read_only: 1, enabled: 2 });

export const P520_EXECUTOR_CONTRACT = Object.freeze({
  schemaVersion: "p5-collab-20-executor-v1",
  target: P519_RENDER_SERVICES,
  controlEnvironmentKeys: Object.freeze([
    "P5_COLLAB_20_INITIAL_MODE",
    "P5_COLLAB_20_TENANT_IDS",
  ]),
  runtimeEnvironmentKeys: Object.freeze(["COLLAB_BUILD_ID"]),
  initialMode: "off",
  rollbackPath: Object.freeze(["read_only", "off"]),
});

function digestJson(value) {
  return createHash("sha256")
    .update(`${JSON.stringify(value)}\n`)
    .digest("hex");
}

function assertMode(mode) {
  if (!Object.hasOwn(MODE_RANK, mode)) {
    throw new Error("p520_executor_mode_invalid");
  }
  return mode;
}

function redactedTarget(binding) {
  return {
    candidateSha: binding.candidateSha,
    environment: P520_RAMP_EXIT_CONTRACT.allowedEnvironment,
    targetFingerprintSha256: binding.targetFingerprintSha256,
    tenantAllowlistSha256: binding.tenantAllowlistSha256,
    tenantCount: binding.tenantCount,
  };
}

function validateManifestTenantIds(manifest) {
  const binding = canonicalizeP520TenantManifest(manifest);
  const tenantIds = manifest.tenantIds.slice().sort();
  if (tenantIds.some((tenantId) => !UUID_PATTERN.test(tenantId))) {
    throw new Error("p520_executor_tenant_manifest_invalid");
  }
  return { ...binding, tenantIds };
}

export function createP520ExecutionBinding({
  currentCommitSha,
  expectedTargetFingerprintSha256,
  manifest,
  observation,
  packet,
  packetSha256 = digestJson(packet),
  observationSha256 = digestJson(observation),
}) {
  const evaluation = evaluateP520RampExitPlan(packet);
  if (!evaluation.ok || !evaluation.liveRampAllowed) {
    throw new Error("p520_executor_authorization_required");
  }
  if (
    !SHA_PATTERN.test(currentCommitSha ?? "") ||
    packet.target.candidateSha !== currentCommitSha
  ) {
    throw new Error("p520_executor_candidate_mismatch");
  }
  if (
    !SHA256_PATTERN.test(expectedTargetFingerprintSha256 ?? "") ||
    packet.target.targetFingerprintSha256 !== expectedTargetFingerprintSha256
  ) {
    throw new Error("p520_executor_target_mismatch");
  }
  if (
    !SHA256_PATTERN.test(packetSha256) ||
    !SHA256_PATTERN.test(observationSha256) ||
    containsP520SecretMaterial(observation)
  ) {
    throw new Error("p520_executor_input_invalid");
  }
  const manifestBinding = validateManifestTenantIds(manifest);
  if (
    manifestBinding.tenantCount !== packet.target.tenantCount ||
    manifestBinding.tenantAllowlistSha256 !==
      packet.target.tenantAllowlistSha256
  ) {
    throw new Error("p520_executor_tenant_binding_mismatch");
  }
  const decision = evaluateP520HoldPoint(observation);
  return Object.freeze({
    candidateSha: currentCommitSha,
    decision: Object.freeze({
      mode: assertMode(decision.mode),
      reasonCodes: Object.freeze(decision.reasons.slice()),
    }),
    observationSha256,
    packetSha256,
    perTenantQuotas: Object.freeze({ ...packet.target.perTenantQuotas }),
    targetFingerprintSha256: expectedTargetFingerprintSha256,
    tenantAllowlistSha256: manifestBinding.tenantAllowlistSha256,
    tenantCount: manifestBinding.tenantCount,
    tenantIds: Object.freeze(manifestBinding.tenantIds),
  });
}

export function selectP520SafeMode({
  activationAuthorized = false,
  currentMode,
  evaluatedMode,
}) {
  const current = assertMode(currentMode);
  const evaluated = assertMode(evaluatedMode);
  if (activationAuthorized) return evaluated;
  return MODE_RANK[evaluated] < MODE_RANK[current] ? evaluated : current;
}

function assertDeployReceipt(receipt) {
  if (
    !receipt ||
    !DEPLOY_PATTERN.test(receipt.controlDeployId ?? "") ||
    !DEPLOY_PATTERN.test(receipt.runtimeDeployId ?? "")
  ) {
    throw new Error("p520_executor_deploy_receipt_invalid");
  }
  return receipt;
}

function assertModeVerification(verification, expectedMode) {
  if (
    verification?.mode !== expectedMode ||
    verification?.authorityAvailable !== true ||
    (expectedMode === "off" &&
      (verification.runtimeReady !== false ||
        verification.documents !== 0 ||
        verification.editConnections !== 0)) ||
    (expectedMode !== "off" && verification.runtimeReady !== true)
  ) {
    throw new Error("p520_executor_verification_failed");
  }
  return verification;
}

export async function executeP520LiveDecision(binding, adapter) {
  if (!binding || !adapter) throw new Error("p520_executor_binding_required");
  await adapter.assertExactTarget({
    candidateSha: binding.candidateSha,
    services: P520_EXECUTOR_CONTRACT.target,
    targetFingerprintSha256: binding.targetFingerprintSha256,
  });
  const environment = {
    control: {
      P5_COLLAB_20_INITIAL_MODE: P520_EXECUTOR_CONTRACT.initialMode,
      P5_COLLAB_20_TENANT_IDS: binding.tenantIds.join(","),
    },
    runtime: { COLLAB_BUILD_ID: binding.candidateSha },
  };
  await adapter.syncEnvironment(environment);
  const deploy = assertDeployReceipt(
    await adapter.deployCandidate(binding.candidateSha),
  );
  await adapter.setMode("off");
  assertModeVerification(await adapter.verifyMode("off"), "off");
  const appliedMode = selectP520SafeMode({
    activationAuthorized: true,
    currentMode: "off",
    evaluatedMode: binding.decision.mode,
  });
  if (appliedMode !== "off") await adapter.setMode(appliedMode);
  const verification = assertModeVerification(
    await adapter.verifyMode(appliedMode),
    appliedMode,
  );
  return {
    schemaVersion: P520_EXECUTOR_CONTRACT.schemaVersion,
    outcome: "pass",
    action: "live-decision",
    appliedMode,
    reasonCodes: binding.decision.reasonCodes,
    deploy: {
      controlDeployId: deploy.controlDeployId,
      runtimeDeployId: deploy.runtimeDeployId,
    },
    target: redactedTarget(binding),
    verification: {
      authorityAvailable: verification.authorityAvailable,
      runtimeReady: verification.runtimeReady,
      documents: verification.documents,
      editConnections: verification.editConnections,
    },
    packetSha256: binding.packetSha256,
    observationSha256: binding.observationSha256,
  };
}

export async function executeP520Rollback(binding, adapter) {
  if (!binding || !adapter) throw new Error("p520_executor_binding_required");
  await adapter.assertExactTarget({
    candidateSha: binding.candidateSha,
    services: P520_EXECUTOR_CONTRACT.target,
    targetFingerprintSha256: binding.targetFingerprintSha256,
  });
  await adapter.setMode("read_only");
  assertModeVerification(await adapter.verifyMode("read_only"), "read_only");
  await adapter.setMode("off");
  const verification = assertModeVerification(
    await adapter.verifyMode("off"),
    "off",
  );
  return {
    schemaVersion: P520_EXECUTOR_CONTRACT.schemaVersion,
    outcome: "pass",
    action: "rollback",
    transitionPath: P520_EXECUTOR_CONTRACT.rollbackPath.join("->"),
    appliedMode: "off",
    target: redactedTarget(binding),
    verification: {
      authorityAvailable: verification.authorityAvailable,
      runtimeReady: false,
      documents: 0,
      editConnections: 0,
    },
    packetSha256: binding.packetSha256,
    observationSha256: binding.observationSha256,
  };
}
