import { APIRequestError } from "@tutorhub/api-client";
import { describe, expect, it } from "vitest";
import { shouldConcealTenantScopedData } from "./tenantDataAccess";

describe("shouldConcealTenantScopedData", () => {
  it.each([401, 403, 404])(
    "conceals cached tenant data after HTTP %s",
    (status) => {
      expect(shouldConcealTenantScopedData(new APIRequestError(status))).toBe(
        true,
      );
    },
  );

  it.each([400, 409, 429, 500, 503])(
    "keeps degraded-data behavior for HTTP %s",
    (status) => {
      expect(shouldConcealTenantScopedData(new APIRequestError(status))).toBe(
        false,
      );
    },
  );

  it("ignores non-API errors", () => {
    expect(
      shouldConcealTenantScopedData(new Error("network unavailable")),
    ).toBe(false);
  });
});
