import assert from "node:assert/strict";
import test from "node:test";

import { createP520AuthorizationPacket } from "./p520-authorization-packet.mjs";
import { P520_RAMP_EXIT_CONTRACT } from "./p520-ramp-exit-contract.mjs";
import {
  createP520PreRampObservation,
  materializeP520AuthorizedPacket,
} from "./p520-live-runner.mjs";
import { bindP520TenantAllowlist } from "./p520-tenant-allowlist.mjs";

const TENANTS = [
  "11111111-1111-4111-8111-111111111111",
  "22222222-2222-4222-8222-222222222222",
];

function preparation() {
  const packet = createP520AuthorizationPacket({
    generatedAt: "2026-09-18T01:00:00.000Z",
    preparedFromCommitSha: "a".repeat(40),
    inheritedBaseline: {
      targetFingerprintSha256: "b".repeat(64),
      deployId: "dep-control.dep-runtime",
    },
  });
  return bindP520TenantAllowlist(packet, {
    schemaVersion: "p5-collab-20-tenant-allowlist-v1",
    tenantIds: TENANTS,
  }).boundPacket;
}

test("live materializer creates exact pending-review authorization without identifiers", () => {
  const packet = materializeP520AuthorizedPacket(preparation(), {
    approvedAt: "2026-09-18T02:00:00.000Z",
    approvedBy: "owner",
  });
  assert.equal(packet.status, "authorized-pending-live-validation");
  assert.equal(packet.posture.liveActionsAuthorized, true);
  assert.equal(packet.posture.providerMutationAuthorized, true);
  assert.equal(packet.posture.productionAuthorized, false);
  assert.equal(packet.posture.sharedStagingAuthorized, false);
  assert.equal(packet.target.environment, "disposable-private-alpha");
  assert.equal(packet.target.tenantCount, 2);
  assert.deepEqual(
    packet.target.perTenantQuotas,
    P520_RAMP_EXIT_CONTRACT.initialPerTenantQuotaCeilings,
  );
  assert.equal(
    Object.values(packet.reviews).every(
      (review) => review.state === "pending-live-validation",
    ),
    true,
  );
  assert.equal(JSON.stringify(packet).includes(TENANTS[0]), false);
  assert.equal(JSON.stringify(packet).includes(TENANTS[1]), false);
});

test("pre-ramp observation selects the bounded enabled hold without claiming reviews", () => {
  const observation = createP520PreRampObservation();
  assert.equal(observation.oneAuthorityInvariant, true);
  assert.equal(observation.crossTenantLeakCount, 0);
  assert.equal(observation.unbilledChargesUsd, 0);
  assert.match(observation.evidenceBasis, /reviews remain pending/u);
});

test("live materializer rejects unbound preparation", () => {
  assert.throws(
    () =>
      materializeP520AuthorizedPacket(
        createP520AuthorizationPacket({
          preparedFromCommitSha: "a".repeat(40),
        }),
      ),
    /preparation_invalid/u,
  );
});
