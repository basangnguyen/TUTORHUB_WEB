import { createHash, randomUUID } from "node:crypto";
import { Pool, type PoolClient } from "pg";
import type { ArtifactObjectExpectation } from "./artifactObjectStore.js";

const CHECKPOINT_SCHEMA_VERSION = 1;
const PROVIDER_VERSION = "hocuspocus@4.6.0+yjs@13.6.27";
const MAX_CHECKPOINT_BYTES = 20 * 1024 * 1024;

export type ArtifactCommandKind =
  "export" | "import_validate" | "restore" | "snapshot";

export interface ArtifactJob {
  actorUserId: string;
  attempts: number;
  commandId: string;
  currentGeneration: number;
  documentId: string;
  generation: number;
  kind: ArtifactCommandKind;
  leaseToken: string;
  providerDocumentName: string;
  revokeGeneration: number;
  source?: ArtifactObjectExpectation & {
    generation: number;
    snapshotId: string;
  };
  targetGeneration?: number;
  targetProviderDocumentName?: string;
  tenantId: string;
}

export interface PublishedArtifact {
  causalWatermarkSha256: string;
  contentSha256: string;
  objectKey: string;
  objectVersionId: string;
  sizeBytes: number;
  verificationKeyId: string;
}

export interface ArtifactQueuePort {
  claim(workerId: string, leaseSeconds: number): Promise<ArtifactJob | null>;
  completeRestore(job: ArtifactJob, providerState: Uint8Array): Promise<void>;
  fail(
    job: ArtifactJob,
    code: string,
    disposition: "failed" | "quarantined" | "retryable",
  ): Promise<void>;
  loadCheckpoint(job: ArtifactJob): Promise<Uint8Array>;
  publishSnapshot(job: ArtifactJob, artifact: PublishedArtifact): Promise<void>;
}

export class ArtifactQueueError extends Error {
  constructor(
    readonly code:
      | "artifact_checkpoint_corrupt"
      | "artifact_checkpoint_missing"
      | "artifact_queue_fence_lost"
      | "artifact_queue_schema_unavailable"
      | "artifact_queue_unavailable",
  ) {
    super(code);
    this.name = "ArtifactQueueError";
  }
}

export class PostgresArtifactQueue implements ArtifactQueuePort {
  private readonly pool: Pool;

  constructor(databaseUrl: string) {
    this.pool = new Pool({
      allowExitOnIdle: true,
      connectionString: databaseUrl,
      connectionTimeoutMillis: 2_000,
      idleTimeoutMillis: 10_000,
      max: 2,
      statement_timeout: 5_000,
    });
  }

  async close(): Promise<void> {
    await this.pool.end();
  }

  async probe(): Promise<void> {
    try {
      const result = await this.pool.query<{
        catalog_allowed: boolean | null;
        checkpoint_allowed: boolean | null;
        command_allowed: boolean | null;
        command_exists: boolean;
      }>(`
        SELECT
          to_regclass('tutorhub.whiteboard_artifact_commands') IS NOT NULL AS command_exists,
          has_table_privilege(
            current_user,
            'tutorhub.whiteboard_artifact_commands',
            'SELECT'
          )
          AND has_column_privilege(
            current_user,
            'tutorhub.whiteboard_artifact_commands',
            'status',
            'UPDATE'
          )
          AND has_column_privilege(
            current_user,
            'tutorhub.whiteboard_artifact_commands',
            'completed_at',
            'UPDATE'
          ) AS command_allowed,
          has_table_privilege(
            current_user,
            'tutorhub.whiteboard_document_checkpoints',
            'SELECT'
          )
          AND has_table_privilege(
            current_user,
            'tutorhub.whiteboard_document_checkpoints',
            'INSERT'
          )
          AND has_table_privilege(
            current_user,
            'tutorhub.whiteboard_document_checkpoints',
            'UPDATE'
          ) AS checkpoint_allowed,
          has_table_privilege(
            current_user,
            'tutorhub.whiteboard_documents',
            'SELECT'
          )
          AND has_table_privilege(
            current_user,
            'tutorhub.whiteboard_document_generations',
            'SELECT'
          )
          AND has_table_privilege(
            current_user,
            'tutorhub.whiteboard_snapshots',
            'SELECT'
          )
          AND has_column_privilege(
            current_user,
            'tutorhub.whiteboard_snapshots',
            'object_key',
            'INSERT'
          ) AS catalog_allowed
      `);
      const row = result.rows[0];
      if (
        !row?.command_exists ||
        row.command_allowed !== true ||
        row.checkpoint_allowed !== true ||
        row.catalog_allowed !== true
      ) {
        throw new ArtifactQueueError("artifact_queue_schema_unavailable");
      }
    } catch (error) {
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
  }

  async claim(
    workerId: string,
    leaseSeconds: number,
  ): Promise<ArtifactJob | null> {
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/.test(workerId)) {
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
    try {
      const result = await this.pool.query<ClaimedRow>(
        `
          WITH candidate AS (
            SELECT command.id
            FROM tutorhub.whiteboard_artifact_commands AS command
            WHERE command.attempts < 5
              AND (
                (command.status = 'pending' AND command.available_at <= NOW())
                OR (command.status = 'processing' AND command.lease_until <= NOW())
              )
            ORDER BY command.available_at, command.requested_at, command.id
            FOR UPDATE OF command SKIP LOCKED
            LIMIT 1
          ), claimed AS (
            UPDATE tutorhub.whiteboard_artifact_commands AS command
            SET status = 'processing',
                lease_owner = $1,
                lease_token = gen_random_uuid(),
                lease_until = NOW() + make_interval(secs => $2),
                attempts = command.attempts + 1,
                started_at = COALESCE(command.started_at, NOW()),
                completed_at = NULL,
                failure_code = NULL,
                updated_at = NOW()
            FROM candidate
            WHERE command.id = candidate.id
            RETURNING command.*
          )
          SELECT
            claimed.id::text AS command_id,
            claimed.tenant_id::text,
            claimed.actor_user_id::text,
            claimed.document_id::text,
            claimed.generation,
            claimed.command_kind,
            claimed.lease_token::text,
            claimed.attempts,
            claimed.target_generation,
            claimed.target_provider_document_name,
            document.current_generation,
            document.revoke_generation,
            generation.provider_document_name,
            snapshot.id::text AS source_snapshot_id,
            snapshot.generation AS source_generation,
            snapshot.object_key AS source_object_key,
            snapshot.object_version_id AS source_object_version_id,
            encode(snapshot.content_sha256, 'hex') AS source_content_sha256,
            snapshot.size_bytes AS source_size_bytes,
            snapshot.verification_key_id AS source_verification_key_id
          FROM claimed
          JOIN tutorhub.whiteboard_documents AS document
            ON document.tenant_id = claimed.tenant_id
           AND document.id = claimed.document_id
          JOIN tutorhub.whiteboard_document_generations AS generation
            ON generation.tenant_id = claimed.tenant_id
           AND generation.document_id = claimed.document_id
           AND generation.generation = claimed.generation
          LEFT JOIN tutorhub.whiteboard_snapshots AS snapshot
            ON snapshot.tenant_id = claimed.tenant_id
           AND snapshot.document_id = claimed.document_id
           AND snapshot.id = claimed.source_snapshot_id
        `,
        [workerId, leaseSeconds],
      );
      const row = result.rows[0];
      return row ? mapClaimedRow(row) : null;
    } catch (error) {
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
  }

  async loadCheckpoint(job: ArtifactJob): Promise<Uint8Array> {
    try {
      const result = await this.pool.query<{
        byte_length: number;
        checksum: string;
        provider_version: string;
        schema_version: number;
        yjs_state: Buffer;
      }>(
        `SELECT schema_version, provider_version, yjs_state, byte_length,
                encode(checksum, 'hex') AS checksum
         FROM tutorhub.whiteboard_document_checkpoints
         WHERE tenant_id = $1 AND document_id = $2 AND generation = $3
           AND provider_document_name = $4
         LIMIT 1`,
        [
          job.tenantId,
          job.documentId,
          job.generation,
          job.providerDocumentName,
        ],
      );
      const row = result.rows[0];
      if (!row) throw new ArtifactQueueError("artifact_checkpoint_missing");
      if (
        row.schema_version !== CHECKPOINT_SCHEMA_VERSION ||
        row.provider_version !== PROVIDER_VERSION ||
        row.byte_length !== row.yjs_state.byteLength ||
        row.byte_length < 1 ||
        row.byte_length > MAX_CHECKPOINT_BYTES ||
        sha256(row.yjs_state) !== row.checksum
      ) {
        throw new ArtifactQueueError("artifact_checkpoint_corrupt");
      }
      return new Uint8Array(row.yjs_state);
    } catch (error) {
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
  }

  async publishSnapshot(
    job: ArtifactJob,
    artifact: PublishedArtifact,
  ): Promise<void> {
    const client = await this.connect();
    const snapshotId = randomUUID();
    try {
      await client.query("BEGIN");
      const fenced = await client.query(
        `SELECT 1 FROM tutorhub.whiteboard_artifact_commands
         WHERE tenant_id = $1 AND id = $2 AND status = 'processing'
           AND lease_token = $3
         FOR UPDATE`,
        [job.tenantId, job.commandId, job.leaseToken],
      );
      if (fenced.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_queue_fence_lost");
      }
      await client.query(
        `INSERT INTO tutorhub.whiteboard_snapshots
          (id, tenant_id, document_id, generation, snapshot_kind,
           format_version, engine_version, authority_version, schema_version,
           causal_watermark_sha256, content_sha256, size_bytes, object_key,
           object_version_id, verification_key_id, provenance_kind, created_by)
         VALUES ($1, $2, $3, $4, $5, '2', '0.18.1', '13.6.27', 1,
                 decode($6, 'hex'), decode($7, 'hex'), $8, $9, $10, $11,
                 'user', $12)`,
        [
          snapshotId,
          job.tenantId,
          job.documentId,
          job.generation,
          job.kind === "export" ? "export" : "manual",
          artifact.causalWatermarkSha256,
          artifact.contentSha256,
          artifact.sizeBytes,
          artifact.objectKey,
          artifact.objectVersionId,
          artifact.verificationKeyId,
          job.actorUserId,
        ],
      );
      const completed = await client.query(
        `UPDATE tutorhub.whiteboard_artifact_commands
         SET status = 'succeeded', result_snapshot_id = $4,
             completed_at = NOW(), updated_at = NOW(),
             lease_owner = NULL, lease_token = NULL, lease_until = NULL
         WHERE tenant_id = $1 AND id = $2 AND status = 'processing'
           AND lease_token = $3`,
        [job.tenantId, job.commandId, job.leaseToken, snapshotId],
      );
      if (completed.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_queue_fence_lost");
      }
      await client.query("COMMIT");
    } catch (error) {
      await rollbackQuietly(client);
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    } finally {
      client.release();
    }
  }

  async completeRestore(
    job: ArtifactJob,
    providerState: Uint8Array,
  ): Promise<void> {
    if (!job.targetGeneration || !job.targetProviderDocumentName) {
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
    if (
      providerState.byteLength < 1 ||
      providerState.byteLength > MAX_CHECKPOINT_BYTES
    ) {
      throw new ArtifactQueueError("artifact_checkpoint_corrupt");
    }
    const client = await this.connect();
    try {
      await client.query("BEGIN");
      const fence = await client.query(
        `SELECT 1 FROM tutorhub.whiteboard_artifact_commands
         WHERE tenant_id = $1 AND id = $2 AND status = 'processing'
           AND lease_token = $3
         FOR UPDATE`,
        [job.tenantId, job.commandId, job.leaseToken],
      );
      if (fence.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_queue_fence_lost");
      }
      const checkpoint = await client.query(
        `INSERT INTO tutorhub.whiteboard_document_checkpoints
          (tenant_id, document_id, generation, provider_document_name,
           schema_version, provider_version, causal_watermark, yjs_state,
           byte_length, checksum, writer_fence, updated_at)
         VALUES ($1, $2, $3, $4, 1, $5, 1, $6, $7, decode($8, 'hex'), $9, NOW())
         ON CONFLICT (tenant_id, document_id, generation) DO UPDATE SET
           provider_document_name = EXCLUDED.provider_document_name,
           schema_version = EXCLUDED.schema_version,
           provider_version = EXCLUDED.provider_version,
           causal_watermark = EXCLUDED.causal_watermark,
           yjs_state = EXCLUDED.yjs_state,
           byte_length = EXCLUDED.byte_length,
           checksum = EXCLUDED.checksum,
           writer_fence = EXCLUDED.writer_fence,
           updated_at = NOW()
         WHERE NOT EXISTS (
           SELECT 1
           FROM tutorhub.whiteboard_document_generations AS generation
           WHERE generation.tenant_id = EXCLUDED.tenant_id
             AND generation.document_id = EXCLUDED.document_id
             AND generation.generation = EXCLUDED.generation
         )`,
        [
          job.tenantId,
          job.documentId,
          job.targetGeneration,
          job.targetProviderDocumentName,
          PROVIDER_VERSION,
          Buffer.from(providerState),
          providerState.byteLength,
          sha256(providerState),
          job.revokeGeneration + 1,
        ],
      );
      if (checkpoint.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_checkpoint_corrupt");
      }
      const completed = await client.query(
        `UPDATE tutorhub.whiteboard_artifact_commands
         SET status = 'succeeded', completed_at = NOW(), updated_at = NOW(),
             lease_owner = NULL, lease_token = NULL, lease_until = NULL
         WHERE tenant_id = $1 AND id = $2 AND status = 'processing'
           AND lease_token = $3`,
        [job.tenantId, job.commandId, job.leaseToken],
      );
      if (completed.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_queue_fence_lost");
      }
      await client.query("COMMIT");
    } catch (error) {
      await rollbackQuietly(client);
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    } finally {
      client.release();
    }
  }

  async fail(
    job: ArtifactJob,
    code: string,
    disposition: "failed" | "quarantined" | "retryable",
  ): Promise<void> {
    const boundedCode = /^[a-z][a-z0-9_]{2,63}$/.test(code)
      ? code
      : "artifact_worker_failed";
    const retry = disposition === "retryable" && job.attempts < 5;
    try {
      const result = await this.pool.query(
        `UPDATE tutorhub.whiteboard_artifact_commands
         SET status = $4,
             available_at = CASE WHEN $4 = 'pending'
               THEN NOW() + make_interval(secs => LEAST(60, attempts * attempts))
               ELSE available_at END,
             failure_code = CASE WHEN $4 = 'pending' THEN NULL ELSE $5 END,
             completed_at = CASE WHEN $4 = 'pending' THEN NULL ELSE NOW() END,
             lease_owner = NULL, lease_token = NULL, lease_until = NULL,
             updated_at = NOW()
         WHERE tenant_id = $1 AND id = $2 AND status = 'processing'
           AND lease_token = $3`,
        [
          job.tenantId,
          job.commandId,
          job.leaseToken,
          retry
            ? "pending"
            : disposition === "quarantined"
              ? "quarantined"
              : "failed",
          boundedCode,
        ],
      );
      if (result.rowCount !== 1) {
        throw new ArtifactQueueError("artifact_queue_fence_lost");
      }
    } catch (error) {
      if (error instanceof ArtifactQueueError) throw error;
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
  }

  private async connect(): Promise<PoolClient> {
    try {
      return await this.pool.connect();
    } catch {
      throw new ArtifactQueueError("artifact_queue_unavailable");
    }
  }
}

interface ClaimedRow {
  actor_user_id: string;
  attempts: number;
  command_id: string;
  command_kind: ArtifactCommandKind;
  current_generation: string;
  document_id: string;
  generation: string;
  lease_token: string;
  provider_document_name: string;
  revoke_generation: string;
  source_content_sha256: string | null;
  source_generation: string | null;
  source_object_key: string | null;
  source_object_version_id: string | null;
  source_size_bytes: string | null;
  source_snapshot_id: string | null;
  source_verification_key_id: string | null;
  target_generation: string | null;
  target_provider_document_name: string | null;
  tenant_id: string;
}

function mapClaimedRow(row: ClaimedRow): ArtifactJob {
  const job: ArtifactJob = {
    actorUserId: row.actor_user_id,
    attempts: row.attempts,
    commandId: row.command_id,
    currentGeneration: positiveInteger(row.current_generation),
    documentId: row.document_id,
    generation: positiveInteger(row.generation),
    kind: row.command_kind,
    leaseToken: row.lease_token,
    providerDocumentName: row.provider_document_name,
    revokeGeneration: positiveInteger(row.revoke_generation),
    tenantId: row.tenant_id,
  };
  if (row.target_generation && row.target_provider_document_name) {
    job.targetGeneration = positiveInteger(row.target_generation);
    job.targetProviderDocumentName = row.target_provider_document_name;
  }
  if (
    row.source_snapshot_id &&
    row.source_generation &&
    row.source_object_key &&
    row.source_object_version_id &&
    row.source_content_sha256 &&
    row.source_size_bytes &&
    row.source_verification_key_id
  ) {
    job.source = {
      contentSha256: row.source_content_sha256,
      generation: positiveInteger(row.source_generation),
      objectKey: row.source_object_key,
      objectVersionId: row.source_object_version_id,
      sizeBytes: positiveInteger(row.source_size_bytes),
      snapshotId: row.source_snapshot_id,
      verificationKeyId: row.source_verification_key_id,
    };
  }
  return job;
}

function positiveInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 1) {
    throw new ArtifactQueueError("artifact_queue_unavailable");
  }
  return parsed;
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

async function rollbackQuietly(client: PoolClient): Promise<void> {
  try {
    await client.query("ROLLBACK");
  } catch {
    // Keep raw database failures out of application logs.
  }
}
