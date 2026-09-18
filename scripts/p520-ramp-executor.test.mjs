import assert from "node:assert/strict";
import test from "node:test";

import {
  P520_RAMP_EXIT_CONTRACT,
  createP520PreparationPlan,
} from "./p520-ramp-exit-contract.mjs";
import {
  createP520ExecutionBinding,
  executeP520AdoptLiveDecision,
  executeP520LiveDecision,
  executeP520Rollback,
  selectP520SafeMode,
} from "./p520-ramp-executor.mjs";
import { canonicalizeP520TenantManifest } from "./p520-tenant-allowlist.mjs";

const CANDIDATE = "a".repeat(40);
const TARGET = "b".repeat(64);
const HASH_A = "c".repeat(64);
const HASH_B = "d".repeat(64);
const TENANTS = Object.freeze([
  "11111111-1111-4111-8111-111111111111",
  "22222222-2222-4222-8222-222222222222",
]);

function manifest() {
  return {
    schemaVersion: "p5-collab-20-tenant-allowlist-v1",
    tenantIds: TENANTS.slice(),
  };
}

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
    candidateSha: CANDIDATE,
    targetFingerprintSha256: TARGET,
    tenantAllowlistSha256:
      canonicalizeP520TenantManifest(manifest()).tenantAllowlistSha256,
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

function binding(overrides = {}) {
  return createP520ExecutionBinding({
    currentCommitSha: CANDIDATE,
    expectedTargetFingerprintSha256: TARGET,
    manifest: manifest(),
    observation: healthyObservation(),
    observationSha256: HASH_A,
    packet: authorizedPacket(),
    packetSha256: HASH_B,
    ...overrides,
  });
}

function fakeAdapter() {
  const calls = [];
  let mode = "off";
  return {
    calls,
    async assertExactTarget(target) {
      calls.push(["target", target.candidateSha]);
    },
    async syncEnvironment(environment) {
      calls.push(["sync", structuredClone(environment)]);
    },
    async deployCandidate(candidateSha) {
      calls.push(["deploy", candidateSha]);
      return {
        controlDeployId: "dep-control",
        runtimeDeployId: "dep-runtime",
      };
    },
    async adoptLiveCandidate(candidateSha) {
      calls.push(["adopt", candidateSha]);
      return {
        controlDeployId: "dep-control",
        runtimeDeployId: "dep-runtime",
      };
    },
    async setMode(next) {
      mode = next;
      calls.push(["mode", next]);
    },
    async verifyMode(expected) {
      calls.push(["verify", expected]);
      assert.equal(mode, expected);
      return {
        authorityAvailable: true,
        documents: expected === "off" ? 0 : 2,
        editConnections: 0,
        mode: expected,
        runtimeReady: expected !== "off",
      };
    },
  };
}

test("execution binding requires exact authorization, candidate, target and tenant hash", () => {
  assert.equal(binding().decision.mode, "enabled");
  assert.throws(
    () => binding({ currentCommitSha: "f".repeat(40) }),
    /candidate_mismatch/u,
  );
  assert.throws(
    () => binding({ expectedTargetFingerprintSha256: "f".repeat(64) }),
    /target_mismatch/u,
  );
  const changed = manifest();
  changed.tenantIds[1] = "33333333-3333-4333-8333-333333333333";
  assert.throws(
    () => binding({ manifest: changed }),
    /tenant_binding_mismatch/u,
  );
});

test("unapproved packets never reach an adapter", async () => {
  assert.throws(
    () =>
      binding({
        packet: createP520PreparationPlan(),
      }),
    /authorization_required/u,
  );
});

test("non-activation decisions are monotonically safe", () => {
  assert.equal(
    selectP520SafeMode({
      currentMode: "read_only",
      evaluatedMode: "enabled",
    }),
    "read_only",
  );
  assert.equal(
    selectP520SafeMode({ currentMode: "enabled", evaluatedMode: "off" }),
    "off",
  );
});

test("live executor deploys off first and emits no tenant UUID", async () => {
  const adapter = fakeAdapter();
  const receipt = await executeP520LiveDecision(binding(), adapter);
  assert.deepEqual(
    adapter.calls.map(([name, value]) =>
      name === "sync" ? [name, Object.keys(value)] : [name, value],
    ),
    [
      ["target", CANDIDATE],
      ["sync", ["control", "runtime"]],
      ["deploy", CANDIDATE],
      ["mode", "off"],
      ["verify", "off"],
      ["mode", "enabled"],
      ["verify", "enabled"],
    ],
  );
  assert.equal(receipt.appliedMode, "enabled");
  const serialized = JSON.stringify(receipt);
  for (const tenantId of TENANTS)
    assert.equal(serialized.includes(tenantId), false);
});

test("critical observation stays force-off after deploy", async () => {
  const adapter = fakeAdapter();
  const critical = binding({
    observation: { ...healthyObservation(), dataLossCount: 1 },
  });
  const receipt = await executeP520LiveDecision(critical, adapter);
  assert.equal(receipt.appliedMode, "off");
  assert.deepEqual(receipt.reasonCodes, ["dataLoss"]);
  assert.equal(adapter.calls.filter(([name]) => name === "mode").length, 1);
});

test("adopt-live executor verifies the exact live candidate and reapplies off first", async () => {
  const adapter = fakeAdapter();
  const receipt = await executeP520AdoptLiveDecision(binding(), adapter);
  assert.deepEqual(adapter.calls, [
    ["target", CANDIDATE],
    ["adopt", CANDIDATE],
    ["mode", "off"],
    ["verify", "off"],
    ["mode", "enabled"],
    ["verify", "enabled"],
  ]);
  assert.equal(receipt.action, "adopt-live-decision");
  assert.equal(receipt.appliedMode, "enabled");
  const serialized = JSON.stringify(receipt);
  for (const tenantId of TENANTS)
    assert.equal(serialized.includes(tenantId), false);
});

test("rollback follows read_only then off and proves zero active state", async () => {
  const adapter = fakeAdapter();
  const receipt = await executeP520Rollback(binding(), adapter);
  assert.deepEqual(
    adapter.calls.filter(([name]) => name === "mode"),
    [
      ["mode", "read_only"],
      ["mode", "off"],
    ],
  );
  assert.equal(receipt.transitionPath, "read_only->off");
  assert.deepEqual(receipt.verification, {
    authorityAvailable: true,
    runtimeReady: false,
    documents: 0,
    editConnections: 0,
  });
});

test("failed force-off verification blocks rollback receipt", async () => {
  const adapter = fakeAdapter();
  adapter.verifyMode = async (expected) => ({
    authorityAvailable: true,
    documents: expected === "off" ? 1 : 2,
    editConnections: 0,
    mode: expected,
    runtimeReady: expected !== "off",
  });
  await assert.rejects(
    executeP520Rollback(binding(), adapter),
    /verification_failed/u,
  );
});
