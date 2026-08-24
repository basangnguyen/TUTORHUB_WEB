import { createRequire } from "node:module";

const requireFromRuntime = createRequire(
  new URL("../services/whiteboard-runtime/package.json", import.meta.url),
);
const { Pool } = requireFromRuntime("pg");

const CANARY_NAME = "P5-COLLAB-18 Internal Canary";
const CANARY_CLASS_CODE = "P518CANARY";
const EXPECTED_QUOTAS = new Map([
  ["whiteboard_documents_per_tenant", 2],
  ["whiteboard_connections_per_tenant", 10],
  ["whiteboard_storage_bytes_per_tenant", 64 * 1024 * 1024],
  ["whiteboard_operations_per_minute", 600],
]);
const WHITEBOARD_TABLES = [
  "whiteboard_documents",
  "whiteboard_document_generations",
  "whiteboard_capability_policies",
  "whiteboard_snapshots",
  "whiteboard_document_mutation_receipts",
  "whiteboard_document_checkpoints",
  "whiteboard_artifact_commands",
  "whiteboard_artifact_purge_queue",
];

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function integer(value) {
  return Number.parseInt(String(value), 10);
}

async function main() {
  const connectionString = process.env.DATABASE_MIGRATION_URL?.trim();
  if (!connectionString) {
    throw new Error("DATABASE_MIGRATION_URL is required");
  }

  const pool = new Pool({
    connectionString,
    max: 1,
    connectionTimeoutMillis: 15_000,
    query_timeout: 15_000,
    statement_timeout: 15_000,
  });
  const client = await pool.connect();

  try {
    await client.query("BEGIN READ ONLY");

    const tenantResult = await client.query(
      `SELECT id
         FROM tutorhub.tenants
        WHERE name = $1 AND status = 'active'`,
      [CANARY_NAME],
    );
    assert(tenantResult.rowCount === 1, "exact active canary tenant missing");
    const tenantID = tenantResult.rows[0].id;

    const membershipResult = await client.query(
      `SELECT role, count(*)::integer AS count
         FROM tutorhub.memberships
        WHERE tenant_id = $1 AND status = 'active'
        GROUP BY role`,
      [tenantID],
    );
    const memberships = new Map(
      membershipResult.rows.map((row) => [row.role, integer(row.count)]),
    );
    assert(
      memberships.size === 3 &&
        memberships.get("org_admin") === 1 &&
        memberships.get("teacher") === 1 &&
        memberships.get("student") === 1,
      "canary role matrix must be exactly one org_admin, teacher and student",
    );

    const featureResult = await client.query(
      `SELECT enabled
         FROM tutorhub.tenant_feature_overrides
        WHERE tenant_id = $1 AND feature_key = 'classroom_whiteboards'`,
      [tenantID],
    );
    assert(
      featureResult.rowCount === 1 && featureResult.rows[0].enabled === true,
      "whiteboard feature override is not enabled",
    );

    const quotaResult = await client.query(
      `SELECT quota_key, limit_value
         FROM tutorhub.tenant_quota_overrides
        WHERE tenant_id = $1 AND quota_key = ANY($2::text[])`,
      [tenantID, [...EXPECTED_QUOTAS.keys()]],
    );
    const quotas = new Map(
      quotaResult.rows.map((row) => [row.quota_key, integer(row.limit_value)]),
    );
    assert(
      quotas.size === EXPECTED_QUOTAS.size,
      "canary quota profile incomplete",
    );
    for (const [key, expected] of EXPECTED_QUOTAS) {
      assert(quotas.get(key) === expected, `unexpected quota value for ${key}`);
    }

    const fixtureResult = await client.query(
      `SELECT c.id AS class_id,
              c.owner_user_id,
              cs.id AS session_id,
              ms.id AS media_space_id
         FROM tutorhub.classes c
         JOIN tutorhub.memberships owner_membership
           ON owner_membership.tenant_id = c.tenant_id
          AND owner_membership.user_id = c.owner_user_id
          AND owner_membership.role = 'teacher'
          AND owner_membership.status = 'active'
         JOIN tutorhub.class_sessions cs
           ON cs.tenant_id = c.tenant_id
          AND cs.class_id = c.id
          AND cs.status = 'scheduled'
         JOIN tutorhub.media_spaces ms
           ON ms.tenant_id = c.tenant_id
          AND ms.class_id = c.id
          AND ms.source_class_session_id = cs.id
          AND ms.source_kind = 'class_session'
          AND ms.status = 'scheduled'
        WHERE c.tenant_id = $1
          AND c.code = $2
          AND c.status = 'active'`,
      [tenantID, CANARY_CLASS_CODE],
    );
    assert(
      fixtureResult.rowCount === 1,
      "exact scheduled canary fixture missing",
    );

    const enrollmentResult = await client.query(
      `SELECT count(*)::integer AS count
         FROM tutorhub.class_enrollments enrollment
         JOIN tutorhub.memberships membership
           ON membership.tenant_id = enrollment.tenant_id
          AND membership.user_id = enrollment.user_id
          AND membership.role = 'student'
          AND membership.status = 'active'
        WHERE enrollment.tenant_id = $1
          AND enrollment.class_id = $2
          AND enrollment.class_role = 'student'
          AND enrollment.status = 'active'`,
      [tenantID, fixtureResult.rows[0].class_id],
    );
    assert(
      integer(enrollmentResult.rows[0].count) === 1,
      "exact active student enrollment missing",
    );

    let residue = 0;
    for (const table of WHITEBOARD_TABLES) {
      const result = await client.query(
        `SELECT count(*)::integer AS count FROM tutorhub.${table} WHERE tenant_id = $1`,
        [tenantID],
      );
      residue += integer(result.rows[0].count);
    }
    assert(residue === 0, "whiteboard lifecycle residue is not zero");

    await client.query("ROLLBACK");
    process.stdout.write(
      "P5-COLLAB-18 shared canary audit: PASS (tenant=1, roles=3, feature=enabled, quotas=4, fixture=1, whiteboard_residue=0).\n",
    );
  } catch (error) {
    try {
      await client.query("ROLLBACK");
    } catch {
      // Preserve the original failure without exposing connection details.
    }
    const code = typeof error?.code === "string" ? error.code : "assertion";
    process.stderr.write(`P5-COLLAB-18 shared canary audit: FAIL (${code}).\n`);
    process.exitCode = 1;
  } finally {
    client.release();
    await pool.end();
  }
}

await main();
