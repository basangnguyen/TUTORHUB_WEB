import { randomBytes, randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Pool, type PoolClient } from "pg";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  type CanonicalExcalidrawSceneV1,
} from "@tutorhub/collaboration-client";
import {
  B2ArtifactObjectStore,
  type ArtifactObjectBinding,
} from "./artifactObjectStore.js";
import { WhiteboardArtifactMaintenance } from "./artifactMaintenance.js";
import { PostgresArtifactQueue } from "./artifactQueue.js";
import { WhiteboardArtifactWorker } from "./artifactWorker.js";
import { NeonCheckpointStore } from "./checkpointStore.js";

const confirmation = "I_UNDERSTAND_P5_COLLAB_07_LIFECYCLE_DISPOSABLE_ONLY";
const enabled = process.env.P5_COLLAB_07_LIFECYCLE_CONFIRM === confirmation;
const integrationDescribe = enabled ? describe : describe.skip;
const bindingKey = {
  id: "p5-collab-07-disposable-binding-v1",
  secret: "p5-collab-07-disposable-binding-secret-not-production",
};

interface Fixture {
  actorId: string;
  documentId: string;
  providerDocumentName: string;
  tenantId: string;
}

interface SnapshotBinding extends ArtifactObjectBinding {
  id: string;
}

integrationDescribe(
  "P5-COLLAB-07 worker and maintenance disposable lifecycle",
  () => {
    let owner: Pool;
    let core: Pool;
    let queue: PostgresArtifactQueue;
    let checkpoints: NeonCheckpointStore;
    let objects: B2ArtifactObjectStore;
    let worker: WhiteboardArtifactWorker;
    let fixture: Fixture | undefined;

    beforeAll(async () => {
      owner = new Pool({
        connectionString: process.env.DATABASE_MIGRATION_URL,
      });
      core = new Pool({ connectionString: process.env.DATABASE_POOL_URL });
      queue = new PostgresArtifactQueue(
        process.env.DATABASE_COLLABORATION_URL ?? "",
      );
      checkpoints = new NeonCheckpointStore(
        process.env.DATABASE_COLLABORATION_URL ?? "",
      );
      objects = new B2ArtifactObjectStore(process.env.B2_BUCKET ?? "", {
        applicationKey: process.env.B2_APPLICATION_KEY ?? "",
        endpoint: process.env.B2_ENDPOINT ?? "",
        keyId: process.env.B2_KEY_ID ?? "",
        region: process.env.B2_REGION ?? "",
      });
      worker = new WhiteboardArtifactWorker(
        queue,
        objects,
        bindingKey,
        undefined,
        30,
        250,
        { event() {} },
      );
      await Promise.all([queue.probe(), checkpoints.probe()]);
      await cleanupStaleFixtures(owner, objects);
      expect(await staleFixtureCount(owner)).toBe(0);
      fixture = await seedFixture(owner);
      await checkpoints.store(
        checkpointScope(fixture, 1, fixture.providerDocumentName),
        createProviderState(fixture, 1),
      );
    }, 30_000);

    afterAll(async () => {
      if (fixture) {
        await cleanupFixture(owner, objects, fixture);
      }
      await Promise.allSettled([
        queue.close(),
        checkpoints.close(),
        core.end(),
        owner.end(),
      ]);
    }, 60_000);

    it("publishes only verified artifacts, quarantines corrupt restore, swaps one generation and purges concurrently", async () => {
      const current = requiredFixture(fixture);
      const snapshot = await produceArtifact(
        core,
        owner,
        worker,
        current,
        "snapshot",
        1,
      );
      const corruptExport = await produceArtifact(
        core,
        owner,
        worker,
        current,
        "export",
        1,
      );
      const purgeExport = await produceArtifact(
        core,
        owner,
        worker,
        current,
        "export",
        1,
      );

      const snapshotExpectation = {
        ...snapshot,
        contentSha256: await snapshotField(
          owner,
          snapshot.id,
          "content_sha256",
        ),
        sizeBytes: Number(
          await snapshotField(owner, snapshot.id, "size_bytes"),
        ),
        verificationKeyId: bindingKey.id,
      };
      const loaded = await objects.getVerified(snapshotExpectation);
      expect(loaded.byteLength).toBeGreaterThan(0);

      await owner.query(
        `UPDATE tutorhub.whiteboard_snapshots
       SET verification_key_id = 'p5-collab-07-corrupt-binding'
       WHERE tenant_id = $1 AND id = $2`,
        [current.tenantId, corruptExport.id],
      );
      const corruptRestore = await enqueueCommand(core, current, "restore", 1, {
        sourceSnapshotId: corruptExport.id,
        targetGeneration: 2,
        targetProviderDocumentName: providerName(),
      });
      expect(await worker.runOnce()).toBe(true);
      expect(await commandStatus(owner, corruptRestore)).toBe("quarantined");
      expect(await documentGeneration(owner, current)).toBe(1);
      expect(await checkpointCount(owner, current, 2)).toBe(0);

      const targetProviderDocumentName = providerName();
      const restore = await enqueueCommand(core, current, "restore", 1, {
        sourceSnapshotId: snapshot.id,
        targetGeneration: 2,
        targetProviderDocumentName,
      });
      expect(await worker.runOnce()).toBe(true);
      expect(await commandStatus(owner, restore)).toBe("succeeded");
      expect(await checkpointCount(owner, current, 2)).toBe(1);
      expect(await documentGeneration(owner, current)).toBe(1);

      await promoteRestore(
        core,
        current,
        restore,
        snapshot.id,
        targetProviderDocumentName,
      );
      expect(await documentGeneration(owner, current)).toBe(2);
      expect(await generationCount(owner, current)).toBe(2);

      const retryExport = await produceArtifact(
        core,
        owner,
        worker,
        current,
        "export",
        2,
      );
      await expireSnapshots(owner, current, [
        corruptExport.id,
        purgeExport.id,
        retryExport.id,
      ]);
      expect(
        await expiredSnapshotEligibility(owner, [
          corruptExport.id,
          purgeExport.id,
          retryExport.id,
        ]),
      ).toEqual({ activeReferences: 0, expired: 3, generationReferences: 0 });
      expect(await enqueueExpiredSnapshots(owner)).toBe(3);
      await orderFixturePurgeQueue(owner, [
        corruptExport.id,
        purgeExport.id,
        retryExport.id,
      ]);
      const isolation = await lockNonFixturePurgeWork(owner, current.tenantId);
      try {
        const first = new WhiteboardArtifactMaintenance(
          process.env.DATABASE_POLL_MAINTENANCE_URL ?? "",
          objects,
        );
        const second = new WhiteboardArtifactMaintenance(
          process.env.DATABASE_POLL_MAINTENANCE_URL ?? "",
          objects,
        );
        try {
          await Promise.all([first.probe(), second.probe()]);
          const results = await Promise.all([
            first.runBatch(1, 30),
            second.runBatch(1, 30),
          ]);
          const purged = results.reduce(
            (sum, result) => sum + result.purged,
            0,
          );
          const failed = results.reduce(
            (sum, result) => sum + result.failed,
            0,
          );
          if (purged !== 2 || failed !== 0) {
            const queueState = await Promise.all(
              [
                ["corrupt", corruptExport.id],
                ["purge", purgeExport.id],
              ].map(async ([label, snapshotId]) => ({
                label,
                queue: await purgeAttempt(owner, snapshotId),
                snapshotExists: await snapshotExists(owner, snapshotId),
              })),
            );
            throw new Error(
              `p507_skip_locked_counts:${JSON.stringify({ queueState, results })}`,
            );
          }
        } finally {
          await Promise.allSettled([first.close(), second.close()]);
        }

        const failing = new WhiteboardArtifactMaintenance(
          process.env.DATABASE_POLL_MAINTENANCE_URL ?? "",
          {
            async deleteVersion() {
              throw new Error("bounded_disposable_failure");
            },
          },
        );
        try {
          expect(await failing.runBatch(1, 30)).toEqual({
            failed: 1,
            purged: 0,
          });
        } finally {
          await failing.close();
        }
        expect(await purgeAttempt(owner, retryExport.id)).toEqual({
          attempts: 1,
          status: "pending",
        });
        await owner.query(
          `UPDATE tutorhub.whiteboard_artifact_purge_queue
           SET available_at = NOW()
           WHERE snapshot_id = $1`,
          [retryExport.id],
        );
        const retry = new WhiteboardArtifactMaintenance(
          process.env.DATABASE_POLL_MAINTENANCE_URL ?? "",
          objects,
        );
        try {
          expect(await retry.runBatch(1, 30)).toEqual({
            failed: 0,
            purged: 1,
          });
        } finally {
          await retry.close();
        }
      } finally {
        await isolation.query("ROLLBACK").catch(() => undefined);
        isolation.release();
      }

      expect(await snapshotExists(owner, snapshot.id)).toBe(true);
      expect(await snapshotExists(owner, corruptExport.id)).toBe(false);
      expect(await snapshotExists(owner, purgeExport.id)).toBe(false);
      expect(await snapshotExists(owner, retryExport.id)).toBe(false);
      expect(await purgeQueueCount(owner, current)).toBe(0);

      await cleanupFixture(owner, objects, current);
      await expect(objects.getVerified(snapshotExpectation)).rejects.toThrow(
        "artifact_object_unavailable",
      );
      expect(await fixtureRowCount(owner, current)).toBe(0);
      fixture = undefined;
    }, 120_000);
  },
);

async function cleanupStaleFixtures(
  owner: Pool,
  objects: B2ArtifactObjectStore,
): Promise<void> {
  const fixtures = await owner.query<{ actor_id: string; tenant_id: string }>(
    `SELECT tenant.id::text AS tenant_id, membership.user_id::text AS actor_id
     FROM tutorhub.tenants AS tenant
     JOIN tutorhub.memberships AS membership
       ON membership.tenant_id = tenant.id
     WHERE tenant.name = 'P5-COLLAB-07 disposable'
       AND tenant.slug LIKE 'p507-%'`,
  );
  for (const row of fixtures.rows) {
    await cleanupFixture(owner, objects, {
      actorId: row.actor_id,
      documentId: "",
      providerDocumentName: "",
      tenantId: row.tenant_id,
    });
  }
}

async function cleanupFixture(
  owner: Pool,
  objects: B2ArtifactObjectStore,
  fixture: Fixture,
): Promise<void> {
  const bindings = await owner.query<SnapshotBinding>(
    `SELECT id::text, object_key AS "objectKey",
            object_version_id AS "objectVersionId"
     FROM tutorhub.whiteboard_snapshots
     WHERE tenant_id = $1`,
    [fixture.tenantId],
  );
  for (const binding of bindings.rows) {
    await objects.deleteVersion(binding).catch(() => undefined);
  }

  const client = await owner.connect();
  try {
    await client.query("BEGIN");
    await client.query(
      `DELETE FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(
      `DELETE FROM tutorhub.whiteboard_artifact_purge_queue WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(
      `DELETE FROM tutorhub.whiteboard_snapshots WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(
      `DELETE FROM tutorhub.media_spaces WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(
      `DELETE FROM tutorhub.class_sessions WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(`DELETE FROM tutorhub.classes WHERE tenant_id = $1`, [
      fixture.tenantId,
    ]);
    await client.query(
      `DELETE FROM tutorhub.memberships WHERE tenant_id = $1`,
      [fixture.tenantId],
    );
    await client.query(`DELETE FROM tutorhub.tenants WHERE id = $1`, [
      fixture.tenantId,
    ]);
    await client.query(`DELETE FROM tutorhub.users WHERE id = $1`, [
      fixture.actorId,
    ]);
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    client.release();
  }
}

async function staleFixtureCount(owner: Pool): Promise<number> {
  const result = await owner.query<{ count: string }>(
    `SELECT count(*) FROM tutorhub.tenants
     WHERE name = 'P5-COLLAB-07 disposable' AND slug LIKE 'p507-%'`,
  );
  return Number(result.rows[0]?.count);
}

async function seedFixture(owner: Pool): Promise<Fixture> {
  const fixture: Fixture = {
    actorId: randomUUID(),
    documentId: randomUUID(),
    providerDocumentName: providerName(),
    tenantId: randomUUID(),
  };
  const classId = randomUUID();
  const sessionId = randomUUID();
  const spaceId = randomUUID();
  const client = await owner.connect();
  try {
    await client.query("BEGIN");
    await client.query(
      `INSERT INTO tutorhub.users (id, email, display_name)
       VALUES ($1, $2, 'P5-COLLAB-07 disposable actor')`,
      [
        fixture.actorId,
        `p507-${fixture.actorId.replaceAll("-", "")}@example.test`,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.tenants (id, slug, name)
       VALUES ($1, $2, 'P5-COLLAB-07 disposable')`,
      [
        fixture.tenantId,
        `p507-${fixture.tenantId.replaceAll("-", "").slice(0, 24)}`,
      ],
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
       VALUES ($1, $2, $3, $4, 'P5-COLLAB-07 class', 'active', 'Asia/Ho_Chi_Minh')`,
      [
        classId,
        fixture.tenantId,
        fixture.actorId,
        `P507${classId.replaceAll("-", "").slice(0, 8).toUpperCase()}`,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.class_sessions
         (id, tenant_id, class_id, title, starts_at, ends_at, timezone,
          created_by, updated_by)
       VALUES ($1, $2, $3, 'P5-COLLAB-07 session', NOW() + INTERVAL '1 hour',
               NOW() + INTERVAL '2 hours', 'Asia/Ho_Chi_Minh', $4, $4)`,
      [sessionId, fixture.tenantId, classId, fixture.actorId],
    );
    await client.query(
      `INSERT INTO tutorhub.media_spaces
         (id, tenant_id, source_kind, class_id, source_class_session_id,
          create_idempotency_key, create_request_fingerprint, created_by, updated_by)
       VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
      [
        spaceId,
        fixture.tenantId,
        classId,
        sessionId,
        idempotency("space"),
        randomBytes(32),
        fixture.actorId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_documents
         (id, tenant_id, media_space_id, create_idempotency_key,
          create_request_fingerprint, created_by, updated_by)
       VALUES ($1, $2, $3, $4, $5, $6, $6)`,
      [
        fixture.documentId,
        fixture.tenantId,
        spaceId,
        idempotency("document"),
        randomBytes(32),
        fixture.actorId,
      ],
    );
    await client.query(
      `INSERT INTO tutorhub.whiteboard_document_generations
         (tenant_id, document_id, generation, provider_document_name, reason, created_by)
       VALUES ($1, $2, 1, $3, 'initial', $4)`,
      [
        fixture.tenantId,
        fixture.documentId,
        fixture.providerDocumentName,
        fixture.actorId,
      ],
    );
    await client.query("COMMIT");
    return fixture;
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    client.release();
  }
}

function checkpointScope(
  fixture: Fixture,
  generation: number,
  providerDocumentName: string,
) {
  return {
    actorId: fixture.actorId,
    authorityLease: randomUUID(),
    capability: "edit" as const,
    documentId: fixture.documentId,
    generation,
    origin: "https://p5-collab-07.invalid",
    providerDocumentName,
    sessionId: randomUUID(),
    tenantId: fixture.tenantId,
    writerFence: generation,
  };
}

function createProviderState(fixture: Fixture, generation: number): Uint8Array {
  const document = new Y.Doc();
  try {
    const authority = new CanonicalExcalidrawAuthority(
      document,
      {
        documentId: fixture.documentId,
        generation,
        tenantId: fixture.tenantId,
      },
      "p5-collab-07-disposable",
    );
    const scene: CanonicalExcalidrawSceneV1 = {
      elements: [
        {
          height: 30,
          id: `text-${generation}`,
          text: "P5-COLLAB-07",
          type: "text",
          width: 140,
          x: 35,
          y: 55,
        },
      ],
      files: {},
      page: { backgroundColor: "#ffffff", id: "page-1", name: "Disposable" },
      schemaVersion: 1,
    };
    authority.initialize(scene);
    authority.destroy();
    return Y.encodeStateAsUpdate(document);
  } finally {
    document.destroy();
  }
}

async function produceArtifact(
  core: Pool,
  owner: Pool,
  worker: WhiteboardArtifactWorker,
  fixture: Fixture,
  kind: "export" | "snapshot",
  generation: number,
): Promise<SnapshotBinding> {
  const commandId = await enqueueCommand(core, fixture, kind, generation);
  expect(await worker.runOnce()).toBe(true);
  const resultStatus = await commandResult(owner, commandId);
  if (resultStatus.status !== "succeeded") {
    throw new Error(
      `p507_artifact_not_published:${resultStatus.status}:${resultStatus.failureCode}`,
    );
  }
  const result = await owner.query<SnapshotBinding>(
    `SELECT snapshot.id::text, snapshot.object_key AS "objectKey",
            snapshot.object_version_id AS "objectVersionId"
     FROM tutorhub.whiteboard_artifact_commands AS command
     JOIN tutorhub.whiteboard_snapshots AS snapshot
       ON snapshot.tenant_id = command.tenant_id
      AND snapshot.id = command.result_snapshot_id
     WHERE command.tenant_id = $1 AND command.id = $2`,
    [fixture.tenantId, commandId],
  );
  if (!result.rows[0]) throw new Error("p507_snapshot_not_published");
  return result.rows[0];
}

async function enqueueCommand(
  core: Pool,
  fixture: Fixture,
  kind: "export" | "restore" | "snapshot",
  generation: number,
  restore?: {
    sourceSnapshotId: string;
    targetGeneration: number;
    targetProviderDocumentName: string;
  },
): Promise<string> {
  const id = randomUUID();
  await core.query(
    `INSERT INTO tutorhub.whiteboard_artifact_commands
       (id, tenant_id, actor_user_id, document_id, generation, command_kind,
        idempotency_key, request_fingerprint, source_snapshot_id,
        target_generation, target_provider_document_name, available_at,
        requested_at, updated_at)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW(), NOW())`,
    [
      id,
      fixture.tenantId,
      fixture.actorId,
      fixture.documentId,
      generation,
      kind,
      idempotency(kind),
      randomBytes(32),
      restore?.sourceSnapshotId ?? null,
      restore?.targetGeneration ?? null,
      restore?.targetProviderDocumentName ?? null,
    ],
  );
  return id;
}

async function promoteRestore(
  core: Pool,
  fixture: Fixture,
  commandId: string,
  snapshotId: string,
  providerDocumentName: string,
): Promise<void> {
  const client = await core.connect();
  try {
    await client.query("BEGIN");
    const prepared = await client.query<{ prepared: boolean }>(
      `SELECT EXISTS (
         SELECT 1
         FROM tutorhub.whiteboard_artifact_commands AS command
         WHERE command.id = $1 AND command.tenant_id = $2
           AND command.actor_user_id = $3 AND command.document_id = $4
           AND command.generation = 1 AND command.command_kind = 'restore'
           AND command.status = 'succeeded' AND command.source_snapshot_id = $5
           AND command.target_generation = 2
           AND command.target_provider_document_name = $6
           AND EXISTS (
             SELECT 1 FROM tutorhub.whiteboard_document_checkpoints AS checkpoint
             WHERE checkpoint.tenant_id = command.tenant_id
               AND checkpoint.document_id = command.document_id
               AND checkpoint.generation = command.target_generation
               AND checkpoint.provider_document_name = command.target_provider_document_name
           )
       ) AS prepared`,
      [
        commandId,
        fixture.tenantId,
        fixture.actorId,
        fixture.documentId,
        snapshotId,
        providerDocumentName,
      ],
    );
    if (prepared.rows[0]?.prepared !== true)
      throw new Error("p507_restore_not_prepared");
    await client.query(
      `INSERT INTO tutorhub.whiteboard_document_generations
         (tenant_id, document_id, generation, provider_document_name, reason,
          restored_from_snapshot_id, created_by)
       VALUES ($1, $2, 2, $3, 'restore', $4, $5)`,
      [
        fixture.tenantId,
        fixture.documentId,
        providerDocumentName,
        snapshotId,
        fixture.actorId,
      ],
    );
    const swapped = await client.query(
      `UPDATE tutorhub.whiteboard_documents
       SET current_generation = 2, revoke_generation = revoke_generation + 1,
           version = version + 1, updated_by = $3, updated_at = NOW()
       WHERE tenant_id = $1 AND id = $2 AND current_generation = 1`,
      [fixture.tenantId, fixture.documentId, fixture.actorId],
    );
    if (swapped.rowCount !== 1) throw new Error("p507_restore_swap_lost");
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    client.release();
  }
}

async function commandStatus(pool: Pool, id: string): Promise<string> {
  const result = await pool.query<{ status: string }>(
    `SELECT status FROM tutorhub.whiteboard_artifact_commands WHERE id = $1`,
    [id],
  );
  return result.rows[0]?.status ?? "missing";
}

async function commandResult(pool: Pool, id: string) {
  const result = await pool.query<{
    failure_code: string | null;
    status: string;
  }>(
    `SELECT failure_code, status
     FROM tutorhub.whiteboard_artifact_commands
     WHERE id = $1`,
    [id],
  );
  return {
    failureCode: result.rows[0]?.failure_code ?? "none",
    status: result.rows[0]?.status ?? "missing",
  };
}

async function snapshotField(
  pool: Pool,
  id: string,
  field: "content_sha256" | "size_bytes",
): Promise<string> {
  const expression =
    field === "content_sha256"
      ? "encode(content_sha256, 'hex')"
      : "size_bytes::text";
  const result = await pool.query<{ value: string }>(
    `SELECT ${expression} AS value FROM tutorhub.whiteboard_snapshots WHERE id = $1`,
    [id],
  );
  return result.rows[0]?.value ?? "";
}

async function documentGeneration(
  pool: Pool,
  fixture: Fixture,
): Promise<number> {
  const result = await pool.query<{ current_generation: string }>(
    `SELECT current_generation FROM tutorhub.whiteboard_documents
     WHERE tenant_id = $1 AND id = $2`,
    [fixture.tenantId, fixture.documentId],
  );
  return Number(result.rows[0]?.current_generation);
}

async function generationCount(pool: Pool, fixture: Fixture): Promise<number> {
  const result = await pool.query<{ count: string }>(
    `SELECT count(*) FROM tutorhub.whiteboard_document_generations
     WHERE tenant_id = $1 AND document_id = $2`,
    [fixture.tenantId, fixture.documentId],
  );
  return Number(result.rows[0]?.count);
}

async function checkpointCount(
  pool: Pool,
  fixture: Fixture,
  generation: number,
): Promise<number> {
  const result = await pool.query<{ count: string }>(
    `SELECT count(*) FROM tutorhub.whiteboard_document_checkpoints
     WHERE tenant_id = $1 AND document_id = $2 AND generation = $3`,
    [fixture.tenantId, fixture.documentId, generation],
  );
  return Number(result.rows[0]?.count);
}

async function expireSnapshots(
  pool: Pool,
  fixture: Fixture,
  ids: string[],
): Promise<void> {
  for (const [index, id] of ids.entries()) {
    await pool.query(
      `UPDATE tutorhub.whiteboard_snapshots
       SET created_at = TIMESTAMPTZ '1900-01-01 00:00:00+00'
                        + make_interval(days => $3),
           retention_until = TIMESTAMPTZ '1900-01-01 00:00:00+00'
                             + make_interval(days => $3)
                             + INTERVAL '14 days'
       WHERE tenant_id = $1 AND id = $2`,
      [fixture.tenantId, id, index],
    );
  }
}

async function enqueueExpiredSnapshots(pool: Pool): Promise<number> {
  const result = await pool.query<{ count: number }>(
    `SELECT tutorhub.enqueue_whiteboard_snapshot_purge(3) AS count`,
  );
  return result.rows[0]?.count ?? 0;
}

async function orderFixturePurgeQueue(
  pool: Pool,
  snapshotIds: string[],
): Promise<void> {
  for (const [index, snapshotId] of snapshotIds.entries()) {
    await pool.query(
      `UPDATE tutorhub.whiteboard_artifact_purge_queue
       SET available_at = TIMESTAMPTZ '1900-02-01 00:00:00+00'
                          + make_interval(days => $2),
           created_at = TIMESTAMPTZ '1900-02-01 00:00:00+00'
                        + make_interval(days => $2),
           updated_at = statement_timestamp()
       WHERE snapshot_id = $1`,
      [snapshotId, index],
    );
  }
}

async function lockNonFixturePurgeWork(
  pool: Pool,
  fixtureTenantId: string,
): Promise<PoolClient> {
  const client = await pool.connect();
  try {
    await client.query("BEGIN");
    await client.query(
      `SELECT id
       FROM tutorhub.whiteboard_snapshots
       WHERE tenant_id <> $1
         AND retention_until <= transaction_timestamp()
       FOR UPDATE`,
      [fixtureTenantId],
    );
    await client.query(
      `SELECT snapshot_id
       FROM tutorhub.whiteboard_artifact_purge_queue
       WHERE tenant_id <> $1
       FOR UPDATE`,
      [fixtureTenantId],
    );
    return client;
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    client.release();
    throw error;
  }
}

async function expiredSnapshotEligibility(pool: Pool, ids: string[]) {
  const result = await pool.query<{
    active_references: string;
    expired: string;
    generation_references: string;
  }>(
    `SELECT
       count(*) FILTER (WHERE snapshot.retention_until <= transaction_timestamp()) AS expired,
       count(*) FILTER (WHERE EXISTS (
         SELECT 1 FROM tutorhub.whiteboard_document_generations AS generation
         WHERE generation.tenant_id = snapshot.tenant_id
           AND generation.document_id = snapshot.document_id
           AND generation.restored_from_snapshot_id = snapshot.id
       )) AS generation_references,
       count(*) FILTER (WHERE EXISTS (
         SELECT 1 FROM tutorhub.whiteboard_artifact_commands AS command
         WHERE command.tenant_id = snapshot.tenant_id
           AND command.document_id = snapshot.document_id
           AND command.status IN ('pending', 'processing')
           AND (command.source_snapshot_id = snapshot.id
                OR command.result_snapshot_id = snapshot.id)
       )) AS active_references
     FROM tutorhub.whiteboard_snapshots AS snapshot
     WHERE snapshot.id = ANY($1::uuid[])`,
    [ids],
  );
  return {
    activeReferences: Number(result.rows[0]?.active_references),
    expired: Number(result.rows[0]?.expired),
    generationReferences: Number(result.rows[0]?.generation_references),
  };
}

async function purgeAttempt(pool: Pool, snapshotId: string) {
  const result = await pool.query<{ attempts: number; status: string }>(
    `SELECT attempts, status FROM tutorhub.whiteboard_artifact_purge_queue
     WHERE snapshot_id = $1`,
    [snapshotId],
  );
  return result.rows[0];
}

async function snapshotExists(pool: Pool, id: string): Promise<boolean> {
  const result = await pool.query<{ present: boolean }>(
    `SELECT EXISTS (SELECT 1 FROM tutorhub.whiteboard_snapshots WHERE id = $1) AS present`,
    [id],
  );
  return result.rows[0]?.present === true;
}

async function purgeQueueCount(pool: Pool, fixture: Fixture): Promise<number> {
  const result = await pool.query<{ count: string }>(
    `SELECT count(*) FROM tutorhub.whiteboard_artifact_purge_queue WHERE tenant_id = $1`,
    [fixture.tenantId],
  );
  return Number(result.rows[0]?.count);
}

async function fixtureRowCount(pool: Pool, fixture: Fixture): Promise<number> {
  const result = await pool.query<{ count: string }>(
    `SELECT
       (SELECT count(*) FROM tutorhub.tenants WHERE id = $1) +
       (SELECT count(*) FROM tutorhub.users WHERE id = $2) AS count`,
    [fixture.tenantId, fixture.actorId],
  );
  return Number(result.rows[0]?.count);
}

function providerName(): string {
  return `wb_${randomBytes(18).toString("base64url")}`;
}

function idempotency(prefix: string): string {
  return `p507-${prefix}-${randomUUID()}`;
}

function requiredFixture(fixture: Fixture | undefined): Fixture {
  if (!fixture) throw new Error("p507_fixture_missing");
  return fixture;
}
