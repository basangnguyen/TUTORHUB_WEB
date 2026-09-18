import { createHash } from "node:crypto";
import { createRequire } from "node:module";

import { canonicalizeP520TenantManifest } from "./p520-tenant-allowlist.mjs";
import { P519_PROVIDER_DOCUMENTS } from "./p519-provider-fixture.mjs";

const require = createRequire(
  new URL("../services/whiteboard-runtime/package.json", import.meta.url),
);
const { Client } = require("pg");

const MODES = new Set([
  "base-provision",
  "provision",
  "verify",
  "cleanup",
  "destroy",
]);
const SYNTHETIC_SLUGS = Object.freeze([
  "p520-r3-disposable-01",
  "p520-r3-disposable-02",
]);
const SYNTHETIC_EMAILS = Object.freeze([
  "p520-r3-disposable-01@example.test",
  "p520-r3-disposable-02@example.test",
]);

function deterministicUuid(label, tenantId) {
  const bytes = createHash("sha256")
    .update(`p520-r3:${label}:${tenantId}`)
    .digest()
    .subarray(0, 16);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = bytes.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function createP520ProviderFixture(manifest) {
  const binding = canonicalizeP520TenantManifest(manifest);
  if (binding.tenantCount !== 2) {
    throw new Error("p520_provider_fixture_tenant_count_invalid");
  }
  const tenantIds = manifest.tenantIds.slice().sort();
  return Object.freeze({
    schemaVersion: "p5-collab-20-provider-fixture-v1",
    tenantIds: Object.freeze(tenantIds),
    documents: Object.freeze(
      tenantIds.map((tenantId, index) =>
        Object.freeze({
          actorId: `p520-r3-actor-${String(index + 1).padStart(2, "0")}`,
          classId: deterministicUuid("class", tenantId),
          documentId: deterministicUuid("document", tenantId),
          mediaSpaceId: deterministicUuid("media-space", tenantId),
          providerDocumentName: P519_PROVIDER_DOCUMENTS[index],
          sessionId: deterministicUuid("session", tenantId),
          tenantId,
        }),
      ),
    ),
  });
}

async function exactLedger(client) {
  const result = await client.query(
    "SELECT version::integer AS version, dirty FROM public.tutorhub_schema_migrations ORDER BY version",
  );
  if (
    result.rows.length !== 1 ||
    Number(result.rows[0]?.version) !== 42 ||
    result.rows[0]?.dirty !== false
  ) {
    throw new Error("p520_provider_fixture_ledger_invalid");
  }
}

async function assertBase(client, fixture) {
  const result = await client.query(
    `SELECT
       (SELECT count(*)::integer FROM tutorhub.tenants WHERE id = ANY($1::uuid[])) AS tenants,
       (SELECT count(*)::integer FROM tutorhub.memberships
         WHERE tenant_id = ANY($1::uuid[]) AND role = 'org_admin' AND status = 'active') AS memberships,
       (SELECT count(*)::integer FROM tutorhub.tenant_private_alpha_enrollments
         WHERE tenant_id = ANY($1::uuid[]) AND program = 'classroom_whiteboards' AND status = 'active') AS enrollments`,
    [fixture.tenantIds],
  );
  const row = result.rows[0];
  if (
    Number(row?.tenants) !== 2 ||
    Number(row?.memberships) !== 2 ||
    Number(row?.enrollments) !== 2
  ) {
    throw new Error("p520_provider_fixture_base_invalid");
  }
}

async function ownerForTenant(client, tenantId) {
  const result = await client.query(
    `SELECT user_id
       FROM tutorhub.memberships
      WHERE tenant_id = $1 AND role = 'org_admin' AND status = 'active'
      ORDER BY joined_at, user_id
      LIMIT 1`,
    [tenantId],
  );
  if (result.rows.length !== 1) {
    throw new Error("p520_provider_fixture_owner_invalid");
  }
  return result.rows[0].user_id;
}

async function cleanupFixture(client, fixture) {
  const tenantIds = fixture.tenantIds;
  const documentIds = fixture.documents.map((document) => document.documentId);
  const mediaSpaceIds = fixture.documents.map(
    (document) => document.mediaSpaceId,
  );
  const sessionIds = fixture.documents.map((document) => document.sessionId);
  const classIds = fixture.documents.map((document) => document.classId);
  await client.query(
    `DELETE FROM tutorhub.whiteboard_artifact_commands
      WHERE tenant_id = ANY($1::uuid[]) OR document_id = ANY($2::uuid[])`,
    [tenantIds, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.whiteboard_artifact_purge_queue
      WHERE tenant_id = ANY($1::uuid[]) OR document_id = ANY($2::uuid[])`,
    [tenantIds, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.whiteboard_snapshots
      WHERE tenant_id = ANY($1::uuid[]) OR document_id = ANY($2::uuid[])`,
    [tenantIds, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.whiteboard_document_checkpoints
      WHERE tenant_id = ANY($1::uuid[]) OR document_id = ANY($2::uuid[])`,
    [tenantIds, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.media_spaces
      WHERE tenant_id = ANY($1::uuid[]) OR id = ANY($2::uuid[])`,
    [tenantIds, mediaSpaceIds],
  );
  await client.query(
    `DELETE FROM tutorhub.class_sessions
      WHERE tenant_id = ANY($1::uuid[]) OR id = ANY($2::uuid[])`,
    [tenantIds, sessionIds],
  );
  await client.query(
    `DELETE FROM tutorhub.classes
      WHERE tenant_id = ANY($1::uuid[]) OR id = ANY($2::uuid[])`,
    [tenantIds, classIds],
  );
}

async function provisionFixture(client, fixture) {
  await cleanupFixture(client, fixture);
  for (const [index, document] of fixture.documents.entries()) {
    const ownerId = await ownerForTenant(client, document.tenantId);
    const ordinal = String(index + 1).padStart(2, "0");
    await client.query(
      `INSERT INTO tutorhub.classes
         (id, tenant_id, owner_user_id, code, title, status, timezone)
       VALUES ($1, $2, $3, $4, $5, 'active', 'Asia/Ho_Chi_Minh')`,
      [
        document.classId,
        document.tenantId,
        ownerId,
        `P520R3${ordinal}`,
        `P5-COLLAB-20 R3 class ${ordinal}`,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.class_sessions
         (id, tenant_id, class_id, title, starts_at, ends_at, timezone,
          created_by, updated_by)
       VALUES ($1, $2, $3, $4, NOW() + INTERVAL '1 hour',
               NOW() + INTERVAL '2 hours', 'Asia/Ho_Chi_Minh', $5, $5)`,
      [
        document.sessionId,
        document.tenantId,
        document.classId,
        `P5-COLLAB-20 R3 session ${ordinal}`,
        ownerId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.media_spaces
         (id, tenant_id, source_kind, class_id, source_class_session_id,
          create_idempotency_key, create_request_fingerprint,
          created_by, updated_by)
       VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
      [
        document.mediaSpaceId,
        document.tenantId,
        document.classId,
        document.sessionId,
        `p520-r3-media-space-${ordinal}`,
        Buffer.alloc(32, index + 21),
        ownerId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_documents
         (id, tenant_id, media_space_id, create_idempotency_key,
          create_request_fingerprint, created_by, updated_by)
       VALUES ($1, $2, $3, $4, $5, $6, $6)`,
      [
        document.documentId,
        document.tenantId,
        document.mediaSpaceId,
        `p520-r3-document-${ordinal}`,
        Buffer.alloc(32, index + 31),
        ownerId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_document_generations
         (tenant_id, document_id, generation, provider_document_name,
          reason, created_by)
       VALUES ($1, $2, 1, $3, 'initial', $4)`,
      [
        document.tenantId,
        document.documentId,
        document.providerDocumentName,
        ownerId,
      ],
    );
  }
}

async function verifyFixture(client, fixture, expectedDocuments) {
  const result = await client.query(
    `SELECT tenant_id::text, document_id::text, provider_document_name
       FROM tutorhub.whiteboard_document_generations
      WHERE tenant_id = ANY($1::uuid[]) AND generation = 1
      ORDER BY provider_document_name`,
    [fixture.tenantIds],
  );
  const expected = fixture.documents
    .map((document) => ({
      tenant_id: document.tenantId,
      document_id: document.documentId,
      provider_document_name: document.providerDocumentName,
    }))
    .sort((left, right) =>
      left.provider_document_name.localeCompare(right.provider_document_name),
    );
  if (
    result.rows.length !== expectedDocuments ||
    (expectedDocuments === expected.length &&
      JSON.stringify(result.rows) !== JSON.stringify(expected))
  ) {
    throw new Error("p520_provider_fixture_mapping_invalid");
  }
}

async function destroyBase(client, fixture) {
  const users = await client.query(
    `SELECT DISTINCT user_id::text
       FROM tutorhub.memberships
      WHERE tenant_id = ANY($1::uuid[])`,
    [fixture.tenantIds],
  );
  await client.query(
    "DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])",
    [fixture.tenantIds],
  );
  const userIds = users.rows.map((row) => row.user_id);
  if (userIds.length > 0) {
    await client.query(
      "DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])",
      [userIds],
    );
  }
}

async function provisionBase(client, fixture) {
  await cleanupFixture(client, fixture);
  await destroyBase(client, fixture);
  const userIds = fixture.tenantIds.map((tenantId) =>
    deterministicUuid("user", tenantId),
  );
  await client.query("DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])", [
    userIds,
  ]);
  for (const [index, tenantId] of fixture.tenantIds.entries()) {
    const userId = userIds[index];
    const ordinal = String(index + 1).padStart(2, "0");
    await client.query(
      `INSERT INTO tutorhub.users (id, email, display_name)
       VALUES ($1, $2, $3)`,
      [
        userId,
        SYNTHETIC_EMAILS[index],
        `P5-COLLAB-20 disposable owner ${ordinal}`,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.tenants (id, slug, name)
       VALUES ($1, $2, $3)`,
      [
        tenantId,
        SYNTHETIC_SLUGS[index],
        `P5-COLLAB-20 disposable tenant ${ordinal}`,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.memberships
         (tenant_id, user_id, role, status, joined_at)
       VALUES ($1, $2, 'org_admin', 'active', NOW())`,
      [tenantId, userId],
    );
    await client.query(
      `INSERT INTO tutorhub.tenant_private_alpha_enrollments
         (tenant_id, program, status, revision, notice_version,
          accepted_at, withdrawn_at, updated_by, created_at, updated_at)
       VALUES ($1, 'classroom_whiteboards', 'active', 1, 'p5-collab-19-v1',
               NOW(), NULL, $2, NOW(), NOW())`,
      [tenantId, userId],
    );
  }
  await assertBase(client, fixture);
}

export async function runP520ProviderFixture(mode, environment, manifest) {
  if (!MODES.has(mode)) throw new Error("p520_provider_fixture_mode_invalid");
  const connectionString = environment?.DATABASE_MIGRATION_URL;
  if (typeof connectionString !== "string" || connectionString.length < 20) {
    throw new Error("p520_provider_fixture_database_required");
  }
  const fixture = createP520ProviderFixture(manifest);
  const client = new Client({ connectionString });
  await client.connect();
  try {
    await client.query("BEGIN ISOLATION LEVEL SERIALIZABLE");
    await client.query(
      "SELECT pg_advisory_xact_lock(hashtext('tutorhub:p520:r3:provider-fixture'))",
    );
    await exactLedger(client);
    if (mode === "base-provision") await provisionBase(client, fixture);
    if (mode !== "destroy" && mode !== "base-provision") {
      await assertBase(client, fixture);
    }
    if (mode === "provision") await provisionFixture(client, fixture);
    if (mode === "cleanup" || mode === "destroy") {
      await cleanupFixture(client, fixture);
    }
    await verifyFixture(
      client,
      fixture,
      mode === "provision" || mode === "verify" ? 2 : 0,
    );
    if (mode === "destroy") await destroyBase(client, fixture);
    await exactLedger(client);
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    await client.end();
  }
  return {
    outcome: "pass",
    mode,
    tenantCount: fixture.tenantIds.length,
    documentCount: mode === "provision" || mode === "verify" ? 2 : 0,
    identifiersLogged: false,
  };
}

export { SYNTHETIC_EMAILS, SYNTHETIC_SLUGS };
