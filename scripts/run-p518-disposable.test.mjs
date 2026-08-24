import assert from "node:assert/strict";
import test from "node:test";
import { parseEnvFile } from "./run-p507-disposable.mjs";
import {
  normalizeP518Arguments,
  validateP518Environment,
} from "./run-p518-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_18_DISPOSABLE_ONLY";

function validEnvironment() {
  return parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-p518.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL=postgresql://tutorhub_runtime:runtime-secret@ep-p518-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_COLLABORATION_URL=postgresql://tutorhub_collab_worker:worker-secret@ep-p518.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POLL_MAINTENANCE_URL=postgresql://tutorhub_poll_maintenance:maintenance-secret@ep-p518.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
B2_ENDPOINT=https://s3.us-west-004.backblazeb2.com
B2_REGION=us-west-004
B2_BUCKET=tutorhub-p518-disposable
B2_KEY_ID=005123456789abcdefghijkl
B2_APPLICATION_KEY=K005_disposable_application_key_not_real
P5_COLLAB_18_DISPOSABLE_CONFIRM=${confirmation}
`);
}

test("P5-COLLAB-18 accepts pnpm's argument separator", () => {
  assert.deepEqual(normalizeP518Arguments(["--", "fixture.env", "provider"]), [
    "fixture.env",
    "provider",
  ]);
  assert.deepEqual(normalizeP518Arguments(["fixture.env", "database"]), [
    "fixture.env",
    "database",
  ]);
});

test("P5-COLLAB-18 accepts exact same-branch roles and scoped B2", () => {
  const result = validateP518Environment(validEnvironment());
  assert.equal(result.P5_COLLAB_18_DISPOSABLE_CONFIRM, confirmation);
  assert.equal(
    result.P5_COLLAB_09_DISPOSABLE_CONFIRM,
    "I_UNDERSTAND_P5_COLLAB_09_DISPOSABLE_ONLY",
  );
  assert.equal(
    result.P5_COLLAB_10_DISPOSABLE_CONFIRM,
    "I_UNDERSTAND_P5_COLLAB_10_DISPOSABLE_ONLY",
  );
  assert.equal(
    result.P5_COLLAB_13_ACL_PROVISION_CONFIRM,
    "I_UNDERSTAND_P5_COLLAB_13_ACL_PROVISION_DISPOSABLE_ONLY",
  );
  assert.equal(result.B2_BUCKET, "tutorhub-p518-disposable");
});

test("P5-COLLAB-18 rejects missing confirmation", () => {
  const values = validEnvironment();
  values.delete("P5_COLLAB_18_DISPOSABLE_CONFIRM");
  assert.throws(() => validateP518Environment(values), /confirmation/u);
});

test("P5-COLLAB-18 rejects cross-branch and duplicate role URLs", () => {
  const crossBranch = validEnvironment();
  crossBranch.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_collab_worker:worker-secret@ep-other.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP518Environment(crossBranch),
    /one disposable branch/u,
  );

  const duplicateRole = validEnvironment();
  duplicateRole.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_runtime:runtime-secret@ep-p518.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP518Environment(duplicateRole),
    /four distinct roles/u,
  );
});
