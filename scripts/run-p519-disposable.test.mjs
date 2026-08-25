import assert from "node:assert/strict";
import test from "node:test";

import {
  P519_ACL_QUERY,
  normalizeP519Arguments,
  resolveP519BindingOutputPath,
  sanitizeP519Output,
  validateP519Environment,
} from "./run-p519-disposable.mjs";

const fakeDatabaseUrl = (username, password, hostname) => {
  const url = new URL("postgresql://example.invalid/neondb");
  url.username = username;
  url.password = password;
  url.hostname = hostname;
  url.searchParams.set("sslmode", "require");
  return url.toString();
};

const fakeEnvironment = () =>
  new Map([
    [
      "P5_COLLAB_19_DISPOSABLE_CONFIRM",
      "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
    ],
    [
      "DATABASE_MIGRATION_URL",
      fakeDatabaseUrl(
        "neondb_owner",
        "owner-password",
        "ep-p519.ap-southeast-1.aws.neon.tech",
      ),
    ],
    [
      "DATABASE_POOL_URL",
      fakeDatabaseUrl(
        "tutorhub_runtime",
        "runtime-password",
        "ep-p519-pooler.ap-southeast-1.aws.neon.tech",
      ),
    ],
    [
      "DATABASE_COLLABORATION_URL",
      fakeDatabaseUrl(
        "tutorhub_collab_worker",
        "worker-password",
        "ep-p519.ap-southeast-1.aws.neon.tech",
      ),
    ],
    [
      "DATABASE_POLL_MAINTENANCE_URL",
      fakeDatabaseUrl(
        "tutorhub_poll_maintenance",
        "maintenance-password",
        "ep-p519.ap-southeast-1.aws.neon.tech",
      ),
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
  assert.deepEqual(
    normalizeP519Arguments([
      "safe.local",
      "binding",
      "tmp/p5-collab-19/run-binding.json",
    ]),
    {
      envFile: "safe.local",
      mode: "binding",
      reportFile: "tmp/p5-collab-19/run-binding.json",
    },
  );
  assert.throws(
    () => normalizeP519Arguments(["safe.local", "binding"]),
    /trusted binding JSON output/u,
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

test("confines trusted binding output below tmp/p5-collab-19", () => {
  const outputPath = resolveP519BindingOutputPath(
    "tmp/p5-collab-19/run-binding.json",
  );
  assert.ok(outputPath.endsWith("run-binding.json"));
  assert.throws(
    () => resolveP519BindingOutputPath("tmp/run-binding.json"),
    /under tmp/u,
  );
  assert.throws(
    () => resolveP519BindingOutputPath("tmp/p5-collab-19/run-binding.txt"),
    /JSON file/u,
  );
});

test("queries exact TutorHub whiteboard ACL relations", () => {
  assert.match(P519_ACL_QUERY, /n\.nspname = 'tutorhub'/u);
  assert.match(P519_ACL_QUERY, /c\.relname ~ '\^whiteboard_'/u);
  assert.doesNotMatch(P519_ACL_QUERY, /n\.nspname = 'public'/u);
  assert.doesNotMatch(P519_ACL_QUERY, /collab%/u);
});

test("accepts four exact disposable roles and scoped B2 credentials", () => {
  const result = validateP519Environment(fakeEnvironment());
  assert.equal(
    new URL(result.DATABASE_COLLABORATION_URL).username,
    "tutorhub_collab_worker",
  );
  assert.equal(result.B2_BUCKET, "tutorhub-p519-disposable");
});

test("accepts a previously proven disposable bucket with exact confirmation", () => {
  const environment = fakeEnvironment();
  environment.set("B2_BUCKET", "tutorhub-p517-private-alpha");
  environment.set(
    "P5_COLLAB_19_B2_DISPOSABLE_CONFIRM",
    "I_UNDERSTAND_P5_COLLAB_19_B2_DISPOSABLE_ONLY",
  );
  const result = validateP519Environment(environment);
  assert.equal(result.B2_BUCKET, "tutorhub-p517-private-alpha");
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
    fakeDatabaseUrl(
      "another_worker",
      "worker-password",
      "ep-p519.ap-southeast-1.aws.neon.tech",
    ),
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

  invalidBucket.set("P5_COLLAB_19_B2_DISPOSABLE_CONFIRM", "YES");
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
