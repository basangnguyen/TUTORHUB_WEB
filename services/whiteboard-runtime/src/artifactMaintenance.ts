import { randomBytes } from "node:crypto";
import { Pool } from "pg";
import type { ArtifactObjectStorePort } from "./artifactWorker.js";

export class ArtifactMaintenanceError extends Error {
  constructor(readonly code: string) {
    super(code);
    this.name = "ArtifactMaintenanceError";
  }
}

interface PurgeClaim {
  lease_token: string;
  object_key: string;
  object_version_id: string;
  snapshot_id: string;
}

export class WhiteboardArtifactMaintenance {
  private readonly pool: Pool;
  private readonly workerId = `purge-${randomBytes(12).toString("hex")}`;

  constructor(
    databaseUrl: string,
    private readonly objects: Pick<ArtifactObjectStorePort, "deleteVersion">,
  ) {
    this.pool = new Pool({
      allowExitOnIdle: true,
      connectionString: databaseUrl,
      connectionTimeoutMillis: 2_000,
      idleTimeoutMillis: 10_000,
      max: 1,
      statement_timeout: 5_000,
    });
  }

  async close(): Promise<void> {
    await this.pool.end();
  }

  async probe(): Promise<void> {
    try {
      const result = await this.pool.query<{ allowed: boolean | null }>(`
        SELECT
          has_function_privilege(
            current_user,
            'tutorhub.claim_whiteboard_snapshot_purge(text,integer,integer)',
            'EXECUTE'
          )
          AND has_function_privilege(
            current_user,
            'tutorhub.complete_whiteboard_snapshot_purge(uuid,uuid)',
            'EXECUTE'
          )
          AND has_function_privilege(
            current_user,
            'tutorhub.fail_whiteboard_snapshot_purge(uuid,uuid,text)',
            'EXECUTE'
          ) AS allowed
      `);
      if (result.rows[0]?.allowed !== true) {
        throw new ArtifactMaintenanceError("artifact_maintenance_acl_denied");
      }
    } catch (error) {
      if (error instanceof ArtifactMaintenanceError) throw error;
      throw new ArtifactMaintenanceError("artifact_maintenance_unavailable");
    }
  }

  async runBatch(
    batch = 10,
    leaseSeconds = 60,
  ): Promise<{
    failed: number;
    purged: number;
  }> {
    let claims: PurgeClaim[];
    try {
      const result = await this.pool.query<PurgeClaim>(
        `SELECT snapshot_id::text, object_key, object_version_id, lease_token::text
         FROM tutorhub.claim_whiteboard_snapshot_purge($1, $2, $3)`,
        [this.workerId, batch, leaseSeconds],
      );
      claims = result.rows;
    } catch {
      throw new ArtifactMaintenanceError("artifact_maintenance_unavailable");
    }

    let purged = 0;
    let failed = 0;
    for (const claim of claims) {
      try {
        await this.objects.deleteVersion({
          objectKey: claim.object_key,
          objectVersionId: claim.object_version_id,
        });
        const result = await this.pool.query<{ complete: boolean }>(
          `SELECT tutorhub.complete_whiteboard_snapshot_purge($1, $2) AS complete`,
          [claim.snapshot_id, claim.lease_token],
        );
        if (result.rows[0]?.complete !== true) {
          throw new ArtifactMaintenanceError("artifact_maintenance_fence_lost");
        }
        purged += 1;
      } catch {
        failed += 1;
        await this.pool
          .query(`SELECT tutorhub.fail_whiteboard_snapshot_purge($1, $2, $3)`, [
            claim.snapshot_id,
            claim.lease_token,
            "artifact_object_unavailable",
          ])
          .catch(() => undefined);
      }
    }
    return { failed, purged };
  }
}
