const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const REQUIRED_REVIEWS = Object.freeze([
  "license",
  "runtime",
  "cost",
  "security",
  "accessibility",
  "providerExit",
]);

export const P520_RAMP_EXIT_CONTRACT = Object.freeze({
  schemaVersion: "p5-collab-20-ramp-exit-v1",
  allowedEnvironment: "disposable-private-alpha",
  minimumHoldSeconds: 3_600,
  initialPerTenantQuotaCeilings: Object.freeze({
    documents: 2,
    connections: 10,
    storageBytes: 64 * 1024 * 1024,
    operationsPerMinute: 600,
  }),
  absolutePerTenantQuotaCeilings: Object.freeze({
    documents: 100,
    connections: 100,
    storageBytes: 10 * 1024 * 1024 * 1024,
    operationsPerMinute: 60_000,
  }),
  priorGate: Object.freeze({
    task: "P5-COLLAB-19",
    status: "DONE",
    candidateSha: "22ebfe1bfecc95d782ee35f4a8049c32f25fdc50",
    ledgerVersion: 42,
    ledgerDirty: false,
    whiteboardMode: "off",
    runtimeReady: false,
    documents: 0,
    editConnections: 0,
  }),
  authority: Object.freeze({
    editor: "Excalidraw 0.18.1",
    documentAuthority: "Yjs 13.6.27",
    transport: "Hocuspocus 4.6.0",
    oneDocumentAuthority: true,
    postgresOperationAuthority: false,
    b2OperationAuthority: false,
  }),
  supportedProfile: Object.freeze({
    name: "FREE_PRIVATE_ALPHA",
    region: "Singapore",
    runtimeInstances: 1,
    redisEnabled: false,
    highAvailability: false,
    automaticScaling: false,
    hardCostCapUsd: 0,
  }),
});

function isRecord(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function requireExact(errors, actual, expected, path) {
  if (actual !== expected) {
    errors.push(`${path} must equal ${JSON.stringify(expected)}`);
  }
}

function validateExactRecord(errors, actual, expected, path) {
  if (!isRecord(actual)) {
    errors.push(`${path} must be an object`);
    return;
  }
  for (const [key, value] of Object.entries(expected)) {
    requireExact(errors, actual[key], value, `${path}.${key}`);
  }
}

function validTimestamp(value) {
  return typeof value === "string" && Number.isFinite(Date.parse(value));
}

function containsSecretMaterial(value) {
  const serialized = JSON.stringify(value);
  return (
    /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/u.test(
      serialized,
    ) ||
    /postgres(?:ql)?:\/\/[^\s]+:[^\s]+@/iu.test(serialized) ||
    /(?:password|applicationKey|accessToken)[^,}\s]{0,16}[:=][^,}\s]{8,}/iu.test(
      serialized,
    )
  );
}

function validateReviews(errors, reviews, authorized) {
  if (!isRecord(reviews)) {
    errors.push("reviews must be an object");
    return;
  }
  for (const category of REQUIRED_REVIEWS) {
    const review = reviews[category];
    if (!isRecord(review)) {
      errors.push(`reviews.${category} must be an object`);
      continue;
    }
    const expectedState = authorized ? "passed" : "pending-live-validation";
    requireExact(
      errors,
      review.state,
      expectedState,
      `reviews.${category}.state`,
    );
    if (authorized) {
      if (
        typeof review.evidenceRef !== "string" ||
        review.evidenceRef.trim().length < 8
      ) {
        errors.push(`reviews.${category}.evidenceRef is required`);
      }
      if (!validTimestamp(review.reviewedAt)) {
        errors.push(`reviews.${category}.reviewedAt must be a timestamp`);
      }
    }
  }
}

function validateQuotas(errors, quotas) {
  if (!isRecord(quotas)) {
    errors.push("target.perTenantQuotas must be an object");
    return;
  }
  for (const [key, initialMaximum] of Object.entries(
    P520_RAMP_EXIT_CONTRACT.initialPerTenantQuotaCeilings,
  )) {
    const value = quotas[key];
    if (!Number.isInteger(value) || value <= 0 || value > initialMaximum) {
      errors.push(
        `target.perTenantQuotas.${key} must be an integer between 1 and ${initialMaximum}`,
      );
    }
  }
}

export function createP520PreparationPlan() {
  return {
    schemaVersion: P520_RAMP_EXIT_CONTRACT.schemaVersion,
    status: "preparation-only",
    posture: {
      liveActionsAuthorized: false,
      providerMutationAuthorized: false,
      productionAuthorized: false,
      sharedStagingAuthorized: false,
    },
    priorGate: { ...P520_RAMP_EXIT_CONTRACT.priorGate },
    authority: { ...P520_RAMP_EXIT_CONTRACT.authority },
    supportedProfile: { ...P520_RAMP_EXIT_CONTRACT.supportedProfile },
    target: {
      environment: null,
      candidateSha: null,
      targetFingerprintSha256: null,
      tenantAllowlistSha256: null,
      tenantCount: null,
      perTenantQuotas: null,
    },
    holdPoint: {
      currentStage: "private-alpha-complete",
      nextStage: "bounded-ramp",
      decision: "pending",
      minimumDurationSeconds: P520_RAMP_EXIT_CONTRACT.minimumHoldSeconds,
      ownerApprovalRequired: true,
      providerObservedEvidenceRequired: true,
    },
    killSwitch: {
      transitionPath: "enabled->read_only->off",
      manualAvailable: true,
      automaticDecisionEvaluator: "evaluateP520HoldPoint",
      liveExecutorReady: false,
      rollbackExecutorReady: false,
      whiteboardStartsOff: true,
    },
    authorization: {
      state: "pending",
      approvedBy: null,
      approvedAt: null,
    },
    reviews: Object.fromEntries(
      REQUIRED_REVIEWS.map((category) => [
        category,
        {
          state: "pending-live-validation",
          evidenceRef: null,
          reviewedAt: null,
        },
      ]),
    ),
    completion: {
      exactCandidateRecorded: false,
      supportedProfileRecorded: true,
      residualRisks: [
        "Single free runtime instance has no high availability.",
        "Live ramp executor and rollback executor are not yet authorized.",
      ],
      deferredWork: [
        "Paid multi-instance production topology remains deferred.",
        "Production and shared staging remain outside this task authorization.",
      ],
      oneAuthorityInvariantRequired: true,
      portabilityRecoveryRequired: true,
    },
  };
}

export function evaluateP520RampExitPlan(plan) {
  const errors = [];
  if (!isRecord(plan)) {
    return {
      ok: false,
      liveRampAllowed: false,
      errors: ["plan must be an object"],
    };
  }
  requireExact(
    errors,
    plan.schemaVersion,
    P520_RAMP_EXIT_CONTRACT.schemaVersion,
    "schemaVersion",
  );
  validateExactRecord(
    errors,
    plan.priorGate,
    P520_RAMP_EXIT_CONTRACT.priorGate,
    "priorGate",
  );
  validateExactRecord(
    errors,
    plan.authority,
    P520_RAMP_EXIT_CONTRACT.authority,
    "authority",
  );
  validateExactRecord(
    errors,
    plan.supportedProfile,
    P520_RAMP_EXIT_CONTRACT.supportedProfile,
    "supportedProfile",
  );
  requireExact(
    errors,
    plan.posture?.productionAuthorized,
    false,
    "posture.productionAuthorized",
  );
  requireExact(
    errors,
    plan.posture?.sharedStagingAuthorized,
    false,
    "posture.sharedStagingAuthorized",
  );
  requireExact(
    errors,
    plan.holdPoint?.currentStage,
    "private-alpha-complete",
    "holdPoint.currentStage",
  );
  requireExact(
    errors,
    plan.holdPoint?.nextStage,
    "bounded-ramp",
    "holdPoint.nextStage",
  );
  requireExact(
    errors,
    plan.holdPoint?.minimumDurationSeconds,
    P520_RAMP_EXIT_CONTRACT.minimumHoldSeconds,
    "holdPoint.minimumDurationSeconds",
  );
  requireExact(
    errors,
    plan.holdPoint?.ownerApprovalRequired,
    true,
    "holdPoint.ownerApprovalRequired",
  );
  requireExact(
    errors,
    plan.holdPoint?.providerObservedEvidenceRequired,
    true,
    "holdPoint.providerObservedEvidenceRequired",
  );
  requireExact(
    errors,
    plan.killSwitch?.transitionPath,
    "enabled->read_only->off",
    "killSwitch.transitionPath",
  );
  requireExact(
    errors,
    plan.killSwitch?.manualAvailable,
    true,
    "killSwitch.manualAvailable",
  );
  requireExact(
    errors,
    plan.killSwitch?.automaticDecisionEvaluator,
    "evaluateP520HoldPoint",
    "killSwitch.automaticDecisionEvaluator",
  );
  requireExact(
    errors,
    plan.killSwitch?.whiteboardStartsOff,
    true,
    "killSwitch.whiteboardStartsOff",
  );
  requireExact(
    errors,
    plan.completion?.supportedProfileRecorded,
    true,
    "completion.supportedProfileRecorded",
  );
  requireExact(
    errors,
    plan.completion?.oneAuthorityInvariantRequired,
    true,
    "completion.oneAuthorityInvariantRequired",
  );
  requireExact(
    errors,
    plan.completion?.portabilityRecoveryRequired,
    true,
    "completion.portabilityRecoveryRequired",
  );
  if (
    !Array.isArray(plan.completion?.residualRisks) ||
    plan.completion.residualRisks.length === 0
  ) {
    errors.push("completion.residualRisks must not be empty");
  }
  if (
    !Array.isArray(plan.completion?.deferredWork) ||
    plan.completion.deferredWork.length === 0
  ) {
    errors.push("completion.deferredWork must not be empty");
  }

  const authorized = plan.posture?.liveActionsAuthorized === true;
  validateReviews(errors, plan.reviews, authorized);

  if (!authorized) {
    requireExact(errors, plan.status, "preparation-only", "status");
    requireExact(
      errors,
      plan.posture?.providerMutationAuthorized,
      false,
      "posture.providerMutationAuthorized",
    );
    requireExact(
      errors,
      plan.authorization?.state,
      "pending",
      "authorization.state",
    );
    requireExact(
      errors,
      plan.holdPoint?.decision,
      "pending",
      "holdPoint.decision",
    );
    requireExact(errors, plan.target?.environment, null, "target.environment");
    requireExact(
      errors,
      plan.target?.candidateSha,
      null,
      "target.candidateSha",
    );
    requireExact(errors, plan.target?.tenantCount, null, "target.tenantCount");
    requireExact(
      errors,
      plan.killSwitch?.liveExecutorReady,
      false,
      "killSwitch.liveExecutorReady",
    );
    requireExact(
      errors,
      plan.killSwitch?.rollbackExecutorReady,
      false,
      "killSwitch.rollbackExecutorReady",
    );
    requireExact(
      errors,
      plan.completion?.exactCandidateRecorded,
      false,
      "completion.exactCandidateRecorded",
    );
  } else {
    requireExact(errors, plan.status, "authorized", "status");
    requireExact(
      errors,
      plan.posture?.providerMutationAuthorized,
      true,
      "posture.providerMutationAuthorized",
    );
    requireExact(
      errors,
      plan.authorization?.state,
      "approved",
      "authorization.state",
    );
    if (
      typeof plan.authorization?.approvedBy !== "string" ||
      plan.authorization.approvedBy.trim().length < 2
    ) {
      errors.push("authorization.approvedBy is required");
    }
    if (!validTimestamp(plan.authorization?.approvedAt)) {
      errors.push("authorization.approvedAt must be a timestamp");
    }
    requireExact(errors, plan.holdPoint?.decision, "go", "holdPoint.decision");
    requireExact(
      errors,
      plan.target?.environment,
      P520_RAMP_EXIT_CONTRACT.allowedEnvironment,
      "target.environment",
    );
    if (!SHA_PATTERN.test(plan.target?.candidateSha ?? "")) {
      errors.push("target.candidateSha must be a full lowercase Git SHA");
    }
    for (const key of ["targetFingerprintSha256", "tenantAllowlistSha256"]) {
      if (!SHA256_PATTERN.test(plan.target?.[key] ?? "")) {
        errors.push(`target.${key} must be a lowercase SHA-256`);
      }
    }
    if (
      !Number.isInteger(plan.target?.tenantCount) ||
      plan.target.tenantCount < 1
    ) {
      errors.push("target.tenantCount must be an integer >= 1");
    }
    validateQuotas(errors, plan.target?.perTenantQuotas);
    requireExact(
      errors,
      plan.killSwitch?.liveExecutorReady,
      true,
      "killSwitch.liveExecutorReady",
    );
    requireExact(
      errors,
      plan.killSwitch?.rollbackExecutorReady,
      true,
      "killSwitch.rollbackExecutorReady",
    );
    requireExact(
      errors,
      plan.completion?.exactCandidateRecorded,
      true,
      "completion.exactCandidateRecorded",
    );
  }
  if (containsSecretMaterial(plan))
    errors.push("plan contains secret material");
  return {
    ok: errors.length === 0,
    liveRampAllowed: authorized && errors.length === 0,
    errors,
  };
}

export function evaluateP520HoldPoint(observation) {
  if (!isRecord(observation)) {
    return { mode: "off", reasons: ["invalid-observation"] };
  }
  const critical = [
    ["oneAuthorityInvariant", observation.oneAuthorityInvariant !== true],
    ["portability", observation.portabilityPassed !== true],
    ["recovery", observation.recoveryPassed !== true],
    ["crossTenantLeak", observation.crossTenantLeakCount > 0],
    ["divergence", observation.divergenceCount > 0],
    ["dataLoss", observation.dataLossCount > 0],
    ["security", observation.securityIncident === true],
    ["privacy", observation.privacyIncident === true],
    ["cost", observation.unbilledChargesUsd > 0],
    ["readOnlyRecoveryExpired", observation.readOnlyRecoveryExpired === true],
  ].filter(([, failed]) => failed);
  if (critical.length > 0) {
    return { mode: "off", reasons: critical.map(([reason]) => reason) };
  }
  const degraded = [
    ["readiness", observation.consecutiveReadinessFailures >= 2],
    ["checkpoint", observation.checkpointPersistenceFailed === true],
    ["quotaTrend", observation.quotaRejectionsIncreasing === true],
    ["freeCap", observation.freeCapUsagePercent >= 75],
    ["accessibility", observation.accessibilityRegression === true],
    ["providerExit", observation.providerExitReviewPassed !== true],
  ].filter(([, failed]) => failed);
  if (degraded.length > 0) {
    return { mode: "read_only", reasons: degraded.map(([reason]) => reason) };
  }
  return { mode: "enabled", reasons: [] };
}
