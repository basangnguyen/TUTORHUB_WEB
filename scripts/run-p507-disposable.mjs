import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_ENV_FILE = ".env.p5-collab-07-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY";
const P502_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_02_DISPOSABLE_ONLY";
const ACL_CONFIRMATION =
  "I_UNDERSTAND_P5_COLLAB_07_ACL_PROVISION_DISPOSABLE_ONLY";
const B2_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_B2_DISPOSABLE_ONLY";
const LIFECYCLE_CONFIRMATION =
  "I_UNDERSTAND_P5_COLLAB_07_LIFECYCLE_DISPOSABLE_ONLY";

export function parseEnvFile(content) {
  const values = new Map();
  for (const rawLine of content.replace(/^\uFEFF/u, "").split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (line === "" || line.startsWith("#")) continue;
    const separator = line.indexOf("=");
    if (separator < 1) throw new Error("invalid env-file line");
    const name = line.slice(0, separator).trim();
    let value = line.slice(separator + 1).trim();
    if (!/^[A-Z][A-Z0-9_]*$/u.test(name)) {
      throw new Error("invalid env-file variable name");
    }
    if (
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'")))
    ) {
      value = value.slice(1, -1);
    }
    if (values.has(name)) {
      throw new Error(`duplicate env-file variable: ${name}`);
    }
    values.set(name, value);
  }
  return values;
}

function postgresURL(name, raw, expectedUser, pooled) {
  let value;
  try {
    value = new URL(raw);
  } catch {
    throw new Error(`${name} must be a valid PostgreSQL URL`);
  }
  if (
    !new Set(["postgres:", "postgresql:"]).has(value.protocol) ||
    !value.hostname ||
    !value.pathname.slice(1) ||
    !value.username ||
    !value.password ||
    value.searchParams.get("sslmode") !== "require"
  ) {
    throw new Error(`${name} must be a password-authenticated TLS URL`);
  }
  if (expectedUser && value.username !== expectedUser) {
    throw new Error(`${name} must use the expected dedicated role`);
  }
  if (value.hostname.includes("-pooler.") !== pooled) {
    throw new Error(`${name} has the wrong direct/pooled endpoint type`);
  }
  return value;
}

function secureOrigin(name, raw) {
  let value;
  try {
    value = new URL(raw);
  } catch {
    throw new Error(`${name} must be a valid HTTPS origin`);
  }
  if (
    value.protocol !== "https:" ||
    value.username ||
    value.password ||
    value.pathname !== "/" ||
    value.search ||
    value.hash
  ) {
    throw new Error(`${name} must be a credential-free HTTPS origin`);
  }
  return value.origin;
}

export function validateP507Environment(values) {
  if (
    (values.get("P5_COLLAB_07_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-07 disposable confirmation is missing or invalid",
    );
  }

  const migration = postgresURL(
    "DATABASE_MIGRATION_URL",
    values.get("DATABASE_MIGRATION_URL") ?? "",
    "neondb_owner",
    false,
  );
  const core = postgresURL(
    "DATABASE_POOL_URL",
    values.get("DATABASE_POOL_URL") ?? "",
    "tutorhub_runtime",
    true,
  );
  const worker = postgresURL(
    "DATABASE_COLLABORATION_URL",
    values.get("DATABASE_COLLABORATION_URL") ?? "",
    "",
    false,
  );
  const maintenance = postgresURL(
    "DATABASE_POLL_MAINTENANCE_URL",
    values.get("DATABASE_POLL_MAINTENANCE_URL") ?? "",
    "tutorhub_poll_maintenance",
    false,
  );

  const normalizedHosts = new Set(
    [migration, core, worker, maintenance].map((value) =>
      value.hostname.replace("-pooler.", "."),
    ),
  );
  const databases = new Set(
    [migration, core, worker, maintenance].map((value) => value.pathname),
  );
  const roles = new Set(
    [migration, core, worker, maintenance].map((value) => value.username),
  );
  if (normalizedHosts.size !== 1 || databases.size !== 1 || roles.size !== 4) {
    throw new Error(
      "P5-COLLAB-07 requires four distinct roles on one disposable branch and database",
    );
  }

  const endpoint = secureOrigin("B2_ENDPOINT", values.get("B2_ENDPOINT") ?? "");
  const region = values.get("B2_REGION")?.trim() ?? "";
  const bucket = values.get("B2_BUCKET")?.trim() ?? "";
  const keyId = values.get("B2_KEY_ID")?.trim() ?? "";
  const applicationKey = values.get("B2_APPLICATION_KEY")?.trim() ?? "";
  if (!/^[a-z0-9-]{3,64}$/u.test(region)) {
    throw new Error("B2_REGION is invalid");
  }
  if (!/^[A-Za-z0-9][A-Za-z0-9.-]{4,62}$/u.test(bucket)) {
    throw new Error("B2_BUCKET is invalid");
  }
  if (
    keyId.length < 12 ||
    applicationKey.length < 20 ||
    keyId === applicationKey
  ) {
    throw new Error("B2 scoped credentials are missing or invalid");
  }

  return {
    B2_APPLICATION_KEY: applicationKey,
    B2_BUCKET: bucket,
    B2_ENDPOINT: endpoint,
    B2_KEY_ID: keyId,
    B2_REGION: region,
    DATABASE_COLLABORATION_URL: worker.toString(),
    DATABASE_MIGRATION_URL: migration.toString(),
    DATABASE_POLL_MAINTENANCE_URL: maintenance.toString(),
    DATABASE_POOL_URL: core.toString(),
    P5_COLLAB_02_DISPOSABLE_CONFIRM: P502_CONFIRMATION,
    P5_COLLAB_07_ACL_PROVISION_CONFIRM: ACL_CONFIRMATION,
    P5_COLLAB_07_B2_CONFIRM: B2_CONFIRMATION,
    P5_COLLAB_07_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
    P5_COLLAB_07_LIFECYCLE_CONFIRM: LIFECYCLE_CONFIRMATION,
  };
}

function run(command, args, environment) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const executable = windowsCommand
    ? (process.env.ComSpec ?? "cmd.exe")
    : command;
  const executableArgs = windowsCommand
    ? ["/d", "/s", "/c", command, ...args]
    : args;
  const result = spawnSync(executable, executableArgs, {
    cwd: process.cwd(),
    env: { ...process.env, ...environment },
    stdio: "inherit",
  });
  if (result.error) {
    process.stderr.write("P5-COLLAB-07 gate process could not be started.\n");
    return 1;
  }
  return result.status ?? 1;
}

function runSilent(command, args, environment) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const executable = windowsCommand
    ? (process.env.ComSpec ?? "cmd.exe")
    : command;
  const executableArgs = windowsCommand
    ? ["/d", "/s", "/c", command, ...args]
    : args;
  const result = spawnSync(executable, executableArgs, {
    cwd: process.cwd(),
    encoding: "utf8",
    env: { ...process.env, ...environment },
    stdio: "pipe",
    windowsHide: true,
  });
  return result.status ?? 1;
}

function databaseVersion(environment) {
  const result = spawnSync(
    "go",
    ["run", "./services/core-api/cmd/migrate", "version"],
    {
      cwd: process.cwd(),
      encoding: "utf8",
      env: { ...process.env, ...environment },
      stdio: "pipe",
      windowsHide: true,
    },
  );
  const match = result.stdout?.trim().match(/^(\d+) (true|false)$/u);
  if (result.status !== 0 || !match) return undefined;
  return { dirty: match[2] === "true", number: Number(match[1]) };
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  const gate = argv[1] ?? "all";
  if (
    !new Set(["preflight", "migration", "acl", "b2", "lifecycle", "all"]).has(
      gate,
    )
  ) {
    process.stderr.write(
      "P5-COLLAB-07 gate must be preflight, migration, acl, b2, lifecycle or all.\n",
    );
    return 2;
  }

  let environment;
  try {
    environment = validateP507Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-07 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }
  process.stdout.write(
    "P5-COLLAB-07 disposable preflight passed; credentials remain hidden.\n",
  );
  if (gate === "preflight") return 0;

  if (gate === "migration" || gate === "all") {
    const before = databaseVersion(environment);
    if (!before || before.dirty || !new Set([38, 39, 40]).has(before.number)) {
      process.stderr.write(
        "P5-COLLAB-07 migration preflight failed; expected clean version 38, 39 or 40.\n",
      );
      return 1;
    }
    if (
      runSilent(
        "go",
        ["run", "./services/core-api/cmd/migrate", "up"],
        environment,
      ) !== 0
    ) {
      process.stderr.write("P5-COLLAB-07 forward migration failed.\n");
      return 1;
    }
    const after = databaseVersion(environment);
    if (!after || after.dirty || after.number !== 40) {
      process.stderr.write(
        "P5-COLLAB-07 forward migration postflight failed.\n",
      );
      return 1;
    }
    if (
      runSilent(
        "go",
        ["run", "./services/core-api/cmd/migrate", "up"],
        environment,
      ) !== 0
    ) {
      process.stderr.write("P5-COLLAB-07 idempotent migration rerun failed.\n");
      return 1;
    }
    const idempotent = databaseVersion(environment);
    if (!idempotent || idempotent.dirty || idempotent.number !== 40) {
      process.stderr.write(
        "P5-COLLAB-07 idempotent migration postflight failed.\n",
      );
      return 1;
    }
    process.stdout.write(
      `P5-COLLAB-07 migration gate passed: ${before.number} false -> 40 false -> 40 false.\n`,
    );
    if (gate === "migration") return 0;
  }

  if (gate === "acl" || gate === "all") {
    const status = run(
      "go",
      [
        "test",
        "-count=1",
        "-tags=integration",
        "-run",
        "^TestProvisionWhiteboardArtifactWorkerExactACL$",
        "./services/core-api/internal/modules/collaboration",
      ],
      environment,
    );
    if (status !== 0) return status;
  }
  if (gate === "b2" || gate === "all") {
    const status = run(
      process.platform === "win32" ? "pnpm.cmd" : "pnpm",
      [
        "--filter",
        "@tutorhub/whiteboard-runtime",
        "exec",
        "vitest",
        "run",
        "src/artifactObjectStore.integration.test.ts",
      ],
      environment,
    );
    if (status !== 0) return status;
    if (gate === "b2") return 0;
  }
  if (gate === "lifecycle" || gate === "all") {
    return run(
      process.platform === "win32" ? "pnpm.cmd" : "pnpm",
      [
        "--filter",
        "@tutorhub/whiteboard-runtime",
        "exec",
        "vitest",
        "run",
        "src/artifactLifecycle.integration.test.ts",
      ],
      environment,
    );
  }
  return 0;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  process.exitCode = main();
}
