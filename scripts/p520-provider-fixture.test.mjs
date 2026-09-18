import assert from "node:assert/strict";
import test from "node:test";

import { createP520ProviderFixture } from "./p520-provider-fixture.mjs";

const TENANT_A = "019f8f25-6c0c-7c32-8df8-6ae713148870";
const TENANT_B = "019f8f25-6c0c-7c32-9f45-a76e69092f73";

function manifest(tenantIds = [TENANT_A, TENANT_B]) {
  return {
    schemaVersion: "p5-collab-20-tenant-allowlist-v1",
    tenantIds,
  };
}

test("provider fixture deterministically maps one document to each tenant", () => {
  const fixture = createP520ProviderFixture(manifest());
  const repeated = createP520ProviderFixture(manifest([TENANT_B, TENANT_A]));
  assert.equal(fixture.tenantIds.length, 2);
  assert.equal(fixture.documents.length, 2);
  assert.equal(new Set(fixture.documents.map((item) => item.tenantId)).size, 2);
  assert.equal(
    new Set(fixture.documents.map((item) => item.providerDocumentName)).size,
    2,
  );
  assert.deepEqual(fixture, repeated);
});

test("provider fixture emits canonical UUID-shaped resource identifiers", () => {
  const fixture = createP520ProviderFixture(manifest());
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
  for (const document of fixture.documents) {
    assert.match(document.classId, uuid);
    assert.match(document.documentId, uuid);
    assert.match(document.mediaSpaceId, uuid);
    assert.match(document.sessionId, uuid);
  }
});

test("provider fixture rejects an invalid tenant manifest", () => {
  assert.throws(() => createP520ProviderFixture(manifest([TENANT_A])));
  assert.throws(() =>
    createP520ProviderFixture(manifest([TENANT_A, TENANT_A])),
  );
});
