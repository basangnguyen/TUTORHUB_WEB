import assert from "node:assert/strict";
import test from "node:test";
import {
  b2CredentialIsRevoked,
  validateB2OutageEnvironment,
} from "./p5-collab-01-gate-f3-b2-outage.mjs";

function validEnvironment() {
  return {
    B2_APPLICATION_KEY: "application-key-that-is-long-enough",
    B2_BUCKET: "tutorhub-p5-f3-disposable",
    B2_ENDPOINT: "https://s3.example.invalid",
    B2_KEY_ID: "b2-key-id-that-is-long-enough",
    B2_REGION: "us-west-004",
    COLLAB_ALLOWED_ORIGINS: "https://p5-f3-client.invalid",
    COLLAB_BUILD_ID: "f09333d",
    COLLAB_CONTROL_PLANE_TOKEN: "control-plane-token-that-is-long-enough",
    COLLAB_CONTROL_PLANE_URL: "https://control.example.invalid",
    COLLAB_INSTANCE_COUNT: "1",
    COLLAB_METRICS_TOKEN: "metrics-token-that-is-long-enough",
    COLLAB_RUNTIME_PROFILE: "FREE_PRIVATE_ALPHA",
    DATABASE_COLLABORATION_URL:
      "postgresql://tutorhub_collab_f3:secret@database.example.invalid/neondb?sslmode=require",
    P5_F3_ALLOWED_ORIGIN: "https://p5-f3-client.invalid",
    P5_F3_B2_BUCKET_ID: "bucket-gate-f3",
    P5_F3_CONTROL_ADMIN_TOKEN: "admin-token-that-is-long-enough",
    P5_F3_CONTROL_TOKEN_CURRENT: "control-plane-token-that-is-long-enough",
    P5_F3_CONTROL_TOKEN_NEXT: "next-token-that-is-long-enough",
    P5_F3_CONTROL_URL: "https://control.example.invalid",
    P5_F3_DATABASE_OWNER_URL:
      "postgresql://neondb_owner:secret@database.example.invalid/neondb?sslmode=require",
    P5_F3_DISPOSABLE_CONFIRM: "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY",
    P5_F3_DISPOSABLE_EXPIRES_AT: new Date(
      Date.now() + 86_400_000,
    ).toISOString(),
    P5_F3_HARD_CAP_USD: "0",
    P5_F3_NEON_BRANCH_ID: "br-disposable-gate-f3",
    P5_F3_OUTAGE_CONFIRM:
      "I_UNDERSTAND_P5_F3_B2_KEY_WILL_REMAIN_UNAVAILABLE_FOR_600_SECONDS",
    P5_F3_OUTAGE_SECONDS: "600",
    P5_F3_OUTAGE_TARGET: "b2",
    P5_F3_PROVIDER_DOCUMENT_NAME:
      "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1",
    P5_F3_RENDER_REGION: "singapore",
    P5_F3_RUNTIME_URL: "https://runtime.example.invalid",
  };
}

test("accepts only exact B2 sustained-outage confirmation", () => {
  const validated = validateB2OutageEnvironment(validEnvironment());
  assert.equal(validated.runtimeDatabase.username, "tutorhub_collab_f3");
});

test("rejects a short B2 outage", () => {
  const env = validEnvironment();
  env.P5_F3_OUTAGE_SECONDS = "60";
  assert.throws(
    () => validateB2OutageEnvironment(env),
    /gate_f3_b2_outage_duration_invalid/,
  );
});

test("rejects a non-B2 target", () => {
  const env = validEnvironment();
  env.P5_F3_OUTAGE_TARGET = "neon";
  assert.throws(
    () => validateB2OutageEnvironment(env),
    /gate_f3_b2_outage_target_invalid/,
  );
});

test("accepts only an explicit B2 authorization rejection as revoked", async () => {
  const env = validEnvironment();
  assert.equal(
    await b2CredentialIsRevoked(env, async () => ({ status: 401 })),
    true,
  );
  assert.equal(
    await b2CredentialIsRevoked(env, async () => ({ status: 200 })),
    false,
  );
  await assert.rejects(
    b2CredentialIsRevoked(env, async () => ({ status: 503 })),
    /gate_f3_b2_authorize_unexpected_status/,
  );
});
