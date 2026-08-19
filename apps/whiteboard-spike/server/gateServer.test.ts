// @vitest-environment node
import { describe, expect, it } from "vitest";
import { OneTimeGrantAuthority } from "./gateServer";

const claims = {
  tenantHash: "tenant-hash",
  documentHash: "document-hash",
  actorHash: "actor-hash",
  capability: "edit" as const,
  generation: 7,
  origin: "http://127.0.0.1:4178",
};

describe("OneTimeGrantAuthority", () => {
  it("exchanges once with exact document and origin binding", () => {
    const authority = new OneTimeGrantAuthority(() => 1_000);
    const issued = authority.issue(claims, 5_000);
    expect(
      authority.exchange(issued.grant, {
        documentHash: claims.documentHash,
        origin: claims.origin,
      }),
    ).toMatchObject(claims);
    expect(() =>
      authority.exchange(issued.grant, {
        documentHash: claims.documentHash,
        origin: claims.origin,
      }),
    ).toThrow("grant_invalid_or_replayed");
  });

  it("consumes a mismatched grant and denies replay", () => {
    const authority = new OneTimeGrantAuthority(() => 1_000);
    const issued = authority.issue(claims, 5_000);
    expect(() =>
      authority.exchange(issued.grant, {
        documentHash: "other-document",
        origin: claims.origin,
      }),
    ).toThrow("grant_binding_mismatch");
    expect(() =>
      authority.exchange(issued.grant, {
        documentHash: claims.documentHash,
        origin: claims.origin,
      }),
    ).toThrow("grant_invalid_or_replayed");
  });

  it("expires without exposing claim values", () => {
    let now = 1_000;
    const authority = new OneTimeGrantAuthority(() => now);
    const issued = authority.issue(claims, 1_000);
    now = 2_001;
    expect(() =>
      authority.exchange(issued.grant, {
        documentHash: claims.documentHash,
        origin: claims.origin,
      }),
    ).toThrow("grant_expired");
  });
});
