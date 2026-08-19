import assert from "node:assert/strict";
import { test } from "node:test";
import { validateGateF3Environment } from "./require-p5-collab-01-gate-f3-confirm.mjs";

test("accepts an exact isolated F.3 environment without returning secrets", () => {
  const result = validateGateF3Environment(validEnvironment());
  assert.deepEqual(Object.keys(result).sort(), [
    "allowedOrigin",
    "controlUrl",
    "ownerDatabase",
    "runtimeDatabase",
    "runtimeUrl",
  ]);
});

test("rejects poolers, shared-stage variables and nonzero spend", () => {
  assert.throws(
    () =>
      validateGateF3Environment({
        ...validEnvironment(),
        DATABASE_COLLABORATION_URL:
          "postgresql://runtime:runtime-secret-value@ep-test-pooler.example/neondb?sslmode=require",
      }),
    /direct TLS PostgreSQL URL/,
  );
  assert.throws(
    () =>
      validateGateF3Environment({
        ...validEnvironment(),
        DATABASE_POOL_URL: "present",
      }),
    /shared-stage or unrelated provider credential is present/,
  );
  assert.throws(
    () =>
      validateGateF3Environment({
        ...validEnvironment(),
        P5_F3_HARD_CAP_USD: "1",
      }),
    /must be exactly 0/,
  );
});

function validEnvironment() {
  return {
    B2_APPLICATION_KEY: Array(8).fill("test").join("-"),
    B2_BUCKET: "tutorhub-gate-f3-disposable",
    B2_ENDPOINT: "https://s3.us-west-004.backblazeb2.com",
    B2_KEY_ID: "b2-key-id-that-is-long-enough",
    B2_REGION: "us-west-004",
    COLLAB_ALLOWED_ORIGINS: "https://client-gate-f3.example",
    COLLAB_BUILD_ID: "504ea79",
    COLLAB_CONTROL_PLANE_TOKEN: "control-token-that-is-long-enough",
    COLLAB_CONTROL_PLANE_URL: "https://control-gate-f3.example",
    COLLAB_INSTANCE_COUNT: "1",
    COLLAB_METRICS_TOKEN: "metrics-token-that-is-long-enough",
    COLLAB_RUNTIME_PROFILE: "FREE_PRIVATE_ALPHA",
    DATABASE_COLLABORATION_URL:
      "postgresql://runtime:runtime-secret-value@ep-test.example/neondb?sslmode=require",
    P5_F3_ALLOWED_ORIGIN: "https://client-gate-f3.example",
    P5_F3_B2_BUCKET_ID: "bucket-gate-f3",
    P5_F3_CONTROL_ADMIN_TOKEN: "admin-token-that-is-long-enough",
    P5_F3_CONTROL_TOKEN_CURRENT: "control-token-that-is-long-enough",
    P5_F3_CONTROL_URL: "https://control-gate-f3.example",
    P5_F3_DATABASE_OWNER_URL:
      "postgresql://owner:owner-secret-value@ep-test.example/neondb?sslmode=require",
    P5_F3_DISPOSABLE_CONFIRM: "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY",
    P5_F3_DISPOSABLE_EXPIRES_AT: new Date(Date.now() + 864e5).toISOString(),
    P5_F3_HARD_CAP_USD: "0",
    P5_F3_NEON_BRANCH_ID: "br-gate-f3",
    P5_F3_PROVIDER_DOCUMENT_NAME:
      "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1",
    P5_F3_RENDER_REGION: "singapore",
    P5_F3_RUNTIME_URL: "https://runtime-gate-f3.example",
  };
}
