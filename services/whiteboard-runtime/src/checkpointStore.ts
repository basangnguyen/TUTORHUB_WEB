import { createHash, timingSafeEqual } from "node:crypto";
import { Pool, type PoolClient } from "pg";
import * as Y from "yjs";
import type {
  CheckpointStore,
  CollaborationScope,
  StoredCheckpoint,
} from "./contracts.js";
import { MAX_DURABLE_DOCUMENT_BYTES } from "./contracts.js";

const CHECKPOINT_SCHEMA_VERSION = 1;
const PROVIDER_VERSION = "hocuspocus@4.6.0+yjs@13.6.27";

export class CheckpointStoreError extends Error {
  constructor(
    readonly code:
      | "checkpoint_corrupt"
      | "checkpoint_schema_unavailable"
      | "checkpoint_stale_writer"
      | "checkpoint_too_large"
      | "checkpoint_unavailable",
  ) {
    super(code);
    this.name = "CheckpointStoreError";
  }
}

export class NeonCheckpointStore implements CheckpointStore {
  private readonly pool: Pool;

  constructor(databaseUrl: string) {
    this.pool = new Pool({
      allowExitOnIdle: true,
      connectionString: databaseUrl,
      connectionTimeoutMillis: 2_000,
      idleTimeoutMillis: 10_000,
      max: 4,
      statement_timeout: 5_000,
    });
  }

  async close(): Promise<void> {
    await this.pool.end();
  }

  async probe(): Promise<void> {
    try {
      const result = await this.pool.query<{
        allowed: boolean | null;
        table_exists: boolean;
      }>(`
        SELECT
          to_regclass('tutorhub.whiteboard_document_checkpoints') IS NOT NULL AS table_exists,
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
          ) AS allowed
      `);
      const row = result.rows[0];
      if (!row?.table_exists || row.allowed !== true) {
        throw new CheckpointStoreError("checkpoint_schema_unavailable");
      }
    } catch (error) {
      if (error instanceof CheckpointStoreError) throw error;
      throw new CheckpointStoreError("checkpoint_unavailable");
    }
  }

  async load(scope: CollaborationScope): Promise<StoredCheckpoint | null> {
    try {
      const result = await this.pool.query<{
        byte_length: number;
        causal_watermark: string;
        checksum: string;
        provider_version: string;
        schema_version: number;
        yjs_state: Buffer;
      }>(
        `
          SELECT
            schema_version,
            provider_version,
            causal_watermark,
            yjs_state,
            byte_length,
            encode(checksum, 'hex') AS checksum
          FROM tutorhub.whiteboard_document_checkpoints
          WHERE tenant_id = $1
            AND document_id = $2
            AND generation = $3
            AND provider_document_name = $4
          LIMIT 1
        `,
        [
          scope.tenantId,
          scope.documentId,
          scope.generation,
          scope.providerDocumentName,
        ],
      );
      const row = result.rows[0];
      if (!row) return null;
      const watermark = Number(row.causal_watermark);
      if (
        row.schema_version !== CHECKPOINT_SCHEMA_VERSION ||
        row.provider_version !== PROVIDER_VERSION ||
        !Number.isSafeInteger(watermark) ||
        watermark < 1 ||
        row.byte_length !== row.yjs_state.byteLength ||
        row.byte_length > MAX_DURABLE_DOCUMENT_BYTES ||
        !checksumMatches(row.yjs_state, row.checksum)
      ) {
        throw new CheckpointStoreError("checkpoint_corrupt");
      }
      return {
        checksum: row.checksum,
        state: new Uint8Array(row.yjs_state),
        watermark,
      };
    } catch (error) {
      if (error instanceof CheckpointStoreError) throw error;
      throw new CheckpointStoreError("checkpoint_unavailable");
    }
  }

  async store(scope: CollaborationScope, state: Uint8Array): Promise<number> {
    if (state.byteLength > MAX_DURABLE_DOCUMENT_BYTES) {
      throw new CheckpointStoreError("checkpoint_too_large");
    }
    validateYjsState(state);
    const checksum = sha256(state);
    let client: PoolClient | undefined;
    try {
      client = await this.pool.connect();
      await client.query("BEGIN");
      const result = await client.query<{ causal_watermark: string }>(
        `
          INSERT INTO tutorhub.whiteboard_document_checkpoints (
            tenant_id,
            document_id,
            generation,
            provider_document_name,
            schema_version,
            provider_version,
            causal_watermark,
            yjs_state,
            byte_length,
            checksum,
            writer_fence,
            updated_at
          ) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, decode($9, 'hex'), $10, NOW())
          ON CONFLICT (tenant_id, document_id, generation)
          DO UPDATE SET
            provider_document_name = EXCLUDED.provider_document_name,
            schema_version = EXCLUDED.schema_version,
            provider_version = EXCLUDED.provider_version,
            causal_watermark = whiteboard_document_checkpoints.causal_watermark + 1,
            yjs_state = EXCLUDED.yjs_state,
            byte_length = EXCLUDED.byte_length,
            checksum = EXCLUDED.checksum,
            writer_fence = EXCLUDED.writer_fence,
            updated_at = NOW()
          WHERE whiteboard_document_checkpoints.writer_fence <= EXCLUDED.writer_fence
            AND whiteboard_document_checkpoints.provider_document_name = EXCLUDED.provider_document_name
          RETURNING causal_watermark
        `,
        [
          scope.tenantId,
          scope.documentId,
          scope.generation,
          scope.providerDocumentName,
          CHECKPOINT_SCHEMA_VERSION,
          PROVIDER_VERSION,
          Buffer.from(state),
          state.byteLength,
          checksum,
          scope.writerFence,
        ],
      );
      const watermark = Number(result.rows[0]?.causal_watermark);
      if (!Number.isSafeInteger(watermark) || watermark < 1) {
        throw new CheckpointStoreError("checkpoint_stale_writer");
      }
      await client.query("COMMIT");
      return watermark;
    } catch (error) {
      if (client) await rollbackQuietly(client);
      if (error instanceof CheckpointStoreError) throw error;
      throw new CheckpointStoreError("checkpoint_unavailable");
    } finally {
      client?.release();
    }
  }
}

function validateYjsState(state: Uint8Array): void {
  try {
    const document = new Y.Doc();
    Y.applyUpdate(document, state);
    document.destroy();
  } catch {
    throw new CheckpointStoreError("checkpoint_corrupt");
  }
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function checksumMatches(value: Uint8Array, expected: string): boolean {
  if (!/^[a-f0-9]{64}$/.test(expected)) return false;
  return timingSafeEqual(
    Buffer.from(sha256(value), "hex"),
    Buffer.from(expected, "hex"),
  );
}

async function rollbackQuietly(client: PoolClient): Promise<void> {
  try {
    await client.query("ROLLBACK");
  } catch {
    // The bounded public error is emitted by the caller; raw database errors are never logged.
  }
}
