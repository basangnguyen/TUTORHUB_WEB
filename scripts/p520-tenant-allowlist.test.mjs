import assert from "node:assert/strict";
import test from "node:test";

import { createP520AuthorizationPacket } from "./p520-authorization-packet.mjs";
import {
  assertP520TenantOutputAvailable,
  bindP520TenantAllowlist,
  canonicalizeP520TenantManifest,
  parseP520TenantBindArgs,
  resolveP520TenantInput,
  resolveP520TenantOutput,
} from "./p520-tenant-allowlist.mjs";

const TENANT_A = "019f8f25-6c0c-7c32-8df8-6ae713148870";
const TENANT_B = "019f8f25-6c0c-7c32-9f45-a76e69092f73";
const TENANT_C = "019f8f25-6c0c-7c32-a24f-ca98a167ae22";

function manifest(tenantIds = [TENANT_A, TENANT_B]) {
  return {
    schemaVersion: "p5-collab-20-tenant-allowlist-v1",
    tenantIds,
  };
}

function packet() {
  return createP520AuthorizationPacket({
    generatedAt: "2026-09-17T10:00:00.000Z",
    preparedFromCommitSha: "f".repeat(40),
  });
}

test("exact-two canonical tenants produce an order-independent hash", () => {
  const forward = canonicalizeP520TenantManifest(manifest());
  const reverse = canonicalizeP520TenantManifest(
    manifest([TENANT_B, TENANT_A]),
  );
  assert.equal(forward.tenantCount, 2);
  assert.match(forward.tenantAllowlistSha256, /^[a-f0-9]{64}$/u);
  assert.equal(forward.tenantAllowlistSha256, reverse.tenantAllowlistSha256);
});

test("tenant manifest rejects wrong count and duplicate tenants", () => {
  for (const tenantIds of [
    [TENANT_A],
    [TENANT_A, TENANT_B, TENANT_C],
    [TENANT_A, TENANT_A],
  ]) {
    assert.throws(() => canonicalizeP520TenantManifest(manifest(tenantIds)));
  }
});

test("tenant manifest rejects non-canonical IDs and extra fields", () => {
  assert.throws(() =>
    canonicalizeP520TenantManifest(
      manifest([TENANT_A.toUpperCase(), TENANT_B]),
    ),
  );
  assert.throws(() =>
    canonicalizeP520TenantManifest({
      ...manifest(),
      authorization: "not-accepted-here",
    }),
  );
});

test("binding records only hash and count while remaining fail-closed", () => {
  const { boundPacket, binding } = bindP520TenantAllowlist(
    packet(),
    manifest(),
  );
  assert.equal(binding.tenantCount, 2);
  assert.equal(
    boundPacket.preparation.proposedTarget.tenantAllowlistSha256,
    binding.tenantAllowlistSha256,
  );
  assert.equal(boundPacket.preparation.proposedTarget.tenantCount, 2);
  assert.equal(boundPacket.target.tenantCount, null);
  assert.equal(boundPacket.posture.liveActionsAuthorized, false);
  assert.equal(boundPacket.posture.providerMutationAuthorized, false);
  assert.equal(JSON.stringify(boundPacket).includes(TENANT_A), false);
  assert.equal(JSON.stringify(boundPacket).includes(TENANT_B), false);
});

test("an already-bound preparation packet cannot be rebound", () => {
  const first = bindP520TenantAllowlist(packet(), manifest()).boundPacket;
  assert.throws(
    () => bindP520TenantAllowlist(first, manifest()),
    /packet_not_bindable/u,
  );
});

test("tenant binder refuses to overwrite an existing output", () => {
  assert.equal(
    assertP520TenantOutputAvailable("authorization-bound.json", () => false),
    "authorization-bound.json",
  );
  assert.throws(
    () =>
      assertP520TenantOutputAvailable("authorization-bound.json", () => true),
    /tenant_output_exists/u,
  );
});

test("tenant binder paths stay below private tmp and exclude env files", () => {
  assert.match(
    resolveP520TenantInput("tmp/p5-collab-20/tenants.json"),
    /tmp[\\/]p5-collab-20[\\/]tenants\.json$/u,
  );
  assert.match(
    resolveP520TenantOutput("tmp/p5-collab-20/authorization-bound.json"),
    /tmp[\\/]p5-collab-20[\\/]authorization-bound\.json$/u,
  );
  assert.throws(() => resolveP520TenantInput("tmp/outside.json"));
  assert.throws(() => resolveP520TenantOutput("tmp/p5-collab-20/.env.local"));
});

test("tenant binder rejects live flags and incomplete arguments", () => {
  assert.throws(
    () =>
      parseP520TenantBindArgs([
        "--packet",
        "tmp/p5-collab-20/authorization.json",
        "--tenants",
        "tmp/p5-collab-20/tenants.json",
        "--output",
        "tmp/p5-collab-20/authorization-bound.json",
        "--live",
      ]),
    /live_execution_not_implemented/u,
  );
  assert.throws(
    () =>
      parseP520TenantBindArgs([
        "--packet",
        "tmp/p5-collab-20/authorization.json",
      ]),
    /usage_invalid/u,
  );
});
