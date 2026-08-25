import assert from "node:assert/strict";
import test from "node:test";

import {
  normalizeP519Arguments,
  sanitizeP519Output,
  validateP519Environment,
} from "./run-p519-disposable.mjs";

const fakeEnvironment = () =>
  new Map([
    [
      "P5_COLLAB_19_DISPOSABLE_CONFIRM",
      "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
    ],
    [
      "DATABASE_MIGRATION_URL",
      "postgresql://neondb_owner:owner-password@ep-p519.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
    ],
    [
      "DATABASE_POOL_URL",
      "postgresql://tutorhub_runtime:runtime-password@ep-p519-pooler.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
    ],
    [
      "DATABASE_COLLABORATION_URL",
      "postgresql://tutorhub_collab_worker:worker-password@ep-p519.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
    ],
    [
      "DATABASE_POLL_MAINTENANCE_URL",
      "postgresql://tutorhub_poll_maintenance:maintenance-password@ep-p519.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
    ],
    ["B2_ENDPOINT", "https://s3.us-west-004.backblazeb2.com"],
    ["B2_REGION", "us-west-004"],
    ["B2_BUCKET", "tutorhub-p519-disposable"],
    ["B2_KEY_ID", "fake-key-id-123456"],
    ["B2_APPLICATION_KEY", "fake-application-key-1234567890"],
  ]);

test("normalizes exact modes and requires a provider report outside preflight", () => {
  assert.deepEqual(normalizeP519Arguments([]), {
    envFile: ".env.p5-collab-19-disposable.local",
    mode: "preflight",
    reportFile: undefined,
  });
  assert.deepEqual(
    normalizeP519Arguments(["safe.local", "all", "report.json"]),
    {
      envFile: "safe.local",
      mode: "all",
      reportFile: "report.json",
    },
  );
  assert.throws(
    () => normalizeP519Arguments(["safe.local", "soak"]),
    /provider-observed report/u,
  );
  assert.throws(
    () => normalizeP519Arguments(["safe.local", "deploy"]),
    /preflight\|soak\|drills\|cleanup\|all/u,
  );
});

test("accepts four exact disposable roles and scoped B2 credentials", () => {
  const result = validateP519Environment(fakeEnvironment());
  assert.equal(
    new URL(result.DATABASE_COLLABORATION_URL).username,
    "tutorhub_collab_worker",
  );
  assert.equal(result.B2_BUCKET, "tutorhub-p519-disposable");
});

test("fails closed for an invalid confirmation, role or B2 target", () => {
  const invalidConfirmation = fakeEnvironment();
  invalidConfirmation.set("P5_COLLAB_19_DISPOSABLE_CONFIRM", "YES");
  assert.throws(
    () => validateP519Environment(invalidConfirmation),
    /confirmation/u,
  );

  const invalidRole = fakeEnvironment();
  invalidRole.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://another_worker:worker-password@ep-p519.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP519Environment(invalidRole),
    /tutorhub_collab_worker/u,
  );

  const invalidBucket = fakeEnvironment();
  invalidBucket.set("B2_BUCKET", "tutorhub-shared-staging");
  assert.throws(
    () => validateP519Environment(invalidBucket),
    /explicitly disposable/u,
  );
});

test("sanitizes exact secret values and credential-bearing URLs", () => {
  const environment = Object.fromEntries(fakeEnvironment());
  const value = [
    environment.DATABASE_MIGRATION_URL,
    environment.B2_APPLICATION_KEY,
    "eyJaaaaaaaaaaa.bbbbbbbbbbb.ccccccccccc",
  ].join(" ");
  const sanitized = sanitizeP519Output(value, environment);
  assert.doesNotMatch(sanitized, /owner-password/u);
  assert.doesNotMatch(sanitized, /fake-application-key/u);
  assert.doesNotMatch(sanitized, /eyJaaaaaaaaaaa/u);
  assert.match(sanitized, /REDACTED/u);
});
