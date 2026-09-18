import assert from "node:assert/strict";
import test from "node:test";

import {
  P520_RAMP_EXIT_CONTRACT,
  createP520PreparationPlan,
  evaluateP520HoldPoint,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";

function authorizedPlan() {
  const plan = createP520PreparationPlan();
  plan.status = "authorized";
  plan.posture.liveActionsAuthorized = true;
  plan.posture.providerMutationAuthorized = true;
  plan.target = {
    environment: P520_RAMP_EXIT_CONTRACT.allowedEnvironment,
    candidateSha: "a".repeat(40),
    targetFingerprintSha256: "b".repeat(64),
    tenantAllowlistSha256: "c".repeat(64),
    tenantCount: 2,
    perTenantQuotas: {
      ...P520_RAMP_EXIT_CONTRACT.initialPerTenantQuotaCeilings,
    },
  };
  plan.holdPoint.decision = "go";
  plan.killSwitch.liveExecutorReady = true;
  plan.killSwitch.rollbackExecutorReady = true;
  plan.authorization = {
    state: "approved",
    approvedBy: "owner",
    approvedAt: "2026-09-17T09:00:00.000Z",
  };
  for (const review of Object.values(plan.reviews)) {
    review.state = "passed";
    review.evidenceRef = "evidence/provider-observed";
    review.reviewedAt = "2026-09-17T08:59:00.000Z";
  }
  plan.completion.exactCandidateRecorded = true;
  return plan;
}

test("preparation contract passes while live ramp remains fail-closed", () => {
  const result = evaluateP520RampExitPlan(createP520PreparationPlan());
  assert.equal(result.ok, true, result.errors.join("\n"));
  assert.equal(result.liveRampAllowed, false);
});

test("an authorized exact disposable plan can become eligible", () => {
  const result = evaluateP520RampExitPlan(authorizedPlan());
  assert.equal(result.ok, true, result.errors.join("\n"));
  assert.equal(result.liveRampAllowed, true);
  assert.equal(result.reviewsComplete, true);
});

test("two-stage authorization permits only the exact live window before reviews complete", () => {
  const plan = authorizedPlan();
  plan.status = "authorized-pending-live-validation";
  for (const review of Object.values(plan.reviews)) {
    review.state = "pending-live-validation";
    review.evidenceRef = null;
    review.reviewedAt = null;
  }
  const result = evaluateP520RampExitPlan(plan);
  assert.equal(result.ok, true, result.errors.join("\n"));
  assert.equal(result.liveRampAllowed, true);
  assert.equal(result.reviewsComplete, false);
  assert.equal(result.reviewsPassed, 0);
});

test("pending live reviews cannot masquerade as a completed authorization", () => {
  const plan = authorizedPlan();
  for (const review of Object.values(plan.reviews)) {
    review.state = "pending-live-validation";
    review.evidenceRef = null;
    review.reviewedAt = null;
  }
  const result = evaluateP520RampExitPlan(plan);
  assert.equal(result.liveRampAllowed, false);
  assert.match(result.errors.join("\n"), /authorized-pending-live-validation/u);
});

test("production and shared staging are outside the authorization boundary", () => {
  for (const environment of ["production", "shared-staging"]) {
    const plan = authorizedPlan();
    plan.target.environment = environment;
    if (environment === "production") plan.posture.productionAuthorized = true;
    if (environment === "shared-staging") {
      plan.posture.sharedStagingAuthorized = true;
    }
    const result = evaluateP520RampExitPlan(plan);
    assert.equal(result.liveRampAllowed, false);
    assert.match(result.errors.join("\n"), /Authorized|environment/u);
  }
});

test("missing approval, executor or exact target keeps live ramp closed", () => {
  const plan = authorizedPlan();
  plan.authorization.approvedBy = null;
  plan.killSwitch.rollbackExecutorReady = false;
  plan.target.tenantAllowlistSha256 = null;
  const result = evaluateP520RampExitPlan(plan);
  assert.equal(result.liveRampAllowed, false);
  assert.match(
    result.errors.join("\n"),
    /approvedBy|rollbackExecutorReady|tenantAllowlistSha256/u,
  );
});

test("initial ramp cannot exceed the proven per-tenant quota profile", () => {
  const plan = authorizedPlan();
  plan.target.perTenantQuotas.connections = 11;
  const result = evaluateP520RampExitPlan(plan);
  assert.equal(result.liveRampAllowed, false);
  assert.match(result.errors.join("\n"), /connections/u);
});

test("initial bounded ramp requires exactly two tenants", () => {
  for (const tenantCount of [1, 3]) {
    const plan = authorizedPlan();
    plan.target.tenantCount = tenantCount;
    const result = evaluateP520RampExitPlan(plan);
    assert.equal(result.liveRampAllowed, false);
    assert.match(result.errors.join("\n"), /target\.tenantCount/u);
  }
});

test("automatic hold-point policy degrades and stops on exact triggers", () => {
  const healthy = {
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
  assert.equal(evaluateP520HoldPoint(healthy).mode, "enabled");
  assert.equal(
    evaluateP520HoldPoint({
      ...healthy,
      consecutiveReadinessFailures: 2,
    }).mode,
    "read_only",
  );
  assert.equal(
    evaluateP520HoldPoint({ ...healthy, divergenceCount: 1 }).mode,
    "off",
  );
  assert.equal(evaluateP520HoldPoint(null).mode, "off");
});

test("secret-like values are rejected", () => {
  const plan = createP520PreparationPlan();
  plan.note = [
    "postgres",
    "://worker:",
    "not-a-real-",
    "password@example.invalid/database",
  ].join("");
  const result = evaluateP520RampExitPlan(plan);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /secret material/u);
});
