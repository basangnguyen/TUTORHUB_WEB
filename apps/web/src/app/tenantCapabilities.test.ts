import { describe, expect, it } from "vitest";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import {
  requireMatchingTenant,
  tenantCapabilityQueryKeys,
  tenantOperationAvailability,
} from "./tenantCapabilities";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

describe("tenant capabilities", () => {
  it("fails closed while loading or unavailable", () => {
    expect(
      tenantOperationAvailability(
        { data: undefined, isError: false, isPending: true },
        "create_class",
      ),
    ).toEqual({ available: false, reason: "capabilities_loading" });
    expect(
      tenantOperationAvailability(
        { data: undefined, isError: true, isPending: false },
        "create_class",
      ),
    ).toEqual({ available: false, reason: "capabilities_unavailable" });
  });

  it("preserves bounded server reasons for disabled and exhausted operations", () => {
    const capabilities = availableTenantCapabilities(tenantID);
    const query = {
      data: {
        ...capabilities,
        operations: {
          ...capabilities.operations,
          create_class: {
            available: false,
            reason: "feature_disabled" as const,
          },
          activate_class: {
            available: false,
            reason: "quota_exhausted" as const,
          },
          create_class_invite_link: {
            available: false,
            reason: "rate_limited" as const,
          },
        },
      },
      isError: false,
      isPending: false,
    };
    expect(tenantOperationAvailability(query, "create_class")).toEqual({
      available: false,
      reason: "feature_disabled",
    });
    expect(tenantOperationAvailability(query, "activate_class")).toEqual({
      available: false,
      reason: "quota_exhausted",
    });
    expect(
      tenantOperationAvailability(query, "create_class_invite_link"),
    ).toEqual({ available: false, reason: "rate_limited" });
  });

  it("rejects a mismatched tenant projection and scopes cache keys by tenant", () => {
    const otherTenantID = "b5e07a4b-d8b2-4552-9f2f-e96b865cad97";
    expect(() =>
      requireMatchingTenant(
        tenantID,
        availableTenantCapabilities(otherTenantID),
      ),
    ).toThrow(/scope did not match/i);
    expect(tenantCapabilityQueryKeys.detail(tenantID)).not.toEqual(
      tenantCapabilityQueryKeys.detail(otherTenantID),
    );
  });
});
