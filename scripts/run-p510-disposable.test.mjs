import assert from "node:assert/strict";
import test from "node:test";
import { parseEnvFile } from "./run-p507-disposable.mjs";
import { validateP510Environment } from "./run-p510-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_10_DISPOSABLE_ONLY";

function validEnvironment() {
  return parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-p510.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL=postgresql://tutorhub_runtime:runtime-secret@ep-p510-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
P5_COLLAB_10_DISPOSABLE_CONFIRM=${confirmation}
`);
}

test("P5-COLLAB-10 runner accepts exact same-branch role boundaries", () => {
  const result = validateP510Environment(validEnvironment());
  assert.equal(result.P5_COLLAB_10_DISPOSABLE_CONFIRM, confirmation);
  assert.match(result.DATABASE_POOL_URL, /tutorhub_runtime/u);
});

test("P5-COLLAB-10 runner rejects missing confirmation and cross-branch URLs", () => {
  const missing = validEnvironment();
  missing.delete("P5_COLLAB_10_DISPOSABLE_CONFIRM");
  assert.throws(() => validateP510Environment(missing), /confirmation/u);

  const crossBranch = validEnvironment();
  crossBranch.set(
    "DATABASE_POOL_URL",
    "postgresql://tutorhub_runtime:runtime-secret@ep-other-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(
    () => validateP510Environment(crossBranch),
    /one disposable branch/u,
  );
});

test("P5-COLLAB-10 runner rejects pooled owner and direct runtime", () => {
  const pooledOwner = validEnvironment();
  pooledOwner.set(
    "DATABASE_MIGRATION_URL",
    "postgresql://neondb_owner:owner-secret@ep-p510-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(() => validateP510Environment(pooledOwner), /endpoint/u);

  const directRuntime = validEnvironment();
  directRuntime.set(
    "DATABASE_POOL_URL",
    "postgresql://tutorhub_runtime:runtime-secret@ep-p510.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require",
  );
  assert.throws(() => validateP510Environment(directRuntime), /endpoint/u);
});
