import { describe, expect, it } from "vitest";

import { RuntimeTelemetry } from "./telemetry.js";

describe("RuntimeTelemetry", () => {
  it("renders only bounded labels and never accepts tenant or document identifiers", () => {
    const telemetry = new RuntimeTelemetry("p5-collab-09-test");
    telemetry.connection("edit", "accepted");
    telemetry.policyRejected("connection_quota");
    telemetry.policyRejected("operation_quota");
    telemetry.dependency("control_plane", true);

    const rendered = telemetry.render();

    expect(rendered).toContain(
      'collab_policy_rejection_total{reason="connection_quota"} 1',
    );
    expect(rendered).toContain(
      'collab_policy_rejection_total{reason="operation_quota"} 1',
    );
    expect(rendered).toContain(
      'collab_dependency_up{dependency="control_plane"} 1',
    );
    for (const forbidden of [
      "tenant_id",
      "document_id",
      "user_id",
      "provider_credential",
      "access_token",
      "document_content",
    ]) {
      expect(rendered).not.toContain(forbidden);
    }
  });
});
