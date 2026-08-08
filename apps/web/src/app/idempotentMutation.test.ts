import { APIRequestError } from "@tutorhub/api-client";
import { describe, expect, it } from "vitest";
import { shouldRetryIdempotentMutation } from "./idempotentMutation";

describe("idempotent mutation retry policy", () => {
  it("allows one bounded retry for a network failure or HTTP 5xx", () => {
    expect(
      shouldRetryIdempotentMutation(0, new TypeError("fetch failed")),
    ).toBe(true);
    expect(shouldRetryIdempotentMutation(0, new APIRequestError(503))).toBe(
      true,
    );
    expect(shouldRetryIdempotentMutation(1, new APIRequestError(503))).toBe(
      false,
    );
  });

  it("never retries validation, conflict, quota or authorization failures", () => {
    for (const status of [400, 401, 403, 404, 409, 429]) {
      expect(
        shouldRetryIdempotentMutation(0, new APIRequestError(status)),
      ).toBe(false);
    }
    expect(shouldRetryIdempotentMutation(0, new Error("invalid input"))).toBe(
      false,
    );
  });
});
