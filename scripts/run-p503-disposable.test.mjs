import assert from "node:assert/strict";
import test from "node:test";

import {
  parseEnvFile,
  validateP503Environment,
} from "./run-p503-disposable.mjs";

const confirmation = "I_UNDERSTAND_P5_COLLAB_03_DISPOSABLE_ONLY";

test("loads only a same-branch owner direct URL and runtime pooled URL", () => {
  const values = parseEnvFile(`
DATABASE_MIGRATION_URL=postgresql://neondb_owner:owner-secret@ep-example.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require
DATABASE_POOL_URL='postgresql://tutorhub_runtime:runtime-secret@ep-example-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require'
P5_COLLAB_03_DISPOSABLE_CONFIRM=${confirmation}
IGNORED_SECRET=must-not-be-forwarded
`);
  const result = validateP503Environment(values);
  assert.deepEqual(Object.keys(result).sort(), [
    "DATABASE_MIGRATION_URL",
    "DATABASE_POOL_URL",
    "P5_COLLAB_03_DISPOSABLE_CONFIRM",
  ]);
});

test("rejects cross-branch, wrong-role, and missing confirmation inputs", () => {
  const base = new Map([
    [
      "DATABASE_MIGRATION_URL",
      "postgresql://neondb_owner:owner-secret@ep-owner.c-2.ap-southeast-1.aws.neon.tech/neondb",
    ],
    [
      "DATABASE_POOL_URL",
      "postgresql://tutorhub_runtime:runtime-secret@ep-other-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb",
    ],
  ]);
  assert.throws(() => validateP503Environment(base), /confirmation/u);
  base.set("P5_COLLAB_03_DISPOSABLE_CONFIRM", confirmation);
  assert.throws(() => validateP503Environment(base), /same disposable branch/u);
  base.set(
    "DATABASE_POOL_URL",
    "postgresql://neondb_owner:runtime-secret@ep-owner-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb",
  );
  assert.throws(() => validateP503Environment(base), /dedicated role/u);
});

test("rejects duplicate env-file variables", () => {
  assert.throws(
    () => parseEnvFile("DATABASE_POOL_URL=first\nDATABASE_POOL_URL=second\n"),
    /duplicate/u,
  );
});
