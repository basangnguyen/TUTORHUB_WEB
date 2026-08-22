import assert from "node:assert/strict";
import test from "node:test";

import {
  parseEnvFile,
  validateP507Environment,
} from "./run-p507-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY";

function validEnvironment() {
  return parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-disposable.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL=postgresql://tutorhub_runtime:core-secret@ep-disposable-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_COLLABORATION_URL=postgresql://tutorhub_collab_worker:worker-secret@ep-disposable.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POLL_MAINTENANCE_URL=postgresql://tutorhub_poll_maintenance:maintenance-secret@ep-disposable.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
B2_ENDPOINT=https://s3.us-west-004.backblazeb2.com
B2_REGION=us-west-004
B2_BUCKET=tutorhub-p5-collab-07-disposable
B2_KEY_ID=disposable-key-id
B2_APPLICATION_KEY=disposable-application-key-value
P5_COLLAB_07_DISPOSABLE_CONFIRM=${confirmation}
IGNORED_SECRET=must-not-be-forwarded
`);
}

test("allowlists four exact database roles and scoped B2 inputs", () => {
  const result = validateP507Environment(validEnvironment());
  assert.deepEqual(Object.keys(result).sort(), [
    "B2_APPLICATION_KEY",
    "B2_BUCKET",
    "B2_ENDPOINT",
    "B2_KEY_ID",
    "B2_REGION",
    "DATABASE_COLLABORATION_URL",
    "DATABASE_MIGRATION_URL",
    "DATABASE_POLL_MAINTENANCE_URL",
    "DATABASE_POOL_URL",
    "P5_COLLAB_02_DISPOSABLE_CONFIRM",
    "P5_COLLAB_07_ACL_PROVISION_CONFIRM",
    "P5_COLLAB_07_B2_CONFIRM",
    "P5_COLLAB_07_DISPOSABLE_CONFIRM",
    "P5_COLLAB_07_LIFECYCLE_CONFIRM",
  ]);
});

test("rejects cross-branch, pooled worker and duplicate database roles", () => {
  const crossBranch = validEnvironment();
  crossBranch.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_collab_worker:worker-secret@ep-other.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP507Environment(crossBranch),
    /one disposable branch/u,
  );

  const pooledWorker = validEnvironment();
  pooledWorker.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_collab_worker:worker-secret@ep-disposable-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(() => validateP507Environment(pooledWorker), /direct\/pooled/u);

  const duplicateRole = validEnvironment();
  duplicateRole.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_runtime:worker-secret@ep-disposable.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP507Environment(duplicateRole),
    /four distinct roles/u,
  );
});

test("rejects missing confirmation and credential-bearing B2 endpoint", () => {
  const missingConfirmation = validEnvironment();
  missingConfirmation.delete("P5_COLLAB_07_DISPOSABLE_CONFIRM");
  assert.throws(
    () => validateP507Environment(missingConfirmation),
    /confirmation/u,
  );

  const unsafeEndpoint = validEnvironment();
  unsafeEndpoint.set(
    "B2_ENDPOINT",
    "https://user:secret@s3.us-west-004.backblazeb2.com/",
  );
  assert.throws(
    () => validateP507Environment(unsafeEndpoint),
    /credential-free/u,
  );
});

test("rejects duplicate env-file variables", () => {
  assert.throws(
    () => parseEnvFile("B2_BUCKET=first\nB2_BUCKET=second\n"),
    /duplicate/u,
  );
});
