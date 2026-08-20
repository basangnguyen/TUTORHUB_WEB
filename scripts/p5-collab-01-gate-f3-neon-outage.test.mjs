import assert from "node:assert/strict";
import { test } from "node:test";
import { validateNeonOutageEnvironment } from "./p5-collab-01-gate-f3-neon-outage.mjs";

test("accepts only the exact disposable Neon role and 600-second confirmation", () => {
  const result = validateNeonOutageEnvironment(validEnvironment());
  assert.equal(result.runtimeRole, "tutorhub_collab_f3");
});

test("rejects a different role, target, duration or confirmation", () => {
  assert.throws(
    () =>
      validateNeonOutageEnvironment({
        ...validEnvironment(),
        DATABASE_COLLABORATION_URL:
          "postgresql://other_role:runtime-secret-value@ep-test.example/neondb?sslmode=require",
      }),
    /runtime_role_not_exact/,
  );
  assert.throws(
    () =>
      validateNeonOutageEnvironment({
        ...validEnvironment(),
        P5_F3_OUTAGE_TARGET: "control",
      }),
    /outage_target_invalid/,
  );
  assert.throws(
    () =>
      validateNeonOutageEnvironment({
        ...validEnvironment(),
        P5_F3_OUTAGE_SECONDS: "60",
      }),
    /outage_duration_invalid/,
  );
  assert.throws(
    () =>
      validateNeonOutageEnvironment({
        ...validEnvironment(),
        P5_F3_OUTAGE_CONFIRM: "wrong",
      }),
    /outage_confirmation_missing/,
  );
});

function validEnvironment() {
  return {
    B2_APPLICATION_KEY: Array(8).fill("test").join("-"),
    B2_BUCKET: "tutorhub-gate-f3-disposable",
    B2_ENDPOINT: "https://s3.us-west-004.backblazeb2.com",
    B2_KEY_ID: Array(9).fill("id").join("-"),
    B2_REGION: "us-west-004",
    COLLAB_ALLOWED_ORIGINS: "https://client-gate-f3.example",
    COLLAB_BUILD_ID: "b58a687",
    COLLAB_CONTROL_PLANE_TOKEN: "control-token-that-is-long-enough",
    COLLAB_CONTROL_PLANE_URL: "https://control-gate-f3.example",
    COLLAB_INSTANCE_COUNT: "1",
    COLLAB_METRICS_TOKEN: "metrics-token-that-is-long-enough",
    COLLAB_RUNTIME_PROFILE: "FREE_PRIVATE_ALPHA",
    DATABASE_COLLABORATION_URL:
      "postgresql://tutorhub_collab_f3:runtime-secret-value@ep-test.example/neondb?sslmode=require",
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
    P5_F3_OUTAGE_CONFIRM:
      "I_UNDERSTAND_P5_F3_NEON_ROLE_WILL_BE_UNAVAILABLE_FOR_600_SECONDS",
    P5_F3_OUTAGE_SECONDS: "600",
    P5_F3_OUTAGE_TARGET: "neon",
    P5_F3_PROVIDER_DOCUMENT_NAME:
      "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1",
    P5_F3_RENDER_REGION: "singapore",
    P5_F3_RUNTIME_URL: "https://runtime-gate-f3.example",
  };
}
