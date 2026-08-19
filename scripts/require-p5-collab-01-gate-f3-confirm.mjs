import { pathToFileURL } from "node:url";

const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY";
const conflictingEnvironment = [
  "DATABASE_MIGRATION_URL",
  "DATABASE_POOL_URL",
  "DATABASE_POLL_MAINTENANCE_URL",
  "LIVEKIT_API_KEY",
  "LIVEKIT_API_SECRET",
  "LIVEKIT_URL",
  "P4_10_SHARED_CONFIRM",
];

export function validateGateF3Environment(env = process.env) {
  if (env.P5_F3_DISPOSABLE_CONFIRM !== EXACT_CONFIRMATION) {
    fail("exact disposable confirmation is missing");
  }
  if (env.P5_F3_RENDER_REGION !== "singapore") {
    fail("P5_F3_RENDER_REGION must be exactly singapore");
  }
  if (env.P5_F3_HARD_CAP_USD !== "0") {
    fail("P5_F3_HARD_CAP_USD must be exactly 0");
  }
  for (const name of conflictingEnvironment) {
    if ((env[name]?.trim() ?? "") !== "") {
      fail("shared-stage or unrelated provider credential is present");
    }
  }

  const runtimeUrl = secureOrigin(env.P5_F3_RUNTIME_URL, "runtime URL");
  const controlUrl = secureOrigin(env.P5_F3_CONTROL_URL, "control URL");
  const allowedOrigin = secureOrigin(
    env.P5_F3_ALLOWED_ORIGIN,
    "allowed origin",
  );
  if (
    runtimeUrl === controlUrl ||
    runtimeUrl === allowedOrigin ||
    controlUrl === allowedOrigin
  ) {
    fail("runtime, control and client origins must be distinct");
  }
  if (env.COLLAB_ALLOWED_ORIGINS !== allowedOrigin) {
    fail(
      "COLLAB_ALLOWED_ORIGINS must equal the exact disposable client origin",
    );
  }
  if (env.COLLAB_CONTROL_PLANE_URL !== controlUrl) {
    fail(
      "COLLAB_CONTROL_PLANE_URL must equal the exact disposable control origin",
    );
  }

  const ownerDatabase = databaseUrl(
    env.P5_F3_DATABASE_OWNER_URL,
    "owner database URL",
  );
  const runtimeDatabase = databaseUrl(
    env.DATABASE_COLLABORATION_URL,
    "runtime database URL",
  );
  if (
    ownerDatabase.hostname !== runtimeDatabase.hostname ||
    ownerDatabase.pathname !== runtimeDatabase.pathname ||
    ownerDatabase.username === runtimeDatabase.username
  ) {
    fail(
      "owner/runtime database URLs must target one database with distinct roles",
    );
  }

  requiredSecret(env, "COLLAB_CONTROL_PLANE_TOKEN");
  requiredSecret(env, "COLLAB_METRICS_TOKEN");
  requiredSecret(env, "P5_F3_CONTROL_ADMIN_TOKEN");
  requiredSecret(env, "P5_F3_CONTROL_TOKEN_CURRENT");
  requiredSecret(env, "B2_KEY_ID");
  requiredSecret(env, "B2_APPLICATION_KEY");
  if (
    env.COLLAB_CONTROL_PLANE_TOKEN !== env.P5_F3_CONTROL_TOKEN_CURRENT ||
    env.COLLAB_CONTROL_PLANE_TOKEN === env.P5_F3_CONTROL_ADMIN_TOKEN ||
    env.B2_KEY_ID === env.B2_APPLICATION_KEY
  ) {
    fail(
      "control tokens must match runtime/fixture and admin must be distinct",
    );
  }
  const nextControlToken = env.P5_F3_CONTROL_TOKEN_NEXT?.trim() ?? "";
  if (
    nextControlToken &&
    (nextControlToken.length < 24 ||
      nextControlToken === env.P5_F3_CONTROL_TOKEN_CURRENT ||
      nextControlToken === env.P5_F3_CONTROL_ADMIN_TOKEN)
  ) {
    fail("next control token is invalid");
  }

  secureOrigin(env.B2_ENDPOINT, "B2 endpoint");
  identifier(env, "B2_BUCKET", /^[A-Za-z0-9][A-Za-z0-9.-]{4,62}$/);
  identifier(env, "B2_REGION", /^[a-z0-9-]{3,64}$/);
  identifier(env, "P5_F3_NEON_BRANCH_ID", /^[A-Za-z0-9_-]{3,128}$/);
  identifier(env, "P5_F3_B2_BUCKET_ID", /^[A-Za-z0-9_-]{3,128}$/);
  identifier(
    env,
    "P5_F3_PROVIDER_DOCUMENT_NAME",
    /^wb\/[a-f0-9]{24}\/[a-f0-9]{24}\/g[1-9][0-9]*$/,
  );
  identifier(env, "COLLAB_BUILD_ID", /^[A-Za-z0-9._-]{7,80}$/);
  if (env.COLLAB_RUNTIME_PROFILE !== "FREE_PRIVATE_ALPHA") {
    fail("COLLAB_RUNTIME_PROFILE must be FREE_PRIVATE_ALPHA");
  }
  if (env.COLLAB_INSTANCE_COUNT !== "1") {
    fail("COLLAB_INSTANCE_COUNT must be 1");
  }
  if ((env.COLLAB_REDIS_URL?.trim() ?? "") || (env.REDIS_URL?.trim() ?? "")) {
    fail("Redis is forbidden for the free private-alpha profile");
  }

  const expiresAt = Date.parse(env.P5_F3_DISPOSABLE_EXPIRES_AT ?? "");
  const remaining = expiresAt - Date.now();
  if (!Number.isFinite(expiresAt) || remaining <= 0 || remaining > 7 * 864e5) {
    fail(
      "disposable expiry must be in the future and no more than 7 days away",
    );
  }

  return {
    allowedOrigin,
    controlUrl,
    ownerDatabase,
    runtimeDatabase,
    runtimeUrl,
  };
}

function fail(message) {
  throw new Error(`P5-COLLAB-01 Gate F.3 refused: ${message}`);
}

function requiredSecret(env, name) {
  if ((env[name]?.trim() ?? "").length < 24)
    fail(`${name} is missing or invalid`);
}

function identifier(env, name, pattern) {
  if (!pattern.test(env[name]?.trim() ?? "")) fail(`${name} is invalid`);
}

function secureOrigin(raw, label) {
  try {
    const value = new URL(raw?.trim() ?? "");
    if (
      value.protocol !== "https:" ||
      value.username ||
      value.password ||
      value.pathname !== "/" ||
      value.search ||
      value.hash
    ) {
      throw new Error("invalid_origin");
    }
    return value.origin;
  } catch {
    fail(`${label} must be a credential-free HTTPS origin`);
  }
}

function databaseUrl(raw, label) {
  try {
    const value = new URL(raw?.trim() ?? "");
    if (
      (value.protocol !== "postgresql:" && value.protocol !== "postgres:") ||
      !value.username ||
      !value.password ||
      !value.hostname ||
      !value.pathname.slice(1) ||
      value.hostname.includes("-pooler") ||
      value.searchParams.get("sslmode") !== "require"
    ) {
      throw new Error("invalid_database_url");
    }
    return value;
  } catch {
    fail(`${label} must be a direct TLS PostgreSQL URL`);
  }
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  try {
    validateGateF3Environment();
    console.log(
      "P5-COLLAB-01 Gate F.3 preflight accepted: disposable=true; region=singapore; hard_cap_usd=0; secrets_present=true; urls_bounded=true.",
    );
  } catch (error) {
    console.error(error instanceof Error ? error.message : "Gate F.3 refused");
    process.exitCode = 2;
  }
}
