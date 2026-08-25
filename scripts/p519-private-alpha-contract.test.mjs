import assert from "node:assert/strict";
import test from "node:test";

import {
  P519_PRIVATE_ALPHA_CONTRACT,
  P519_SUPPORT_PATH,
  createRedactedP519Summary,
  evaluateP519PrivateAlphaReport,
  percentile95,
} from "./p519-private-alpha-contract.mjs";

const samples = (count, value) => Array.from({ length: count }, () => value);

function validReport() {
  const baSang = String.fromCodePoint(66, 225, 32, 83, 225, 110, 103);
  const duyManh = String.fromCodePoint(68, 117, 121, 32, 77, 7841, 110, 104);
  return {
    schemaVersion: P519_PRIVATE_ALPHA_CONTRACT.schemaVersion,
    runId: "p519-private-alpha-20260824",
    source: "provider-observed",
    startedAt: "2026-08-24T08:00:00.000Z",
    endedAt: "2026-08-24T09:00:00.000Z",
    plan: {
      durationSeconds: P519_PRIVATE_ALPHA_CONTRACT.durationSeconds,
      phases: { ...P519_PRIVATE_ALPHA_CONTRACT.phases },
      workload: { ...P519_PRIVATE_ALPHA_CONTRACT.workload },
      cadence: { ...P519_PRIVATE_ALPHA_CONTRACT.cadence },
    },
    publication: {
      tenantOptInPublished: true,
      teacherGuidancePublished: true,
      limitationsPublished: true,
      accessibilityNoticePublished: true,
      supportPath: P519_SUPPORT_PATH,
    },
    evidence: {
      fresh: [
        {
          source: "p519-provider-run",
          verifiedAt: "2026-08-24T09:00:00.000Z",
        },
      ],
      reused: [
        {
          source: "P5-COLLAB-16 provider-exit evidence",
          verifiedAt: "2026-08-24T07:30:00.000Z",
          validityRationale:
            "The same immutable runtime build and exact provider contract were rechecked before the run.",
        },
      ],
    },
    owners: {
      primaryOnCall: baSang,
      backupOnCall: duyManh,
      securityIncidentOwner: baSang,
      costOwner: baSang,
      approvedAt: "2026-08-24T09:00:00.000Z",
    },
    observations: {
      joinMs: samples(10, 600),
      reconnectMs: samples(50, 700),
      convergenceMs: samples(60, 350),
      acknowledgementMs: samples(60, 180),
      artifactMs: samples(4, 800),
      metricsSampleCount: 120,
      semanticCheckCount: 12,
      reconnectEventCount: 50,
      operationsPerDocument: [6000, 6000],
      divergenceCount: 0,
      dataLossCount: 0,
      unplanned5xxCount: 0,
      costUsd: 0,
      semanticHashesMatch: true,
      optionalBurst: {
        enabled: true,
        operationsPerMinutePerDocument: 480,
        durationSeconds: 60,
      },
    },
    drills: {
      reconnect: { executed: true, recovered: true },
      controlAuthorityOutage: {
        executed: true,
        durationSeconds:
          P519_PRIVATE_ALPHA_CONTRACT.drills.controlAuthorityOutageSeconds,
        existingDocumentRecovered: true,
        newDocumentFailedClosed: true,
      },
      neonOutage: {
        executed: true,
        failedClosed: true,
        recovered: true,
      },
      b2Outage: {
        executed: true,
        lastGoodArtifactReadable: true,
        recovered: true,
      },
      credentialRotation: {
        executed: true,
        oldCredentialRejected: true,
        newCredentialAccepted: true,
      },
      forceOff: {
        executed: true,
        newGrantRejected: true,
        activeSessionsClosedOrReadOnly: true,
      },
      incident: { executed: true },
      export: { executed: true, portableRoundTrip: true },
      restore: { executed: true, rtoMs: 90_000, semanticHashMatch: true },
      revoke: {
        executed: true,
        oldGrantRejected: true,
        oldCredentialRejected: true,
      },
    },
    cleanup: {
      syntheticPrefix: P519_PRIVATE_ALPHA_CONTRACT.syntheticPrefix,
      elapsedMs: 1200,
      databaseRows: 0,
      runtimeConnections: 0,
      runtimeDocuments: 0,
      b2CurrentObjects: 0,
      b2Versions: 0,
      b2MultipartUploads: 0,
      verified: true,
    },
  };
}

test("exact provider-observed private-alpha report passes", () => {
  const result = evaluateP519PrivateAlphaReport(validReport());
  assert.equal(result.ok, true, result.errors.join("\n"));
  assert.equal(result.metrics.joinP95Ms, 600);
});

test("percentile95 uses the nearest-rank definition", () => {
  assert.equal(percentile95([1, 2, 3, 4, 100]), 100);
  assert.equal(percentile95(samples(20, 7)), 7);
});

test("simulated or short soak evidence cannot pass", () => {
  const report = validReport();
  report.source = "simulated";
  report.endedAt = "2026-08-24T08:59:59.000Z";
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /source must equal|3600 seconds/u);
});

test("threshold, divergence and B2 cleanup failures are fail-closed", () => {
  const report = validReport();
  report.observations.joinMs[9] = 8_000;
  report.observations.divergenceCount = 1;
  report.cleanup.b2Versions = 1;
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /joinP95Ms/u);
  assert.match(result.errors.join("\n"), /divergenceCount/u);
  assert.match(result.errors.join("\n"), /cleanup\.b2Versions/u);
});

test("reused evidence requires an explicit validity rationale", () => {
  const report = validReport();
  report.evidence.reused[0].validityRationale = "same";
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /validityRationale/u);
});

test("fresh evidence and owner sign-off must belong to the current run", () => {
  const report = validReport();
  report.evidence.fresh = [
    {},
    {
      source: "stale-provider-run",
      verifiedAt: "2026-08-24T07:59:59.000Z",
    },
  ];
  report.owners.approvedAt = "2026-08-24T09:00:01.000Z";
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /evidence\.fresh\[0\]/u);
  assert.match(result.errors.join("\n"), /current provider-observed window/u);
  assert.match(result.errors.join("\n"), /owners\.approvedAt/u);
});

test("provider incident matrix is exact and fail-closed", () => {
  const report = validReport();
  report.drills.controlAuthorityOutage.durationSeconds = 599;
  report.drills.forceOff.newGrantRejected = false;
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(
    result.errors.join("\n"),
    /controlAuthorityOutage\.durationSeconds/u,
  );
  assert.match(result.errors.join("\n"), /forceOff\.newGrantRejected/u);
});

test("secret-like material is rejected and omitted from redacted summary", () => {
  const report = validReport();
  report.observations.token = "eyJaaaaaaaaaaa.bbbbbbbbbbb.ccccccccccc";
  const result = evaluateP519PrivateAlphaReport(report);
  assert.equal(result.ok, false);
  assert.match(result.errors.join("\n"), /secret material/u);
  const summary = createRedactedP519Summary(report, result);
  const serialized = JSON.stringify(summary);
  assert.doesNotMatch(serialized, /eyJaaaaaaaaaaa/u);
  assert.equal(Object.hasOwn(summary, "observations"), false);
});
