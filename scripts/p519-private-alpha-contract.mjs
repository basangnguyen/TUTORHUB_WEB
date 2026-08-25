const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const SAFE_IDENTIFIER_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/u;

export const P519_SUPPORT_PATH =
  "/app/settings?source=whiteboard-private-alpha";

export const P519_PRIVATE_ALPHA_CONTRACT = Object.freeze({
  schemaVersion: "p5-collab-19-private-alpha-v1",
  bindingSchemaVersion: "p5-collab-19-run-binding-v1",
  durationSeconds: 3_600,
  phases: Object.freeze({
    warmupSeconds: 300,
    steadySeconds: 3_000,
    recoverySeconds: 300,
  }),
  workload: Object.freeze({
    documents: 2,
    clientsPerDocument: 5,
    totalConnections: 10,
    shapesPerDocument: 500,
    steadyOperationsPerMinutePerDocument: 120,
    burstOperationsPerMinutePerDocument: 480,
    burstDurationSeconds: 60,
  }),
  cadence: Object.freeze({
    reconnectSeconds: 600,
    metricsSeconds: 30,
    semanticCheckSeconds: 300,
  }),
  drills: Object.freeze({ controlAuthorityOutageSeconds: 600 }),
  thresholds: Object.freeze({
    joinP95Ms: 7_500,
    reconnectP95Ms: 7_500,
    convergenceP95Ms: 2_500,
    acknowledgementP95Ms: 1_000,
    artifactP95Ms: 2_500,
    recoveryTimeObjectiveMs: 300_000,
    cleanupMs: 3_000,
    costUsd: 0,
  }),
  syntheticPrefix: "p519-private-alpha-",
});

const REQUIRED_BINDING_KEYS = Object.freeze([
  "schemaVersion",
  "manifestSha256",
  "runId",
  "targetFingerprint",
  "commitSha",
  "deployId",
  "syntheticPrefix",
  "generatedAt",
  "ledgerProbeSha256",
  "aclProbeSha256",
]);
const BOUND_ITEM_KEYS = Object.freeze([
  "runId",
  "targetFingerprint",
  "commitSha",
  "deployId",
  "manifestSha256",
]);
const TRUSTED_BINDING_KEYS = Object.freeze([
  "schemaVersion",
  "manifestSha256",
  "runId",
  "targetFingerprint",
  "commitSha",
  "deployId",
  "syntheticPrefix",
  "ledgerProbeSha256",
  "aclProbeSha256",
]);
const DRILL_REQUIREMENTS = Object.freeze({
  reconnect: ["executed", "recovered"],
  controlAuthorityOutage: [
    "executed",
    "existingDocumentRecovered",
    "newDocumentFailedClosed",
  ],
  neonOutage: ["executed", "failedClosed", "recovered"],
  b2Outage: ["executed", "lastGoodArtifactReadable", "recovered"],
  credentialRotation: [
    "executed",
    "oldCredentialRejected",
    "newCredentialAccepted",
  ],
  forceOff: ["executed", "newGrantRejected", "activeSessionsClosedOrReadOnly"],
  incident: ["executed"],
  export: ["executed", "portableRoundTrip"],
  restore: ["executed", "semanticHashMatch"],
  revoke: ["executed", "oldGrantRejected", "oldCredentialRejected"],
});
const EXPECTED_OWNERS = Object.freeze({
  primaryOnCall: String.fromCodePoint(66, 225, 32, 83, 225, 110, 103),
  backupOnCall: String.fromCodePoint(68, 117, 121, 32, 77, 7841, 110, 104),
  securityIncidentOwner: String.fromCodePoint(66, 225, 32, 83, 225, 110, 103),
  costOwner: String.fromCodePoint(66, 225, 32, 83, 225, 110, 103),
});

function isRecord(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function requireExact(errors, actual, expected, path) {
  if (actual !== expected) {
    errors.push(path + " must equal " + JSON.stringify(expected));
  }
}

function requireBooleanTrue(errors, record, name, path) {
  if (!isRecord(record) || record[name] !== true) {
    errors.push(path + "." + name + " must be true");
  }
}

function parseTimestamp(errors, value, path) {
  const parsed = typeof value === "string" ? Date.parse(value) : Number.NaN;
  if (!Number.isFinite(parsed))
    errors.push(path + " must be a valid timestamp");
  return parsed;
}

function validateExactRecord(errors, actual, expected, path) {
  if (!isRecord(actual)) {
    errors.push(path + " must be an object");
    return;
  }
  for (const [name, value] of Object.entries(expected)) {
    requireExact(errors, actual[name], value, path + "." + name);
  }
}

export function percentile95(samples) {
  if (
    !Array.isArray(samples) ||
    samples.length === 0 ||
    !samples.every(Number.isFinite)
  ) {
    return Number.NaN;
  }
  const sorted = samples.slice().sort((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(sorted.length * 0.95) - 1)];
}

function validateRunBinding(errors, binding, expectedBinding) {
  if (!isRecord(binding)) {
    errors.push("binding must be an object");
    return;
  }
  for (const key of REQUIRED_BINDING_KEYS) {
    if (!(key in binding)) errors.push("binding." + key + " is required");
  }
  requireExact(
    errors,
    binding.schemaVersion,
    P519_PRIVATE_ALPHA_CONTRACT.bindingSchemaVersion,
    "binding.schemaVersion",
  );
  for (const key of [
    "manifestSha256",
    "targetFingerprint",
    "ledgerProbeSha256",
    "aclProbeSha256",
  ]) {
    if (!SHA256_PATTERN.test(binding[key] ?? "")) {
      errors.push("binding." + key + " must be a lowercase SHA-256");
    }
  }
  for (const key of ["runId", "commitSha", "deployId", "syntheticPrefix"]) {
    if (!SAFE_IDENTIFIER_PATTERN.test(binding[key] ?? "")) {
      errors.push("binding." + key + " must be a safe identifier");
    }
  }
  parseTimestamp(errors, binding.generatedAt, "binding.generatedAt");
  if (isRecord(expectedBinding)) {
    for (const key of TRUSTED_BINDING_KEYS) {
      requireExact(
        errors,
        binding[key],
        expectedBinding[key],
        "binding." + key,
      );
    }
  }
}

function validateBoundItem(errors, item, binding, path) {
  if (!isRecord(item)) {
    errors.push(path + " must be an object");
    return;
  }
  for (const key of BOUND_ITEM_KEYS) {
    requireExact(errors, item[key], binding[key], path + "." + key);
  }
}

function validateFreshEvidence(
  errors,
  item,
  index,
  startedMs,
  endedMs,
  binding,
  identities,
  hashes,
) {
  const path = "evidence.fresh[" + index + "]";
  if (
    !isRecord(item) ||
    typeof item.source !== "string" ||
    item.source.length < 3
  ) {
    errors.push(path + ".source is required");
  }
  const verifiedMs = parseTimestamp(
    errors,
    item?.verifiedAt,
    path + ".verifiedAt",
  );
  if (
    Number.isFinite(verifiedMs) &&
    (verifiedMs < startedMs || verifiedMs > endedMs)
  ) {
    errors.push(path + " must belong to the current provider-observed window");
  }
  if (!binding) return;
  validateBoundItem(errors, item, binding, path);
  if (!SAFE_IDENTIFIER_PATTERN.test(item?.evidenceId ?? "")) {
    errors.push(path + ".evidenceId must be a safe identifier");
  }
  if (!SHA256_PATTERN.test(item?.sha256 ?? "")) {
    errors.push(path + ".sha256 must be a lowercase SHA-256");
  }
  if (identities.has(item?.evidenceId))
    errors.push(path + ".evidenceId must be unique");
  if (hashes.has(item?.sha256)) errors.push(path + ".sha256 must be unique");
  identities.add(item?.evidenceId);
  hashes.add(item?.sha256);
}

function validateEvidence(errors, evidence, startedMs, endedMs, binding) {
  if (!isRecord(evidence)) {
    errors.push("evidence must be an object");
    return;
  }
  const fresh = Array.isArray(evidence.fresh) ? evidence.fresh : [];
  if (fresh.length === 0)
    errors.push("evidence.fresh must contain current-run evidence");
  const identities = new Set();
  const hashes = new Set();
  fresh.forEach((item, index) => {
    validateFreshEvidence(
      errors,
      item,
      index,
      startedMs,
      endedMs,
      binding,
      identities,
      hashes,
    );
  });
  const reused = Array.isArray(evidence.reused) ? evidence.reused : [];
  reused.forEach((item, index) => {
    const rationale = isRecord(item) ? item.validityRationale : undefined;
    if (typeof rationale !== "string" || rationale.trim().length < 20) {
      errors.push(
        "evidence.reused[" +
          index +
          "].validityRationale must contain at least 20 characters",
      );
    }
  });
}

function containsSecretMaterial(value) {
  const serialized = JSON.stringify(value);
  const jwt = /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/u;
  const databaseUrl = /postgres(?:ql)?:\/\/[^\s]+:[^\s]+@/iu;
  const namedSecret =
    /(?:secret|password|applicationKey|accessToken)[^,}\s]{0,16}[:=][^,}\s]{8,}/iu;
  return (
    jwt.test(serialized) ||
    databaseUrl.test(serialized) ||
    namedSecret.test(serialized)
  );
}

export function redactSensitiveText(value) {
  return String(value)
    .replace(
      /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/gu,
      "[REDACTED]",
    )
    .replace(/postgres(?:ql)?:\/\/[^\s]+:[^\s]+@/giu, "[REDACTED]@")
    .replace(
      /((?:secret|password|applicationKey|accessToken)[^,}\s]{0,16}[:=])[^,}\s]{8,}/giu,
      "$1[REDACTED]",
    );
}

function validatePlanAndPublication(errors, report) {
  const contract = P519_PRIVATE_ALPHA_CONTRACT;
  requireExact(
    errors,
    report.plan?.durationSeconds,
    contract.durationSeconds,
    "plan.durationSeconds",
  );
  validateExactRecord(
    errors,
    report.plan?.phases,
    contract.phases,
    "plan.phases",
  );
  validateExactRecord(
    errors,
    report.plan?.workload,
    contract.workload,
    "plan.workload",
  );
  validateExactRecord(
    errors,
    report.plan?.cadence,
    contract.cadence,
    "plan.cadence",
  );
  for (const key of [
    "tenantOptInPublished",
    "teacherGuidancePublished",
    "limitationsPublished",
    "accessibilityNoticePublished",
  ]) {
    requireBooleanTrue(errors, report.publication, key, "publication");
  }
  requireExact(
    errors,
    report.publication?.supportPath,
    P519_SUPPORT_PATH,
    "publication.supportPath",
  );
}

function validateOwners(errors, owners, startedMs, endedMs) {
  validateExactRecord(errors, owners, EXPECTED_OWNERS, "owners");
  const approvedMs = parseTimestamp(
    errors,
    owners?.approvedAt,
    "owners.approvedAt",
  );
  if (
    Number.isFinite(approvedMs) &&
    (approvedMs < startedMs || approvedMs > endedMs)
  ) {
    errors.push(
      "owners.approvedAt must belong to the current provider-observed window",
    );
  }
}

function validateLatency(errors, metrics, name, samples, threshold) {
  const value = percentile95(samples);
  metrics[name] = value;
  if (!Number.isFinite(value) || value > threshold) {
    errors.push(name + " must be finite and <= " + threshold);
  }
}

function validateObservations(errors, metrics, observations) {
  const thresholds = P519_PRIVATE_ALPHA_CONTRACT.thresholds;
  validateLatency(
    errors,
    metrics,
    "joinP95Ms",
    observations?.joinMs,
    thresholds.joinP95Ms,
  );
  validateLatency(
    errors,
    metrics,
    "reconnectP95Ms",
    observations?.reconnectMs,
    thresholds.reconnectP95Ms,
  );
  validateLatency(
    errors,
    metrics,
    "convergenceP95Ms",
    observations?.convergenceMs,
    thresholds.convergenceP95Ms,
  );
  validateLatency(
    errors,
    metrics,
    "acknowledgementP95Ms",
    observations?.acknowledgementMs,
    thresholds.acknowledgementP95Ms,
  );
  validateLatency(
    errors,
    metrics,
    "artifactP95Ms",
    observations?.artifactMs,
    thresholds.artifactP95Ms,
  );
  if (
    !Number.isInteger(observations?.metricsSampleCount) ||
    observations.metricsSampleCount < 120
  ) {
    errors.push("observations.metricsSampleCount must be an integer >= 120");
  }
  if (
    !Number.isInteger(observations?.semanticCheckCount) ||
    observations.semanticCheckCount < 12
  ) {
    errors.push("observations.semanticCheckCount must be an integer >= 12");
  }
  if (
    !Number.isInteger(observations?.reconnectEventCount) ||
    observations.reconnectEventCount < 50
  ) {
    errors.push("observations.reconnectEventCount must be an integer >= 50");
  }
  if (
    !Array.isArray(observations?.operationsPerDocument) ||
    observations.operationsPerDocument.length !== 2 ||
    !observations.operationsPerDocument.every(
      (value) => Number.isInteger(value) && value >= 6_000,
    )
  ) {
    errors.push(
      "observations.operationsPerDocument must contain two integers >= 6000",
    );
  }
  if (observations?.optionalBurst?.enabled === true) {
    requireExact(
      errors,
      observations.optionalBurst.operationsPerMinutePerDocument,
      480,
      "observations.optionalBurst.operationsPerMinutePerDocument",
    );
    requireExact(
      errors,
      observations.optionalBurst.durationSeconds,
      60,
      "observations.optionalBurst.durationSeconds",
    );
  }
  for (const key of [
    "divergenceCount",
    "dataLossCount",
    "unplanned5xxCount",
    "costUsd",
  ]) {
    requireExact(errors, observations?.[key], 0, "observations." + key);
  }
  requireExact(
    errors,
    observations?.semanticHashesMatch,
    true,
    "observations.semanticHashesMatch",
  );
}

function validateDrills(errors, drills, binding) {
  if (!isRecord(drills)) {
    errors.push("drills must be an object");
    return;
  }
  for (const [name, requirements] of Object.entries(DRILL_REQUIREMENTS)) {
    const drill = drills[name];
    for (const requirement of requirements) {
      requireBooleanTrue(errors, drill, requirement, "drills." + name);
    }
    if (binding) validateBoundItem(errors, drill, binding, "drills." + name);
  }
  requireExact(
    errors,
    drills.controlAuthorityOutage?.durationSeconds,
    P519_PRIVATE_ALPHA_CONTRACT.drills.controlAuthorityOutageSeconds,
    "drills.controlAuthorityOutage.durationSeconds",
  );
  if (
    !Number.isFinite(drills.restore?.rtoMs) ||
    drills.restore.rtoMs >
      P519_PRIVATE_ALPHA_CONTRACT.thresholds.recoveryTimeObjectiveMs
  ) {
    errors.push("drills.restore.rtoMs must be finite and <= 300000");
  }
}

function validateCleanup(errors, cleanup, binding) {
  if (!isRecord(cleanup)) {
    errors.push("cleanup must be an object");
    return;
  }
  requireExact(
    errors,
    cleanup.syntheticPrefix,
    P519_PRIVATE_ALPHA_CONTRACT.syntheticPrefix,
    "cleanup.syntheticPrefix",
  );
  if (
    !Number.isFinite(cleanup.elapsedMs) ||
    cleanup.elapsedMs > P519_PRIVATE_ALPHA_CONTRACT.thresholds.cleanupMs
  ) {
    errors.push("cleanup.elapsedMs must be finite and <= 3000");
  }
  for (const key of [
    "databaseRows",
    "runtimeConnections",
    "runtimeDocuments",
    "b2CurrentObjects",
    "b2Versions",
    "b2MultipartUploads",
  ]) {
    requireExact(errors, cleanup[key], 0, "cleanup." + key);
  }
  requireBooleanTrue(errors, cleanup, "verified", "cleanup");
  if (binding) validateBoundItem(errors, cleanup, binding, "cleanup");
}

export function evaluateP519PrivateAlphaReport(report, options = {}) {
  const errors = [];
  const metrics = {};
  if (!isRecord(report))
    return { ok: false, errors: ["report must be an object"], metrics };

  requireExact(
    errors,
    report.schemaVersion,
    P519_PRIVATE_ALPHA_CONTRACT.schemaVersion,
    "schemaVersion",
  );
  requireExact(errors, report.source, "provider-observed", "source");
  if (!SAFE_IDENTIFIER_PATTERN.test(report.runId ?? "")) {
    errors.push("runId must be a safe identifier");
  }

  const startedMs = parseTimestamp(errors, report.startedAt, "startedAt");
  const endedMs = parseTimestamp(errors, report.endedAt, "endedAt");
  if (
    Number.isFinite(startedMs) &&
    Number.isFinite(endedMs) &&
    endedMs - startedMs !== P519_PRIVATE_ALPHA_CONTRACT.durationSeconds * 1_000
  ) {
    errors.push("provider-observed window must equal exactly 3600 seconds");
  }
  if (Number.isFinite(options.nowMs) && Number.isFinite(endedMs)) {
    if (endedMs > options.nowMs || options.nowMs - endedMs > 300_000) {
      errors.push("endedAt must be within five minutes of the trusted clock");
    }
  }

  let binding;
  if (isRecord(options.expectedBinding)) {
    binding = report.binding;
    validateRunBinding(errors, binding, options.expectedBinding);
  } else if (report.binding !== undefined) {
    binding = report.binding;
    validateRunBinding(errors, binding);
  }
  if (binding) {
    requireExact(errors, report.runId, binding.runId, "runId");
    requireExact(
      errors,
      report.cleanup?.syntheticPrefix,
      binding.syntheticPrefix,
      "cleanup.syntheticPrefix",
    );
  }

  validatePlanAndPublication(errors, report);
  validateEvidence(errors, report.evidence, startedMs, endedMs, binding);
  validateOwners(errors, report.owners, startedMs, endedMs);
  validateObservations(errors, metrics, report.observations);
  validateDrills(errors, report.drills, binding);
  validateCleanup(errors, report.cleanup, binding);
  if (containsSecretMaterial(report))
    errors.push("report contains secret material");
  return { ok: errors.length === 0, errors, metrics };
}

export function createRedactedP519Summary(report, evaluation) {
  return {
    schemaVersion: report?.schemaVersion,
    runId: report?.runId,
    source: report?.source,
    startedAt: report?.startedAt,
    endedAt: report?.endedAt,
    ok: evaluation?.ok === true,
    errors: Array.isArray(evaluation?.errors) ? evaluation.errors.slice() : [],
    metrics: isRecord(evaluation?.metrics) ? { ...evaluation.metrics } : {},
    publication: {
      tenantOptInPublished: report?.publication?.tenantOptInPublished === true,
      teacherGuidancePublished:
        report?.publication?.teacherGuidancePublished === true,
      limitationsPublished: report?.publication?.limitationsPublished === true,
      accessibilityNoticePublished:
        report?.publication?.accessibilityNoticePublished === true,
      supportPath: report?.publication?.supportPath,
    },
    cleanup: {
      databaseRows: report?.cleanup?.databaseRows,
      runtimeConnections: report?.cleanup?.runtimeConnections,
      runtimeDocuments: report?.cleanup?.runtimeDocuments,
      b2CurrentObjects: report?.cleanup?.b2CurrentObjects,
      b2Versions: report?.cleanup?.b2Versions,
      b2MultipartUploads: report?.cleanup?.b2MultipartUploads,
      verified: report?.cleanup?.verified === true,
    },
  };
}
