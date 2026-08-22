import assert from "node:assert/strict";
import test from "node:test";
import { parseEnvFile } from "./run-p507-disposable.mjs";
import { validateP508Environment } from "./run-p508-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_08_DISPOSABLE_ONLY";

function validEnvironment() {
  return parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-p508.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL=postgresql://tutorhub_runtime:runtime-secret@ep-p508-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_COLLABORATION_URL=postgresql://tutorhub_collab_worker:worker-secret@ep-p508.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POLL_MAINTENANCE_URL=postgresql://tutorhub_poll_maintenance:maintenance-secret@ep-p508.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
B2_ENDPOINT=https://s3.us-west-004.backblazeb2.com
B2_REGION=us-west-004
B2_BUCKET=tutorhub-p5-collab-08-disposable
B2_KEY_ID=disposable-key-id
B2_APPLICATION_KEY=disposable-application-key-value
P5_COLLAB_08_DISPOSABLE_CONFIRM=${confirmation}
`);
}

test("P5-COLLAB-08 runner validates a same-branch four-role disposable environment", () => {
  const result = validateP508Environment(validEnvironment());
  assert.equal(result.P5_COLLAB_08_DISPOSABLE_CONFIRM, confirmation);
  assert.equal(
    result.P5_COLLAB_07_LIFECYCLE_CONFIRM,
    "I_UNDERSTAND_P5_COLLAB_07_LIFECYCLE_DISPOSABLE_ONLY",
  );
});

test("P5-COLLAB-08 runner rejects missing confirmation and cross-branch URLs", () => {
  const missingConfirmation = validEnvironment();
  missingConfirmation.delete("P5_COLLAB_08_DISPOSABLE_CONFIRM");
  assert.throws(
    () => validateP508Environment(missingConfirmation),
    /confirmation/u,
  );

  const crossBranch = validEnvironment();
  crossBranch.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_collab_worker:worker-secret@ep-other.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP508Environment(crossBranch),
    /one disposable branch/u,
  );
});
