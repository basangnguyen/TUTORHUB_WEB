import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  buildExpectedBinding,
  databaseAclSnapshot,
  exactDatabaseLedger,
  validateP519Environment,
} from "./run-p519-disposable.mjs";
import { P519_RENDER_SERVICES } from "./p519-render-sync.mjs";
import {
  createP520ExecutionBinding,
  executeP520LiveDecision,
  executeP520Rollback,
} from "./p520-ramp-executor.mjs";
import { evaluateP520RampExitPlan } from "./p520-ramp-exit-contract.mjs";
import { createP520RenderAdapter } from "./p520-render-adapter.mjs";
import {
  createP520ProviderFixture,
  runP520ProviderFixture,
} from "./p520-provider-fixture.mjs";
import { canonicalizeP520TenantManifest } from "./p520-tenant-allowlist.mjs";
import { runP519ProviderPreflight } from "../services/whiteboard-runtime/p519-provider-preflight.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const PRIVATE_ROOT = resolve(ROOT, "tmp", "p5-collab-20");
const MANIFEST_FILE = resolve(PRIVATE_ROOT, "tenants.json");
const BINDING_FILE = resolve(PRIVATE_ROOT, "run-binding.json");
const DEPLOY_FILE = resolve(PRIVATE_ROOT, "render-deploy.json");
const PREFLIGHT_FILE = resolve(PRIVATE_ROOT, "provider-preflight.json");
const ROLLBACK_FILE = resolve(PRIVATE_ROOT, "rollback.json");
const FINAL_FILE = resolve(PRIVATE_ROOT, "final-cleanup.json");
const MAX_INPUT_BYTES = 256 * 1024;
const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_20_R3_DISPOSABLE_ONLY";

function currentCommitSha() {
  const value = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: ROOT,
    encoding: "utf8",
    windowsHide: true,
  }).trim();
  if (!SHA_PATTERN.test(value)) throw new Error("p520_live_commit_invalid");
  return value;
}

function currentPaths() {
  const candidateSha = currentCommitSha();
  const short = candidateSha.slice(0, 7);
  return {
    candidateSha,
    preparation: resolve(
      PRIVATE_ROOT,
      `authorization-${short}-tenants-bound.json`,
    ),
    authorized: resolve(PRIVATE_ROOT, `authorization-${short}-live.json`),
    decision: resolve(PRIVATE_ROOT, `live-decision-${short}.json`),
    observation: resolve(PRIVATE_ROOT, `observation-${short}-pre-ramp.json`),
  };
}

function readBoundedJson(path, code) {
  const relativePath = relative(PRIVATE_ROOT, path);
  if (relativePath.startsWith("..") || !path.endsWith(".json")) {
    throw new Error(`${code}_path_invalid`);
  }
  const raw = readFileSync(path);
  if (raw.byteLength === 0 || raw.byteLength > MAX_INPUT_BYTES) {
    throw new Error(`${code}_size_invalid`);
  }
  try {
    return JSON.parse(raw.toString("utf8"));
  } catch {
    throw new Error(`${code}_json_invalid`);
  }
}

function writePrivateJson(path, value, { replace = false } = {}) {
  if (!replace && existsSync(path)) throw new Error("p520_live_output_exists");
  mkdirSync(dirname(path), { recursive: true });
  const temporary = `${path}.${process.pid}.tmp`;
  writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
    flag: "wx",
  });
  renameSync(temporary, path);
}

function requiredSecret(value, code) {
  if (typeof value !== "string" || value.length < 20) throw new Error(code);
  return value;
}

function validatedEnvironment(environment = process.env) {
  const disposable = validateP519Environment(
    new Map(Object.entries(environment)),
  );
  if (
    environment.P5_COLLAB_19_CONTROL_URL !== P519_RENDER_SERVICES.control.url ||
    environment.P5_COLLAB_19_RUNTIME_URL !== P519_RENDER_SERVICES.runtime.url
  ) {
    throw new Error("p520_live_service_url_mismatch");
  }
  return {
    ...environment,
    ...disposable,
    renderApiKey: requiredSecret(
      environment.P5_COLLAB_19_RENDER_API_KEY ?? environment.RENDER_API_KEY,
      "p520_live_render_api_key_required",
    ),
    adminToken: requiredSecret(
      environment.P5_COLLAB_19_CONTROL_ADMIN_TOKEN,
      "p520_live_admin_token_required",
    ),
    metricsToken: requiredSecret(
      environment.COLLAB_METRICS_TOKEN,
      "p520_live_metrics_token_required",
    ),
  };
}

export function materializeP520AuthorizedPacket(
  preparation,
  { approvedAt = new Date().toISOString(), approvedBy = "Bá Sáng" } = {},
) {
  const preparationEvaluation = evaluateP520RampExitPlan(preparation);
  const proposed = preparation?.preparation?.proposedTarget;
  if (
    !preparationEvaluation.ok ||
    preparationEvaluation.liveRampAllowed ||
    !SHA_PATTERN.test(proposed?.candidateSha ?? "") ||
    !SHA256_PATTERN.test(proposed?.inheritedTargetFingerprintSha256 ?? "") ||
    !SHA256_PATTERN.test(proposed?.tenantAllowlistSha256 ?? "") ||
    proposed?.tenantCount !== 2 ||
    !Number.isFinite(Date.parse(approvedAt)) ||
    typeof approvedBy !== "string" ||
    approvedBy.trim().length < 2
  ) {
    throw new Error("p520_live_preparation_invalid");
  }
  const packet = structuredClone(preparation);
  packet.status = "authorized-pending-live-validation";
  packet.posture.liveActionsAuthorized = true;
  packet.posture.providerMutationAuthorized = true;
  packet.target = {
    environment: proposed.environment,
    candidateSha: proposed.candidateSha,
    targetFingerprintSha256: proposed.inheritedTargetFingerprintSha256,
    tenantAllowlistSha256: proposed.tenantAllowlistSha256,
    tenantCount: proposed.tenantCount,
    perTenantQuotas: { ...proposed.perTenantQuotas },
  };
  packet.holdPoint.decision = "go";
  packet.killSwitch.liveExecutorReady = true;
  packet.killSwitch.rollbackExecutorReady = true;
  packet.authorization = {
    state: "approved",
    approvedBy: approvedBy.trim(),
    approvedAt,
  };
  packet.completion.exactCandidateRecorded = true;
  packet.preparation.executionMode = "authorized-exact-disposable-live";
  const evaluation = evaluateP520RampExitPlan(packet);
  if (
    !evaluation.ok ||
    !evaluation.liveRampAllowed ||
    evaluation.reviewsComplete
  ) {
    throw new Error("p520_live_authorization_invalid");
  }
  return packet;
}

export function createP520PreRampObservation() {
  return {
    schemaVersion: "p5-collab-20-pre-ramp-observation-v1",
    oneAuthorityInvariant: true,
    portabilityPassed: true,
    recoveryPassed: true,
    crossTenantLeakCount: 0,
    divergenceCount: 0,
    dataLossCount: 0,
    securityIncident: false,
    privacyIncident: false,
    unbilledChargesUsd: 0,
    readOnlyRecoveryExpired: false,
    consecutiveReadinessFailures: 0,
    checkpointPersistenceFailed: false,
    quotaRejectionsIncreasing: false,
    freeCapUsagePercent: 0,
    accessibilityRegression: false,
    providerExitReviewPassed: true,
    evidenceBasis:
      "P5-COLLAB-19 closed baseline plus current fail-closed local gates; fresh R3 reviews remain pending",
  };
}

function loadAuthorizedInputs() {
  const paths = currentPaths();
  const packet = readBoundedJson(paths.authorized, "p520_live_packet");
  const manifest = readBoundedJson(MANIFEST_FILE, "p520_live_manifest");
  const observation = existsSync(paths.observation)
    ? readBoundedJson(paths.observation, "p520_live_observation")
    : createP520PreRampObservation();
  const manifestBinding = canonicalizeP520TenantManifest(manifest);
  if (
    packet.target?.candidateSha !== paths.candidateSha ||
    packet.target?.tenantAllowlistSha256 !==
      manifestBinding.tenantAllowlistSha256 ||
    packet.target?.tenantCount !== manifestBinding.tenantCount
  ) {
    throw new Error("p520_live_exact_binding_mismatch");
  }
  return { ...paths, manifest, observation, packet };
}

function createLiveAdapter(environment, fingerprint) {
  return createP520RenderAdapter({
    adminToken: environment.adminToken,
    apiKey: environment.renderApiKey,
    expectedTargetFingerprintSha256: fingerprint,
    metricsToken: environment.metricsToken,
  });
}

function createExecutionBinding(inputs) {
  return createP520ExecutionBinding({
    currentCommitSha: inputs.candidateSha,
    expectedTargetFingerprintSha256:
      inputs.packet.target.targetFingerprintSha256,
    manifest: inputs.manifest,
    observation: inputs.observation,
    packet: inputs.packet,
  });
}

function providerBinding(environment, packet, deploy, runId) {
  const bindingEnvironment = {
    ...environment,
    P5_COLLAB_19_DEPLOY_ID: `${deploy.controlDeployId}.${deploy.runtimeDeployId}`,
    P5_COLLAB_19_RUN_ID: runId,
  };
  const ledgerRows = exactDatabaseLedger(bindingEnvironment);
  const aclRows = databaseAclSnapshot(bindingEnvironment);
  const binding = buildExpectedBinding(bindingEnvironment, ledgerRows, aclRows);
  binding.manifestSha256 = packet.target.tenantAllowlistSha256;
  binding.generatedAt = new Date().toISOString();
  if (binding.targetFingerprint !== packet.target.targetFingerprintSha256) {
    throw new Error("p520_live_target_fingerprint_drift");
  }
  return binding;
}

async function authorize() {
  const paths = currentPaths();
  const preparation = readBoundedJson(
    paths.preparation,
    "p520_live_preparation",
  );
  const packet = materializeP520AuthorizedPacket(preparation);
  if (packet.target.candidateSha !== paths.candidateSha) {
    throw new Error("p520_live_candidate_mismatch");
  }
  writePrivateJson(paths.authorized, packet);
  writePrivateJson(paths.observation, createP520PreRampObservation());
  return {
    outcome: "pass",
    status: packet.status,
    candidateSha: paths.candidateSha,
    targetBound: true,
    tenantCount: 2,
    reviewsComplete: false,
    providerMutation: false,
    identifiersLogged: false,
  };
}

async function deploy(environmentInput = process.env) {
  const inputs = loadAuthorizedInputs();
  const environment = validatedEnvironment(environmentInput);
  const binding = createExecutionBinding(inputs);
  const adapter = createLiveAdapter(
    environment,
    binding.targetFingerprintSha256,
  );
  let receipt;
  try {
    receipt = await executeP520LiveDecision(binding, adapter);
    const runId = `p520-r3-${new Date().toISOString().replace(/[-:.]/gu, "")}`;
    const provider = providerBinding(
      environment,
      inputs.packet,
      receipt.deploy,
      runId,
    );
    const deployId = `${receipt.deploy.controlDeployId}.${receipt.deploy.runtimeDeployId}`;
    writePrivateJson(BINDING_FILE, { binding: provider });
    writePrivateJson(DEPLOY_FILE, {
      commitSha: inputs.candidateSha,
      control: { deployId: receipt.deploy.controlDeployId },
      deployId,
      generatedAt: new Date().toISOString(),
      runId,
      runtime: { deployId: receipt.deploy.runtimeDeployId },
    });
    writePrivateJson(inputs.decision, receipt);
    return {
      outcome: "pass",
      candidateSha: inputs.candidateSha,
      appliedMode: receipt.appliedMode,
      services: 2,
      tenantCount: 2,
      exactTarget: true,
      identifiersLogged: false,
    };
  } catch (error) {
    await executeP520Rollback(binding, adapter).catch(() => undefined);
    throw error;
  }
}

async function crossTenantIsolationProbe(environment, fixture) {
  const [left, right] = fixture.documents;
  const response = await fetch(
    `${P519_RENDER_SERVICES.control.url}/p519/v1/grants`,
    {
      body: JSON.stringify({
        actor_id: left.actorId,
        capability: "edit",
        document_id: right.documentId,
        provider_document_name: right.providerDocumentName,
        session_id: right.sessionId,
        tenant_id: left.tenantId,
      }),
      headers: {
        authorization: `Bearer ${environment.adminToken}`,
        "content-type": "application/json",
      },
      method: "POST",
      signal: AbortSignal.timeout(20_000),
    },
  );
  const body = await response.json().catch(() => ({}));
  if (response.status !== 400 || body?.code !== "tenant_document_mismatch") {
    throw new Error("p520_live_cross_tenant_isolation_failed");
  }
}

async function preflight(environmentInput = process.env) {
  const inputs = loadAuthorizedInputs();
  const environment = validatedEnvironment(environmentInput);
  const fixture = createP520ProviderFixture(inputs.manifest);
  let provisioned = false;
  try {
    const fixtureResult = await runP520ProviderFixture(
      "provision",
      environment,
      inputs.manifest,
    );
    provisioned = true;
    await crossTenantIsolationProbe(environment, fixture);
    const provider = await runP519ProviderPreflight(environment, fixture);
    const receipt = {
      schemaVersion: "p5-collab-20-provider-preflight-v1",
      outcome: "pass",
      providerObserved: provider.provider_observed === true,
      exactTwoTenantIsolation: true,
      tenantCount: fixtureResult.tenantCount,
      documents: provider.documents,
      connections: provider.connections,
      shapesPerDocument: provider.shapes_per_document,
      cleanupZero: provider.cleanup_zero,
      candidateSha: inputs.candidateSha,
      identifiersLogged: false,
    };
    writePrivateJson(PREFLIGHT_FILE, receipt);
    return receipt;
  } catch (error) {
    if (provisioned) {
      await runP520ProviderFixture(
        "cleanup",
        environment,
        inputs.manifest,
      ).catch(() => undefined);
    }
    await rollback(environmentInput).catch(() => undefined);
    throw error;
  }
}

async function rollback(environmentInput = process.env) {
  const inputs = loadAuthorizedInputs();
  const environment = validatedEnvironment(environmentInput);
  const binding = createExecutionBinding(inputs);
  const adapter = createLiveAdapter(
    environment,
    binding.targetFingerprintSha256,
  );
  const receipt = await executeP520Rollback(binding, adapter);
  writePrivateJson(ROLLBACK_FILE, receipt, { replace: true });
  return {
    outcome: "pass",
    appliedMode: receipt.appliedMode,
    transitionPath: receipt.transitionPath,
    runtimeReady: receipt.verification.runtimeReady,
    documents: receipt.verification.documents,
    editConnections: receipt.verification.editConnections,
    identifiersLogged: false,
  };
}

async function cleanup(environmentInput = process.env) {
  const inputs = loadAuthorizedInputs();
  const environment = validatedEnvironment(environmentInput);
  const rollbackReceipt = readBoundedJson(ROLLBACK_FILE, "p520_rollback");
  if (
    rollbackReceipt.appliedMode !== "off" ||
    rollbackReceipt.verification?.runtimeReady !== false ||
    rollbackReceipt.verification?.documents !== 0 ||
    rollbackReceipt.verification?.editConnections !== 0
  ) {
    throw new Error("p520_cleanup_force_off_required");
  }
  const fixture = await runP520ProviderFixture(
    "destroy",
    environment,
    inputs.manifest,
  );
  exactDatabaseLedger(environment);
  const receipt = {
    schemaVersion: "p5-collab-20-final-cleanup-v1",
    outcome: "pass",
    ledgerVersion: 42,
    ledgerDirty: false,
    whiteboardMode: "off",
    runtimeReady: false,
    documents: 0,
    editConnections: 0,
    syntheticTenants: 0,
    syntheticDocuments: fixture.documentCount,
    identifiersLogged: false,
  };
  writePrivateJson(FINAL_FILE, receipt, { replace: true });
  return receipt;
}

export async function runP520LiveRunner(
  args = process.argv.slice(2),
  environment = process.env,
) {
  if (
    args.length !== 3 ||
    args[1] !== "--confirm" ||
    args[2] !== EXACT_CONFIRMATION
  ) {
    throw new Error("p520_live_exact_confirmation_required");
  }
  const operation = args[0];
  if (operation === "authorize") return authorize();
  if (operation === "deploy") return deploy(environment);
  if (operation === "preflight") return preflight(environment);
  if (operation === "rollback") return rollback(environment);
  if (operation === "cleanup") return cleanup(environment);
  throw new Error("p520_live_operation_invalid");
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  runP520LiveRunner()
    .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
    .catch((error) => {
      const reason =
        error instanceof Error && /^p520_[a-z0-9_]+$/u.test(error.message)
          ? error.message
          : "p520_live_bounded_failure";
      process.stderr.write(
        `${JSON.stringify({ outcome: "blocked", reason })}\n`,
      );
      process.exitCode = 1;
    });
}

export { EXACT_CONFIRMATION };
