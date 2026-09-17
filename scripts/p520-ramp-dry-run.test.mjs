import assert from "node:assert/strict";
import test from "node:test";

import {
  P520_RAMP_EXIT_CONTRACT,
  createP520PreparationPlan,
} from "./p520-ramp-exit-contract.mjs";
import {
  assertP520PacketOutputAvailable,
  createP520AuthorizationPacket,
  createP520AuthorizationPacketSummary,
  resolveP520PacketOutput,
  validateP520InheritedBaseline,
} from "./p520-authorization-packet.mjs";
import {
  evaluateP520DryRun,
  parseP520DryRunArgs,
  resolveP520JsonInput,
} from "./p520-ramp-dry-run.mjs";

const HASH_A = "a".repeat(64);
const HASH_B = "b".repeat(64);

function healthyObservation() {
  return {
    oneAuthorityInvariant: true,
    portabilityPassed: true,
    recoveryPassed: true,
    crossTenantLeakCount: 0,
    divergenceCount: 0,
    dataLossCount: 0,
    unbilledChargesUsd: 0,
    consecutiveReadinessFailures: 0,
    freeCapUsagePercent: 20,
    providerExitReviewPassed: true,
  };
}

function authorizedPacket() {
  const packet = createP520PreparationPlan();
  packet.status = "authorized";
  packet.posture.liveActionsAuthorized = true;
  packet.posture.providerMutationAuthorized = true;
  packet.target = {
    environment: P520_RAMP_EXIT_CONTRACT.allowedEnvironment,
    candidateSha: "c".repeat(40),
    targetFingerprintSha256: "d".repeat(64),
    tenantAllowlistSha256: "e".repeat(64),
    tenantCount: 2,
    perTenantQuotas: {
      ...P520_RAMP_EXIT_CONTRACT.initialPerTenantQuotaCeilings,
    },
  };
  packet.holdPoint.decision = "go";
  packet.killSwitch.liveExecutorReady = true;
  packet.killSwitch.rollbackExecutorReady = true;
  packet.authorization = {
    state: "approved",
    approvedBy: "owner",
    approvedAt: "2026-09-17T10:00:00.000Z",
  };
  for (const review of Object.values(packet.reviews)) {
    review.state = "passed";
    review.evidenceRef = "evidence/provider-observed";
    review.reviewedAt = "2026-09-17T09:59:00.000Z";
  }
  packet.completion.exactCandidateRecorded = true;
  return packet;
}

test("generated authorization packet is valid preparation and remains closed", () => {
  const packet = createP520AuthorizationPacket({
    generatedAt: "2026-09-17T10:00:00.000Z",
    preparedFromCommitSha: "f".repeat(40),
  });
  const summary = createP520AuthorizationPacketSummary(packet);
  assert.deepEqual(summary, {
    schemaVersion: P520_RAMP_EXIT_CONTRACT.schemaVersion,
    status: "preparation-only",
    preparedFromCommitSha: "f".repeat(40),
    proposedCandidateSha: "f".repeat(40),
    inheritedTargetFingerprintAvailable: false,
    liveRampAllowed: false,
    providerMutationAuthorized: false,
    productionAuthorized: false,
    sharedStagingAuthorized: false,
    targetBound: false,
    valid: true,
  });
});

test("P5-19 binding and deploy artifacts produce an allowlisted baseline", () => {
  const binding = {
    binding: {
      schemaVersion: "p5-collab-19-run-binding-v1",
      targetFingerprint: "1".repeat(64),
      commitSha: P520_RAMP_EXIT_CONTRACT.priorGate.candidateSha,
      deployId: "dep-control.dep-runtime",
      runId: "p519-private-alpha-test",
      generatedAt: "2026-09-16T03:54:37.936Z",
    },
  };
  const deploy = {
    commitSha: P520_RAMP_EXIT_CONTRACT.priorGate.candidateSha,
    deployId: "dep-control.dep-runtime",
    runId: "p519-private-alpha-test",
    generatedAt: "2026-09-16T03:54:37.941Z",
    control: { deployId: "dep-control" },
    runtime: { deployId: "dep-runtime" },
  };
  assert.deepEqual(validateP520InheritedBaseline(binding, deploy), {
    sourceTask: "P5-COLLAB-19",
    candidateSha: P520_RAMP_EXIT_CONTRACT.priorGate.candidateSha,
    targetFingerprintSha256: "1".repeat(64),
    deployId: "dep-control.dep-runtime",
    controlDeployId: "dep-control",
    runtimeDeployId: "dep-runtime",
    runId: "p519-private-alpha-test",
    bindingGeneratedAt: "2026-09-16T03:54:37.936Z",
    deployGeneratedAt: "2026-09-16T03:54:37.941Z",
  });
  deploy.runId = "different-run";
  assert.throws(
    () => validateP520InheritedBaseline(binding, deploy),
    /inherited_baseline_mismatch/u,
  );
});

test("packet output is confined to a new JSON file below private tmp", () => {
  assert.match(
    resolveP520PacketOutput("tmp/p5-collab-20/authorization.json"),
    /tmp[\\/]p5-collab-20[\\/]authorization\.json$/u,
  );
  assert.throws(
    () => resolveP520PacketOutput("tmp/p5-collab-20/.env.local"),
    /outside_private_tmp/u,
  );
  assert.throws(
    () => resolveP520PacketOutput("tmp/outside.json"),
    /outside_private_tmp/u,
  );
});

test("packet materializer refuses to overwrite an existing file", () => {
  assert.equal(
    assertP520PacketOutputAvailable("authorization.json", () => false),
    "authorization.json",
  );
  assert.throws(
    () => assertP520PacketOutputAvailable("authorization.json", () => true),
    /packet_output_exists/u,
  );
});

test("dry-run blocks an unapproved preparation packet and proposes off", () => {
  const receipt = evaluateP520DryRun({
    packet: createP520PreparationPlan(),
    packetSha256: HASH_A,
    observation: healthyObservation(),
    observationSha256: HASH_B,
  });
  assert.equal(receipt.outcome, "blocked");
  assert.equal(receipt.proposedMode, "off");
  assert.deepEqual(receipt.reasons, ["p520_live_authorization_required"]);
  assert.equal(Object.hasOwn(receipt, "observation"), false);
});

test("authorized healthy packet produces a redacted dry-run receipt", () => {
  const receipt = evaluateP520DryRun({
    packet: authorizedPacket(),
    packetSha256: HASH_A,
    observation: healthyObservation(),
    observationSha256: HASH_B,
  });
  assert.equal(receipt.outcome, "dry-run-pass");
  assert.equal(receipt.proposedMode, "enabled");
  assert.equal(receipt.target.tenantCount, 2);
  assert.equal(Object.hasOwn(receipt, "authorization"), false);
  assert.equal(Object.hasOwn(receipt, "reviews"), false);
});

test("critical observation proposes force-off without echoing input", () => {
  const receipt = evaluateP520DryRun({
    packet: authorizedPacket(),
    packetSha256: HASH_A,
    observation: { ...healthyObservation(), dataLossCount: 1 },
    observationSha256: HASH_B,
  });
  assert.equal(receipt.outcome, "dry-run-pass");
  assert.equal(receipt.proposedMode, "off");
  assert.deepEqual(receipt.reasons, ["dataLoss"]);
});

test("secret-like observation fails closed", () => {
  const observation = healthyObservation();
  observation.note = [
    "postgres",
    "://worker:",
    "not-a-real-",
    "password@example.invalid/database",
  ].join("");
  const receipt = evaluateP520DryRun({
    packet: authorizedPacket(),
    packetSha256: HASH_A,
    observation,
    observationSha256: HASH_B,
  });
  assert.equal(receipt.outcome, "blocked");
  assert.equal(receipt.proposedMode, "off");
  assert.deepEqual(receipt.reasons, [
    "p520_observation_contains_secret_material",
  ]);
});

test("input paths are confined to private JSON files below tmp/p5-collab-20", () => {
  assert.match(
    resolveP520JsonInput("tmp/p5-collab-20/authorization.json"),
    /tmp[\\/]p5-collab-20[\\/]authorization\.json$/u,
  );
  assert.throws(
    () => resolveP520JsonInput(".env.p5-collab-19-disposable.local"),
    /outside_private_tmp/u,
  );
  assert.throws(
    () => resolveP520JsonInput("tmp/p5-collab-20/../outside.json"),
    /outside_private_tmp/u,
  );
});

test("live execution flags are rejected before any input is read", () => {
  assert.throws(
    () =>
      parseP520DryRunArgs([
        "--packet",
        "tmp/p5-collab-20/authorization.json",
        "--observation",
        "tmp/p5-collab-20/observation.json",
        "--execute",
      ]),
    /live_execution_not_implemented/u,
  );
});
