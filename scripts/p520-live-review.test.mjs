import assert from "node:assert/strict";
import test from "node:test";

import { createP520AuthorizationPacket } from "./p520-authorization-packet.mjs";
import { completeP520LiveReviewPacket } from "./p520-live-review.mjs";
import { materializeP520AuthorizedPacket } from "./p520-live-runner.mjs";
import { bindP520TenantAllowlist } from "./p520-tenant-allowlist.mjs";

function pendingPacket() {
  const packet = createP520AuthorizationPacket({
    preparedFromCommitSha: "a".repeat(40),
    inheritedBaseline: {
      targetFingerprintSha256: "b".repeat(64),
      deployId: "dep-control.dep-runtime",
    },
  });
  const bound = bindP520TenantAllowlist(packet, {
    schemaVersion: "p5-collab-20-tenant-allowlist-v1",
    tenantIds: [
      "11111111-1111-4111-8111-111111111111",
      "22222222-2222-4222-8222-222222222222",
    ],
  }).boundPacket;
  return materializeP520AuthorizedPacket(bound, {
    approvedAt: "2026-09-18T02:00:00.000Z",
    approvedBy: "owner",
  });
}

test("live review completion records all six evidence-bound reviews", () => {
  const completed = completeP520LiveReviewPacket(pendingPacket(), {
    reviewedAt: "2026-09-18T03:00:00.000Z",
  });
  assert.equal(completed.status, "authorized");
  assert.equal(
    Object.values(completed.reviews).every(
      (review) =>
        review.state === "passed" &&
        review.evidenceRef.startsWith("tmp/p5-collab-20/"),
    ),
    true,
  );
  assert.equal(JSON.stringify(completed).includes("11111111-1111"), false);
});

test("live review completion rejects an already-completed packet", () => {
  const completed = completeP520LiveReviewPacket(pendingPacket(), {
    reviewedAt: "2026-09-18T03:00:00.000Z",
  });
  assert.throws(
    () => completeP520LiveReviewPacket(completed),
    /packet_not_pending/u,
  );
});
