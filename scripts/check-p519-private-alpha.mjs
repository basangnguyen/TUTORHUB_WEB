import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  P519_PRIVATE_ALPHA_CONTRACT,
  P519_SUPPORT_PATH,
} from "./p519-private-alpha-contract.mjs";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const read = (path) => readFileSync(resolve(root, path), "utf8");

const exampleEnvironment = read(".env.example");
assert.match(
  exampleEnvironment,
  /^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false$/mu,
  "classroom whiteboards must remain globally force-off",
);
assert.match(
  exampleEnvironment,
  /^FEATURE_CONTROL_CLASSROOM_WHITEBOARD_CANARY_TENANT_IDS=$/mu,
  "private-alpha tenant allowlist must default to blank",
);

const acceptance = read("docs/P5_COLLAB_19_PRIVATE_ALPHA_ACCEPTANCE.md");
for (const requiredText of [
  "provider-observed",
  "60-minute provider-observed soak",
  "60 phút",
  "preflight",
  "soak",
  "drills",
  "cleanup",
  "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
  "validityRationale",
  P519_SUPPORT_PATH,
]) {
  assert.ok(
    acceptance.includes(requiredText),
    `acceptance document is missing ${JSON.stringify(requiredText)}`,
  );
}
assert.match(
  acceptance,
  /Local chỉ kiểm tra schema và regression/iu,
  "acceptance must distinguish local schema validation from provider soak",
);

assert.equal(P519_PRIVATE_ALPHA_CONTRACT.durationSeconds, 3_600);
assert.deepEqual(P519_PRIVATE_ALPHA_CONTRACT.phases, {
  warmupSeconds: 300,
  steadySeconds: 3_000,
  recoverySeconds: 300,
});
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.workload.documents, 2);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.workload.clientsPerDocument, 5);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.workload.totalConnections, 10);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.workload.shapesPerDocument, 500);
assert.equal(
  P519_PRIVATE_ALPHA_CONTRACT.workload.steadyOperationsPerMinutePerDocument,
  120,
);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.cadence.reconnectSeconds, 600);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.cadence.metricsSeconds, 30);
assert.equal(
  P519_PRIVATE_ALPHA_CONTRACT.drills.controlAuthorityOutageSeconds,
  600,
);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.cadence.semanticCheckSeconds, 300);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.joinP95Ms, 7_500);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.reconnectP95Ms, 7_500);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.convergenceP95Ms, 2_500);
assert.equal(
  P519_PRIVATE_ALPHA_CONTRACT.thresholds.acknowledgementP95Ms,
  1_000,
);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.artifactP95Ms, 2_500);
assert.equal(
  P519_PRIVATE_ALPHA_CONTRACT.thresholds.recoveryTimeObjectiveMs,
  300_000,
);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.cleanupMs, 3_000);
assert.equal(P519_PRIVATE_ALPHA_CONTRACT.thresholds.costUsd, 0);

console.log("[P5-COLLAB-19] private-alpha static contract: PASS");
