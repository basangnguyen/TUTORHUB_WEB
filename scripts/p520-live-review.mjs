import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual } from "node:util";

import {
  P519_PRIVATE_ALPHA_CONTRACT,
  evaluateP519PrivateAlphaReport,
} from "./p519-private-alpha-contract.mjs";
import {
  P520_RAMP_EXIT_CONTRACT,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";
import { EXACT_CONFIRMATION } from "./p520-live-runner.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const PRIVATE_ROOT = resolve(ROOT, "tmp", "p5-collab-20");
const BINDING_FILE = resolve(PRIVATE_ROOT, "run-binding.json");
const PREFLIGHT_FILE = resolve(PRIVATE_ROOT, "provider-preflight.json");
const REPORT_FILE = resolve(PRIVATE_ROOT, "provider-report.json");
const EVIDENCE_FILE = resolve(PRIVATE_ROOT, "live-review-evidence.json");
const MAX_INPUT_BYTES = 2 * 1024 * 1024;
const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const REVIEW_NAMES = Object.freeze([
  "license",
  "runtime",
  "cost",
  "security",
  "accessibility",
  "providerExit",
]);

function readPrivateJson(path, code) {
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

function writePrivateJson(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  const temporary = `${path}.${process.pid}.tmp`;
  writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  renameSync(temporary, path);
}

function git(args) {
  return execFileSync("git", args, {
    cwd: ROOT,
    encoding: "utf8",
    windowsHide: true,
  }).trim();
}

function candidateJson(candidateSha, path) {
  try {
    return JSON.parse(git(["show", `${candidateSha}:${path}`]));
  } catch {
    throw new Error("p520_review_candidate_json_invalid");
  }
}

function candidateText(candidateSha, path) {
  try {
    return git(["show", `${candidateSha}:${path}`]);
  } catch {
    throw new Error("p520_review_candidate_file_missing");
  }
}

export function collectP520RepositoryReviewEvidence(candidateSha) {
  if (!SHA_PATTERN.test(candidateSha)) {
    throw new Error("p520_review_candidate_invalid");
  }
  const baseline = P520_RAMP_EXIT_CONTRACT.priorGate.candidateSha;
  const baselineWebPackage = candidateJson(baseline, "apps/web/package.json");
  const baselineRuntimePackage = candidateJson(
    baseline,
    "services/whiteboard-runtime/package.json",
  );
  const webPackage = candidateJson(candidateSha, "apps/web/package.json");
  const runtimePackage = candidateJson(
    candidateSha,
    "services/whiteboard-runtime/package.json",
  );
  const dependencyPinsMatch =
    webPackage.dependencies?.["@excalidraw/excalidraw"] === "0.18.1" &&
    runtimePackage.dependencies?.["@hocuspocus/server"] === "4.6.0" &&
    runtimePackage.dependencies?.yjs === "13.6.27" &&
    runtimePackage.devDependencies?.["@hocuspocus/provider"] === "4.6.0";
  const dependencySections = [
    "dependencies",
    "devDependencies",
    "optionalDependencies",
    "peerDependencies",
  ];
  const dependencyScopeUnchanged =
    dependencySections.every(
      (section) =>
        isDeepStrictEqual(
          webPackage[section] ?? {},
          baselineWebPackage[section] ?? {},
        ) &&
        isDeepStrictEqual(
          runtimePackage[section] ?? {},
          baselineRuntimePackage[section] ?? {},
        ),
    ) &&
    candidateText(candidateSha, "pnpm-lock.yaml") ===
      candidateText(baseline, "pnpm-lock.yaml");
  const accessibilityDrift = git([
    "diff",
    "--name-only",
    `${baseline}..${candidateSha}`,
    "--",
    "apps/web/src/features/collaboration",
    "packages/collaboration-client",
  ]);
  const lockfileSha256 = createHash("sha256")
    .update(candidateText(candidateSha, "pnpm-lock.yaml"))
    .digest("hex");
  return {
    accessibilityScopeUnchanged: accessibilityDrift.length === 0,
    dependencyPinsMatch,
    dependencyScopeUnchanged,
    lockfileSha256,
  };
}

export function completeP520LiveReviewPacket(
  packet,
  {
    evidenceRef = "tmp/p5-collab-20/live-review-evidence.json",
    reviewedAt = new Date().toISOString(),
  } = {},
) {
  const before = evaluateP520RampExitPlan(packet);
  if (
    !before.ok ||
    !before.liveRampAllowed ||
    before.reviewsComplete ||
    packet.status !== "authorized-pending-live-validation" ||
    !Number.isFinite(Date.parse(reviewedAt))
  ) {
    throw new Error("p520_review_packet_not_pending");
  }
  const completed = structuredClone(packet);
  for (const reviewName of REVIEW_NAMES) {
    completed.reviews[reviewName] = {
      state: "passed",
      evidenceRef: `${evidenceRef}#${reviewName}`,
      reviewedAt,
    };
  }
  completed.status = "authorized";
  const after = evaluateP520RampExitPlan(completed);
  if (
    !after.ok ||
    !after.liveRampAllowed ||
    !after.reviewsComplete ||
    after.reviewsPassed !== REVIEW_NAMES.length
  ) {
    throw new Error("p520_review_completion_invalid");
  }
  return completed;
}

function requireLiveEvidence(packet, binding, preflight, report, repository) {
  const reportEvaluation = evaluateP519PrivateAlphaReport(report, {
    expectedBinding: binding,
    nowMs: Date.now(),
  });
  const exactBinding =
    report.binding?.commitSha === packet.target?.candidateSha &&
    report.binding?.targetFingerprint ===
      packet.target?.targetFingerprintSha256 &&
    report.binding?.manifestSha256 === packet.target?.tenantAllowlistSha256;
  const preflightPassed =
    preflight?.outcome === "pass" &&
    preflight?.providerObserved === true &&
    preflight?.exactTwoTenantIsolation === true &&
    preflight?.tenantCount === P520_RAMP_EXIT_CONTRACT.initialRampTenantCount &&
    preflight?.candidateSha === packet.target?.candidateSha;
  const repositoryPassed =
    repository.dependencyPinsMatch === true &&
    repository.dependencyScopeUnchanged === true &&
    repository.accessibilityScopeUnchanged === true &&
    SHA256_PATTERN.test(repository.lockfileSha256 ?? "");
  if (
    !reportEvaluation.ok ||
    !exactBinding ||
    !preflightPassed ||
    !repositoryPassed ||
    report.observations?.costUsd !== 0 ||
    report.observations?.optionalBurst?.enabled !== false ||
    report.publication?.accessibilityNoticePublished !== true
  ) {
    throw new Error("p520_review_live_evidence_failed");
  }
  return reportEvaluation;
}

export function createP520LiveReviewEvidence({
  packet,
  report,
  reportEvaluation,
  repository,
  reviewedAt,
}) {
  return {
    schemaVersion: "p5-collab-20-live-review-evidence-v1",
    candidateSha: packet.target.candidateSha,
    reviewedAt,
    report: {
      schemaVersion: report.schemaVersion,
      source: report.source,
      durationSeconds: P519_PRIVATE_ALPHA_CONTRACT.durationSeconds,
      passed: reportEvaluation.ok === true,
    },
    reviews: {
      license: {
        dependencyPinsMatch: repository.dependencyPinsMatch,
        dependencyScopeUnchanged: repository.dependencyScopeUnchanged,
        lockfileSha256: repository.lockfileSha256,
      },
      runtime: { providerReportPassed: true, cleanupVerified: true },
      cost: { costUsd: 0, optionalBurstEnabled: false },
      security: { exactTwoTenantIsolation: true, providerDrillsPassed: true },
      accessibility: {
        noticePublished: true,
        collaborationScopeUnchanged: repository.accessibilityScopeUnchanged,
      },
      providerExit: {
        portableExportRestorePassed: true,
        forceOffRecoveryPassed: true,
      },
    },
    identifiersLogged: false,
  };
}

async function finalize() {
  const candidateSha = git(["rev-parse", "HEAD"]);
  if (!SHA_PATTERN.test(candidateSha)) {
    throw new Error("p520_review_candidate_invalid");
  }
  const short = candidateSha.slice(0, 7);
  const packet = readPrivateJson(
    resolve(PRIVATE_ROOT, `authorization-${short}-live.json`),
    "p520_review_packet",
  );
  const bindingEnvelope = readPrivateJson(BINDING_FILE, "p520_review_binding");
  const preflight = readPrivateJson(PREFLIGHT_FILE, "p520_review_preflight");
  const report = readPrivateJson(REPORT_FILE, "p520_review_report");
  const repository = collectP520RepositoryReviewEvidence(candidateSha);
  const reportEvaluation = requireLiveEvidence(
    packet,
    bindingEnvelope.binding,
    preflight,
    report,
    repository,
  );
  const reviewedAt = new Date().toISOString();
  const completed = completeP520LiveReviewPacket(packet, { reviewedAt });
  const evidence = createP520LiveReviewEvidence({
    packet,
    report,
    reportEvaluation,
    repository,
    reviewedAt,
  });
  writePrivateJson(EVIDENCE_FILE, evidence);
  writePrivateJson(
    resolve(PRIVATE_ROOT, `authorization-${short}-completed.json`),
    completed,
  );
  return {
    outcome: "pass",
    status: completed.status,
    reviewsPassed: REVIEW_NAMES.length,
    providerObserved: true,
    identifiersLogged: false,
  };
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  const authorized =
    process.argv.length === 4 &&
    process.argv[2] === "--confirm" &&
    process.argv[3] === EXACT_CONFIRMATION;
  const operation = authorized
    ? finalize()
    : Promise.reject(new Error("p520_review_exact_confirmation_required"));
  operation
    .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
    .catch((error) => {
      const reason =
        error instanceof Error && /^p520_[a-z0-9_]+$/u.test(error.message)
          ? error.message
          : "p520_review_bounded_failure";
      process.stderr.write(`${JSON.stringify({ outcome: "fail", reason })}\n`);
      process.exitCode = 1;
    });
}
