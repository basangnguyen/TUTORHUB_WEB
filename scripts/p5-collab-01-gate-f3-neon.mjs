import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { validateGateF3Environment } from "./require-p5-collab-01-gate-f3-confirm.mjs";

const requireFromRuntime = createRequire(
  new URL("../services/whiteboard-runtime/package.json", import.meta.url),
);
const { Pool } = requireFromRuntime("pg");

export async function provisionGateF3CheckpointSchema(env = process.env) {
  const validated = validateGateF3Environment(env);
  const runtimeRole = decodeURIComponent(validated.runtimeDatabase.username);
  if (!/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(runtimeRole)) {
    throw new Error("gate_f3_runtime_role_invalid");
  }
  const role = quoteIdentifier(runtimeRole);
  const owner = new Pool({
    allowExitOnIdle: true,
    connectionString: validated.ownerDatabase.toString(),
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 5_000,
    max: 1,
    statement_timeout: 10_000,
  });
  try {
    const roleExists = await owner.query(
      "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1) AS exists",
      [runtimeRole],
    );
    if (roleExists.rows[0]?.exists !== true) {
      throw new Error("gate_f3_runtime_role_missing");
    }
    await owner.query("BEGIN");
    try {
      await owner.query(`
        CREATE TABLE IF NOT EXISTS public.collaboration_document_checkpoints (
          tenant_id text NOT NULL,
          document_id text NOT NULL,
          generation bigint NOT NULL CHECK (generation >= 1),
          provider_document_name text NOT NULL,
          schema_version integer NOT NULL,
          provider_version text NOT NULL,
          causal_watermark bigint NOT NULL CHECK (causal_watermark >= 1),
          yjs_state bytea NOT NULL,
          byte_length integer NOT NULL CHECK (byte_length >= 0),
          checksum text NOT NULL CHECK (checksum ~ '^[a-f0-9]{64}$'),
          writer_fence bigint NOT NULL CHECK (writer_fence >= 1),
          updated_at timestamptz NOT NULL,
          PRIMARY KEY (tenant_id, document_id, generation),
          UNIQUE (provider_document_name)
        )
      `);
      await owner.query(
        "REVOKE ALL ON TABLE public.collaboration_document_checkpoints FROM PUBLIC",
      );
      await owner.query(
        `REVOKE ALL ON TABLE public.collaboration_document_checkpoints FROM ${role}`,
      );
      await owner.query(`GRANT USAGE ON SCHEMA public TO ${role}`);
      await owner.query(
        `GRANT SELECT, INSERT, UPDATE ON TABLE public.collaboration_document_checkpoints TO ${role}`,
      );
      await owner.query("COMMIT");
    } catch (error) {
      await owner.query("ROLLBACK").catch(() => undefined);
      throw error;
    }
  } finally {
    await owner.end();
  }

  const runtime = new Pool({
    allowExitOnIdle: true,
    connectionString: validated.runtimeDatabase.toString(),
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 5_000,
    max: 1,
    statement_timeout: 10_000,
  });
  try {
    const result = await runtime.query(`
      SELECT
        to_regclass('public.collaboration_document_checkpoints') IS NOT NULL AS table_exists,
        has_table_privilege(current_user, 'public.collaboration_document_checkpoints', 'SELECT,INSERT,UPDATE') AS allowed,
        has_table_privilege(current_user, 'public.collaboration_document_checkpoints', 'DELETE,TRUNCATE,REFERENCES,TRIGGER') AS forbidden
    `);
    const row = result.rows[0];
    if (!row?.table_exists || row.allowed !== true || row.forbidden !== false) {
      throw new Error("gate_f3_runtime_acl_not_exact");
    }
  } finally {
    await runtime.end();
  }
  return { aclExact: true, schemaReady: true };
}

function quoteIdentifier(value) {
  return `"${value.replaceAll('"', '""')}"`;
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  provisionGateF3CheckpointSchema()
    .then(() => {
      console.log(
        "P5-COLLAB-01 Gate F.3 Neon provision PASS: schema_ready=true; acl_exact=true; disposable=true.",
      );
    })
    .catch(() => {
      console.error(
        "P5-COLLAB-01 Gate F.3 Neon provision FAILED with bounded output.",
      );
      process.exitCode = 1;
    });
}
