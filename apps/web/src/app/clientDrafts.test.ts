import { afterEach, describe, expect, it } from "vitest";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import {
  clearClientDrafts,
  readTenantFeatureControlDraft,
  removeTenantFeatureControlDraft,
  writeTenantFeatureControlDraft,
  type TenantFeatureControlDraft,
} from "./clientDrafts";

const scope = {
  actorID: "be85eb92-0f18-4163-85ba-50e4d343d632",
  tenantID: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
};

function draft(): TenantFeatureControlDraft {
  const capabilities = availableTenantCapabilities(scope.tenantID);
  return {
    base: { tenantID: scope.tenantID, version: capabilities.version },
    features: Object.fromEntries(
      Object.entries(capabilities.features).map(([key, value]) => [
        key,
        value.configured_enabled ?? value.enabled,
      ]),
    ) as TenantFeatureControlDraft["features"],
    quotas: Object.fromEntries(
      Object.entries(capabilities.quotas).map(([key, value]) => [
        key,
        String(value.configured_limit ?? value.limit),
      ]),
    ) as TenantFeatureControlDraft["quotas"],
  };
}

describe("principal-bound client drafts", () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it("round-trips a bounded feature-control draft only in the exact scope", () => {
    const value = draft();
    value.quotas.message_sends_per_hour = "6000";

    expect(writeTenantFeatureControlDraft(scope, value, 1_000)).toBe(true);
    expect(readTenantFeatureControlDraft(scope, 1_001)).toEqual(value);
    expect(
      readTenantFeatureControlDraft({ ...scope, actorID: crypto.randomUUID() }),
    ).toBeNull();
    expect(
      readTenantFeatureControlDraft({
        ...scope,
        tenantID: crypto.randomUUID(),
      }),
    ).toBeNull();
  });

  it("deletes expired and malformed records without throwing", () => {
    expect(writeTenantFeatureControlDraft(scope, draft(), 1_000)).toBe(true);
    expect(
      readTenantFeatureControlDraft(scope, 1_000 + 8 * 60 * 60 * 1000 + 1),
    ).toBeNull();
    expect(sessionStorage.length).toBe(0);

    sessionStorage.setItem(
      "tutorhub:draft:v1:tenant_feature_controls:broken",
      "not-json",
    );
    expect(() => clearClientDrafts()).not.toThrow();
    expect(sessionStorage.length).toBe(0);
  });

  it("removes one draft or every TutorHub draft without touching other state", () => {
    expect(writeTenantFeatureControlDraft(scope, draft())).toBe(true);
    sessionStorage.setItem("another-product", "keep");
    removeTenantFeatureControlDraft(scope);
    expect(readTenantFeatureControlDraft(scope)).toBeNull();

    expect(writeTenantFeatureControlDraft(scope, draft())).toBe(true);
    clearClientDrafts();
    expect(readTenantFeatureControlDraft(scope)).toBeNull();
    expect(sessionStorage.getItem("another-product")).toBe("keep");
  });
});
