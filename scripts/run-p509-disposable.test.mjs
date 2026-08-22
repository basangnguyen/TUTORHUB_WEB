import assert from "node:assert/strict";
import test from "node:test";
import { parseEnvFile } from "./run-p507-disposable.mjs";
import { validateP509Environment } from "./run-p509-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_09_DISPOSABLE_ONLY";

function validEnvironment() {
  return parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-p509.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL=postgresql://tutorhub_runtime:runtime-secret@ep-p509-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_COLLABORATION_URL=postgresql://tutorhub_collab_worker:worker-secret@ep-p509.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
P5_COLLAB_09_DISPOSABLE_CONFIRM=${confirmation}
`);
}

test("P5-COLLAB-09 runner accepts exact same-branch role boundaries", () => {
  const result = validateP509Environment(validEnvironment());
  assert.equal(result.P5_COLLAB_09_DISPOSABLE_CONFIRM, confirmation);
  assert.match(result.DATABASE_POOL_URL, /tutorhub_runtime/u);
});

test("P5-COLLAB-09 runner rejects missing confirmation and cross-branch URLs", () => {
  const missing = validEnvironment();
  missing.delete("P5_COLLAB_09_DISPOSABLE_CONFIRM");
  assert.throws(() => validateP509Environment(missing), /confirmation/u);

  const crossBranch = validEnvironment();
  crossBranch.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_collab_worker:worker-secret@ep-other.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP509Environment(crossBranch),
    /one disposable branch/u,
  );
});

test("P5-COLLAB-09 runner rejects pooled owner/worker and wrong role", () => {
  const pooledOwner = validEnvironment();
  pooledOwner.set(
    "DATABASE_MIGRATION_URL",
    "postgresql://neondb_owner:owner-secret@ep-p509-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(() => validateP509Environment(pooledOwner), /endpoint/u);

  const wrongWorker = validEnvironment();
  wrongWorker.set(
    "DATABASE_COLLABORATION_URL",
    "postgresql://tutorhub_runtime:worker-secret@ep-p509.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(() => validateP509Environment(wrongWorker), /expected/u);
});
