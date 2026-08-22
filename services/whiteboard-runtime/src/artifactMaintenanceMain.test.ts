import { describe, expect, it } from "vitest";
import {
  ArtifactMaintenanceConfigurationError,
  loadArtifactMaintenanceConfig,
} from "./artifactMaintenanceMain.js";

const validEnvironment = {
  B2_APPLICATION_KEY: "application-key-value-1234567890",
  B2_BUCKET: "tutorhub-artifact-private",
  B2_ENDPOINT: "https://s3.us-west-004.backblazeb2.com",
  B2_KEY_ID: "key-id-value-123456789012345",
  B2_REGION: "us-west-004",
  DATABASE_POLL_MAINTENANCE_URL:
    "postgresql://maintenance:password-value@db.example/neondb?sslmode=require",
  P5_COLLAB_07_MAINTENANCE_CONFIRM:
    "I_UNDERSTAND_P5_COLLAB_07_MAINTENANCE_ONLY",
} as const;

describe("artifact maintenance configuration", () => {
  it("loads the bounded one-shot profile", () => {
    const config = loadArtifactMaintenanceConfig(validEnvironment);
    expect(config.batch).toBe(10);
    expect(config.leaseSeconds).toBe(120);
    expect(config.b2.endpoint).toBe("https://s3.us-west-004.backblazeb2.com");
  });

  it.each([
    [
      { ...validEnvironment, P5_COLLAB_07_MAINTENANCE_CONFIRM: "" },
      "artifact_maintenance_confirmation_required",
    ],
    [
      {
        ...validEnvironment,
        DATABASE_POLL_MAINTENANCE_URL:
          "postgresql://maintenance:password@db-pooler.example/neondb?sslmode=require",
      },
      "artifact_maintenance_database_url_invalid",
    ],
    [
      { ...validEnvironment, B2_ENDPOINT: "http://s3.example" },
      "b2_endpoint_invalid",
    ],
    [
      { ...validEnvironment, COLLAB_ARTIFACT_PURGE_BATCH: "26" },
      "collab_artifact_purge_batch_invalid",
    ],
  ])("fails closed for invalid maintenance configuration", (env, code) => {
    expect(() => loadArtifactMaintenanceConfig(env)).toThrowError(
      new ArtifactMaintenanceConfigurationError(code),
    );
  });
});
