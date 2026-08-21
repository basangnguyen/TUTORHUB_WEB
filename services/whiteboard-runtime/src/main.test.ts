import { describe, expect, it } from "vitest";
import { RuntimeConfigurationError } from "./config.js";
import { startupFailureCode } from "./main.js";
import { ProviderAuthorityGuardError } from "./providerAuthorityGuard.js";

describe("startupFailureCode", () => {
  it("preserves only bounded runtime configuration codes", () => {
    expect(
      startupFailureCode(
        new RuntimeConfigurationError("database_collaboration_url_required"),
      ),
    ).toBe("database_collaboration_url_required");
  });

  it("reports the pinned Node version mismatch", () => {
    expect(startupFailureCode(new Error("runtime_node_version_mismatch"))).toBe(
      "runtime_node_version_mismatch",
    );
  });

  it("preserves only bounded provider authority guard codes", () => {
    expect(
      startupFailureCode(
        new ProviderAuthorityGuardError("provider_authority_duplicate"),
      ),
    ).toBe("provider_authority_duplicate");
  });

  it("does not expose unexpected error messages", () => {
    expect(startupFailureCode(new Error("secret-value"))).toBe(
      "runtime_start_unknown",
    );
  });
});
