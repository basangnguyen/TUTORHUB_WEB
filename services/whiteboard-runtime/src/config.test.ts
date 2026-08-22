import { describe, expect, it } from "vitest";
import { loadRuntimeConfig, RuntimeConfigurationError } from "./config.js";

const validEnvironment = {
  B2_APPLICATION_KEY: "application-key-value-that-is-long-enough",
  B2_BUCKET: "tutorhub-private-alpha",
  B2_ENDPOINT: "https://s3.ap-southeast-1.backblazeb2.com",
  B2_KEY_ID: "key-id-value-that-is-long-enough",
  B2_REGION: "ap-southeast-1",
  COLLAB_ALLOWED_ORIGINS: "https://tutorhub-web.pages.dev",
  COLLAB_CONTROL_PLANE_TOKEN: "control-plane-token-that-is-long-enough",
  COLLAB_CONTROL_PLANE_URL: "https://api.tutorhub.example",
  COLLAB_INSTANCE_COUNT: "1",
  COLLAB_METRICS_TOKEN: "metrics-token-that-is-long-enough",
  COLLAB_RUNTIME_PROFILE: "FREE_PRIVATE_ALPHA",
  DATABASE_COLLABORATION_URL:
    "postgresql://collaboration:secret-value@ep-direct.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  NODE_ENV: "production",
} as const;

describe("loadRuntimeConfig", () => {
  it("accepts the exact single-instance free private-alpha profile", () => {
    const config = loadRuntimeConfig(validEnvironment);

    expect(config.profile).toBe("FREE_PRIVATE_ALPHA");
    expect(config.allowedOrigins).toEqual(
      new Set(["https://tutorhub-web.pages.dev"]),
    );
    expect(config.maxConnectionsPerDocument).toBe(50);
    expect(config.maxConnectionsPerTenant).toBe(100);
    expect(config.maxDocumentBytes).toBe(10 * 1024 * 1024);
    expect(config.maxUpdateBytes).toBe(768 * 1024);
    expect(config.maxAwarenessBytes).toBe(16 * 1024);
    expect(config.maxAwarenessStates).toBe(1);
    expect(config.maxReconnectAttempts).toBe(8);
    expect(config.drainTimeoutMs).toBe(45_000);
    expect(config.artifactWorker.enabled).toBe(false);
  });

  it("enables the artifact worker only with an explicit binding key", () => {
    const config = loadRuntimeConfig({
      ...validEnvironment,
      COLLAB_ARTIFACT_WORKER_ENABLED: "true",
      COLLAB_SNAPSHOT_BINDING_KEY_ID: "snapshot-binding-v1",
      COLLAB_SNAPSHOT_BINDING_KEY:
        "not-a-real-secret-but-long-enough-for-tests",
    });

    expect(config.artifactWorker.enabled).toBe(true);
    expect(config.artifactWorker.leaseSeconds).toBe(30);
    expect(config.artifactWorker.pollIntervalMs).toBe(500);
  });

  it.each([
    [
      { ...validEnvironment, COLLAB_INSTANCE_COUNT: "2" },
      "collab_instance_count_invalid",
    ],
    [
      { ...validEnvironment, REDIS_URL: "rediss://not-used.example" },
      "runtime_redis_forbidden_for_free_profile",
    ],
    [
      { ...validEnvironment, COLLAB_MAX_AWARENESS_STATES: "2" },
      "collab_max_awareness_states_invalid",
    ],
    [
      {
        ...validEnvironment,
        DATABASE_COLLABORATION_URL:
          "postgresql://runtime:secret@ep-name-pooler.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
      },
      "database_url_invalid",
    ],
    [
      { ...validEnvironment, B2_ENDPOINT: "http://storage.example" },
      "b2_endpoint_invalid",
    ],
    [
      {
        ...validEnvironment,
        COLLAB_MAX_CONNECTIONS: "10",
        COLLAB_MAX_CONNECTIONS_PER_TENANT: "11",
      },
      "runtime_policy_limits_invalid",
    ],
    [
      {
        ...validEnvironment,
        COLLAB_MAX_FRAME_BYTES: String(512 * 1024),
        COLLAB_MAX_UPDATE_BYTES: String(768 * 1024),
      },
      "runtime_policy_limits_invalid",
    ],
    [
      {
        ...validEnvironment,
        COLLAB_MAX_DOCUMENT_BYTES: String(10 * 1024 * 1024 + 1),
      },
      "collab_max_document_bytes_invalid",
    ],
    [
      { ...validEnvironment, COLLAB_ARTIFACT_WORKER_ENABLED: "true" },
      "artifact_binding_key_required",
    ],
  ])(
    "fails closed for invalid topology or credential metadata",
    (environment, code) => {
      expect(() => loadRuntimeConfig(environment)).toThrowError(
        expect.objectContaining<Partial<RuntimeConfigurationError>>({ code }),
      );
    },
  );
});
