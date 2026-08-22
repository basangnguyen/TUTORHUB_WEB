import { pathToFileURL } from "node:url";
import { WhiteboardArtifactMaintenance } from "./artifactMaintenance.js";
import { B2ArtifactObjectStore } from "./artifactObjectStore.js";

const MAINTENANCE_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_MAINTENANCE_ONLY";

type Environment = Readonly<Record<string, string | undefined>>;

export interface ArtifactMaintenanceConfig {
  b2: {
    applicationKey: string;
    bucket: string;
    endpoint: string;
    keyId: string;
    region: string;
  };
  batch: number;
  databaseUrl: string;
  leaseSeconds: number;
}

export class ArtifactMaintenanceConfigurationError extends Error {
  constructor(readonly code: string) {
    super(code);
    this.name = "ArtifactMaintenanceConfigurationError";
  }
}

export function loadArtifactMaintenanceConfig(
  env: Environment = process.env,
): ArtifactMaintenanceConfig {
  if (
    env.P5_COLLAB_07_MAINTENANCE_CONFIRM?.trim() !== MAINTENANCE_CONFIRMATION
  ) {
    throw new ArtifactMaintenanceConfigurationError(
      "artifact_maintenance_confirmation_required",
    );
  }
  const databaseUrl = required(env, "DATABASE_POLL_MAINTENANCE_URL");
  validateDatabaseUrl(databaseUrl);
  return {
    b2: {
      applicationKey: secret(env, "B2_APPLICATION_KEY"),
      bucket: identifier(env, "B2_BUCKET", /^[A-Za-z0-9][A-Za-z0-9.-]{4,62}$/),
      endpoint: validateHttpsOrigin(required(env, "B2_ENDPOINT")),
      keyId: secret(env, "B2_KEY_ID"),
      region: identifier(env, "B2_REGION", /^[a-z0-9-]{3,64}$/),
    },
    batch: integer(env, "COLLAB_ARTIFACT_PURGE_BATCH", 10, 1, 25),
    databaseUrl,
    leaseSeconds: integer(
      env,
      "COLLAB_ARTIFACT_PURGE_LEASE_SECONDS",
      120,
      30,
      120,
    ),
  };
}

export async function runArtifactMaintenance(
  env: Environment = process.env,
): Promise<{ failed: number; purged: number }> {
  const config = loadArtifactMaintenanceConfig(env);
  const objects = new B2ArtifactObjectStore(config.b2.bucket, config.b2);
  const maintenance = new WhiteboardArtifactMaintenance(
    config.databaseUrl,
    objects,
  );
  try {
    await maintenance.probe();
    return await maintenance.runBatch(config.batch, config.leaseSeconds);
  } finally {
    await maintenance.close().catch(() => undefined);
  }
}

function required(env: Environment, name: string): string {
  const value = env[name]?.trim();
  if (!value) {
    throw new ArtifactMaintenanceConfigurationError(
      `${name.toLowerCase()}_required`,
    );
  }
  return value;
}

function secret(env: Environment, name: string): string {
  const value = required(env, name);
  if (value.length < 24) {
    throw new ArtifactMaintenanceConfigurationError(
      `${name.toLowerCase()}_invalid`,
    );
  }
  return value;
}

function identifier(env: Environment, name: string, pattern: RegExp): string {
  const value = required(env, name);
  if (!pattern.test(value)) {
    throw new ArtifactMaintenanceConfigurationError(
      `${name.toLowerCase()}_invalid`,
    );
  }
  return value;
}

function integer(
  env: Environment,
  name: string,
  fallback: number,
  minimum: number,
  maximum: number,
): number {
  const raw = env[name]?.trim();
  const value = raw ? Number(raw) : fallback;
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new ArtifactMaintenanceConfigurationError(
      `${name.toLowerCase()}_invalid`,
    );
  }
  return value;
}

function validateHttpsOrigin(value: string): string {
  try {
    const parsed = new URL(value);
    if (
      parsed.protocol !== "https:" ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash ||
      parsed.pathname !== "/"
    ) {
      throw new Error("invalid");
    }
    return parsed.origin;
  } catch {
    throw new ArtifactMaintenanceConfigurationError("b2_endpoint_invalid");
  }
}

function validateDatabaseUrl(value: string): void {
  try {
    const parsed = new URL(value);
    if (
      (parsed.protocol !== "postgresql:" && parsed.protocol !== "postgres:") ||
      !parsed.username ||
      !parsed.password ||
      !parsed.hostname ||
      !parsed.pathname.slice(1) ||
      parsed.hostname.includes("-pooler") ||
      parsed.searchParams.get("sslmode") !== "require"
    ) {
      throw new Error("invalid");
    }
  } catch {
    throw new ArtifactMaintenanceConfigurationError(
      "artifact_maintenance_database_url_invalid",
    );
  }
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  runArtifactMaintenance()
    .then((result) => {
      process.stdout.write(
        `${JSON.stringify({ event_code: "artifact_maintenance_batch", outcome: result.failed === 0 ? "succeeded" : "partial", ...result })}\n`,
      );
      if (result.failed > 0) process.exitCode = 1;
    })
    .catch((error: unknown) => {
      process.stderr.write(
        `${JSON.stringify({
          event_code: "artifact_maintenance_batch",
          outcome: "failed",
          reason_code:
            error instanceof ArtifactMaintenanceConfigurationError
              ? error.code
              : "artifact_maintenance_failed",
        })}\n`,
      );
      process.exitCode = 1;
    });
}
