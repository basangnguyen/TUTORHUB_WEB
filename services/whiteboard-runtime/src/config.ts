import { RUNTIME_VERSIONS } from "./contracts.js";

export interface RuntimeConfig {
  address: string;
  allowedOrigins: ReadonlySet<string>;
  b2: {
    applicationKey: string;
    bucket: string;
    endpoint: string;
    keyId: string;
    region: string;
  };
  buildId: string;
  controlPlaneToken: string;
  controlPlaneUrl: string;
  databaseUrl: string;
  drainTimeoutMs: number;
  maxConnections: number;
  maxConnectionsPerActor: number;
  maxConnectionsPerDocument: number;
  maxFrameBytes: number;
  maxMessagesPerSecond: number;
  metricsToken: string;
  port: number;
  probeTimeoutMs: number;
  profile: typeof RUNTIME_VERSIONS.profile;
}

type Environment = Readonly<Record<string, string | undefined>>;

export class RuntimeConfigurationError extends Error {
  constructor(readonly code: string) {
    super(code);
    this.name = "RuntimeConfigurationError";
  }
}

export function loadRuntimeConfig(
  env: Environment = process.env,
): RuntimeConfig {
  const profile = required(env, "COLLAB_RUNTIME_PROFILE");
  if (profile !== RUNTIME_VERSIONS.profile) {
    throw new RuntimeConfigurationError("runtime_profile_invalid");
  }
  if (integer(env, "COLLAB_INSTANCE_COUNT", 1, 1, 1) !== 1) {
    throw new RuntimeConfigurationError("runtime_instance_count_invalid");
  }
  if (present(env, "COLLAB_REDIS_URL") || present(env, "REDIS_URL")) {
    throw new RuntimeConfigurationError(
      "runtime_redis_forbidden_for_free_profile",
    );
  }

  const production = env.NODE_ENV === "production";
  const databaseUrl = required(env, "DATABASE_COLLABORATION_URL");
  validateDatabaseUrl(databaseUrl, production);
  const controlPlaneUrl = validateOrigin(
    required(env, "COLLAB_CONTROL_PLANE_URL"),
    production,
    "control_plane_url_invalid",
  );
  const b2Endpoint = validateOrigin(
    required(env, "B2_ENDPOINT"),
    true,
    "b2_endpoint_invalid",
  );

  const allowedOrigins = new Set(
    required(env, "COLLAB_ALLOWED_ORIGINS")
      .split(",")
      .map((value) =>
        validateOrigin(value.trim(), production, "allowed_origin_invalid"),
      ),
  );
  if (allowedOrigins.size === 0) {
    throw new RuntimeConfigurationError("allowed_origin_required");
  }

  const buildId = env.COLLAB_BUILD_ID?.trim() || "development";
  if (!/^[A-Za-z0-9._-]{3,80}$/.test(buildId)) {
    throw new RuntimeConfigurationError("build_id_invalid");
  }

  return {
    address: env.HOST?.trim() || "0.0.0.0",
    allowedOrigins,
    b2: {
      applicationKey: secret(env, "B2_APPLICATION_KEY"),
      bucket: identifier(env, "B2_BUCKET", /^[A-Za-z0-9][A-Za-z0-9.-]{4,62}$/),
      endpoint: b2Endpoint,
      keyId: secret(env, "B2_KEY_ID"),
      region: identifier(env, "B2_REGION", /^[a-z0-9-]{3,64}$/),
    },
    buildId,
    controlPlaneToken: secret(env, "COLLAB_CONTROL_PLANE_TOKEN"),
    controlPlaneUrl,
    databaseUrl,
    drainTimeoutMs: integer(
      env,
      "COLLAB_DRAIN_TIMEOUT_MS",
      45_000,
      5_000,
      45_000,
    ),
    maxConnections: integer(env, "COLLAB_MAX_CONNECTIONS", 100, 1, 500),
    maxConnectionsPerActor: integer(
      env,
      "COLLAB_MAX_CONNECTIONS_PER_ACTOR",
      3,
      1,
      10,
    ),
    maxConnectionsPerDocument: integer(
      env,
      "COLLAB_MAX_CONNECTIONS_PER_DOCUMENT",
      50,
      1,
      100,
    ),
    maxFrameBytes: integer(
      env,
      "COLLAB_MAX_FRAME_BYTES",
      512 * 1024,
      1024,
      1024 * 1024,
    ),
    maxMessagesPerSecond: integer(
      env,
      "COLLAB_MAX_MESSAGES_PER_SECOND",
      60,
      1,
      120,
    ),
    metricsToken: secret(env, "COLLAB_METRICS_TOKEN"),
    port: integer(env, "PORT", 3000, 0, 65_535),
    probeTimeoutMs: integer(env, "COLLAB_PROBE_TIMEOUT_MS", 2_000, 250, 5_000),
    profile: RUNTIME_VERSIONS.profile,
  };
}

function present(env: Environment, name: string): boolean {
  return Boolean(env[name]?.trim());
}

function required(env: Environment, name: string): string {
  const value = env[name]?.trim();
  if (!value)
    throw new RuntimeConfigurationError(`${name.toLowerCase()}_required`);
  return value;
}

function secret(env: Environment, name: string): string {
  const value = required(env, name);
  if (value.length < 24) {
    throw new RuntimeConfigurationError(`${name.toLowerCase()}_invalid`);
  }
  return value;
}

function identifier(env: Environment, name: string, pattern: RegExp): string {
  const value = required(env, name);
  if (!pattern.test(value)) {
    throw new RuntimeConfigurationError(`${name.toLowerCase()}_invalid`);
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
    throw new RuntimeConfigurationError(`${name.toLowerCase()}_invalid`);
  }
  return value;
}

function validateOrigin(
  value: string,
  requireHttps: boolean,
  code: string,
): string {
  try {
    const parsed = new URL(value);
    const localHttp =
      parsed.protocol === "http:" &&
      (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost");
    if (
      (requireHttps && parsed.protocol !== "https:") ||
      (!requireHttps && parsed.protocol !== "https:" && !localHttp)
    ) {
      throw new Error(code);
    }
    if (
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash ||
      parsed.pathname !== "/"
    ) {
      throw new Error(code);
    }
    return parsed.origin;
  } catch {
    throw new RuntimeConfigurationError(code);
  }
}

function validateDatabaseUrl(value: string, production: boolean): void {
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== "postgresql:" && parsed.protocol !== "postgres:") {
      throw new Error("database_url_invalid");
    }
    if (
      !parsed.username ||
      !parsed.password ||
      !parsed.hostname ||
      !parsed.pathname.slice(1)
    ) {
      throw new Error("database_url_invalid");
    }
    if (production) {
      if (
        parsed.hostname.includes("-pooler") ||
        parsed.searchParams.get("sslmode") !== "require"
      ) {
        throw new Error("database_url_invalid");
      }
    }
  } catch {
    throw new RuntimeConfigurationError("database_url_invalid");
  }
}
