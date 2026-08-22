import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parseEnvFile } from "./run-p507-disposable.mjs";

const DEFAULT_ENV_FILE = ".env.p5-collab-10-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_10_DISPOSABLE_ONLY";

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
    value.username !== expectedUser ||
    !value.password ||
    value.searchParams.get("sslmode") !== "require"
  ) {
    throw new Error(
      `${name} must use the expected password-authenticated TLS role`,
    );
  }
  if (value.hostname.includes("-pooler.") !== pooled) {
    throw new Error(`${name} has the wrong direct/pooled endpoint type`);
  }
  return value;
}

export function validateP510Environment(values) {
  if (
    (values.get("P5_COLLAB_10_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-10 disposable confirmation is missing or invalid",
    );
  }
  const migration = postgresURL(
    "DATABASE_MIGRATION_URL",
    values.get("DATABASE_MIGRATION_URL") ?? "",
    "neondb_owner",
    false,
  );
  const runtime = postgresURL(
    "DATABASE_POOL_URL",
    values.get("DATABASE_POOL_URL") ?? "",
    "tutorhub_runtime",
    true,
  );
  if (
    migration.hostname.replace("-pooler.", ".") !==
      runtime.hostname.replace("-pooler.", ".") ||
    migration.pathname !== runtime.pathname
  ) {
    throw new Error(
      "P5-COLLAB-10 requires owner and runtime roles on one disposable branch and database",
    );
  }
  return {
    DATABASE_MIGRATION_URL: migration.toString(),
    DATABASE_POOL_URL: runtime.toString(),
    P5_COLLAB_10_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
  };
}

function run(command, args, environment, silent = false) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const result = spawnSync(
    windowsCommand ? (process.env.ComSpec ?? "cmd.exe") : command,
    windowsCommand ? ["/d", "/s", "/c", command, ...args] : args,
    {
      cwd: process.cwd(),
      encoding: silent ? "utf8" : undefined,
      env: { ...process.env, ...environment },
      stdio: silent ? "pipe" : "inherit",
      windowsHide: true,
    },
  );
  return result;
}

function databaseVersion(environment) {
  const result = run(
    "go",
    ["run", "./services/core-api/cmd/migrate", "version"],
    environment,
    true,
  );
  const match = result.stdout?.trim().match(/^(\d+) (true|false)$/u);
  if (result.status !== 0 || !match) return undefined;
  return { dirty: match[2] === "true", number: Number(match[1]) };
}

function forwardRetainedMigrations(environment) {
  const before = databaseVersion(environment);
  if (!before || before.dirty || !new Set([37, 41]).has(before.number)) {
    process.stderr.write(
      "P5-COLLAB-10 retained forward migration requires clean ledger 37 false or 41 false.\n",
    );
    return 1;
  }
  for (const label of ["forward", "idempotent rerun"]) {
    const result = run(
      "go",
      ["run", "./services/core-api/cmd/migrate", "up"],
      environment,
      true,
    );
    if (result.status !== 0) {
      process.stderr.write(
        `P5-COLLAB-10 ${label} retained migration failed.\n`,
      );
      return 1;
    }
    const after = databaseVersion(environment);
    if (!after || after.dirty || after.number !== 41) {
      process.stderr.write(
        `P5-COLLAB-10 ${label} retained migration postflight failed.\n`,
      );
      return 1;
    }
  }
  process.stdout.write(
    `P5-COLLAB-10 retained migration passed: ${before.number} false -> 41 false -> 41 false.\n`,
  );
  return 0;
}

function goTest(pattern, integration, environment) {
  const args = ["test", "-count=1"];
  if (integration) args.push("-tags=integration");
  args.push(
    "-run",
    pattern,
    "./services/core-api/internal/modules/collaboration",
  );
  return run("go", args, environment).status ?? 1;
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  const gate = argv[1] ?? "all";
  if (
    !new Set(["preflight", "migration", "database", "runtime", "all"]).has(gate)
  ) {
    process.stderr.write(
      "P5-COLLAB-10 gate must be preflight, migration, database, runtime or all.\n",
    );
    return 2;
  }
  let environment;
  try {
    environment = validateP510Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-10 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }
  if (goTest("^TestP510RolePreflight$", true, environment) !== 0) return 1;
  if (gate === "migration") return forwardRetainedMigrations(environment);
  const version = databaseVersion(environment);
  if (!version || version.dirty || version.number !== 41) {
    const observed = version
      ? `${version.number} ${version.dirty}`
      : "unavailable";
    process.stderr.write(
      `P5-COLLAB-10 requires a clean disposable database at 41 false; observed ${observed}; no migration is run.\n`,
    );
    return 1;
  }
  process.stdout.write(
    "P5-COLLAB-10 disposable preflight passed at 41 false; credentials remain hidden.\n",
  );
  if (gate === "preflight") return 0;
  if (gate === "database" || gate === "all") {
    if (
      goTest(
        "^TestP510AuthorizationTenantIsolationPostgres$",
        true,
        environment,
      ) !== 0
    ) {
      return 1;
    }
    if (gate === "database") return 0;
  }
  if (gate === "runtime" || gate === "all") {
    if (
      goTest(
        "^(TestServiceAuthorizationMatrixUsesCurrentServerAuthority|TestServiceRejectsForgedOrganizationRoleBeforeRepositoryAccess|TestServiceSnapshotPaginationReturnsScopedCursorAndForwardsKeyset|TestSnapshotCursorIsBoundToTenantPrincipalDocumentGenerationAndLimit|TestSnapshotCursorRejectsMalformedOrNonCanonicalPayload)$",
        false,
        environment,
      ) !== 0
    ) {
      return 1;
    }
  }
  process.stdout.write("P5-COLLAB-10 disposable authorization gates passed.\n");
  return 0;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  process.exitCode = main();
}
