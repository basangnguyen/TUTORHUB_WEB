import { pathToFileURL } from "node:url";

import pg from "pg";

import { P519_PROVIDER_FIXTURE } from "../../scripts/p519-provider-fixture.mjs";

const { Client } = pg;
const MODES = new Set(["provision", "verify", "cleanup"]);

async function cleanupFixture(client) {
  const { actorId, classId, tenantId } = P519_PROVIDER_FIXTURE;
  const documentIds = P519_PROVIDER_FIXTURE.documents.map(
    (document) => document.documentId,
  );
  const sessionIds = P519_PROVIDER_FIXTURE.documents.map(
    (document) => document.sessionId,
  );
  const mediaSpaceIds = P519_PROVIDER_FIXTURE.documents.map(
    (document) => document.mediaSpaceId,
  );

  await client.query(
    `DELETE FROM tutorhub.whiteboard_artifact_commands
     WHERE tenant_id = $1 OR document_id = ANY($2::uuid[])`,
    [tenantId, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.whiteboard_artifact_purge_queue
     WHERE tenant_id = $1 OR document_id = ANY($2::uuid[])`,
    [tenantId, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.whiteboard_snapshots
     WHERE tenant_id = $1 OR document_id = ANY($2::uuid[])`,
    [tenantId, documentIds],
  );
  await client.query(
    `DELETE FROM tutorhub.media_spaces
     WHERE tenant_id = $1 OR id = ANY($2::uuid[])`,
    [tenantId, mediaSpaceIds],
  );
  await client.query(
    `DELETE FROM tutorhub.class_sessions
     WHERE tenant_id = $1 OR id = ANY($2::uuid[])`,
    [tenantId, sessionIds],
  );
  await client.query(
    `DELETE FROM tutorhub.classes WHERE tenant_id = $1 OR id = $2`,
    [tenantId, classId],
  );
  await client.query(
    `DELETE FROM tutorhub.memberships
     WHERE tenant_id = $1 OR user_id = $2`,
    [tenantId, actorId],
  );
  await client.query(`DELETE FROM tutorhub.tenants WHERE id = $1`, [tenantId]);
  await client.query(`DELETE FROM tutorhub.users WHERE id = $1`, [actorId]);
}

async function provisionFixture(client) {
  const fixture = P519_PROVIDER_FIXTURE;
  await cleanupFixture(client);
  await client.query(
    `INSERT INTO tutorhub.users (id, email, display_name)
     VALUES ($1, 'p519-private-alpha-disposable@example.test',
             'P5-COLLAB-19 disposable actor')`,
    [fixture.actorId],
  );
  await client.query(
    `INSERT INTO tutorhub.tenants (id, slug, name)
     VALUES ($1, $2, 'P5-COLLAB-19 private alpha disposable')`,
    [fixture.tenantId, fixture.tenantSlug],
  );
  await client.query(
    `INSERT INTO tutorhub.memberships
       (tenant_id, user_id, role, status, joined_at)
     VALUES ($1, $2, 'teacher', 'active', NOW())`,
    [fixture.tenantId, fixture.actorId],
  );
  await client.query(
    `INSERT INTO tutorhub.classes
       (id, tenant_id, owner_user_id, code, title, status, timezone)
     VALUES ($1, $2, $3, 'P519ALPHA', 'P5-COLLAB-19 private alpha',
             'active', 'Asia/Ho_Chi_Minh')`,
    [fixture.classId, fixture.tenantId, fixture.actorId],
  );

  for (const [index, document] of fixture.documents.entries()) {
    const suffix = String(index + 1).padStart(2, "0");
    await client.query(
      `INSERT INTO tutorhub.class_sessions
         (id, tenant_id, class_id, title, starts_at, ends_at, timezone,
          created_by, updated_by)
       VALUES ($1, $2, $3, $4, NOW() + INTERVAL '1 hour',
               NOW() + INTERVAL '2 hours', 'Asia/Ho_Chi_Minh', $5, $5)`,
      [
        document.sessionId,
        fixture.tenantId,
        fixture.classId,
        `P5-COLLAB-19 session ${suffix}`,
        fixture.actorId,
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
        fixture.tenantId,
        fixture.classId,
        document.sessionId,
        `p519-media-space-${suffix}`,
        Buffer.alloc(32, index + 1),
        fixture.actorId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_documents
         (id, tenant_id, media_space_id, create_idempotency_key,
          create_request_fingerprint, created_by, updated_by)
       VALUES ($1, $2, $3, $4, $5, $6, $6)`,
      [
        document.documentId,
        fixture.tenantId,
        document.mediaSpaceId,
        `p519-document-${suffix}`,
        Buffer.alloc(32, index + 11),
        fixture.actorId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_document_generations
         (tenant_id, document_id, generation, provider_document_name,
          reason, created_by)
       VALUES ($1, $2, 1, $3, 'initial', $4)`,
      [
        fixture.tenantId,
        document.documentId,
        document.providerDocumentName,
        fixture.actorId,
      ],
    );
  }
}

async function verifyFixture(client, expectedCount) {
  const result = await client.query(
    `SELECT g.document_id, g.provider_document_name
     FROM tutorhub.whiteboard_document_generations g
     JOIN tutorhub.whiteboard_documents d
       ON d.tenant_id = g.tenant_id AND d.id = g.document_id
     JOIN tutorhub.media_spaces m
       ON m.tenant_id = d.tenant_id AND m.id = d.media_space_id
     JOIN tutorhub.class_sessions s
       ON s.tenant_id = m.tenant_id AND s.id = m.source_class_session_id
     WHERE g.tenant_id = $1 AND g.generation = 1
     ORDER BY g.provider_document_name`,
    [P519_PROVIDER_FIXTURE.tenantId],
  );
  const expected = P519_PROVIDER_FIXTURE.documents.map((document) => ({
    document_id: document.documentId,
    provider_document_name: document.providerDocumentName,
  }));
  if (
    result.rows.length !== expectedCount ||
    (expectedCount === expected.length &&
      JSON.stringify(result.rows) !== JSON.stringify(expected))
  ) {
    throw new Error("p519_provider_fixture_mismatch");
  }
}

export async function runP519ProviderFixture(mode, environment = process.env) {
  if (!MODES.has(mode)) throw new Error("p519_provider_fixture_mode_invalid");
  const connectionString = environment.DATABASE_MIGRATION_URL;
  if (typeof connectionString !== "string" || connectionString.length < 20) {
    throw new Error("p519_provider_fixture_database_required");
  }
  const client = new Client({ connectionString });
  await client.connect();
  try {
    await client.query("BEGIN");
    if (mode === "provision") await provisionFixture(client);
    if (mode === "cleanup") await cleanupFixture(client);
    await verifyFixture(client, mode === "cleanup" ? 0 : 2);
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    await client.end();
  }
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  const mode = process.argv[2] ?? "verify";
  runP519ProviderFixture(mode)
    .then(() =>
      process.stdout.write(`[P5-COLLAB-19] provider fixture ${mode}: PASS\n`),
    )
    .catch((error) => {
      const code = typeof error?.code === "string" ? error.code : "unknown";
      process.stderr.write(
        `[P5-COLLAB-19] provider fixture operation failed (${code})\n`,
      );
      process.exitCode = 1;
    });
}
