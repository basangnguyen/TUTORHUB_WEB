import assert from "node:assert/strict";
import { test } from "vitest";
import {
  P519_DOCUMENTS,
  buildP519GrantRequest,
  cleanupZero,
  dependencyUp,
  durationBucket,
  metricValue,
  validateP519ProviderEnvironment,
} from "./p519-provider-preflight.mjs";
import { P519_PROVIDER_FIXTURE } from "../../scripts/p519-provider-fixture.mjs";

const validEnvironment = {
  COLLAB_METRICS_TOKEN: "m".repeat(32),
  P5_COLLAB_19_ALLOWED_ORIGIN: "https://p519-private-alpha.invalid",
  P5_COLLAB_19_CONTROL_ADMIN_TOKEN: "a".repeat(32),
  P5_COLLAB_19_CONTROL_URL: "https://p519-control.example",
  P5_COLLAB_19_DISPOSABLE_CONFIRM: "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
  P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES: P519_DOCUMENTS.join(","),
  P5_COLLAB_19_RUNTIME_URL: "https://p519-runtime.example",
};

test("validates exact isolated provider environment", () => {
  const result = validateP519ProviderEnvironment(validEnvironment);
  assert.deepEqual(result.documents, P519_DOCUMENTS);
  assert.equal(result.controlUrl, "https://p519-control.example");
  assert.equal(result.runtimeUrl, "https://p519-runtime.example");
});

test("uses UUID-backed disposable provider fixtures for grant requests", () => {
  const request = buildP519GrantRequest(P519_DOCUMENTS[0]);
  assert.equal(request.actor_id, P519_PROVIDER_FIXTURE.actorId);
  assert.equal(request.tenant_id, P519_PROVIDER_FIXTURE.tenantId);
  assert.equal(
    request.document_id,
    P519_PROVIDER_FIXTURE.documents[0].documentId,
  );
  assert.equal(
    request.session_id,
    P519_PROVIDER_FIXTURE.documents[0].sessionId,
  );
});

test("uses one deterministic synthetic actor per provider client", () => {
  const actorIds = Array.from(
    { length: 10 },
    (_, participantIndex) =>
      buildP519GrantRequest(
        P519_DOCUMENTS[participantIndex < 5 ? 0 : 1],
        participantIndex,
      ).actor_id,
  );
  assert.equal(actorIds[0], P519_PROVIDER_FIXTURE.actorId);
  assert.equal(new Set(actorIds).size, 10);
  assert.throws(
    () => buildP519GrantRequest(P519_DOCUMENTS[0], -1),
    /participant_index/u,
  );
  assert.throws(
    () => buildP519GrantRequest(P519_DOCUMENTS[0], 10),
    /participant_index/u,
  );
});

test("rejects missing confirmation, bad documents and shared endpoint", () => {
  assert.throws(
    () =>
      validateP519ProviderEnvironment({
        ...validEnvironment,
        P5_COLLAB_19_DISPOSABLE_CONFIRM: "",
      }),
    /confirmation/u,
  );
  assert.throws(
    () =>
      validateP519ProviderEnvironment({
        ...validEnvironment,
        P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES: P519_DOCUMENTS[0],
      }),
    /documents/u,
  );
  assert.throws(
    () =>
      validateP519ProviderEnvironment({
        ...validEnvironment,
        P5_COLLAB_19_RUNTIME_URL: validEnvironment.P5_COLLAB_19_CONTROL_URL,
      }),
    /isolated/u,
  );
});

test("parses exact metrics and verifies cleanup dependencies", () => {
  const metrics = [
    'collab_connections_current{capability="edit"} 0',
    'collab_connections_current{capability="present"} 0',
    'collab_connections_current{capability="view"} 0',
    "collab_documents_current 0",
    "collab_dirty_documents 0",
    'collab_dependency_up{dependency="control_plane"} 1',
    'collab_dependency_up{dependency="persistence"} 1',
    'collab_dependency_up{dependency="snapshot"} 1',
  ].join("\n");
  assert.equal(metricValue(metrics, "collab_documents_current"), 0);
  assert.equal(cleanupZero(metrics), true);
  assert.equal(dependencyUp(metrics, "snapshot"), true);
  assert.equal(dependencyUp(metrics, "authority_guard"), false);
});

test("uses bounded non-sensitive duration buckets", () => {
  assert.equal(durationBucket(999), "lt_1s");
  assert.equal(durationBucket(2_499), "lt_2_5s");
  assert.equal(durationBucket(7_499), "lt_7_5s");
  assert.equal(durationBucket(119_999), "lt_120s");
  assert.equal(durationBucket(120_000), "gte_120s");
});
