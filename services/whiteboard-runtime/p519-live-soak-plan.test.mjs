import assert from "node:assert/strict";
import test from "node:test";

import {
  P519_DOCUMENTS,
  cadenceMoments,
  createP519LivePlan,
  dueCount,
} from "./p519-live-soak-plan.mjs";

test("creates the exact P5-COLLAB-19 60-minute workload", () => {
  const plan = createP519LivePlan();
  assert.equal(plan.durationMs, 3_600_000);
  assert.deepEqual(plan.documents, P519_DOCUMENTS);
  assert.equal(plan.clientsPerDocument, 5);
  assert.equal(plan.totalConnections, 10);
  assert.equal(plan.shapesPerDocument, 500);
  assert.equal(plan.operationIntervalMs, 500);
  assert.equal(plan.operationsPerDocument, 7_200);
  assert.equal(plan.metricsMomentsMs.length, 120);
  assert.equal(plan.semanticMomentsMs.length, 12);
  assert.equal(plan.reconnectMomentsMs.length, 5);
  assert.equal(plan.reconnectEvents, 50);
});

test("cadence counters are deterministic at boundaries", () => {
  const moments = cadenceMoments(100, 25, true);
  assert.deepEqual(moments, [0, 25, 50, 75]);
  assert.equal(dueCount(moments, -1), 0);
  assert.equal(dueCount(moments, 0), 1);
  assert.equal(dueCount(moments, 49), 2);
  assert.equal(dueCount(moments, 100), 4);
});
