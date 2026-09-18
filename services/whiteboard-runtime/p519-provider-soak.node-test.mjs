import assert from "node:assert/strict";
import test from "node:test";

import {
  createBoundItem,
  enqueueReconnect,
  operationsDue,
  selectOperationSource,
  selectProviderReportOutputFile,
  shouldDeferSemanticCheck,
} from "./p519-provider-soak.mjs";

test("steady workload produces exactly 6000 operations per document", () => {
  assert.equal(operationsDue(299_999), 0);
  assert.equal(operationsDue(300_000), 1);
  assert.equal(operationsDue(3_299_999), 6_000);
  assert.equal(operationsDue(3_600_000), 6_000);
});

test("embedded soak wrappers ignore their confirmation token as an output path", () => {
  const argv = ["node", "wrapper.mjs", "--confirm", "confirmation-token"];
  assert.equal(
    selectProviderReportOutputFile("tmp/provider-report.json", false, argv),
    "tmp/provider-report.json",
  );
  assert.equal(
    selectProviderReportOutputFile("provider-report.json", true, argv),
    "confirmation-token",
  );
});

test("drill evidence is bound to all immutable run identifiers", () => {
  const binding = {
    commitSha: "commit",
    deployId: "deploy",
    manifestSha256: "manifest",
    runId: "run",
    targetFingerprint: "target",
  };
  assert.deepEqual(createBoundItem(binding, { executed: true }), {
    commitSha: "commit",
    deployId: "deploy",
    executed: true,
    manifestSha256: "manifest",
    runId: "run",
    targetFingerprint: "target",
  });
});

test("semantic checks defer before a scheduled reconnect starts", () => {
  assert.equal(
    shouldDeferSemanticCheck({
      plannedDisruption: false,
      reconnecting: false,
      scheduledReconnects: 1,
    }),
    true,
  );
  assert.equal(
    shouldDeferSemanticCheck({
      plannedDisruption: false,
      reconnecting: false,
      scheduledReconnects: 0,
    }),
    false,
  );
});

test("queued reconnects hold the semantic barrier until completion", async () => {
  let releaseReconnect;
  const reconnectGate = new Promise((resolve) => {
    releaseReconnect = resolve;
  });
  const state = {
    backgroundFailure: null,
    plannedDisruption: false,
    reconnecting: false,
    reconnectChain: Promise.resolve(),
    scheduledReconnects: 0,
  };

  const pendingReconnect = enqueueReconnect(state, () => reconnectGate);
  assert.equal(state.scheduledReconnects, 1);
  assert.equal(shouldDeferSemanticCheck(state), true);

  releaseReconnect();
  await pendingReconnect;
  assert.equal(state.scheduledReconnects, 0);
  assert.equal(shouldDeferSemanticCheck(state), false);
});

test("operations prefer a live authenticated source during rolling reconnect", () => {
  const destroyed = {
    documentName: "document-a",
    provider: { isAuthenticated: true },
    transportDestroyed: true,
  };
  const live = {
    documentName: "document-a",
    provider: { isAuthenticated: true },
    transportDestroyed: false,
  };

  assert.equal(selectOperationSource([destroyed, live], "document-a"), live);
  assert.equal(selectOperationSource([destroyed], "document-a"), destroyed);
});
