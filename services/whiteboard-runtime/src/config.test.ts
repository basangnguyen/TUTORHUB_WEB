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
    expect(config.drainTimeoutMs).toBe(45_000);
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
  ])(
    "fails closed for invalid topology or credential metadata",
    (environment, code) => {
      expect(() => loadRuntimeConfig(environment)).toThrowError(
        expect.objectContaining<Partial<RuntimeConfigurationError>>({ code }),
      );
    },
  );
});
